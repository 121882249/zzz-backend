package service

import (
	"context"
	"encoding/json"
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
	"github.com/tidwall/sjson"
)

const tokenProLegacySkillCatalogFixture = "- imagegen: Generate or edit raster images through TokenPro. (file: /Users/fixture/.codex/skills/imagegen/SKILL.md)"

func tokenProTextToolsFixture(t *testing.T, role, catalog string) []byte {
	t.Helper()
	body, err := json.Marshal(gin.H{
		"model": "gpt-6-channel-mapped", "stream": false, "instructions": "Original instructions.",
		"tools": []any{gin.H{"type": "custom", "name": "exec", "format": gin.H{"type": "text"}}, gin.H{"type": "web_search"}},
		"input": []any{gin.H{"role": role, "content": []any{gin.H{"type": "input_text", "text": catalog}}}, gin.H{"role": "user", "content": "draw a cat"}},
	})
	require.NoError(t, err)
	return body
}

func tokenProTextToolsContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	groupID := int64(69)
	c.Set("api_key", &APIKey{ID: 99, UserID: 77, KeyType: APIKeyTypeGlobal, GroupID: &groupID,
		Group: &Group{ID: groupID, Platform: PlatformOpenAI, AllowImageGeneration: true}})
	c.Set(TokenProTextImageDeliveryContextKey, true)
	c.Set(TokenProImageTurnContextKey, &TokenProPendingImageTurn{Model: "gpt-6-luna"})
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	return c, rec
}

func TestTokenProNativeImagesNeverInventClientTools(t *testing.T) {
	for _, tools := range []string{
		`[]`, `[{"type":"custom","name":"exec"}]`,
		`[{"type":"namespace","name":"image_gen","tools":[]}]`,
		`[{"type":"namespace","name":"image_gen","tools":[{"type":"custom","name":"imagegen"}]}]`,
		`[{"type":"image_generation","model":"gpt-image-2"}]`,
	} {
		t.Run(tools, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-luna","tools":` + tools + `,"input":"cat","tool_choice":"auto"}`)
			adapted, err := PrepareTokenProNativeImages(body)
			require.NoError(t, err)
			require.Equal(t, body, adapted, "an upstream schema cannot register a local Codex tool handler")
		})
	}
}

func TestTokenProTextImageSkillUsesHostedBridgeNotClientSkill(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
	adapted, err := prepareTokenProImageTools(c, body)
	require.NoError(t, err)
	require.False(t, TokenProNativeImageDelivery(c))
	require.False(t, tokenProHostedImageBridgeRequested(c))
	require.Equal(t, body, adapted, "a global GPT text group keeps the client skill catalog")
	require.Contains(t, string(adapted), "SKILL.md")
}

func TestTokenProTextImageFastDeliveryChangesCatalogOnly(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
	var err error
	body, err = sjson.SetBytes(body, "reasoning", gin.H{"effort": "xhigh", "summary": "auto"})
	require.NoError(t, err)
	body, err = sjson.SetBytes(body, "tool_choice", "auto")
	require.NoError(t, err)
	body, err = sjson.SetBytes(body, "instructions", "Original instructions.\n\n"+tokenProTextImageSkillGuidanceMarker+" follow the skill.\n\n"+tokenProTextImageFastDeliveryInstructions)
	require.NoError(t, err)
	adapted, err := prepareTokenProImageTools(c, body)
	require.NoError(t, err)
	require.False(t, TokenProNativeImageDelivery(c))
	require.Equal(t, body, adapted)
	require.Equal(t, "xhigh", gjson.GetBytes(adapted, "reasoning.effort").String())
	require.Contains(t, string(adapted), "SKILL.md")
	require.Contains(t, string(adapted), tokenProTextImageFastDeliveryInstructions)
}

