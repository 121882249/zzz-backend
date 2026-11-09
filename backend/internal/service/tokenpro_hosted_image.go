package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const tokenProHostedImagePublisherKey = "tokenpro_hosted_image_publisher"

// TokenProHostedImagePublisher stores one upstream image and returns an HTTPS URL.
// It must not record usage: the text-row forward already billed the image.
type TokenProHostedImagePublisher func(ctx context.Context, contentType string, data []byte) (string, error)

// SetTokenProHostedImagePublisher installs the handler's chat-image store on
// the request before Forward. The service does not import the handler.
func SetTokenProHostedImagePublisher(c *gin.Context, publish TokenProHostedImagePublisher) {
	if c == nil || publish == nil {
		return
	}
	c.Set(tokenProHostedImagePublisherKey, publish)
}

func hostedImagePublisherFromContext(c *gin.Context) TokenProHostedImagePublisher {
	if c == nil {
		return nil
	}
	value, ok := c.Get(tokenProHostedImagePublisherKey)
	if !ok {
		return nil
	}
	publish, _ := value.(TokenProHostedImagePublisher)
	return publish
}

// armTokenProHostedImageDelivery wraps the client writer after the Codex bridge
// actually injects hosted image_generation. Clients that already advertised the
// tool, or that have image_gen.imagegen, are left alone.
//
// Upstream still generates the image and the text-row image counter still sees
// the original SSE. This wrapper only changes bytes written to the client, so
// the image is not generated or billed a second time.
func armTokenProHostedImageDelivery(c *gin.Context, clientBody []byte) {
	if c == nil || c.Writer == nil {
		return
	}
	if _, ok := c.Writer.(*tokenProHostedImageWriter); ok {
		return
	}
	c.Writer = &tokenProHostedImageWriter{
		ResponseWriter: c.Writer,
		ginCtx:         c,
		clientBody:     append([]byte(nil), clientBody...),
		publish:        hostedImagePublisherFromContext(c),
	}
}

// tokenProHostedImageWriter turns an unrendered image_generation_call into the
// desktop local-download call. Codex Desktop shows the picture on the next
// turn, after exec prints the absolute path.
type tokenProHostedImageWriter struct {
	gin.ResponseWriter
	mu          sync.Mutex
	ginCtx      *gin.Context
	clientBody  []byte
	publish     TokenProHostedImagePublisher
	pending     []byte
	streamMode  bool
	docDone     bool
	result      string
	format      string
	directURL   string
	item        map[string]any
	itemReady   bool
	emitted     bool
	logged      bool
	maxIndex    int
	sawIndex    bool
	outputIndex int
	indexSet    bool
}

