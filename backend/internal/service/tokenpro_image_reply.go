package service

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

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
