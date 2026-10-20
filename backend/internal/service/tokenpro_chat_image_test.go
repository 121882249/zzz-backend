package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractTokenProChatImagePromptAndEdit(t *testing.T) {
	t.Parallel()
	generated, err := ExtractTokenProChatImage([]byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"## My request:\n猫"}]}]}`))
	require.NoError(t, err)
	require.Equal(t, "猫", generated.Prompt)
	require.False(t, generated.Edit)

	bare, err := ExtractTokenProChatImage([]byte(`{"input":"猫"}`))
	require.NoError(t, err)
	require.Equal(t, "猫", bare.Prompt)
	require.False(t, bare.Edit)

	edited, err := ExtractTokenProChatImage([]byte(`{"input":[{"role":"user","content":"old"},{"role":"assistant","content":[{"type":"output_text","text":"图片已生成：https://tokenpro.work/v1/tokenpro/images/old"}]},{"role":"user","content":[{"type":"input_text","text":"## My request:\n带上墨镜\n"},{"type":"input_image","image_url":{"url":"https://cdn.example/cat.png"}}]}]}`))
	require.NoError(t, err)
	require.Equal(t, "带上墨镜", edited.Prompt)
	require.True(t, edited.Edit)
	require.Equal(t, []string{"https://cdn.example/cat.png"}, edited.ImageURLs)

	prior, err := ExtractTokenProChatImage([]byte(`{"input":[{"role":"assistant","content":[{"type":"output_text","text":"图片已生成：https://tokenpro.work/old.png"}]},{"role":"user","content":[{"type":"input_text","text":"<image path=\"/tmp/local.png\">\n换背景"},{"type":"input_image"}]}]}`))
	require.NoError(t, err)
	require.True(t, prior.Edit)
	require.Equal(t, []string{"https://tokenpro.work/old.png"}, prior.ImageURLs)

	fresh, err := ExtractTokenProChatImage([]byte(`{"input":[{"role":"assistant","content":[{"type":"output_text","text":"图片已生成：https://tokenpro.work/old.png"}]},{"role":"user","content":"再画一只"}]}`))
	require.NoError(t, err)
	require.False(t, fresh.Edit)
	require.Empty(t, fresh.ImageURLs)
}

func TestExtractTokenProChatImageRejectsMissingSource(t *testing.T) {
	t.Parallel()
	_, err := ExtractTokenProChatImage([]byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"<image path=\"/tmp/local.png\">\n换背景"},{"type":"input_image"}]}]}`))
	require.ErrorIs(t, err, ErrTokenProChatImageSource)
	_, err = ExtractTokenProChatImage([]byte(`{"input":" "}`))
	require.ErrorIs(t, err, ErrTokenProChatImagePrompt)
	_, err = ExtractTokenProChatImage([]byte(`{"input":"猫","previous_response_id":"resp_old"}`))
	require.ErrorIs(t, err, ErrTokenProChatImageHistory)
	_, err = ExtractTokenProChatImage([]byte(`{`))
	require.ErrorIs(t, err, ErrTokenProChatImageInvalidJSON)
}