func (w *tokenProHostedImageWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *tokenProHostedImageWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *tokenProHostedImageWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.streamMode && len(bytes.TrimSpace(w.pending)) > 0 {
		_ = w.writeFrame(w.pending)
		w.pending = nil
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *tokenProHostedImageWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// A pre-output failure can switch the response to JSON after a keepalive
	// comment has already marked this writer as a stream.
	if w.leavingStreamForJSON() {
		if err := w.finishStream(); err != nil {
			return 0, err
		}
	}
	if w.asStream(p) {
		w.pending = append(w.pending, p...)
		if err := w.consume(); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	if w.docDone {
		return w.ResponseWriter.Write(p)
	}
	w.docDone = true
	rewritten := w.rewriteDocument(p)
	if _, err := w.ResponseWriter.Write(rewritten); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *tokenProHostedImageWriter) leavingStreamForJSON() bool {
	if !w.streamMode {
		return false
	}
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	return strings.Contains(contentType, "json") && !strings.Contains(contentType, "text/event-stream")
}

func (w *tokenProHostedImageWriter) finishStream() error {
	if err := w.consume(); err != nil {
		return err
	}
	if len(bytes.TrimSpace(w.pending)) > 0 {
		if err := w.writeFrame(w.pending); err != nil {
			return err
		}
		w.pending = nil
	}
	w.streamMode = false
	return nil
}

func (w *tokenProHostedImageWriter) asStream(p []byte) bool {
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	if strings.Contains(contentType, "json") && !strings.Contains(contentType, "text/event-stream") {
		return false
	}
	if w.streamMode || strings.Contains(contentType, "text/event-stream") {
		w.streamMode = true
		return true
	}
	trim := bytes.TrimSpace(p)
	w.streamMode = bytes.HasPrefix(trim, []byte("data:")) || bytes.HasPrefix(trim, []byte("event:")) || bytes.HasPrefix(trim, []byte(":"))
	return w.streamMode
}

func (w *tokenProHostedImageWriter) consume() error {
	for {
		end := bytes.Index(w.pending, []byte("\n\n"))
		separator := 2
		if crlf := bytes.Index(w.pending, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
			end = crlf
			separator = 4
		}
		if end < 0 {
			return nil
		}
		frame := append([]byte(nil), w.pending[:end]...)
		w.pending = w.pending[end+separator:]
		if err := w.writeFrame(frame); err != nil {
			return err
		}
	}
}

func (w *tokenProHostedImageWriter) writeFrame(frame []byte) error {
	if len(bytes.TrimSpace(frame)) == 0 {
		return nil
	}
	data := sseDataPayload(frame)
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return w.writeRaw(frame)
	}
	kind := gjson.GetBytes(data, "type").String()
	if dropHostedImageEvent(kind, data) {
		w.capture(data)
		return nil
	}
	if kind == "response.completed" || kind == "response.done" {
		return w.writeTerminal(frame, data)
	}
	w.noteIndex(data)
	if bytes.Contains(data, []byte(`"image_generation_call"`)) {
		w.capture(data)
		rewritten, err := rewriteHostedImageDocument(data, nil)
		if err != nil {
			return w.writeRaw(frame)
		}
		return w.writeRaw(replaceDataPayload(frame, rewritten))
	}
	return w.writeRaw(frame)
}

func (w *tokenProHostedImageWriter) writeTerminal(frame, data []byte) error {
	w.capture(data)
	item := w.deliveryItem()
	if item != nil && !w.emitted {
		if err := w.writeToolEvents(item); err != nil {
			return err
		}
		w.emitted = true
		w.logDelivery()
	}
	if item == nil && !bytes.Contains(data, []byte(`"image_generation_call"`)) {
		return w.writeRaw(frame)
	}
	rewritten, err := rewriteHostedImageDocument(data, item)
	if err != nil {
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Hosted image_generation rewrite failed: %v", err)
		return w.writeRaw(frame)
	}
	return w.writeRaw(replaceDataPayload(frame, rewritten))
}

func (w *tokenProHostedImageWriter) rewriteDocument(p []byte) []byte {
	if !gjson.ValidBytes(p) || !bytes.Contains(p, []byte(`"image_generation_call"`)) {
		return p
	}
	w.capture(p)
	item := w.deliveryItem()
	rewritten, err := rewriteHostedImageDocument(p, item)
	if err != nil {
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Hosted image_generation rewrite failed: %v", err)
		return p
	}
	if item != nil {
		w.logDelivery()
	}
	return rewritten
}

func (w *tokenProHostedImageWriter) logDelivery() {
	if w.logged {
		return
	}
	w.logged = true
	mode := "undelivered"
	if kind, _ := w.item["type"].(string); kind == "custom_tool_call" || kind == "function_call" || kind == "local_shell_call" {
		mode = "local-v1"
	} else if text := hostedImageMessageText(w.item); strings.HasPrefix(text, tokenProChatImageSuccessPrefix) {
		mode = "url-v1"
	}
	logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Rewrote hosted image_generation_call as %s", mode)
}

func (w *tokenProHostedImageWriter) deliveryItem() map[string]any {
	if w.itemReady {
		return w.item
	}
	if w.result == "" && w.directURL == "" {
		return nil
	}
	w.itemReady = true
	imageURL := ""
	if w.result != "" {
		data, err := decodeHostedImage(w.result)
		if err != nil || len(data) == 0 || w.publish == nil {
			if err == nil {
				err = errTokenProHostedImageURL
			}
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Hosted image_generation publish failed: %v", err)
			w.item = hostedImageTextMessage("图片没有保存下来，请再试一次。")
			return w.item
		}
		ctx := context.Background()
		if w.ginCtx != nil && w.ginCtx.Request != nil {
			ctx = w.ginCtx.Request.Context()
		}
		published, pubErr := w.publish(ctx, hostedImageContentType(w.format), data)
		if pubErr != nil || !safeTokenProImageURL(published) {
			if pubErr == nil {
				pubErr = errTokenProHostedImageURL
			}
			logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Hosted image_generation publish failed: %v", pubErr)
			w.item = hostedImageTextMessage("图片没有保存下来，请再试一次。")
			return w.item
		}
		imageURL = published
	} else {
		imageURL = strings.TrimSpace(w.directURL)
	}
	if item, ok := BuildTokenProLocalImageCall(w.clientBody, imageURL, hostedImageThreadID(w.ginCtx, w.clientBody)); ok {
		rememberTokenProHostedLocalImage(w.ginCtx, item, imageURL)
		w.item = item
		return item
	}
	w.item = hostedImageTextMessage(TokenProChatImageSuccessText(imageURL, ""))
	return w.item
}

func (w *tokenProHostedImageWriter) writeToolEvents(item map[string]any) error {
	index := w.nextIndex()
	kind, _ := item["type"].(string)
	switch kind {
	case "function_call":
		added := cloneHostedImageMap(item)
		added["arguments"] = ""
		if err := w.writeData(map[string]any{"type": "response.output_item.added", "output_index": index, "item": added}); err != nil {
			return err
		}
		if err := w.writeData(map[string]any{
			"type": "response.function_call_arguments.delta", "output_index": index,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"delta": item["arguments"],
		}); err != nil {
			return err
		}
		if err := w.writeData(map[string]any{
			"type": "response.function_call_arguments.done", "output_index": index,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"arguments": item["arguments"],
		}); err != nil {
			return err
		}
	case "custom_tool_call":
		added := cloneHostedImageMap(item)
		added["input"] = ""
		if err := w.writeData(map[string]any{"type": "response.output_item.added", "output_index": index, "item": added}); err != nil {
			return err
		}
		if err := w.writeData(map[string]any{
			"type": "response.custom_tool_call_input.delta", "output_index": index,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"delta": item["input"],
		}); err != nil {
			return err
		}
		if err := w.writeData(map[string]any{
			"type": "response.custom_tool_call_input.done", "output_index": index,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"input": item["input"],
		}); err != nil {
			return err
		}
	default:
		if err := w.writeData(map[string]any{"type": "response.output_item.added", "output_index": index, "item": item}); err != nil {
			return err
		}
	}
	return w.writeData(map[string]any{"type": "response.output_item.done", "output_index": index, "item": item})
}

func (w *tokenProHostedImageWriter) nextIndex() int {
	if w.indexSet {
		return w.outputIndex
	}
	w.indexSet = true
	if w.sawIndex {
		w.outputIndex = w.maxIndex + 1
	}
	return w.outputIndex
}

func (w *tokenProHostedImageWriter) noteIndex(data []byte) {
	index := gjson.GetBytes(data, "output_index")
	if !index.Exists() {
		return
	}
	value := int(index.Int())
	if !w.sawIndex || value > w.maxIndex {
		w.maxIndex = value
		w.sawIndex = true
	}
}

func (w *tokenProHostedImageWriter) capture(data []byte) {
	root := gjson.ParseBytes(data)
	w.considerItem(root.Get("item"))
	kind := root.Get("type").String()
	if strings.Contains(kind, "image_generation_call") && !strings.Contains(kind, "partial") {
		w.considerResult(firstNonEmpty(root.Get("result").String(), root.Get("b64_json").String()), root.Get("output_format").String(), root.Get("status").String())
	}
	for _, item := range root.Get("response.output").Array() {
		w.considerItem(item)
	}
	for _, item := range root.Get("output").Array() {
		w.considerItem(item)
	}
}

func (w *tokenProHostedImageWriter) considerItem(item gjson.Result) {
	if !item.Exists() {
		return
	}
	kind := item.Get("type").String()
	if kind != "" && kind != "image_generation_call" {
		return
	}
	w.considerResult(firstNonEmpty(item.Get("result").String(), item.Get("b64_json").String()), firstNonEmpty(item.Get("output_format").String(), item.Get("format").String()), item.Get("status").String())
}

func (w *tokenProHostedImageWriter) considerResult(result, format, status string) {
	result = strings.TrimSpace(result)
	if result == "" || strings.Contains(strings.ToLower(status), "partial") {
		return
	}
	if strings.HasPrefix(result, "https://") {
		if w.directURL == "" && safeTokenProImageURL(result) {
			w.directURL = result
		}
		return
	}
	if strings.HasPrefix(result, "http://") {
		return
	}
	if status == "completed" || len(result) >= len(w.result) {
		w.result = result
		if strings.TrimSpace(format) != "" {
			w.format = format
		}
	}
}

func (w *tokenProHostedImageWriter) writeData(event map[string]any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = w.ResponseWriter.Write(append(append([]byte("data: "), payload...), []byte("\n\n")...))
	return err
}

func (w *tokenProHostedImageWriter) writeRaw(frame []byte) error {
	_, err := w.ResponseWriter.Write(append(append([]byte{}, frame...), []byte("\n\n")...))
	return err
}

func dropHostedImageEvent(kind string, data []byte) bool {
	if strings.Contains(kind, "image_generation_call") {
		return true
	}
	switch kind {
	case "response.output_item.added", "response.output_item.done", "response.output_item.delta":
		return gjson.GetBytes(data, "item.type").String() == "image_generation_call"
	default:
		return false
	}
}

func sseDataPayload(frame []byte) []byte {
	var payload []byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		chunk := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(chunk, []byte("[DONE]")) {
			return chunk
		}
		if len(payload) > 0 {
			payload = append(payload, '\n')
		}
		payload = append(payload, chunk...)
	}
	return payload
}