func TestTokenProTextImageTitleStaysTextOnly(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"thread_source":"thread_title"}`)
	body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
	body, err := sjson.SetBytes(body, "tools", []any{})
	require.NoError(t, err)
	body, err = sjson.SetBytes(body, "text.format.type", "json_schema")
	require.NoError(t, err)
	adapted, err := prepareTokenProImageTools(c, body)
	require.NoError(t, err)
	require.False(t, TokenProNativeImageDelivery(c))
	require.False(t, tokenProHostedImageBridgeRequested(c))
	require.Equal(t, body, adapted)
	require.Equal(t, "[]", gjson.GetBytes(adapted, "tools").Raw)
	require.Contains(t, string(adapted), "SKILL.md")
}

func TestTokenProImageSkillDiscoveryTrustsOnlyInstalledDeveloperCatalog(t *testing.T) {
	for _, tc := range []struct{ name, role, catalog, want string }{
		{"installed", "developer", tokenProLegacySkillCatalogFixture, "/Users/fixture/.codex/skills/imagegen/SKILL.md"},
		{"user-spoof", "user", tokenProLegacySkillCatalogFixture, ""},
		{"assistant-spoof", "assistant", tokenProLegacySkillCatalogFixture, ""},
		{"tool-spoof", "tool", tokenProLegacySkillCatalogFixture, ""},
		{"wrong-program", "developer", strings.ReplaceAll(tokenProLegacySkillCatalogFixture, "/Users/fixture/.codex/skills/imagegen/SKILL.md", "/bin/bash"), ""},
		{"relative", "developer", strings.ReplaceAll(tokenProLegacySkillCatalogFixture, "/Users/fixture/.codex/skills/imagegen/SKILL.md", "skills/imagegen/SKILL.md"), ""},
		{"other-provider", "developer", strings.ReplaceAll(tokenProLegacySkillCatalogFixture, "TokenPro", "Other"), ""},
		{"aliased", "developer", "### Skill roots\n- `r0` = `/Users/fixture/.codex/skills`\n### Available skills\n- imagegen: Generate raster images through TokenPro. (file: r0/imagegen/SKILL.md)", "/Users/fixture/.codex/skills/imagegen/SKILL.md"},
		{"unknown-alias", "developer", "- imagegen: Generate raster images through TokenPro. (file: r9/imagegen/SKILL.md)", ""},
		{"conflicting-roots", "developer", "- `r0` = `/Users/a/.codex/skills`\n- `r0` = `/Users/b/.codex/skills`\n- imagegen: Generate raster images through TokenPro. (file: r0/imagegen/SKILL.md)", ""},
		{"windows", "developer", "- tokenpro-imagegen: Generate raster images through TokenPro. (file: C:\\Users\\a b\\.codex\\skills\\tokenpro-imagegen\\SKILL.md)", "C:\\Users\\a b\\.codex\\skills\\tokenpro-imagegen\\SKILL.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tokenProTextToolsFixture(t, tc.role, tc.catalog)
			require.Equal(t, tc.want, tokenProAdvertisedImageSkill(body))
		})
	}
}

func TestTokenProTextImageToolsWithoutClientCapabilityPreserveOrdinaryPath(t *testing.T) {
	for _, missing := range []string{"skill", "global-key", "group", "selection"} {
		t.Run(missing, func(t *testing.T) {
			c, _ := tokenProTextToolsContext(t)
			body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
			key := getAPIKeyFromContext(c)
			switch missing {
			case "skill":
				body = tokenProTextToolsFixture(t, "user", tokenProLegacySkillCatalogFixture)
			case "global-key":
				key.KeyType = APIKeyTypeGroup
			case "group":
				key.GroupID = nil
			case "selection":
				c.Set(TokenProImageTurnContextKey, &TokenProPendingImageTurn{Model: "gpt-image-2"})
			}
			adapted, err := prepareTokenProImageTools(c, body)
			require.NoError(t, err)
			require.Equal(t, body, adapted)
			require.NotContains(t, string(adapted), "TokenPro fast image delivery")
			require.False(t, TokenProNativeImageDelivery(c))
		})
	}
}

func TestTokenProTextImageForwardRespectsActualClientCapabilities(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, hasSkill := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/skill=%t", passthrough, hasSkill), func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Gateway.ForceCodexCLI = true
				cfg.Gateway.CodexImageGenerationBridgeEnabled = true
				var forwarded []string
				for _, global := range []bool{false, true} {
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_fixture","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}}
					svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
					account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true, "openai_passthrough": passthrough}}
					c, _ := tokenProTextToolsContext(t)
					if !global {
						getAPIKeyFromContext(c).KeyType = APIKeyTypeGroup
						c.Set(TokenProTextImageDeliveryContextKey, false)
					}
					role := "user"
					if hasSkill {
						role = "developer"
					}
					body := tokenProTextToolsFixture(t, role, tokenProLegacySkillCatalogFixture)
					result, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Zero(t, result.ImageCount)
					forwarded = append(forwarded, string(upstream.lastBody))
					for _, tool := range gjson.GetBytes(upstream.lastBody, "tools").Array() {
						require.NotEqual(t, "image_gen", tool.Get("name").String(), "never invent a native handler")
					}
					instructions := gjson.GetBytes(upstream.lastBody, "instructions").String()
					require.NotContains(t, instructions, tokenProTextImageSkillGuidanceMarker)
					require.NotContains(t, instructions, tokenProTextImageFastDeliveryInstructions)
					require.NotContains(t, instructions, "--model")
					if global && hasSkill {
						require.Contains(t, string(upstream.lastBody), "SKILL.md")
						imageTool := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`)
						if passthrough {
							require.False(t, imageTool.Exists(), "passthrough accounts do not gain an injected image tool")
						} else {
							require.True(t, imageTool.Exists(), "the upstream bridge still injects image_generation")
							require.Equal(t, "png", imageTool.Get("output_format").String())
							require.False(t, imageTool.Get("model").Exists(), "text-model bridge leaves the image model unset")
						}
						require.Zero(t, result.ImageCount)
						require.Len(t, upstream.bodies, 1)
					}
				}
				if !hasSkill && !passthrough {
					require.JSONEq(t, forwarded[0], forwarded[1], "without a skill catalog, global and group Keys use the same Responses pipeline")
				}
				if !hasSkill && passthrough {
					require.False(t, gjson.Get(forwarded[0], `tools.#(type=="image_generation")`).Exists(), "group passthrough stays on its ordinary body")
					require.False(t, gjson.Get(forwarded[1], `tools.#(type=="image_generation")`).Exists(), "global passthrough does not inject image_generation")
				}
			})
		}
	}
}

