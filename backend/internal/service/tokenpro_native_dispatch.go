package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"strings"
)

// A stateless Codex-only tool dispatcher. It never generates image data or bills inference.
func tokenProDispatchID(prefix string) string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(bytes[:])
}

func BuildTokenProNativeImageItem(body []byte) (gin.H, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid JSON")
	}
	if gjson.GetBytes(body, "previous_response_id").String() != "" {
		return nil, fmt.Errorf("native image dispatch requires full input history")
	}
	input := gjson.GetBytes(body, "input")
	if input.Type == gjson.String {
		if strings.TrimSpace(input.String()) == "" {
			return nil, fmt.Errorf("missing image prompt")
		}
		args, _ := json.Marshal(gin.H{"prompt": input.String()})
		return gin.H{"type": "function_call", "id": tokenProDispatchID("fc_"), "call_id": tokenProDispatchID("call_tp_pure_"), "namespace": "image_gen", "name": "imagegen", "arguments": string(args)}, nil
	}
	var prompt string
	var pending string
	items := input.Array()
	lastUser := 0
	for index, item := range items {
		if item.Get("role").String() == "user" {
			lastUser = index
		}
	}
	for _, item := range items[lastUser:] {
		if item.Get("role").String() == "user" {
			prompt, pending = "", ""
			if item.Get("content").Type == gjson.String {
				prompt = item.Get("content").String()
			}
			var contents []gjson.Result
			if item.Get("content").IsArray() {
				contents = item.Get("content").Array()
			}
			for _, content := range contents {
				if content.Get("type").String() != "input_text" {
					return nil, fmt.Errorf("native image dispatch currently supports new images only; image inputs must use Images edits")
				}
				prompt += content.Get("text").String() + "\n"
			}
		}
		if item.Get("type").String() == "function_call" && item.Get("name").String() == "imagegen" && strings.HasPrefix(item.Get("call_id").String(), "call_tp_pure_") {
			pending = item.Get("call_id").String()
		}
		if item.Get("type").String() == "function_call_output" && pending != "" && item.Get("call_id").String() == pending {
			return gin.H{"type": "message", "id": tokenProDispatchID("msg_"), "role": "assistant", "status": "completed", "content": []any{gin.H{"type": "output_text", "text": "图片工具已返回，请查看本次工具结果；如失败，请按错误提示处理。", "annotations": []any{}}}}, nil
		}
	}
	if pending != "" {
		return nil, fmt.Errorf("missing matching native image tool output")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("missing image prompt")
	}
	args, _ := json.Marshal(gin.H{"prompt": strings.TrimSpace(prompt)})
	return gin.H{"type": "function_call", "id": tokenProDispatchID("fc_"), "call_id": tokenProDispatchID("call_tp_pure_"), "namespace": "image_gen", "name": "imagegen", "arguments": string(args)}, nil
}
