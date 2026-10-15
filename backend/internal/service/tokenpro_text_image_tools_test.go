package service

import (
	"context"
	"encoding/base64"
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

func TestTokenProTextImageSkillUsesOnlyDeclaredToolsAndSelectedTextRoute(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
	adapted, err := prepareTokenProImageTools(c, body)
	require.NoError(t, err)
	require.True(t, TokenProNativeImageDelivery(c))
	require.Equal(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(adapted, "tools").Raw)
	require.Equal(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(adapted, "input").Raw)
	require.Equal(t, "gpt-6-channel-mapped", gjson.GetBytes(adapted, "model").String())
	guidance := gjson.GetBytes(adapted, "instructions").String()
	route := "tp-g69-" + base64.RawURLEncoding.EncodeToString([]byte("gpt-6-luna"))
	require.Contains(t, guidance, "Original instructions.")
	require.Contains(t, guidance, "/Users/fixture/.codex/skills/imagegen/SKILL.md")
	require.Contains(t, guidance, "--model \""+route+"\"")
	require.NotContains(t, guidance, tokenProNativeImageInstructions)
	require.NotContains(t, guidance, "gpt-image-2.5-sunburst")
	require.Contains(t, guidance, tokenProTextImageFastDeliveryInstructions)
	twice, err := prepareTokenProImageTools(c, adapted)
	require.NoError(t, err)
	require.Equal(t, adapted, twice)
}

func TestTokenProTextImageFastDeliveryChangesInstructionsOnly(t *testing.T) {
	c, _ := tokenProTextToolsContext(t)
	body := tokenProTextToolsFixture(t, "developer", tokenProLegacySkillCatalogFixture)
	var err error
	body, err = sjson.SetBytes(body, "reasoning", gin.H{"effort": "xhigh", "summary": "auto"})
	require.NoError(t, err)
	body, err = sjson.SetBytes(body, "tool_choice", "auto")
	require.NoError(t, err)
	adapted, err := prepareTokenProImageTools(c, body)
	require.NoError(t, err)
	originalWithoutInstructions, err := sjson.DeleteBytes(body, "instructions")
	require.NoError(t, err)
	adaptedWithoutInstructions, err := sjson.DeleteBytes(adapted, "instructions")
	require.NoError(t, err)
	require.JSONEq(t, string(originalWithoutInstructions), string(adaptedWithoutInstructions))
	guidance := gjson.GetBytes(adapted, "instructions").String()
	require.Contains(t, guidance, "yield_time_ms: 30000")
	require.Contains(t, guidance, "exit code 0 and an actual saved absolute image path")
	require.Contains(t, guidance, "Failed, incomplete or ambiguous outputs must not be treated as success")
	require.Contains(t, guidance, "this shortcut does not discard it")
	require.Equal(t, "xhigh", gjson.GetBytes(adapted, "reasoning.effort").String())
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
					if global && hasSkill {
						require.Contains(t, gjson.GetBytes(upstream.lastBody, "instructions").String(), "--model")
						require.Len(t, gjson.GetBytes(upstream.lastBody, "tools").Array(), 2)
						require.Equal(t, "exec", gjson.GetBytes(upstream.lastBody, "tools.0.name").String())
					}
				}
				if !hasSkill {
					require.JSONEq(t, forwarded[0], forwarded[1], "without native/Skill support, global and group Keys must use the same Responses pipeline")
				}
			})
		}
	}
}