func replaceDataPayload(frame, payload []byte) []byte {
	lines := bytes.Split(frame, []byte("\n"))
	for i, line := range lines {
		trimmed := bytes.TrimRight(line, "\r")
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		lines[i] = append([]byte("data: "), payload...)
		return bytes.Join(lines, []byte("\n"))
	}
	return append(append([]byte("data: "), payload...), '\n')
}

func rewriteHostedImageDocument(data []byte, extra map[string]any) ([]byte, error) {
	var obj map[string]any
	if err := tokenProDecodeImageReply(data, &obj); err != nil {
		return nil, err
	}
	stripHostedImageCalls(obj)
	if extra != nil {
		appendHostedImageOutput(obj, extra)
	}
	return json.Marshal(obj)
}

func stripHostedImageCalls(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if kind, _ := typed["type"].(string); kind == "image_generation_call" {
			return nil
		}
		for key, child := range typed {
			typed[key] = stripHostedImageCalls(child)
		}
		return typed
	case []any:
		kept := make([]any, 0, len(typed))
		for _, child := range typed {
			if item, ok := child.(map[string]any); ok {
				if kind, _ := item["type"].(string); kind == "image_generation_call" {
					continue
				}
			}
			kept = append(kept, stripHostedImageCalls(child))
		}
		return kept
	default:
		return value
	}
}

