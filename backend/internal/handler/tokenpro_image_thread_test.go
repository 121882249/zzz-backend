package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type imageThreadSpy struct {
	service.TokenProImageTurnStore
	threads                  service.TokenProImageThreadStore
	writer                   gin.ResponseWriter
	rememberedBeforeResponse bool
}

func (s *imageThreadSpy) RememberThread(ctx context.Context, key string, route service.TokenProImageTurnRoute) error {
	s.rememberedBeforeResponse = s.writer != nil && !s.writer.Written()
	return s.threads.RememberThread(ctx, key, route)
}

func (s *imageThreadSpy) LookupThread(ctx context.Context, key string) (service.TokenProImageTurnRoute, error) {
	return s.threads.LookupThread(ctx, key)
}

type imageThreadFailStore struct{}

func (imageThreadFailStore) Bind(context.Context, string, service.TokenProImageTurnRoute) error {
	return nil
}
func (imageThreadFailStore) Lookup(context.Context, string) (service.TokenProImageTurnRoute, error) {
	return service.TokenProImageTurnRoute{}, service.ErrImageTurnMissing
}
func (imageThreadFailStore) RememberThread(context.Context, string, service.TokenProImageTurnRoute) error {
	return errors.New("redis unavailable")
}
func (imageThreadFailStore) LookupThread(context.Context, string) (service.TokenProImageTurnRoute, error) {
	return service.TokenProImageTurnRoute{}, service.ErrImageTurnMissing
}

func TestImageFunctionCallStoresThreadBeforeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	base := repository.NewTokenProImageTurnStore(client)
	groupA, groupB := int64(65), int64(77)
	keyA := &service.APIKey{ID: 1, UserID: 9, Key: "key-a", KeyType: service.APIKeyTypeGlobal, GroupID: &groupA,
		Group: &service.Group{ID: 65, Platform: service.PlatformOpenAI, Description: "生图"}}
	keyB := &service.APIKey{ID: 2, UserID: 9, Key: "key-b", KeyType: service.APIKeyTypeGlobal, GroupID: &groupB,
		Group: &service.Group{ID: 77, Platform: service.PlatformOpenAI, Description: "生图"}}
	threadA, threadB := uuid.NewString(), uuid.NewString()
	h := &OpenAIGatewayHandler{}

	dispatch := func(key *service.APIKey, thread, model string, store service.TokenProImageTurnStore) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := `{"input":"draw a cat","client_metadata":{"turn_id":"` + uuid.NewString() + `","thread_id":"` + thread + `"},"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Set(service.TokenProNativeImagesContextKey, true)
		c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{Store: store, ThreadID: thread, TurnID: uuid.NewString()})
		started := false
		handled := h.dispatchTokenProNativeImage(c, key, model, service.PlatformOpenAI, []byte(body), true, &started)
		require.True(t, handled)
		return rec
	}

	spyA := &imageThreadSpy{TokenProImageTurnStore: base, threads: base.(service.TokenProImageThreadStore)}
	recA := httptest.NewRecorder()
	cA, _ := gin.CreateTestContext(recA)
	spyA.writer = cA.Writer
	bodyA := `{"input":"draw a cat","client_metadata":{"turn_id":"` + uuid.NewString() + `","thread_id":"` + threadA + `"},"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}]}`
	cA.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	cA.Set(service.TokenProNativeImagesContextKey, true)
	cA.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{Store: spyA, ThreadID: threadA, TurnID: uuid.NewString()})
	started := false
	require.True(t, h.dispatchTokenProNativeImage(cA, keyA, "gpt-image-2.5-flare", service.PlatformOpenAI, []byte(bodyA), true, &started))
	require.Equal(t, 200, recA.Code, recA.Body.String())
	require.True(t, spyA.rememberedBeforeResponse)
	require.Contains(t, recA.Body.String(), `"name":"imagegen"`)
	got, err := base.(service.TokenProImageThreadStore).LookupThread(context.Background(), service.TokenProImageThreadKey(keyA, threadA))
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-flare", got.Model)
	require.Equal(t, groupA, got.GroupID)

	recSwitch := dispatch(keyA, threadA, "gpt-image-2.5-sunburst", base)
	require.Equal(t, 200, recSwitch.Code, recSwitch.Body.String())
	got, err = base.(service.TokenProImageThreadStore).LookupThread(context.Background(), service.TokenProImageThreadKey(keyA, threadA))
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-sunburst", got.Model)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := dispatch(keyB, threadB, "gpt-image-1.5", base)
		require.Equal(t, 200, rec.Code, rec.Body.String())
	}()
	wg.Wait()
	other, err := base.(service.TokenProImageThreadStore).LookupThread(context.Background(), service.TokenProImageThreadKey(keyB, threadB))
	require.NoError(t, err)
	require.Equal(t, "gpt-image-1.5", other.Model)
	stillA, err := base.(service.TokenProImageThreadStore).LookupThread(context.Background(), service.TokenProImageThreadKey(keyA, threadA))
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2.5-sunburst", stillA.Model)

	failed := dispatch(keyA, uuid.NewString(), "gpt-image-2.5-flare", imageThreadFailStore{})
	require.Equal(t, http.StatusServiceUnavailable, failed.Code, failed.Body.String())
	require.Contains(t, failed.Body.String(), "无法记录本次对话的生图模型")
	require.NotContains(t, failed.Body.String(), `"name":"imagegen"`)
	require.Empty(t, failed.Header().Get("X-TokenPro-Native-Dispatch"))
}
