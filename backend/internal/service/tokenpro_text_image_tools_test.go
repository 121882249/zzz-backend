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
	require.False(t, TokenProNativeImageDelivery(c), "hosted image_generation bridge must stay enabled")
	require.True(t, tokenProHostedImageBridgeRequested(c))
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(adapted, "tools").Raw)
	require.Equal(t, "gpt-6-channel-mapped", gjson.GetBytes(adapted, "model").String())
	require.Equal(t, "Original instructions.", gjson.GetBytes(adapted, "instructions").String())
	catalog := gjson.GetBytes(adapted, "input.0.content.0.text").String()
	require.NotContains(t, catalog, "SKILL.md")
	require.NotContains(t, catalog, "tokenpro-imagegen/SKILL")
	require.Contains(t, catalog, tokenProImageSkillNeutralLine)
	require.NotContains(t, string(adapted), tokenProTextImageSkillGuidanceMarker)
	require.NotContains(t, string(adapted), tokenProTextImageFastDeliveryInstructions)
	require.NotContains(t, string(adapted), "--model")
	require.NotContains(t, string(adapted), "gpt-image-2.5-sunburst")
	twice, err := prepareTokenProImageTools(c, adapted)
	require.NoError(t, err)
	require.Equal(t, adapted, twice)
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
	require.Equal(t, "xhigh", gjson.GetBytes(adapted, "reasoning.effort").String())
	require.Equal(t, "auto", gjson.GetBytes(adapted, "tool_choice").String())
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(adapted, "tools").Raw)
	require.Equal(t, "Original instructions.", gjson.GetBytes(adapted, "instructions").String())
	require.NotContains(t, string(adapted), "yield_time_ms: 30000")
	require.NotContains(t, string(adapted), "SKILL.md")
	require.Contains(t, gjson.GetBytes(adapted, "input.0.content.0.text").String(), "hosted image_generation tool")
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
	require.True(t, TokenProNativeImageDelivery(c), "title turns must not arm the hosted image bridge")
	require.False(t, tokenProHostedImageBridgeRequested(c))
	require.Equal(t, "[]", gjson.GetBytes(adapted, "tools").Raw)
	require.Equal(t, "json_schema", gjson.GetBytes(adapted, "text.format.type").String())
	catalog := gjson.GetBytes(adapted, "input.0.content.0.text").String()
	require.Contains(t, catalog, tokenProImageSkillNeutralTitleLine)
	require.NotContains(t, catalog, "hosted image_generation tool")
	require.NotContains(t, string(adapted), "SKILL.md")
	require.NotContains(t, string(adapted), tokenProTextImageFastDeliveryInstructions)
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
						require.NotContains(t, string(upstream.lastBody), "SKILL.md")
						require.Contains(t, string(upstream.lastBody), "Do not read or run imagegen or tokenpro-imagegen")
						imageTool := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`)
						require.True(t, imageTool.Exists(), "skill catalog must not block the hosted image tool")
						require.Equal(t, "png", imageTool.Get("output_format").String())
						require.False(t, imageTool.Get("model").Exists(), "text-model bridge leaves the image model unset")
						require.Zero(t, result.ImageCount)
						require.Len(t, upstream.bodies, 1)
					}
				}
				if !hasSkill && !passthrough {
					require.JSONEq(t, forwarded[0], forwarded[1], "without native/Skill support, global and group Keys must use the same Responses pipeline")
				}
				if !hasSkill && passthrough {
					require.False(t, gjson.Get(forwarded[0], `tools.#(type=="image_generation")`).Exists(), "group passthrough stays on its ordinary body")
					imageTool := gjson.Get(forwarded[1], `tools.#(type=="image_generation")`)
					require.True(t, imageTool.Exists(), "a global text turn uses the hosted tool even when passthrough is on")
					require.False(t, imageTool.Get("model").Exists())
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
			// The handler classifies the original text body before Forward injects the tool.
			SetOpenAIImageIntentHint(c, false)
			SetTokenProHostedImagePublisher(c, func(context.Context, string, []byte) (string, error) {
				return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
			})
			body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 1, "the skill CLI must not open a second billed generation")
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, "gpt-6-channel-mapped", result.Model)
			require.Equal(t, "gpt-image-2", result.BillingModel, "an omitted hosted image_generation model bills as gpt-image-2 on this same row")
			imageTool := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`)
			require.True(t, imageTool.Exists())
			require.False(t, imageTool.Get("model").Exists())
			require.NotContains(t, string(upstream.lastBody), "SKILL.md")
			require.NotContains(t, string(upstream.lastBody), tokenProTextImageFastDeliveryInstructions)
			client := rec.Body.String()
			require.NotContains(t, client, hostedTestPNG)
			require.NotContains(t, client, "image_generation_call")
			require.Equal(t, "custom_tool_call", gjson.Get(client, "output.0.type").String())
			require.Contains(t, gjson.Get(client, "output.0.input").String(), "https://tokenpro.work/cat.png?exp=1&sig=ff")
			require.Contains(t, gjson.Get(client, "output.0.input").String(), ".codex/generated_images/tokenpro/")
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
			if tc.passthrough {
				require.Empty(t, result.BillingModel)
				require.Equal(t, "gpt-6-channel-mapped", result.Model)
			} else {
				require.Equal(t, "gpt-6-channel-mapped", result.BillingModel)
			}
			require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.NotContains(t, string(upstream.lastBody), "SKILL.md")
			require.NotContains(t, string(upstream.lastBody), tokenProTextImageFastDeliveryInstructions)
			require.NotContains(t, rec.Body.String(), "image_generation")
			require.True(t, TokenProNativeImageDelivery(c))
			require.False(t, tokenProHostedImageBridgeRequested(c))
		})
	}
}
