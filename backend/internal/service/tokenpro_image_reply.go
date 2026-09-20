package service

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// BuildTokenProCompletedImageMessage recognizes the narrow Responses
// continuation produced after the client has already displayed a native image.
// Returning a fixed message lets the gateway finish the turn without asking a
// text model to inspect a multi-megabyte image result just to acknowledge it.
//
// The shortcut is deliberately all-or-nothing: every tool call after the last
// user message must be image_gen.imagegen, every call must have a matching
// successful output, and no assistant message may already have completed the
// turn. Failures and mixed-tool workflows keep the normal model continuation.
func BuildTokenProCompletedImageMessage(body []byte) (gin.H, bool) {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return nil, false
	}
	items := input.Array()
	lastUser := -1
	for i, item := range items {
		if item.Get("role").String() == "user" {
			lastUser = i
		}
	}
	if lastUser < 0 {
		return nil, false
	}

	type callState struct {
		succeeded bool
	}
	calls := make(map[string]*callState)
	callCount := 0
	for _, item := range items[lastUser+1:] {
		kind := item.Get("type").String()
		switch kind {
		case "reasoning":
			continue
		case "function_call":
			if item.Get("namespace").String() != "image_gen" || item.Get("name").String() != "imagegen" {
				return nil, false
			}
			callID := strings.TrimSpace(item.Get("call_id").String())
			if callID == "" {
				return nil, false
			}
			if _, exists := calls[callID]; exists {
				return nil, false
			}
			calls[callID] = &callState{}
			callCount++
		case "function_call_output":
			call := calls[strings.TrimSpace(item.Get("call_id").String())]
			if call == nil || call.succeeded || !tokenProImageToolOutputSucceeded(item.Get("output")) {
				return nil, false
			}
			call.succeeded = true
		case "message":
			// A user message would have become lastUser above. Any remaining
			// message means an assistant/developer turn already exists here.
			return nil, false
		default:
			// Unknown call/output types may belong to another client tool.
			// Preserve the normal model continuation rather than swallowing it.
			if strings.Contains(kind, "call") || strings.Contains(kind, "output") {
				return nil, false
			}
		}
	}
	if callCount == 0 {
		return nil, false
	}
	for _, call := range calls {
		if !call.succeeded {
			return nil, false
		}
	}
	return gin.H{"type": "message", "role": "assistant", "status": "completed",
		"content": []any{gin.H{"type": "output_text", "text": "图片生成好了 ✨", "annotations": []any{}}}}, true
}

func tokenProImageToolOutputSucceeded(output gjson.Result) bool {
	if output.Type == gjson.String {
		decoded := gjson.Parse(output.String())
		if !decoded.IsArray() {
			return false
		}
		output = decoded
	}
	if !output.IsArray() {
		return false
	}
	for _, part := range output.Array() {
		if part.Get("type").String() != "input_image" {
			continue
		}
		imageURL := strings.TrimSpace(part.Get("image_url").String())
		comma := strings.IndexByte(imageURL, ',')
		if comma > len("data:image/") && strings.HasPrefix(imageURL, "data:image/") && comma+1 < len(imageURL) {
			return true
		}
	}
	return false
}

// Scope suppression to successful native tool results after the latest user input.
func tokenProDisplayedImagePaths(body []byte) []string {
	items := gjson.GetBytes(body, "input").Array()
	last := 0
	for i, item := range items {
		if item.Get("role").String() == "user" {
			last = i
		}
	}
	calls := map[string]bool{}
	paths := []string{}
	for _, item := range items[last:] {
		if item.Get("type").String() == "function_call" && item.Get("name").String() == "imagegen" && item.Get("namespace").String() == "image_gen" {
			calls[item.Get("call_id").String()] = true
		}
		if item.Get("type").String() != "function_call_output" || !calls[item.Get("call_id").String()] {
			continue
		}
		output := item.Get("output")
		if output.Type == gjson.String {
			output = gjson.Parse(output.String())
		}
		success := false
		for _, part := range output.Array() {
			if part.Get("type").String() == "input_image" && strings.HasPrefix(part.Get("image_url").String(), "data:image/") {
				success = true
			}
		}
		if !success {
			continue
		}
		for _, part := range output.Array() {
			if part.Get("type").String() != "input_text" {
				continue
			}
			paths = append(paths, tokenProSavedImagePath.FindAllString(part.Get("text").String(), -1)...)
		}
	}
	return uniqueTokenProImagePaths(paths)
}

