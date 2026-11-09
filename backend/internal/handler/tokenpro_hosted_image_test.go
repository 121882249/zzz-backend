package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexDesktopTextModelLocalImageFollowUpDoesNotBill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := "/Users/me/.codex/generated_images/tokenpro/thread/abc.png"
	body, err := json.Marshal(map[string]any{
		"model":  "gpt-6-sol",
		"stream": true,
		"input": []any{
			map[string]any{"role": "user", "content": "画一只猫"},
			map[string]any{
				"type": "custom_tool_call", "name": "exec", "call_id": "call_tp_local_abc",
				"input": "// tokenpro_image_url=https://tokenpro.work/cat.png?exp=1&sig=ff\nconst result = await tools.exec_command({});\ntext(output);\n",
			},
			map[string]any{
				"type": "custom_tool_call_output", "call_id": "call_tp_local_abc",
				"output": []any{map[string]any{"type": "input_text", "text": "TOKENPRO_IMAGE_PATH " + path}},
			},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "function", "name": "wait"},
			map[string]any{"type": "web_search"},
		},
	})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("User-Agent", "Codex Desktop/0.163.0-alpha.10.2 (Codex Desktop; 26.1013.40934)")
	req.Header.Set("originator", "codex_chatgpt_desktop")
	c.Request = req
	groupID := int64(16)
	key := &service.APIKey{
		ID: 9, UserID: 7, Key: "global-desktop", KeyType: service.APIKeyTypeGlobal, GroupID: &groupID,
		User:  &service.User{ID: 7},
		Group: &service.Group{ID: 16, Platform: service.PlatformOpenAI, AllowImageGeneration: true, Description: "对话"},
	}
	require.True(t, key.IsGlobal())
	h := &OpenAIGatewayHandler{}
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		t.Fatal("follow-up generated another image")
		return nil, nil
	}
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) {
		t.Fatal("follow-up published another image")
		return "", nil
	}
	h.chatImageRecordUsage = func(*service.OpenAIRecordUsageInput) {
		t.Fatal("follow-up recorded usage")
	}
	started := false
	require.True(t, h.finishTokenProLocalImageFollowUp(c, key, "gpt-6-sol", body, true, &started))
	require.True(t, started)
	require.Equal(t, "local-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	text := ""
	for _, block := range splitSSE(rec.Body.String()) {
		event := gjson.Parse(block)
		if event.Get("type").String() == "response.output_item.done" {
			text = event.Get("item.content.0.text").String()
		}
	}
	require.Equal(t, "![]("+path+")\n图片已保存，可在对话里直接查看。", text)

	fresh := []byte(`{"model":"gpt-6-sol","stream":true,"input":"画一只猫","tools":[{"type":"custom","name":"exec"}]}`)
	require.False(t, h.finishTokenProLocalImageFollowUp(c, key, "gpt-6-sol", fresh, true, &started))
}

func splitSSE(body string) []string {
	var parts []string
	rest := body
	for {
		index := -1
		for i := 0; i+1 < len(rest); i++ {
			if rest[i] == '\n' && rest[i+1] == '\n' {
				index = i
				break
			}
		}
		if index < 0 {
			return parts
		}
		block := rest[:index]
		rest = rest[index+2:]
		if len(block) > 6 && block[:6] == "data: " {
			parts = append(parts, block[6:])
		}
	}
}
