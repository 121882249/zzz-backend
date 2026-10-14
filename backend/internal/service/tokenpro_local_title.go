package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Desktop title generation is metadata work, not a second model selection.
// Only the authenticated global-key ingress calls this function. An explicit
// thread source and the desktop's exact structured-output shape are required.
func ServeTokenProLocalTitle(c *gin.Context, body []byte) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost ||
		(c.Request.URL.Path != "/v1/responses" && c.Request.URL.Path != "/responses") || !gjson.ValidBytes(body) {
		return false
	}
	metadata := c.GetHeader("X-Codex-Turn-Metadata")
	if len(c.Request.Header.Values("X-Codex-Turn-Metadata")) > 1 || (metadata != "" && !gjson.Valid(metadata)) {
		return false
	}
	source := gjson.Get(metadata, "thread_source").String()
	bodySource := gjson.GetBytes(body, "client_metadata.thread_source").String()
	if bodyMetadata := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String(); bodySource == "" && gjson.Valid(bodyMetadata) {
		bodySource = gjson.Get(bodyMetadata, "thread_source").String()
	}
	if source != "" && bodySource != "" && source != bodySource {
		return false
	}
	if source == "" {
		source = bodySource
	}
	if source != "thread_title" {
		return false
	}
	schema := gjson.GetBytes(body, "text.format.schema")
	if gjson.GetBytes(body, "text.format.type").String() != "json_schema" ||
		schema.Get("type").String() != "object" || schema.Get("properties.title.type").String() != "string" ||
		schema.Get("properties.description.type").String() != "string" {
		return false
	}
	properties := 0
	schema.Get("properties").ForEach(func(_, _ gjson.Result) bool { properties++; return true })
	if properties != 2 {
		return false
	}
	text := tokenProLocalTitleText(body)
	encoded, _ := json.Marshal(gin.H{"title": text, "description": text})
	id := tokenProDispatchID("resp_tp_title_")
	outputPart := gin.H{"type": "output_text", "text": string(encoded), "annotations": []any{}}
	item := gin.H{"type": "message", "id": tokenProDispatchID("msg_"), "role": "assistant", "status": "completed",
		"content": []any{outputPart}}
	response := gin.H{"id": id, "object": "response", "created_at": time.Now().Unix(), "model": gjson.GetBytes(body, "model").String(),
		"status": "completed", "output": []any{item}, "usage": gin.H{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}}
	c.Header("X-TokenPro-Local-Title", "v1")
	c.Header("Cache-Control", "no-store")
	if !gjson.GetBytes(body, "stream").Bool() {
		c.JSON(http.StatusOK, response)
		return true
	}
	c.Header("Content-Type", "text/event-stream")
	for _, event := range []gin.H{
		{"type": "response.created", "response": gin.H{"id": id, "object": "response", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": gin.H{"id": item["id"], "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
		{"type": "response.content_part.added", "output_index": 0, "item_id": item["id"], "content_index": 0, "part": gin.H{"type": "output_text", "text": "", "annotations": []any{}}},
		{"type": "response.output_text.delta", "output_index": 0, "item_id": item["id"], "content_index": 0, "delta": string(encoded)},
		{"type": "response.output_text.done", "output_index": 0, "item_id": item["id"], "content_index": 0, "text": string(encoded)},
		{"type": "response.content_part.done", "output_index": 0, "item_id": item["id"], "content_index": 0, "part": outputPart},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": response},
	} {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			break
		}
	}
	c.Writer.Flush()
	return true
}

func tokenProLocalTitleText(body []byte) string {
	text := ""
	input := gjson.GetBytes(body, "input")
	if input.Type == gjson.String {
		text = input.String()
	}
	for _, item := range input.Array() {
		if item.Get("role").String() != "user" {
			continue
		}
		if item.Get("content").Type == gjson.String {
			text = item.Get("content").String()
		}
		for _, part := range item.Get("content").Array() {
			if part.Get("type").String() == "input_text" {
				text = part.Get("text").String()
			}
		}
	}
	for _, marker := range []string{"## My request:", "User prompt:", "User request:", "<user_request>"} {
		if index := strings.LastIndex(text, marker); index >= 0 {
			text = text[index+len(marker):]
		}
	}
	text = strings.TrimSpace(strings.TrimSuffix(text, "</user_request>"))
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 36 {
		text = string(runes[:36])
	}
	if text == "" {
		text = "新聊天"
	}
	return text
}