func TestTokenProTextImageHostedForwardBillsOneTextRow(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.ForceCodexCLI = true
			cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_cat","object":"response","model":"gpt-6-channel-mapped","output":[{"id":"ig_cat","type":"image_generation_call","status":"completed","output_format":"png","result":"` + hostedTestPNG + `"}],"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true, "openai_passthrough": passthrough}}
			c, rec := tokenProTextToolsContext(t)
			SetOpenAIImageIntentHint(c, false)
			SetTokenProHostedImagePublisher(c, func(context.Context, string, []byte) (string, error) {
				t.Fatal("global text group must not store the upstream image")
				return "", nil
			})
			body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 1, "the text forward is the only billed request")
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, "gpt-6-channel-mapped", result.Model)
			require.Equal(t, 11, result.Usage.InputTokens)
			require.Equal(t, 22, result.Usage.OutputTokens)
			require.Equal(t, "gpt-image-2", result.BillingModel, "an upstream image on a text turn bills gpt-image-2 once")
			imageTool := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`)
			if passthrough {
				require.False(t, imageTool.Exists(), "passthrough does not inject image_generation")
			} else {
				require.True(t, imageTool.Exists())
				require.False(t, imageTool.Get("model").Exists())
			}
			require.Contains(t, string(upstream.lastBody), "SKILL.md")
			client := rec.Body.String()
			require.Contains(t, client, "image_generation_call")
			require.Contains(t, client, hostedTestPNG)
			require.NotContains(t, client, "custom_tool_call")
			require.NotContains(t, client, "data:image/jpeg;base64,")
			require.Empty(t, rec.Header().Get("X-TokenPro-Chat-Image"))
		})
	}
}

func TestTokenProTitleWithoutTextImageDeliveryGetsNoImageTool(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header string
		tools  string
	}{
		{name: "no tools json_schema", tools: `[]`},
		{name: "thread_title", header: `{"thread_source":"thread_title"}`, tools: `[{"type":"custom","name":"exec"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.ForceCodexCLI = true
			cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_title","object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"{\"title\":\"猫\"}"}]}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true}}
			c, _ := tokenProTextToolsContext(t)
			getAPIKeyFromContext(c).KeyType = APIKeyTypeGroup
			c.Set(TokenProTextImageDeliveryContextKey, false)
			if tc.header != "" {
				c.Request.Header.Set("X-Codex-Turn-Metadata", tc.header)
			}
			body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
			body, err := sjson.SetRawBytes(body, "tools", []byte(tc.tools))
			require.NoError(t, err)
			body, err = sjson.SetBytes(body, "text.format.type", "json_schema")
			require.NoError(t, err)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Zero(t, result.ImageCount)
			require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.NotContains(t, string(upstream.lastBody), "sub2api-codex-image-generation")
		})
	}
}

