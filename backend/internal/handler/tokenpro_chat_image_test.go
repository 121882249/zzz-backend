package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProChatImageSSEAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"猫","stream":true}`))
	c.Set(service.TokenProNativeImagesContextKey, true)
	var billed atomic.Int32
	h := &OpenAIGatewayHandler{chatImageKeepalive: 5 * time.Millisecond}
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		time.Sleep(40 * time.Millisecond)
		return &tokenProChatImageGenerated{
			Bytes: []byte{0x89, 'P', 'N', 'G'}, ContentType: "image/png", Revised: "一只猫",
			Result:  &service.OpenAIForwardResult{ImageCount: 1, Model: "gpt-image-2.5-flare", Usage: service.OpenAIUsage{InputTokens: 3, OutputTokens: 4}},
			Account: &service.Account{ID: 8},
		}, nil
	}
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) {
		return "https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff", nil
	}
	h.chatImageRecordUsage = func(input *service.OpenAIRecordUsageInput) {
		require.Equal(t, 1, input.Result.ImageCount)
		require.Equal(t, int64(8), input.Account.ID)
		billed.Add(1)
	}
	started := false
	handled := h.dispatchTokenProNativeImage(c, tokenProChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(`{"input":"猫","stream":true}`), true, &started)
	require.True(t, handled)
	require.True(t, started)
	require.Equal(t, "url-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	require.Empty(t, rec.Header().Get("X-TokenPro-Native-Dispatch"))
	require.Equal(t, int32(1), billed.Load())
	events := parseTokenProSSE(t, rec.Body.String())
	require.GreaterOrEqual(t, len(events), 5)
	require.Equal(t, "response.created", events[0].Get("type").String())
	require.Equal(t, "response.in_progress", events[1].Get("type").String())
	index := 1
	for index < len(events) && events[index].Get("type").String() == "response.in_progress" {
		index++
	}
	require.GreaterOrEqual(t, index-1, 2)
	require.Equal(t, "response.output_item.added", events[index].Get("type").String())
	require.Equal(t, "response.output_item.done", events[index+1].Get("type").String())
	require.Equal(t, "response.completed", events[len(events)-1].Get("type").String())
	require.Equal(t, events[0].Get("response.id").String(), events[len(events)-1].Get("response.id").String())
	text := events[index+1].Get("item.content.0.text").String()
	require.Contains(t, text, "图片已生成：https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff")
	require.Contains(t, text, "一只猫")
	require.NotContains(t, text, "data:")
	require.NotContains(t, rec.Body.String(), "\n: ")
	completed := events[len(events)-1].Get("response")
	require.Equal(t, int64(3), completed.Get("usage.input_tokens").Int())
	require.Equal(t, int64(4), completed.Get("usage.output_tokens").Int())
	require.Equal(t, int64(7), completed.Get("usage.total_tokens").Int())
	require.False(t, completed.Get("usage.output_tokens_details").Exists())
}

func TestTokenProChatImageNonStreamAndNativeV1Unchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	h := &OpenAIGatewayHandler{}
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		t.Fatal("native image tool request must not generate on the server")
		return nil, nil
	}
	started := false
	body := []byte(`{"input":"猫","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`)
	handled := h.dispatchTokenProNativeImage(c, tokenProChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, body, false, &started)
	require.True(t, handled)
	require.Equal(t, "v1", rec.Header().Get("X-TokenPro-Native-Dispatch"))
	require.Contains(t, rec.Body.String(), `"namespace":"image_gen"`)
	require.NotContains(t, rec.Body.String(), "图片已生成")

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	var billed atomic.Int32
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		return &tokenProChatImageGenerated{Bytes: []byte("img"), ContentType: "image/png", Result: &service.OpenAIForwardResult{ImageCount: 1}, Account: &service.Account{ID: 1}}, nil
	}
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) {
		return "https://tokenpro.work/cat.png", nil
	}
	h.chatImageRecordUsage = func(*service.OpenAIRecordUsageInput) { billed.Add(1) }
	handled = h.dispatchTokenProNativeImage(c, tokenProChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(`{"input":"猫"}`), false, &started)
	require.True(t, handled)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, int64(0), gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
	require.Equal(t, int32(1), billed.Load())
}

func TestTokenProChatImageErrorDoesNotBill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	var billed atomic.Int32
	h := &OpenAIGatewayHandler{}
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) { return "https://tokenpro.work/x.png", nil }
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		return nil, errString("upstream content policy https://up.example/images?access_token=secret")
	}
	h.chatImageRecordUsage = func(*service.OpenAIRecordUsageInput) { billed.Add(1) }
	started := false
	handled := h.dispatchTokenProNativeImage(c, tokenProChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(`{"input":"猫","stream":true}`), true, &started)
	started = false
	require.True(t, handled)
	require.Equal(t, int32(0), billed.Load())
	require.Contains(t, rec.Body.String(), "这次请求未通过检查，请调整图片描述后再试。")
	require.Contains(t, rec.Body.String(), `"type":"response.completed"`)
	require.NotContains(t, rec.Body.String(), "secret")
	require.NotContains(t, rec.Body.String(), "access_token")

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	h = &OpenAIGatewayHandler{}
	handled = h.dispatchTokenProNativeImage(c, tokenProChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(`{"input":" "}`), false, &started)
	require.True(t, handled)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "请先描述要生成的图片。")
}

func TestTokenProChatImageDownloadExpiry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	h := &OpenAIGatewayHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		TokenProImagePublicBaseURL: "https://tokenpro.work",
		TokenProImageURLSecret:     "download-secret",
		TokenProImageStoreDir:      t.TempDir(),
		TokenProImageURLTTLHours:   1,
	}}}
	imageURL, err := h.publishTokenProChatImage(context.Background(), "image/png", []byte{0x89, 'P', 'N', 'G'})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, imageURL, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: imageID(imageURL)}}
	h.TokenProChatImageDownload(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, []byte{0x89, 'P', 'N', 'G'}, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	badURL := strings.Replace(imageURL, "sig=", "sig=00", 1)
	c.Request = httptest.NewRequest(http.MethodGet, badURL, nil)
	c.Params = gin.Params{{Key: "id", Value: imageID(imageURL)}}
	h.TokenProChatImageDownload(c)
	require.Equal(t, http.StatusNotFound, rec.Code)

	h.chatImageFiles.SetNow(now.Add(2 * time.Hour))
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, imageURL, nil)
	c.Params = gin.Params{{Key: "id", Value: imageID(imageURL)}}
	h.TokenProChatImageDownload(c)
	require.Equal(t, http.StatusGone, rec.Code)
	require.Contains(t, rec.Body.String(), "链接已过期")
}

func tokenProChatImageKey() *service.APIKey {
	return &service.APIKey{KeyType: service.APIKeyTypeGlobal, User: &service.User{ID: 7}, Group: &service.Group{Platform: service.PlatformOpenAI, Description: "生图"}}
}

func parseTokenProSSE(t *testing.T, body string) []gjson.Result {
	t.Helper()
	var events []gjson.Result
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		require.Truef(t, strings.HasPrefix(block, "data: "), "event %q", block)
		payload := strings.TrimPrefix(block, "data: ")
		require.True(t, gjson.Valid(payload))
		events = append(events, gjson.Parse(payload))
	}
	return events
}

func imageID(raw string) string {
	tail := raw[strings.LastIndex(raw, "/")+1:]
	id, _, _ := strings.Cut(tail, "?")
	return id
}

type errString string

func (e errString) Error() string { return string(e) }
