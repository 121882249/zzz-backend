//go:build unit

package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestNativeV2TurnRouting(t *testing.T) {
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := repository.NewTokenProImageTurnStore(client)
	key := &service.APIKey{ID: 1, UserID: 2, Key: "fixture", KeyType: service.APIKeyTypeGlobal}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
	r.Use(TokenProDirectRoute(store))
	r.POST("/v1/responses", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		group, _ := strconv.ParseInt(c.GetHeader(tokenProGroupIDHeader), 10, 64)
		resolved := *key
		resolved.GroupID = &group
		resolved.Group = &service.Group{ID: group, Platform: service.PlatformOpenAI, Description: "生图"}
		if err := service.BindTokenProImageTurn(c, &resolved); err != nil {
			c.Status(409)
			return
		}
		c.JSON(200, gin.H{"model": gjson.GetBytes(body, "model").String()})
	})
	for _, p := range []string{"/v1/images/generations", "/v1/images/edits"} {
		r.POST(p, func(c *gin.Context) {
			data, _ := io.ReadAll(c.Request.Body)
			c.JSON(200, gin.H{"body": string(data), "group": c.GetHeader(tokenProGroupIDHeader), "driver": service.TokenProNativeImageDriver(c), "turn_header": c.GetHeader("X-Codex-Image-Turn-Id")})
		})
	}
	request := func(path, contentType, body, turn string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-TokenPro-Image-Mode", "native-v2")
		// A stale provider-level route must not win over the authenticated turn.
		req.Header.Set(tokenProImageRouteHeader, "tp-g999-Z3B0LWltYWdlLTI")
		if turn != "" {
			req.Header.Set("X-Codex-Image-Turn-Id", turn)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			turn, thread := uuid.NewString(), uuid.NewString()
			model := "gpt-image-2.5-flare"
			group := 65
			if i%2 == 1 {
				model = "gpt-image-2.5-sunburst"
				group = 66
			}
			body, _ := json.Marshal(map[string]any{"model": fmt.Sprintf("tp-g%d-%s", group, base64.RawURLEncoding.EncodeToString([]byte(model))), "client_metadata": map[string]string{"turn_id": turn, "thread_id": thread}})
			require.Equal(t, 200, request("/v1/responses", "application/json", string(body), "").Code)
			for repeat := 0; repeat < 2; repeat++ {
				w := request("/v1/images/generations", "application/json", `{"model":"gpt-image-2","prompt":"identical prompt"}`, turn)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, strconv.Itoa(group), gjson.Get(w.Body.String(), "group").String())
				require.Equal(t, model, gjson.Get(gjson.Get(w.Body.String(), "body").String(), "model").String())
				require.Empty(t, gjson.Get(w.Body.String(), "turn_header").String())
			}
			bad := request("/v1/images/generations", "application/json", `{"model":"gpt-image-2.5-other"}`, turn)
			require.Equal(t, 400, bad.Code)
		}(i)
	}
	wg.Wait()
	require.Equal(t, 409, request("/v1/images/generations", "application/json", `{"model":"gpt-image-2"}`, uuid.NewString()).Code)
	require.Equal(t, 400, request("/v1/images/generations", "application/json", `{"model":"gpt-image-2"}`, "").Code)
	turn := uuid.NewString()
	require.NoError(t, store.Bind(context.Background(), service.TokenProImageTurnKey(key, turn), service.TokenProImageTurnRoute{GroupID: 16, Model: "gpt-5.6-sol", ThreadID: uuid.NewString()}))
	w := request("/v1/images/generations", "application/json", `{"model":"gpt-image-2"}`, turn)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "gpt-5.6-sol", gjson.Get(w.Body.String(), "driver").String())
	require.Equal(t, "16", gjson.Get(w.Body.String(), "group").String())
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	require.NoError(t, mw.WriteField("model", "gpt-image-2"))
	require.NoError(t, mw.WriteField("prompt", "same"))
	part, err := mw.CreateFormFile("image", "fixture.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fixture-image-bytes"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	w = request("/v1/images/edits", mw.FormDataContentType(), b.String(), turn)
	require.Equal(t, 200, w.Code)
	require.Contains(t, gjson.Get(w.Body.String(), "body").String(), "fixture-image-bytes")
}
