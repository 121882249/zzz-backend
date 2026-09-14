package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProNativeImagesPreserveHistoryAndToolCorrelation(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","metadata":{"id":9007199254740993},"input":[{"type":"function_call_output","call_id":"call_image_A","output":"returned-image-A"}],"tools":[{"type":"image_generation"},{"type":"function","name":"other","parameters":{"type":"object"}}],"tool_choice":{"type":"image_generation"}}`)
	adapted, err := PrepareTokenProNativeImages(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(adapted, "model").String())
	require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(adapted, "input").Raw)
	require.Equal(t, "9007199254740993", gjson.GetBytes(adapted, "metadata.id").Raw)
	require.Equal(t, "auto", gjson.GetBytes(adapted, "tool_choice").String())
	require.Equal(t, "other", gjson.GetBytes(adapted, "tools.0.name").String())
	require.Equal(t, "image_gen", gjson.GetBytes(adapted, "tools.1.name").String())
	require.Equal(t, "imagegen", gjson.GetBytes(adapted, "tools.1.tools.0.name").String())
	twice, err := PrepareTokenProNativeImages(adapted)
	require.NoError(t, err)
	require.JSONEq(t, string(adapted), string(twice))
}

func TestTokenProNativeImagesForwardDisablesLegacyHostedInjection(t *testing.T) {
	for _, tc := range []struct {
		model       string
		passthrough bool
	}{
		{"gpt-5.6-sol", false},
		{"gpt-5.6-sol", true},
	} {
		t.Run(tc.model+fmt.Sprint(tc.passthrough), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_native","output":[{"id":"fc_A","type":"function_call","call_id":"call_A","name":"imagegen","namespace":"image_gen","arguments":"{\"prompt\":\"cat\"}"}],"usage":{"input_tokens":1,"output_tokens":2}}`))}}
			cfg := &config.Config{}
			cfg.Gateway.ForceCodexCLI = true
			cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true}}
			account.Extra["openai_passthrough"] = tc.passthrough
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", &APIKey{Group: &Group{AllowImageGeneration: true}})
			c.Set(TokenProNativeImagesContextKey, true)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"`+tc.model+`","stream":false,"input":"draw a cat"}`))
			require.NoError(t, err)
			require.Zero(t, result.ImageCount, "dispatching a tool must not be billed as an already generated image")
			for _, tool := range gjson.GetBytes(upstream.lastBody, "tools").Array() {
				require.NotEqual(t, "image_generation", tool.Get("type").String())
			}
			require.Contains(t, gjson.GetBytes(upstream.lastBody, "instructions").String(), tokenProNativeImageInstructions)
			require.Equal(t, "call_A", gjson.Get(rec.Body.String(), "output.0.call_id").String())
		})
	}
}

func TestTokenProNativeImagesDoesNotDuplicateNamespace(t *testing.T) {
	body, err := PrepareTokenProNativeImages([]byte(`{"model":"gpt-5.6-sol","tools":[{"type":"namespace","name":"image_gen","tools":[]}],"input":[]}`))
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(body, "tools").Array(), 1)
	require.Len(t, gjson.GetBytes(body, "tools.0.tools").Array(), 1)
}

func TestTokenProPureImageNeverBorrowsTextModel(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.5-flare","input":"cat","tools":[{"type":"image_generation"}]}`)
	result, err := PrepareTokenProNativeImages(body)
	require.NoError(t, err)
	require.Equal(t, body, result)
}

func TestTokenProNativeImagesGroupPolicyDisablesAllDownstreamAdapters(t *testing.T) {
	for _, tc := range []struct {
		name, platform, description, model string
		want                               bool
	}{
		{"image-group", PlatformOpenAI, "生图", "gpt-image-2.5-flare", true},
		{"trim", PlatformOpenAI, " 生图 ", "gpt-image-2.5-flare", true},
		{"name-only", PlatformOpenAI, "", "gpt-image-2.5-flare", false},
		{"other-description", PlatformOpenAI, "普通", "gpt-image-2.5-flare", false},
		{"other-platform", PlatformAnthropic, "生图", "gpt-image-2.5-flare", false},
		{"ordinary-text", PlatformOpenAI, "普通", "gpt-5.6-sol", false},
		{"image-group-text", PlatformOpenAI, "生图", "gpt-5.6-sol", true},
		{"composite-image-group", PlatformComposite, "生图", "gpt-image-2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set(TokenProNativeImagesContextKey, true)
			c.Set(TokenProNativeImageDriverContextKey, "gpt-5.6-sol")
			RestrictTokenProNativeImages(c, &Group{Name: "生图", Platform: tc.platform, Description: tc.description}, tc.model)
			require.Equal(t, tc.want, TokenProNativeImages(c))
			if !tc.want {
				require.Empty(t, c.GetString(TokenProNativeImageDriverContextKey))
			}
		})
	}
}

func TestTokenProNativeTextImageRequestKeepsSeparateDriverAndImage(t *testing.T) {
	parsed := &OpenAIImagesRequest{Model: "gpt-image-2", ResponsesModel: "gpt-5.6-sol", Endpoint: "/v1/images/generations", Prompt: "orange cat", N: 1}
	body, err := buildOpenAIImagesResponsesRequest(parsed, parsed.Model)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(body, "model").String())
	require.Equal(t, "gpt-image-2", gjson.GetBytes(body, "tools.0.model").String())
	require.Equal(t, "image_generation", gjson.GetBytes(body, "tool_choice.type").String())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	untrusted, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, []byte(`{"model":"gpt-image-2","prompt":"cat","ResponsesModel":"unauthorized-driver"}`))
	require.NoError(t, err)
	require.Empty(t, untrusted.ResponsesModel)
}
