package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImageThreadBindingSwitchAndConcurrentThreads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := repository.NewTokenProImageTurnStore(client)
	threads := store.(service.TokenProImageThreadStore)
	key := &service.APIKey{ID: 1, UserID: 2, Key: "fixture", KeyType: service.APIKeyTypeGlobal}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
	r.Use(TokenProDirectRoute(store))
	r.POST("/v1/images/generations", func(c *gin.Context) {
		data, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{
			"model":  gjson.GetBytes(data, "model").String(),
			"group":  c.GetHeader(tokenProGroupIDHeader),
			"header": c.GetHeader("X-TokenPro-Image-Model"),
			"bound":  c.GetBool(service.TokenProImageThreadBoundContextKey),
		})
	})
	r.POST("/v1/images/edits", func(c *gin.Context) {
		data, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"model": gjson.GetBytes(data, "model").String(), "group": c.GetHeader(tokenProGroupIDHeader)})
	})

	threadA, threadB := uuid.NewString(), uuid.NewString()
	remember := func(thread, model string, group int64) {
		require.NoError(t, threads.RememberThread(context.Background(), service.TokenProImageThreadKey(key, thread), service.TokenProImageTurnRoute{
			GroupID: group, Model: model, ThreadID: thread,
		}))
	}
	call := func(path, thread, staleModel, staleGroup string) *httptest.ResponseRecorder {
		body := []byte(`{"model":"gpt-image-2","prompt":"same prompt"}`)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Codex-Turn-Metadata", `{"thread_id":"`+thread+`","turn_id":"`+uuid.NewString()+`"}`)
		req.Header.Set("X-Codex-Image-Turn-Id", uuid.NewString())
		if staleModel != "" {
			req.Header.Set("X-TokenPro-Image-Model", staleModel)
			req.Header.Set(tokenProGroupIDHeader, staleGroup)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	remember(threadA, "gpt-image-2.5-flare", 65)
	remember(threadB, "gpt-image-1.5", 77)
	first := call("/v1/images/generations", threadA, "gpt-image-2.5-sunburst", "66")
	require.Equal(t, 200, first.Code, first.Body.String())
	require.Equal(t, "gpt-image-2.5-flare", gjson.Get(first.Body.String(), "model").String())
	require.Equal(t, "65", gjson.Get(first.Body.String(), "group").String())
	require.Empty(t, gjson.Get(first.Body.String(), "header").String())
	require.True(t, gjson.Get(first.Body.String(), "bound").Bool())

	remember(threadA, "gpt-image-2.5-sunburst", 65)
	switched := call("/v1/images/edits", threadA, "gpt-image-2.5-flare", "65")
	require.Equal(t, 200, switched.Code, switched.Body.String())
	require.Equal(t, "gpt-image-2.5-sunburst", gjson.Get(switched.Body.String(), "model").String())
	require.Equal(t, "65", gjson.Get(switched.Body.String(), "group").String())
	untouched := call("/v1/images/generations", threadB, "gpt-image-2.5-flare", "65")
	require.Equal(t, 200, untouched.Code, untouched.Body.String())
	require.Equal(t, "gpt-image-1.5", gjson.Get(untouched.Body.String(), "model").String())
	require.Equal(t, "77", gjson.Get(untouched.Body.String(), "group").String())

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			thread := uuid.NewString()
			model := "gpt-image-2.5-flare"
			group := int64(65)
			if i%2 == 1 {
				model = "gpt-image-2.5-sunburst"
				group = 66
			}
			require.NoError(t, threads.RememberThread(context.Background(), service.TokenProImageThreadKey(key, thread), service.TokenProImageTurnRoute{
				GroupID: group, Model: model, ThreadID: thread,
			}))
			w := call("/v1/images/generations", thread, "gpt-image-2", strconv.FormatInt(group, 10))
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, model, gjson.Get(w.Body.String(), "model").String())
			require.Equal(t, strconv.FormatInt(group, 10), gjson.Get(w.Body.String(), "group").String())
		}(i)
	}
	wg.Wait()

	ambiguousReq := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader([]byte(`{"model":"gpt-image-2","prompt":"cat"}`)))
	ambiguousReq.Header.Set("Content-Type", "application/json")
	ambiguousReq.Header.Set("X-Codex-Turn-Metadata", `{"thread_id":"`+uuid.NewString()+`","turn_id":"`+uuid.NewString()+`"}`)
	ambiguous := httptest.NewRecorder()
	r.ServeHTTP(ambiguous, ambiguousReq)
	require.Equal(t, http.StatusBadRequest, ambiguous.Code)
	require.Contains(t, ambiguous.Body.String(), "无法确定本次生图模型")
	require.NotContains(t, ambiguous.Body.String(), "gpt-image-2.5")

	fallback := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader([]byte(`{"model":"gpt-image-2","prompt":"cat"}`)))
	fallback.Header.Set("Content-Type", "application/json")
	fallback.Header.Set("X-Codex-Turn-Metadata", `{"thread_id":"`+uuid.NewString()+`"}`)
	fallback.Header.Set("X-TokenPro-Image-Model", "gpt-image-2.5-flare")
	fallback.Header.Set(tokenProGroupIDHeader, "65")
	fallbackW := httptest.NewRecorder()
	r.ServeHTTP(fallbackW, fallback)
	require.Equal(t, 200, fallbackW.Code, fallbackW.Body.String())
	require.False(t, gjson.Get(fallbackW.Body.String(), "bound").Bool())
	require.Equal(t, "gpt-image-2.5-flare", gjson.Get(fallbackW.Body.String(), "header").String())
	require.Equal(t, "gpt-image-2", gjson.Get(fallbackW.Body.String(), "model").String())
}