func TestTokenProTextImageTitleForwardStaysUnbilledAndTextOnly(t *testing.T) {
	for _, tc := range []struct {
		name        string
		header      string
		tools       string
		passthrough bool
	}{
		{name: "no tools json_schema", tools: `[]`},
		{name: "thread_title", header: `{"thread_source":"thread_title"}`, tools: `[{"type":"custom","name":"exec"}]`},
		{name: "passthrough no tools json_schema", tools: `[]`, passthrough: true},
		{name: "passthrough thread_title", header: `{"thread_source":"thread_title"}`, tools: `[{"type":"custom","name":"exec"}]`, passthrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.ForceCodexCLI = true
			cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_title","object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"{\"title\":\"猫\"}"}]}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true, "openai_passthrough": tc.passthrough}}
			c, rec := tokenProTextToolsContext(t)
			if tc.header != "" {
				c.Request.Header.Set("X-Codex-Turn-Metadata", tc.header)
			}
			body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
			body, err := sjson.SetRawBytes(body, "tools", []byte(tc.tools))
			require.NoError(t, err)
			body, err = sjson.SetBytes(body, "text.format.type", "json_schema")
			require.NoError(t, err)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 1)
			require.Zero(t, result.ImageCount)
			require.NotEqual(t, "gpt-image-2", result.BillingModel)
			require.Contains(t, string(upstream.lastBody), "SKILL.md")
			require.False(t, tokenProHostedImageBridgeRequested(c))
			imageTool := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`)
			if tc.passthrough {
				require.Empty(t, result.BillingModel)
				require.Equal(t, "gpt-6-channel-mapped", result.Model)
				require.False(t, imageTool.Exists(), "passthrough does not inject on a title turn")
			} else {
				require.Equal(t, "gpt-6-channel-mapped", result.BillingModel)
				require.True(t, imageTool.Exists(), "a global text group uses the upstream bridge, including title turns")
			}
			require.NotContains(t, rec.Body.String(), "custom_tool_call")
		})
	}
}

func TestGlobalGPTTextGroupStreamsImageCallUnchanged(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ForceCodexCLI = true
	cfg.Gateway.CodexImageGenerationBridgeEnabled = true
	sse := "data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"" + hostedTestPNG + "\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_cat\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"" + hostedTestPNG + "\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_cat\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"画好啦\"}]},{\"id\":\"ig_cat\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"" + hostedTestPNG + "\"}],\"usage\":{\"input_tokens\":11,\"output_tokens\":22,\"total_tokens\":33}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true}}
	c, rec := tokenProTextToolsContext(t)
	body := tokenProTextToolsFixture(t, "user", "draw a cat")
	body, err := sjson.SetBytes(body, "stream", true)
	require.NoError(t, err)
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", result.BillingModel)
	require.Equal(t, 11, result.Usage.InputTokens)
	client := rec.Body.String()
	require.Contains(t, client, "response.image_generation_call.partial_image")
	require.Contains(t, client, "image_generation_call")
	require.Contains(t, client, hostedTestPNG)
	require.Contains(t, client, "画好啦")
	require.NotContains(t, client, "custom_tool_call")
	require.NotContains(t, client, "data:image/jpeg;base64,")
}

func TestGlobalGPTTextGroupFollowUpAndErrorDoNotBillImage(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ForceCodexCLI = true
	cfg.Gateway.CodexImageGenerationBridgeEnabled = true
	account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"}, Extra: map[string]any{"use_responses_api": true}}
	body := tokenProTextToolsFixture(t, "user", "thanks")

	followUpstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_next","object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"好的"}]}],"usage":{"input_tokens":5,"output_tokens":6,"total_tokens":11}}`)),
	}}
	c, _ := tokenProTextToolsContext(t)
	follow, err := (&OpenAIGatewayService{cfg: cfg, httpUpstream: followUpstream}).Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Zero(t, follow.ImageCount)
	require.NotEqual(t, "gpt-image-2", follow.BillingModel)
	require.Equal(t, 5, follow.Usage.InputTokens)

	errorUpstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 500,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"api_error","message":"upstream failed"}}`)),
	}}
	c, _ = tokenProTextToolsContext(t)
	failed, err := (&OpenAIGatewayService{cfg: cfg, httpUpstream: errorUpstream}).Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Nil(t, failed)
}

func TestGroupKeyTextImageStillRewritesHostedCall(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.ForceCodexCLI = true
	cfg.Gateway.CodexImageGenerationBridgeEnabled = true
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_cat","object":"response","output":[{"id":"ig_cat","type":"image_generation_call","status":"completed","result":"` + hostedTestPNG + `"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`)),
	}}
	c, rec := tokenProTextToolsContext(t)
	getAPIKeyFromContext(c).KeyType = APIKeyTypeGroup
	c.Set(TokenProTextImageDeliveryContextKey, false)
	SetTokenProHostedImagePublisher(c, func(context.Context, string, []byte) (string, error) {
		return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
	})
	body := tokenProTextToolsFixture(t, "user", "draw a cat")
	result, err := (&OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}).Forward(context.Background(), c, &Account{
		ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fixture", "base_url": "https://example.com"},
		Extra:       map[string]any{"use_responses_api": true},
	}, body)
	require.NoError(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", result.BillingModel)
	require.NotContains(t, rec.Body.String(), "image_generation_call")
	require.Contains(t, rec.Body.String(), "custom_tool_call")
}

