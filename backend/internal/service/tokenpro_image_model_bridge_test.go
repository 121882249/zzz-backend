package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
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

func TestTokenProAnthropicImageBridgeProducesClientExecutedTool(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-5",
		"stream":true,
		"input":"draw a nebula",
		"tools":[{"type":"image_generation","model":"gpt-image-2"}],
		"tool_choice":{"type":"image_generation"}
	}`)

	bridged, changed, err := applyTokenProCodexClientImageToolBridge(body, "gpt-image-2.5-sunburst")
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, jsonStringAt(t, bridged, "instructions"), codexClientImageBridgeMarker)

	adapted, mapping, err := adaptResponsesClientToolsForAnthropic(bridged)
	require.NoError(t, err)
	require.Equal(t, apicompat.ResponsesNamespaceName{Namespace: "image_gen", Name: "imagegen"}, mapping.NamespaceTools["image_gen__imagegen"])

	var request apicompat.ResponsesRequest
	require.NoError(t, json.Unmarshal(adapted, &request))
	anthropicRequest, err := apicompat.ResponsesToAnthropicRequest(&request)
	require.NoError(t, err)
	require.Len(t, anthropicRequest.Tools, 1)
	require.Equal(t, "image_gen__imagegen", anthropicRequest.Tools[0].Name)
	require.Empty(t, anthropicRequest.Tools[0].Type)
	require.NotEmpty(t, anthropicRequest.Tools[0].InputSchema)
	require.JSONEq(t, `{"type":"auto"}`, string(anthropicRequest.ToolChoice))
}

func TestTokenProAnthropicImageBridgeKeepsExistingNamespaceSingle(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-5",
		"input":"draw",
		"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen","parameters":{"type":"object"}}]}]
	}`)

	bridged, changed, err := applyTokenProCodexClientImageToolBridge(body, "gpt-image-2.5-sunburst")
	require.NoError(t, err)
	require.True(t, changed)
	var request map[string]any
	require.NoError(t, json.Unmarshal(bridged, &request))
	tools := request["tools"].([]any)
	require.Len(t, tools, 1)
	require.True(t, hasCodexImageGenerationClientTool(request))
}

func jsonStringAt(t *testing.T, body []byte, key string) string {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal(body, &value))
	result, _ := value[key].(string)
	return result
}
