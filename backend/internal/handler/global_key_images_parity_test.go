//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
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

type globalImageTextChannelRepo struct {
	service.ChannelRepository
}

func (globalImageTextChannelRepo) ListAll(context.Context) ([]service.Channel, error) {
	return []service.Channel{{ID: 14, Status: service.StatusActive, GroupIDs: []int64{69}, RestrictModels: true,
		BillingModelSource: service.BillingModelSourceChannelMapped,
		ModelPricing:       []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"gpt-6.1-sol"}}}}}, nil
}

func (globalImageTextChannelRepo) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{69: service.PlatformOpenAI}, nil
}

type globalImageParityUpstream struct {
	service.HTTPUpstream
	requests []*http.Request
	bodies   []string
}

func (u *globalImageParityUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, string(body))
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"created":1,"data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":10,"output_tokens":20}}`))}, nil
}

func TestGlobalTextImagesReuseGroupKeyPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
			t.Run(accountType+endpoint, func(t *testing.T) {
				groupID := int64(16)
				credentials := map[string]any{"api_key": "fixture", "base_url": "https://configured-text.example/v1"}
				if accountType == service.AccountTypeOAuth {
					credentials = map[string]any{"access_token": "fixture"}
				}
				account := service.Account{ID: 1, Name: "text-group-account", Platform: service.PlatformOpenAI, Type: accountType,
					Status: service.StatusActive, Schedulable: true, Credentials: credentials}
				upstream := &globalImageParityUpstream{}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				gateway := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: []service.Account{account}},
					nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing,
					service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				body := `{"model":"gpt-image-2","prompt":"draw a cat","n":1}`
				if strings.HasSuffix(endpoint, "/edits") {
					body = `{"model":"gpt-image-2","prompt":"make it blue","images":[{"image_url":"data:image/png;base64,aGVsbG8="}]}`
				}
				var outputs []string
				for _, global := range []bool{false, true} {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
					c.Request.Header.Set("Content-Type", "application/json")
					key := &service.APIKey{ID: 99, UserID: 100, GroupID: &groupID,
						Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
						User:  &service.User{ID: 100}}
					if global {
						key.KeyType = service.APIKeyTypeGlobal
						// Simulate the existing authenticated ingress result. The
						// selection is for group authorization, not image forwarding.
						c.Set(tokenProGlobalGroupResolvedContextKey, true)
						c.Set(service.TokenProNativeImageDriverContextKey, "gpt-6-luna")
						c.Set(service.TokenProTextImageDeliveryContextKey, true)
					}
					c.Set(string(middleware2.ContextKeyAPIKey), key)
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
					h.Images(c)
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					require.Equal(t, "aGVsbG8=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
					outputs = append(outputs, rec.Body.String())
				}
				require.Len(t, upstream.requests, 2)
				require.Equal(t, upstream.requests[0].URL.String(), upstream.requests[1].URL.String())
				require.JSONEq(t, upstream.bodies[0], upstream.bodies[1], "global routing must not change the image payload")
				require.JSONEq(t, outputs[0], outputs[1])
				expected := "https://configured-text.example" + endpoint
				if accountType == service.AccountTypeOAuth {
					expected = "https://chatgpt.com/backend-api/codex" + strings.TrimPrefix(endpoint, "/v1")
				}
				require.Equal(t, expected, upstream.requests[1].URL.String())
				require.Equal(t, "gpt-image-2", gjson.Get(upstream.bodies[1], "model").String())
				require.NotContains(t, upstream.bodies[1], "gpt-6-luna")
				require.NotContains(t, upstream.requests[1].URL.Path, "/responses")
				require.Empty(t, upstream.requests[1].Header.Get("X-TokenPro-Group-Id"))
			})
		}
	}
}

func TestGlobalTextImagesRestrictedChannelKeepsOrdinaryForwarding(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeOAuth} {
		for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
			t.Run(accountType+endpoint, func(t *testing.T) {
				groupID := int64(69)
				credentials := map[string]any{"api_key": "fixture", "base_url": "https://configured-text.example/v1"}
				if accountType == service.AccountTypeOAuth {
					credentials = map[string]any{"access_token": "fixture"}
				}
				account := service.Account{ID: 47, Platform: service.PlatformOpenAI, Type: accountType, Status: service.StatusActive, Schedulable: true, Credentials: credentials}
				upstream := &globalImageParityUpstream{}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				channel := service.NewChannelService(globalImageTextChannelRepo{}, nil, nil, nil, nil)
				gateway := service.NewOpenAIGatewayService(openAIImagesFailoverAccountRepo{accounts: []service.Account{account}},
					nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, channel, nil, nil, nil)
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing,
					service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				body := `{"model":"gpt-image-2","prompt":"draw a cat","n":1}`
				if strings.HasSuffix(endpoint, "/edits") {
					body = `{"model":"gpt-image-2","prompt":"make it blue","images":[{"image_url":"data:image/png;base64,aGVsbG8="}]}`
				}
				for _, driver := range []string{"gpt-6.1-sol", "gpt-6-astra", ""} {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99, UserID: 100, GroupID: &groupID, KeyType: service.APIKeyTypeGlobal,
						Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true}, User: &service.User{ID: 100}})
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100})
					c.Set(tokenProGlobalGroupResolvedContextKey, true)
					c.Set(service.TokenProNativeImageDriverContextKey, driver)
					c.Set(service.TokenProTextImageDeliveryContextKey, driver != "")
					h.Images(c)
					if driver == "gpt-6.1-sol" {
						require.Equal(t, 200, rec.Code, rec.Body.String())
					} else {
						require.GreaterOrEqual(t, rec.Code, 400, "unlisted and absent driver must stay denied")
					}
				}
				require.Len(t, upstream.requests, 1)
				require.True(t, strings.HasSuffix(upstream.requests[0].URL.Path, strings.TrimPrefix(endpoint, "/v1")))
				require.Equal(t, "gpt-image-2", gjson.Get(upstream.bodies[0], "model").String())
				require.NotContains(t, upstream.bodies[0], "gpt-6.1-sol")
				require.NotContains(t, upstream.requests[0].URL.Path, "/responses")
			})
		}
	}
}
