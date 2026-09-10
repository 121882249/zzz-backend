package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestApplyTokenProPreferredImageModelOverridesCodexImagesDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set(tokenProImageModelHeader, "gpt-image-2.5-sunburst")
	svc := &OpenAIGatewayService{}

	rewritten, changed, err := svc.ApplyTokenProPreferredImageModel(c, []byte(`{"model":"gpt-image-2","prompt":"draw"}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "gpt-image-2.5-sunburst", jsonStringAt(t, rewritten, "model"))
	require.Empty(t, c.GetHeader(tokenProImageModelHeader))

	// Account failover and repeated parsing reuse the choice retained on Gin.
	rewritten, changed, err = svc.ApplyTokenProPreferredImageModel(c, []byte(`{"model":"gpt-image-2","prompt":"draw again"}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "gpt-image-2.5-sunburst", jsonStringAt(t, rewritten, "model"))
}

func TestTokenProNonOpenAIAccountStripsImageToolsWithoutDesktopHeader(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-5",
		"stream":true,
		"input":"draw a nebula",
		"tools":[
			{"type":"image_generation","model":"gpt-image-2"},
			{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen","parameters":{"type":"object"}}]},
			{"type":"function","name":"shell","parameters":{"type":"object"}}
		],
		"tool_choice":{"type":"image_generation"}
	}`)

	stripped, changed, err := stripImageGenerationToolsForAccount(&Account{Platform: PlatformAnthropic}, body)
	require.NoError(t, err)
	require.True(t, changed)
	var request map[string]any
	require.NoError(t, json.Unmarshal(stripped, &request))
	tools, ok := request["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	require.Equal(t, "shell", tools[0].(map[string]any)["name"])
	require.NotContains(t, request, "tool_choice")
}

func TestTokenProOpenAIAccountKeepsImageTools(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","tools":[{"type":"image_generation","model":"gpt-image-2"}]}`)
	kept, changed, err := stripImageGenerationToolsForAccount(&Account{Platform: PlatformOpenAI}, body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, kept)
}

func TestImageModelChosenInCodexOverridesFallbackAndForcesImageTool(t *testing.T) {
	req := map[string]any{
		"model":       "gpt-image-2.5-sunburst",
		"tool_choice": "auto",
		"tools": []any{map[string]any{
			"type":  "image_generation",
			"model": "gpt-image-2",
		}},
	}

	require.True(t, normalizeOpenAIResponsesImageOnlyModel(req))
	require.Equal(t, openAIImagesResponsesMainModelValue(), req["model"])
	tools, ok := req["tools"].([]any)
	require.True(t, ok)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "gpt-image-2.5-sunburst", tool["model"])
	toolChoice, ok := req["tool_choice"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "image_generation", toolChoice["type"])
}

func jsonStringAt(t *testing.T, body []byte, key string) string {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal(body, &value))
	result, _ := value[key].(string)
	return result
}
