//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type openAIImagesFixedHTTPUpstream struct {
	service.HTTPUpstream
	contentType string
	body        string
}

func (u *openAIImagesFixedHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{u.contentType}},
		Body:       io.NopCloser(bytes.NewBufferString(u.body)),
	}, nil
}

func runImagesNoBillCase(t *testing.T, upstream *openAIImagesFixedHTTPUpstream, body string) *observer.ObservedLogs {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(4242)
	accounts := []service.Account{{
		ID: 7, Name: "img-apikey", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://upstream.invalid"},
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: accounts},
		nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	h := NewOpenAIGatewayHandler(gatewayService, service.NewConcurrencyService(nil), billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 1

	core, logs := observer.New(zap.DebugLevel)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body)).
		WithContext(logger.IntoContext(context.Background(), zap.New(core)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID: 99, GroupID: &groupID,
		Group: &service.Group{ID: groupID, AllowImageGeneration: true},
		User:  &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100})
	h.Images(c)
	return logs
}

// Upstream answered 200 but delivered no image: the request must not be
// billed (previously it was billed as parsed.N images).
func TestOpenAIImages_SuccessStatusWithoutImagesIsNotBilled(t *testing.T) {
	logs := runImagesNoBillCase(t, &openAIImagesFixedHTTPUpstream{
		contentType: "application/json",
		body:        `{"created":1,"data":[]}`,
	}, `{"model":"gpt-image-1","prompt":"cat","n":2}`)
	require.NotEmpty(t, logs.FilterMessage("openai.images.success_without_images_not_billed").All())
	require.Empty(t, logs.FilterMessage("openai.images.request_completed").All())
}

func TestOpenAIImages_StreamErrorEventWithoutImagesIsNotBilled(t *testing.T) {
	logs := runImagesNoBillCase(t, &openAIImagesFixedHTTPUpstream{
		contentType: "text/event-stream",
		body:        "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"rejected\"}}\n\n",
	}, `{"model":"gpt-image-1","prompt":"cat","stream":true}`)
	require.Empty(t, logs.FilterMessage("openai.images.request_completed").All())
}

func TestOpenAIImagesResultBillable(t *testing.T) {
	require.False(t, openAIImagesResultBillable(nil))
	require.False(t, openAIImagesResultBillable(&service.OpenAIForwardResult{}))
	require.True(t, openAIImagesResultBillable(&service.OpenAIForwardResult{ImageCount: 1}))
}