func appendHostedImageOutput(obj map[string]any, item map[string]any) {
	target := obj
	if response, ok := obj["response"].(map[string]any); ok {
		target = response
	}
	raw, _ := target["output"].([]any)
	if raw == nil {
		raw = []any{}
	}
	target["output"] = append(raw, item)
}

func hostedImageTextMessage(text string) map[string]any {
	return map[string]any{
		"type": "message", "id": "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"role": "assistant", "status": "completed",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
	}
}

func hostedImageMessageText(item map[string]any) string {
	content, _ := item["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func cloneHostedImageMap(item map[string]any) map[string]any {
	out := make(map[string]any, len(item))
	for key, value := range item {
		out[key] = value
	}
	return out
}

func decodeHostedImage(result string) ([]byte, error) {
	result = strings.TrimSpace(result)
	if comma := strings.IndexByte(result, ','); comma >= 0 && strings.HasPrefix(result, "data:") {
		result = result[comma+1:]
	}
	if data, err := base64.StdEncoding.DecodeString(result); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(result)
}

func hostedImageContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg", "image/jpeg":
		return "image/jpeg"
	case "webp", "image/webp":
		return "image/webp"
	case "gif", "image/gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

func hostedImageThreadID(c *gin.Context, body []byte) string {
	if c != nil {
		if value, ok := c.Get(TokenProImageTurnContextKey); ok {
			if pending, ok := value.(*TokenProPendingImageTurn); ok && pending != nil && strings.TrimSpace(pending.ThreadID) != "" {
				return pending.ThreadID
			}
		}
	}
	return TokenProRequestThreadID(body)
}

func rememberTokenProHostedLocalImage(c *gin.Context, item map[string]any, imageURL string) {
	if c == nil || c.Request == nil {
		return
	}
	value, ok := c.Get(TokenProImageTurnContextKey)
	if !ok {
		return
	}
	pending, ok := value.(*TokenProPendingImageTurn)
	if !ok || pending == nil || pending.Store == nil {
		return
	}
	store, ok := pending.Store.(TokenProLocalImageStore)
	if !ok {
		return
	}
	callID, _ := item["call_id"].(string)
	apiKey := getAPIKeyFromContext(c)
	if callID == "" || apiKey == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := store.RememberLocalImage(ctx, TokenProLocalImageKey(apiKey, callID), TokenProLocalImageRecord{URL: imageURL}); err != nil {
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Hosted image local remember failed: %v", err)
	}
}

var errTokenProHostedImageURL = errors.New("hosted image publish returned no https url")
