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
			return gin.H{"type": "message", "id": tokenProDispatchID("msg_"), "role": "assistant", "status": "completed", "content": []any{gin.H{"type": "output_text", "text": tokenProNativeImageResultText(item.Get("output")), "annotations": []any{}}}}, nil
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

func tokenProNativeImageResultText(output gjson.Result) string {
	if output.IsArray() {
		for _, part := range output.Array() {
			if part.Get("type").String() == "input_image" && strings.TrimSpace(part.Get("image_url").String()) != "" {
				return "图片生成好了 ✨"
			}
		}
	}

	message := output.Raw
	if output.Type == gjson.String {
		message = output.String()
	}
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "timed out"), strings.Contains(message, "超时"):
		return "图片生成超时了，请重新发起。"
	case strings.Contains(message, "rate limit"), strings.Contains(message, "overload"), strings.Contains(message, "busy"), strings.Contains(message, "繁忙"):
		return "生图服务有点忙，请稍后再试。"
	case strings.Contains(message, "content policy"), strings.Contains(message, "moderation"), strings.Contains(message, "safety"), strings.Contains(message, "审核"):
		return "这次请求未通过检查，请调整图片描述后再试。"
	case strings.Contains(message, "error"), strings.Contains(message, "fail"), strings.Contains(message, "失败"):
		return "这次没能生成图片，请稍后再试。"
	default:
		return "图片未能正常返回，请重新生成。"
	}
}
