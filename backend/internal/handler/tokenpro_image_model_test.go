package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageSelectionAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r imageSelectionAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrAccountNotFound
}

func (r imageSelectionAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return append([]service.Account(nil), r.accounts...), nil
}

func (r imageSelectionAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r imageSelectionAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r imageSelectionAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.byPlatform(platform), nil
}

func (r imageSelectionAccountRepo) byPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

type imageSelectionGroupRepo struct {
	service.GroupRepository
	group *service.Group
	err   error
}

func (r imageSelectionGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.group == nil || r.group.ID != id {
		return nil, service.ErrGroupNotFound
	}
	copied := *r.group
	return &copied, nil
}

func (r imageSelectionGroupRepo) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	return r.GetByID(ctx, id)
}

type imageSelectionUpstream struct {
	service.HTTPUpstream
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (u *imageSelectionUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(context.Background())
	u.mu.Lock()
	u.requests = append(u.requests, cloned)
	u.bodies = append(u.bodies, string(body))
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`)),
	}, nil
}

type imageSelectionUsageRepo struct {
	service.UsageLogRepository
	mu     sync.Mutex
	models []string
}

func (r *imageSelectionUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if log != nil {
		r.models = append(r.models, log.Model)
	}
	return true, nil
}

func imageSelectionAccount(mapping map[string]any) service.Account {
	return service.Account{
		ID: 40, Name: "image-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"api_key":       "fixture",
			"base_url":      "https://images.example/v1",
			"model_mapping": mapping,
		},
	}
}

func imageSelectionGroup(description string) *service.Group {
	return &service.Group{
		ID: 65, Platform: service.PlatformOpenAI, Description: description,
		Status: service.StatusActive, AllowImageGeneration: true,
	}
}

func newImageSelectionHandler(t *testing.T, accounts []service.Account, group *service.Group, groupErr error) (*OpenAIGatewayHandler, *imageSelectionUpstream, *imageSelectionUsageRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := imageSelectionAccountRepo{accounts: accounts}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	upstream := &imageSelectionUpstream{}
	usage := &imageSelectionUsageRepo{}
	billingService := service.NewBillingService(cfg, nil)
	deferred := service.NewDeferredService(repo, nil, time.Minute)
	gateway := service.NewOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg, nil, nil, billingService, nil, nil, upstream, deferred, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	resolver := service.NewGatewayService(repo, imageSelectionGroupRepo{group: group, err: groupErr},
		nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.SetGlobalGroupResolver(resolver)
	return handler, upstream, usage
}

func postImageSelection(handler *OpenAIGatewayHandler, endpoint, body, imageModel, groupID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if groupID != "" {
		c.Request.Header.Set(tokenProGroupIDHeader, groupID)
	}
	if imageModel != "" {
		c.Request.Header.Set(tokenProImageModelHeader, imageModel)
	}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID: 99, UserID: 100, Key: "fixture-key", KeyType: service.APIKeyTypeGlobal,
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	handler.Images(c)
	return rec
}

func TestTokenProImageModelSelectionRewritesBeforeScheduleAndBilling(t *testing.T) {
	flareOnly := map[string]any{"gpt-image-2.5-flare": "gpt-image-2.5-flare"}
	for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
		t.Run(endpoint, func(t *testing.T) {
			handler, upstream, usage := newImageSelectionHandler(t, []service.Account{imageSelectionAccount(flareOnly)}, imageSelectionGroup("生图"), nil)
			body := `{"model":"gpt-image-2","prompt":"draw a cat","n":1}`
			if endpoint == "/v1/images/edits" {
				body = `{"model":"gpt-image-2","prompt":"make it blue","images":[{"image_url":"data:image/png;base64,aGVsbG8="}]}`
			}
			rec := postImageSelection(handler, endpoint, body, "GPT-Image-2.5-Flare", "65")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, upstream.bodies, 1)
			require.Equal(t, "gpt-image-2.5-flare", gjson.Get(upstream.bodies[0], "model").String())
			require.NotContains(t, upstream.bodies[0], "gpt-image-2\"")
			require.Empty(t, upstream.requests[0].Header.Get("X-TokenPro-Image-Model"))
			require.Empty(t, upstream.requests[0].Header.Get("X-TokenPro-Group-Id"))
			require.Equal(t, []string{"gpt-image-2.5-flare"}, usage.models)
		})
	}
}

func TestTokenProImageModelSelectionAbsentHeaderUnchanged(t *testing.T) {
	handler, upstream, usage := newImageSelectionHandler(t,
		[]service.Account{imageSelectionAccount(map[string]any{"gpt-image-2": "gpt-image-2"})},
		imageSelectionGroup("生图"), nil)
	rec := postImageSelection(handler, "/v1/images/generations", `{"model":"gpt-image-2","prompt":"draw a cat"}`, "", "65")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "gpt-image-2", gjson.Get(upstream.bodies[0], "model").String())
	require.Equal(t, []string{"gpt-image-2"}, usage.models)
}

func TestTokenProImageModelSelectionRejectsInvalidSelection(t *testing.T) {
	mapped := map[string]any{
		"gpt-image-1": "gpt-image-1", "gpt-image-1.5": "gpt-image-1.5", "gpt-image-2": "gpt-image-2",
		"gpt-image-2.5-flare": "gpt-image-2.5-flare", "gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst",
	}
	body := `{"model":"gpt-image-2","prompt":"draw a cat"}`
	cases := []struct {
		name     string
		group    *service.Group
		groupErr error
		header   string
		groupID  string
		want     string
		wantCode int
	}{
		{name: "text model", group: imageSelectionGroup("生图"), header: "gpt-5.4", groupID: "65", want: "指定的模型不是生图模型", wantCode: 400},
		{name: "unmapped", group: imageSelectionGroup("生图"), header: "gpt-image-9", groupID: "65", want: "该生图分组不支持这个图片模型", wantCode: 400},
		{name: "not image group", group: imageSelectionGroup("对话"), header: "gpt-image-2.5-flare", groupID: "65", want: "当前分组不是生图分组", wantCode: 400},
		{name: "missing group", group: imageSelectionGroup("生图"), header: "gpt-image-2.5-flare", want: "生图请求缺少有效分组", wantCode: 400},
		{name: "database", groupErr: errImageSelectionDB, header: "gpt-image-2.5-flare", groupID: "65", want: "无法确认生图模型", wantCode: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, upstream, _ := newImageSelectionHandler(t, []service.Account{imageSelectionAccount(mapped)}, tc.group, tc.groupErr)
			rec := postImageSelection(handler, "/v1/images/generations", body, tc.header, tc.groupID)
			require.Equal(t, tc.wantCode, rec.Code, rec.Body.String())
			require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), tc.want)
			require.Contains(t, rec.Body.String(), "没有改用其他模型")
			require.Empty(t, upstream.bodies)
		})
	}
}

var errImageSelectionDB = imageSelectionError("db down")

type imageSelectionError string

func (e imageSelectionError) Error() string { return string(e) }

func TestTokenProImageModelSelectionAcceptsContainedDescription(t *testing.T) {
	handler, upstream, usage := newImageSelectionHandler(t,
		[]service.Account{imageSelectionAccount(map[string]any{"gpt-image-2.5-sunburst": "gpt-image-2.5-sunburst"})},
		imageSelectionGroup("高速 生图"), nil)
	rec := postImageSelection(handler, "/v1/images/edits",
		`{"model":"gpt-image-2","prompt":"make it blue","images":[{"image_url":"data:image/png;base64,aGVsbG8="}]}`,
		"gpt-image-2.5-sunburst", "65")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "gpt-image-2.5-sunburst", gjson.Get(upstream.bodies[0], "model").String())
	require.Equal(t, []string{"gpt-image-2.5-sunburst"}, usage.models)
}