func TestTokenProExplicitNativeImageFlow(t *testing.T) {
	t.Parallel()
	require.True(t, TokenProExplicitNativeImageFlow([]byte(`{"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`)))
	require.True(t, TokenProExplicitNativeImageFlow([]byte(`{"input":[{"type":"function_call","call_id":"call_tp_pure_abc","name":"imagegen"}]}`)))
	require.True(t, TokenProExplicitNativeImageFlow([]byte(`{"input":[{"type":"custom_tool_call","call_id":"call_tp_receipt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)))
	require.False(t, TokenProExplicitNativeImageFlow([]byte(`{"input":"猫","tools":[{"type":"function","name":"exec"}]}`)))
}

func TestTokenProChatImageMessagesAndErrorMapping(t *testing.T) {
	t.Parallel()
	text := TokenProChatImageSuccessText("https://tokenpro.work/cat.png", strings.Repeat("描述", 80))
	require.Contains(t, text, "图片已生成：https://tokenpro.work/cat.png")
	require.Contains(t, text, "调整后的描述：")
	require.NotContains(t, text, "data:")
	require.LessOrEqual(t, len([]rune(strings.TrimPrefix(strings.Split(text, "调整后的描述：")[1], ""))), 120)

	require.Equal(t, "这次请求未通过检查，请调整图片描述后再试。", TokenProChatImageFailureText(errString("content policy violation")))
	require.Equal(t, "当前没有可用的生图账号，请稍后再试。", TokenProChatImageFailureText(ErrTokenProChatImageNoAccount))
	require.Equal(t, "图片未能正常返回，请重新生成。", TokenProChatImageFailureText(ErrTokenProChatImageEmpty))
	status, message, ok := TokenProChatImageClientError(ErrTokenProChatImagePrompt)
	require.True(t, ok)
	require.Equal(t, 400, status)
	require.Equal(t, "请先描述要生成的图片。", message)
	status, message, ok = TokenProChatImageClientError(ErrImageTurnConflict)
	require.True(t, ok)
	require.Equal(t, 409, status)
	require.Contains(t, message, "已经完成")
}

func TestLogTokenProChatImageUpstreamErrorSanitizes(t *testing.T) {
	var got string
	previous := tokenProChatImageUpstreamLogHook
	tokenProChatImageUpstreamLogHook = func(detail string) { got = detail }
	t.Cleanup(func() { tokenProChatImageUpstreamLogHook = previous })
	long := strings.Repeat("x", 250)
	LogTokenProChatImageUpstreamError(errString("boom https://up.example/v1/images?access_token=secret&next=1 " + long))
	require.NotContains(t, got, "secret")
	require.Contains(t, got, "access_token=***")
	require.LessOrEqual(t, len([]rune(got)), 200)
}

func TestBuildTokenProChatImagesPayloadParses(t *testing.T) {
	endpoint, body, err := BuildTokenProChatImagesPayload("gpt-image-2.5-flare", &TokenProChatImageRequest{
		Prompt: "带上墨镜", Edit: true, ImageURLs: []string{"https://cdn.example/cat.png"},
	})
	require.NoError(t, err)
	require.Equal(t, "/v1/images/edits", endpoint)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	parsed, err := (&OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.Equal(t, "带上墨镜", parsed.Prompt)
	require.Equal(t, []string{"https://cdn.example/cat.png"}, parsed.InputImageURLs)
	require.False(t, parsed.Stream)
	require.Equal(t, "b64_json", parsed.ResponseFormat)
	require.True(t, parsed.IsEdits())
}

func TestBeginTokenProChatImageReceipt(t *testing.T) {
	store := &fakeChatImageCallStore{}
	finish, err := BeginTokenProChatImageReceipt(context.Background(), store, "turn", []byte("body"), "猫", false)
	require.NoError(t, err)
	require.Equal(t, "running", store.call.State)
	require.NoError(t, finish(true))
	require.Equal(t, "succeeded", store.call.State)

	store.prepareErr = ErrImageTurnConflict
	_, err = BeginTokenProChatImageReceipt(context.Background(), store, "turn", []byte("body"), "猫", true)
	require.ErrorIs(t, err, ErrImageTurnConflict)
	finish, err = BeginTokenProChatImageReceipt(context.Background(), nil, "", nil, "", false)
	require.NoError(t, err)
	require.Nil(t, finish)
}

type errString string

func (e errString) Error() string { return string(e) }

type fakeChatImageCallStore struct {
	call       TokenProImageCall
	prepareErr error
}

func (f *fakeChatImageCallStore) PrepareCall(_ context.Context, _ string, call TokenProImageCall) (TokenProImageCall, error) {
	if f.prepareErr != nil {
		return call, f.prepareErr
	}
	call.State = "pending"
	f.call = call
	return call, nil
}

func (f *fakeChatImageCallStore) ClaimCall(_ context.Context, _ string, promptHash, action string) (string, error) {
	if f.call.State != "pending" || f.call.PromptHash != promptHash || f.call.Action != action {
		return "", ErrImageTurnConflict
	}
	f.call.State = "running"
	return f.call.CallID, nil
}

func (f *fakeChatImageCallStore) FinishCall(_ context.Context, _ string, callID string, success bool) error {
	if f.call.CallID != callID || f.call.State != "running" {
		return ErrImageTurnConflict
	}
	f.call.State = "failed"
	if success {
		f.call.State = "succeeded"
	}
	return nil
}

func (f *fakeChatImageCallStore) CallResult(context.Context, string, string) (string, error) {
	return f.call.State, nil
}

func TestTokenProChatImageURLSignExpiry(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	store := NewTokenProChatImageFiles(TokenProChatImageFileConfig{
		Secret: "test-secret", PublicBase: "https://tokenpro.work", TTL: time.Hour, Now: func() time.Time { return now },
	})
	require.Nil(t, NewTokenProChatImageFiles(TokenProChatImageFileConfig{PublicBase: "http://tokenpro.work", Secret: "x"}))
	imageURL, err := store.Save("image/png", []byte{0x89, 'P', 'N', 'G'})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(imageURL, "https://tokenpro.work/v1/tokenpro/images/"))
	id, exp, sig := splitChatImageURL(t, imageURL)
	contentType, data, err := store.Open(id, exp, sig)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, []byte{0x89, 'P', 'N', 'G'}, data)
	_, _, err = store.Open(id, exp, sig+"00")
	require.ErrorIs(t, err, ErrTokenProChatImageDenied)
	store.SetNow(now.Add(2 * time.Hour))
	_, _, err = store.Open(id, exp, sig)
	require.ErrorIs(t, err, ErrTokenProChatImageExpired)
	_, _, err = store.Open(strings.Repeat("ab", 32), exp, sig)
	require.ErrorIs(t, err, ErrTokenProChatImageDenied)
}

func TestTokenProChatImageURLDiskSurvivesNewStore(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	cfg := TokenProChatImageFileConfig{Dir: dir, Secret: "disk-secret", PublicBase: "https://tokenpro.work", TTL: 48 * time.Hour, Now: func() time.Time { return now }}
	store := NewTokenProChatImageFiles(cfg)
	imageURL, err := store.Save("image/jpeg", []byte{0xff, 0xd8, 0xff})
	require.NoError(t, err)
	reopened := NewTokenProChatImageFiles(cfg)
	id, exp, sig := splitChatImageURL(t, imageURL)
	contentType, data, err := reopened.Open(id, exp, sig)
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", contentType)
	require.Equal(t, []byte{0xff, 0xd8, 0xff}, data)
}

func splitChatImageURL(t *testing.T, raw string) (string, string, string) {
	t.Helper()
	require.NotContains(t, raw, "data:")
	parts := strings.Split(raw, "/")
	tail := parts[len(parts)-1]
	id, query, ok := strings.Cut(tail, "?")
	require.True(t, ok)
	values := map[string]string{}
	for _, pair := range strings.Split(query, "&") {
		key, value, _ := strings.Cut(pair, "=")
		values[key] = value
	}
	return id, values["exp"], values["sig"]
}
