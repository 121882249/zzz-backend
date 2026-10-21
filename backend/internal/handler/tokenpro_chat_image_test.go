package handler

import (
	"context"
	"errors"
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

func TestTokenProChatImageDownloadsThroughExecCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	mem := &tokenProLocalImageMem{}
	c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{
		Store:    mem,
		ThreadID: "11111111-1111-4111-8111-111111111111",
	})
	var billed atomic.Int32
	var generated atomic.Int32
	h := &OpenAIGatewayHandler{}
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		generated.Add(1)
		return &tokenProChatImageGenerated{
			Bytes: []byte{0x89, 'P', 'N', 'G'}, ContentType: "image/png",
			Result:  &service.OpenAIForwardResult{ImageCount: 1, Model: "gpt-image-2.5-flare"},
			Account: &service.Account{ID: 8},
		}, nil
	}
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) {
		return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
	}
	h.chatImageRecordUsage = func(*service.OpenAIRecordUsageInput) { billed.Add(1) }
	body := []byte(`{"input":"画一只猫","stream":true,"tools":[{"type":"function","name":"exec_command","description":"run a command"},{"type":"function","name":"apply_patch"}]}`)
	started := false
	handled := h.dispatchTokenProNativeImage(c, tokenProLocalChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, body, true, &started)
	require.True(t, handled)
	require.Equal(t, int32(1), generated.Load())
	require.Equal(t, int32(1), billed.Load())
	require.Equal(t, "local-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	require.Empty(t, rec.Header().Get("X-TokenPro-Native-Dispatch"))
	events := parseTokenProSSE(t, rec.Body.String())
	var done gjson.Result
	for _, event := range events {
		if event.Get("type").String() == "response.function_call_arguments.done" {
			done = event
		}
	}
	require.Equal(t, "exec_command", done.Get("name").String())
	cmd := gjson.Parse(done.Get("arguments").String()).Get("cmd").String()
	require.Contains(t, cmd, "curl -fsSL")
	require.Contains(t, cmd, "tokenpro_image_url=https://tokenpro.work/cat.png?exp=1&sig=ff")
	require.Contains(t, cmd, ".codex/generated_images/tokenpro/11111111-1111-4111-8111-111111111111/")
	require.True(t, strings.HasPrefix(done.Get("call_id").String(), "call_tp_local_"))
	completed := events[len(events)-1]
	require.Equal(t, "function_call", completed.Get("response.output.0.type").String())
	require.NotContains(t, rec.Body.String(), "图片已生成")
	require.Len(t, mem.records, 1)
	for _, record := range mem.records {
		require.Equal(t, "https://tokenpro.work/cat.png?exp=1&sig=ff", record.URL)
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{Store: &tokenProLocalImageMem{errRemember: errors.New("redis down")}})
	generated.Store(0)
	billed.Store(0)
	handled = h.dispatchTokenProNativeImage(c, tokenProLocalChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, body, false, &started)
	require.True(t, handled)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "local-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	require.Equal(t, "function_call", gjson.Get(rec.Body.String(), "output.0.type").String())
	require.Equal(t, int32(1), billed.Load())
}