func TestGlobalGPTTextGroupKeepsReplayedImageResult(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	big := strings.Repeat("A", tokenProReplayImageResultLimit+8)
	body := []byte(`{"model":"gpt-6-sol","input":[{"type":"image_generation_call","id":"ig_1","result":"` + big + `"}]}`)
	require.True(t, tokenProGlobalGPTTextGroup(c, body))
	out, changed := StripTokenProReplayImageResults(body)
	require.True(t, changed)
	kept, skip := body, false
	if !tokenProGlobalGPTTextGroup(c, body) {
		kept, skip = StripTokenProReplayImageResults(body)
	}
	require.False(t, skip)
	require.Equal(t, body, kept)
	require.NotEqual(t, body, out)

	imageGroup := getAPIKeyFromContext(c)
	imageGroup.Group.Description = "生图"
	require.False(t, tokenProGlobalGPTTextGroup(c, body))

	dataBody := []byte(`{"model":"gpt-6-sol","input":[{"role":"assistant","content":"图片已生成：\n\n![](data:image/jpeg;base64,QUFB)\n\n[原图链接](https://tokenpro.work/cat.png)"}]}`)
	imageGroup.Group.Description = ""
	stripped, changed := StripTokenProDataImageMarkdown(dataBody)
	require.True(t, changed)
	require.Contains(t, string(stripped), "[已生成图片]")
	require.NotContains(t, string(stripped), "data:image/jpeg;base64,")
	require.Contains(t, string(stripped), "https://tokenpro.work/cat.png")
}