var tokenProSavedImagePath = regexp.MustCompile(`/[^\s<>"']*/generated_images/[^\s<>"']+\.(?:png|jpg|jpeg|webp)`)

func tokenProCleanImageReply(text string, paths []string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	for _, path := range paths {
		// Match only an exact delivered destination, with optional angle brackets/title.
		pattern := regexp.MustCompile(`!?\[[^\]\n]*\]\(<?` + regexp.QuoteMeta(path) + `>?(?:\s+"[^"\n]*")?\)`)
		text = pattern.ReplaceAllString(text, "")
		html := regexp.MustCompile(`(?i)<img\b[^>]*\bsrc=["']` + regexp.QuoteMeta(path) + `["'][^>]*>`)
		text = html.ReplaceAllString(text, "")
	}
	if strings.TrimSpace(text) == "" {
		return "图片生成好了 ✨"
	}
	return text
}

// Buffer text deltas until text.done so a split Markdown reference never renders.
// Other SSE events (tools, reasoning, usage, errors) pass through unchanged.
type tokenProImageReplyWriter struct {
	gin.ResponseWriter
	paths   []string
	pending []byte
	deltas  map[string]string
}

func (w *tokenProImageReplyWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *tokenProImageReplyWriter) Write(p []byte) (int, error) {
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		var obj any
		if tokenProDecodeImageReply(p, &obj) == nil {
			w.clean(obj)
			data, err := json.Marshal(obj)
			if err != nil {
				return 0, err
			}
			_, err = w.ResponseWriter.Write(data)
			return len(p), err
		}
		return w.ResponseWriter.Write(p)
	}
	w.pending = append(w.pending, p...)
	for {
		end := bytes.Index(w.pending, []byte("\n\n"))
		separator := 2
		if crlf := bytes.Index(w.pending, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
			end = crlf
			separator = 4
		}
		if end < 0 {
			break
		}
		event := append([]byte(nil), w.pending[:end]...)
		w.pending = w.pending[end+separator:]
		for _, line := range bytes.Split(event, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			var obj map[string]any
			if tokenProDecodeImageReply(bytes.TrimSpace(line[5:]), &obj) != nil {
				break
			}
			kind, _ := obj["type"].(string)
			key := gjson.GetBytes(line[5:], "item_id").String() + ":" + gjson.GetBytes(line[5:], "content_index").Raw
			if kind == "response.output_text.delta" {
				delta, _ := obj["delta"].(string)
				w.deltas[key] += delta
				event = nil
				break
			}
			if kind == "response.output_text.done" {
				text, _ := obj["text"].(string)
				if text == "" {
					text = w.deltas[key]
				}
				obj["text"] = text
				delta := map[string]any{}
				for k, v := range obj {
					delta[k] = v
				}
				delete(delta, "text")
				delta["type"] = "response.output_text.delta"
				delta["delta"] = tokenProCleanImageReply(text, w.paths)
				encoded, _ := json.Marshal(delta)
				if _, err := w.ResponseWriter.Write(append(append([]byte("data: "), encoded...), []byte("\n\n")...)); err != nil {
					return 0, err
				}
				delete(w.deltas, key)
			}
			w.clean(obj)
			encoded, _ := json.Marshal(obj)
			// Keep an explicit SSE event name synchronized with its unchanged event type.
			event = bytes.Replace(event, line, append([]byte("data: "), encoded...), 1)
			break
		}
		if event != nil {
			if _, err := w.ResponseWriter.Write(append(event, []byte("\n\n")...)); err != nil {
				return 0, err
			}
		}
	}
	return len(p), nil
}
func (w *tokenProImageReplyWriter) clean(value any) {
	switch v := value.(type) {
	case map[string]any:
		kind, _ := v["type"].(string)
		if kind == "output_text" || kind == "response.output_text.done" {
			if s, ok := v["text"].(string); ok {
				v["text"] = tokenProCleanImageReply(s, w.paths)
			}
		}
		for _, child := range v {
			w.clean(child)
		}
	case []any:
		for _, child := range v {
			w.clean(child)
		}
	}
}

func tokenProDecodeImageReply(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(value)
}
