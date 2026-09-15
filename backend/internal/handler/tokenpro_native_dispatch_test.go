package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProPureDispatchAfterAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		auth, group, allow, native, stream bool
		status                             int
	}{
		{"allowed-json", true, true, true, true, false, 200},
		{"allowed-sse", true, true, true, true, true, 200},
		{"unauthenticated", false, true, true, true, false, 401},
		{"image-disabled", true, true, false, true, false, 403},
		{"unresolved-global-group", true, false, true, true, false, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			body := `{"model":"gpt-image-2.5-flare","input":"draw a cat","stream":false}`
			if tc.stream {
				body = strings.Replace(body, `"stream":false`, `"stream":true`, 1)
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			if tc.native {
				c.Set(service.TokenProNativeImagesContextKey, true)
			}
			groupID := int64(65)
			key := &service.APIKey{ID: 99, KeyType: service.APIKeyTypeGlobal, User: &service.User{ID: 77, Status: service.StatusActive}}
			if tc.group {
				key.GroupID = &groupID
				key.Group = &service.Group{ID: 65, Platform: service.PlatformOpenAI, Description: "生图", AllowImageGeneration: tc.allow, Status: service.StatusActive}
			}
			if tc.auth {
				c.Set(string(middleware2.ContextKeyAPIKey), key)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 77, Concurrency: 1})
			}
			bill := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
			defer bill.Stop()
			h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, billingCacheService: bill, apiKeyService: &service.APIKeyService{},
				concurrencyHelper: &ConcurrencyHelper{concurrencyService: service.NewConcurrencyService(&helperConcurrencyCacheStub{userSeq: []bool{true}})}, cfg: &config.Config{}, imageLimiter: &imageConcurrencyLimiter{}}
			h.Responses(c)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status == 200 {
				require.Equal(t, "v1", rec.Header().Get("X-TokenPro-Native-Dispatch"))
				require.Contains(t, rec.Body.String(), `"namespace":"image_gen"`)
				require.NotContains(t, rec.Body.String(), "gpt-5.6-luna")
				if !tc.stream {
					require.Equal(t, int64(0), gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
				}
			} else {
				require.Empty(t, rec.Header().Get("X-TokenPro-Native-Dispatch"))
			}
		})
	}
}

func TestTokenProPureDispatchOptInOnly(t *testing.T) {
	for _, native := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		if native {
			c.Set(service.TokenProNativeImagesContextKey, true)
		}
		started := false
		h := &OpenAIGatewayHandler{}
		handled := h.dispatchTokenProNativeImage(c, &service.APIKey{KeyType: service.APIKeyTypeGlobal}, "gpt-5.6-sol", service.PlatformOpenAI, []byte(`{"input":"hello"}`), false, &started)
		require.False(t, handled)
	}
}

func TestTokenProCompletedImageDispatch(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"draw"},{"type":"function_call","namespace":"image_gen","name":"imagegen","call_id":"call_a"},{"type":"function_call_output","call_id":"call_a","output":[{"type":"input_image","image_url":"data:image/png;base64,YQ=="},{"type":"input_text","text":"Saved to /tmp/generated_images/a.png"}]}]}`)
	for _, stream := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Set(service.TokenProTextImageDeliveryContextKey, true)
		started := false
		h := &OpenAIGatewayHandler{}
		handled := h.dispatchTokenProCompletedImage(c, "gpt-5.6-sol", body, stream, &started)
		require.True(t, handled)
		require.Equal(t, "text-v1", rec.Header().Get("X-TokenPro-Image-Completion"))
		require.Contains(t, rec.Body.String(), "图片生成好了")
		require.Contains(t, rec.Body.String(), `"total_tokens":0`)
		require.Equal(t, stream, started)
	}
}

func TestTokenProCompletedImageDispatchRequiresDeliveryBinding(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":"draw"},{"type":"function_call","namespace":"image_gen","name":"imagegen","call_id":"call_a"},{"type":"function_call_output","call_id":"call_a","output":[{"type":"input_image","image_url":"data:image/png;base64,YQ=="}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	started := false
	h := &OpenAIGatewayHandler{}
	require.False(t, h.dispatchTokenProCompletedImage(c, "gpt-5.6-sol", body, false, &started))
	require.Empty(t, rec.Body.String())
}

func TestTokenProPureDispatchGroupDescriptionField(t *testing.T) {
	for _, tc := range []struct {
		name, platform, groupName, description string
		missingGroup, want                     bool
	}{
		{name: "description-field", platform: service.PlatformOpenAI, groupName: "images-65", description: "生图", want: true},
		{name: "trim-description", platform: service.PlatformOpenAI, description: " 生图\n", want: true},
		{name: "name-is-not-description", platform: service.PlatformOpenAI, groupName: "生图"},
		{name: "not-substring-match", platform: service.PlatformOpenAI, description: "不支持生图"},
		{name: "other-description", platform: service.PlatformOpenAI, description: "文本"},
		{name: "other-platform", platform: service.PlatformAnthropic, description: "生图"},
		{name: "missing-group", missingGroup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			// Client-provided labels must never override the resolved backend group.
			c.Request.Header.Set("X-TokenPro-Group-Description", "生图")
			c.Set(service.TokenProNativeImagesContextKey, true)
			key := &service.APIKey{KeyType: service.APIKeyTypeGlobal}
			if !tc.missingGroup {
				key.Group = &service.Group{Platform: tc.platform, Name: tc.groupName, Description: tc.description}
			}
			started := false
			h := &OpenAIGatewayHandler{}
			handled := h.dispatchTokenProNativeImage(c, key, "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(`{"input":"draw a cat"}`), false, &started)
			require.Equal(t, tc.want, handled)
			if tc.want {
				require.Equal(t, "v1", rec.Header().Get("X-TokenPro-Native-Dispatch"))
			} else {
				require.Empty(t, rec.Body.String())
				require.Empty(t, rec.Header().Get("X-TokenPro-Native-Dispatch"))
			}
		})
	}
}