func TestTokenProChatImageFollowUpRendersLocalMarkdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := "/Users/me/.codex/generated_images/tokenpro/thread/abc.png"
	body := []byte(`{"input":[
		{"role":"user","content":"画一只猫"},
		{"type":"function_call","name":"exec_command","call_id":"call_tp_local_abc","arguments":"tokenpro_image_url=https://tokenpro.work/cat.png"},
		{"type":"function_call_output","call_id":"call_tp_local_abc","output":"TOKENPRO_IMAGE_PATH ` + path + `"}
	],"tools":[{"type":"function","name":"exec_command"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	var generated atomic.Int32
	var billed atomic.Int32
	h := &OpenAIGatewayHandler{}
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		generated.Add(1)
		return nil, nil
	}
	h.chatImageRecordUsage = func(*service.OpenAIRecordUsageInput) { billed.Add(1) }
	started := false
	handled := h.dispatchTokenProNativeImage(c, tokenProLocalChatImageKey(), "gpt-image-2.5-flare", service.PlatformOpenAI, body, true, &started)
	require.True(t, handled)
	require.Equal(t, int32(0), generated.Load())
	require.Equal(t, int32(0), billed.Load())
	require.Equal(t, "local-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	text := ""
	for _, event := range parseTokenProSSE(t, rec.Body.String()) {
		if event.Get("type").String() == "response.output_item.done" {
			text = event.Get("item.content.0.text").String()
		}
	}
	require.Equal(t, "![]("+path+")\n图片已保存，可在对话里直接查看。", text)

	failed := []byte(`{"input":[
		{"role":"user","content":"画一只猫"},
		{"type":"function_call","name":"exec_command","call_id":"call_tp_local_abc","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_tp_local_abc","output":"TOKENPRO_IMAGE_FAIL"}
	]}`)
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	key := tokenProLocalChatImageKey()
	mem := &tokenProLocalImageMem{records: map[string]service.TokenProLocalImageRecord{
		service.TokenProLocalImageKey(key, "call_tp_local_abc"): {URL: "https://tokenpro.work/cat.png"},
	}}
	c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{Store: mem})
	handled = h.dispatchTokenProNativeImage(c, key, "gpt-image-2.5-flare", service.PlatformOpenAI, failed, false, &started)
	require.True(t, handled)
	require.Equal(t, int32(0), generated.Load())
	require.Equal(t, int32(0), billed.Load())
	require.Equal(t, "url-v1", rec.Header().Get("X-TokenPro-Chat-Image"))
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())

	again := []byte(`{"input":[
		{"role":"user","content":"画一只猫"},
		{"type":"function_call_output","call_id":"call_tp_local_abc","output":"TOKENPRO_IMAGE_PATH ` + path + `"},
		{"role":"user","content":"再画一张"}
	]}`)
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(service.TokenProNativeImagesContextKey, true)
	h.chatImagePublish = func(context.Context, string, []byte) (string, error) { return "https://tokenpro.work/next.png", nil }
	h.chatImageGenerate = func(context.Context, *gin.Context, *service.APIKey, string, *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
		generated.Add(1)
		return &tokenProChatImageGenerated{Bytes: []byte("img"), ContentType: "image/png", Result: &service.OpenAIForwardResult{ImageCount: 1}, Account: &service.Account{ID: 1}}, nil
	}
	handled = h.dispatchTokenProNativeImage(c, key, "gpt-image-2.5-flare", service.PlatformOpenAI, again, false, &started)
	require.True(t, handled)
	require.Equal(t, int32(1), generated.Load())
	require.Equal(t, int32(1), billed.Load())
	require.Equal(t, "图片已生成：https://tokenpro.work/next.png", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}

type tokenProLocalImageMem struct {
	records     map[string]service.TokenProLocalImageRecord
	errRemember error
}

func (m *tokenProLocalImageMem) Bind(context.Context, string, service.TokenProImageTurnRoute) error {
	return nil
}

func (m *tokenProLocalImageMem) Lookup(context.Context, string) (service.TokenProImageTurnRoute, error) {
	return service.TokenProImageTurnRoute{}, service.ErrImageTurnMissing
}

func (m *tokenProLocalImageMem) RememberLocalImage(_ context.Context, key string, record service.TokenProLocalImageRecord) error {
	if m.errRemember != nil {
		return m.errRemember
	}
	if m.records == nil {
		m.records = map[string]service.TokenProLocalImageRecord{}
	}
	m.records[key] = record
	return nil
}

func (m *tokenProLocalImageMem) LookupLocalImage(_ context.Context, key string) (service.TokenProLocalImageRecord, error) {
	record, ok := m.records[key]
	if !ok {
		return service.TokenProLocalImageRecord{}, service.ErrImageTurnMissing
	}
	return record, nil
}

func tokenProLocalChatImageKey() *service.APIKey {
	key := tokenProChatImageKey()
	key.ID = 9
	key.UserID = 7
	key.Key = "global-key"
	return key
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
