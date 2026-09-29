//go:build unit

package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
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

func TestTextImageDeliveryKeepsAuthorizedGroupAndDriver(t *testing.T) {
	rdb := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := repository.NewTokenProImageTurnStore(client)
	key := &service.APIKey{ID: 737, UserID: 1, Key: "fixture", KeyType: service.APIKeyTypeGlobal}
	groupID := int64(57)
	group := &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Description: "普通 GPT", AllowImageGeneration: true}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
	r.Use(TokenProDirectRoute(store))
	for _, path := range []string{"/v1/responses", "/v1/images/generations", "/v1/images/edits"} {
		r.POST(path, func(c *gin.Context) {
			resolved := *key
			resolved.GroupID, resolved.Group = &groupID, group
			if err := service.BindTokenProImageTurn(c, &resolved); err != nil {
				c.Status(409)
				return
			}
			body, _ := io.ReadAll(c.Request.Body)
			model := gjson.GetBytes(body, "model").String()
			service.RestrictTokenProNativeImages(c, group, model)
			c.JSON(200, gin.H{"pure": service.TokenProNativeImages(c), "delivery": service.TokenProNativeImageDelivery(c),
				"group": c.GetHeader(tokenProGroupIDHeader), "driver": service.TokenProNativeImageDriver(c), "model": model})
		})
	}
	request := func(path, body, turn string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if turn != "" {
			req.Header.Set("X-Codex-Image-Turn-Id", turn)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	turn, thread := uuid.NewString(), uuid.NewString()
	model := "tp-g57-" + base64.RawURLEncoding.EncodeToString([]byte("gpt-5.6-luna"))
	w := request("/v1/responses", fmt.Sprintf(`{"model":%q,"input":"draw a pig","client_metadata":{"turn_id":%q,"thread_id":%q}}`, model, turn, thread), "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.False(t, gjson.Get(w.Body.String(), "pure").Bool(), "ordinary GPT must not enter pure-image dispatch")
	require.True(t, gjson.Get(w.Body.String(), "delivery").Bool())
	route, err := store.Lookup(context.Background(), service.TokenProImageTurnKey(key, turn))
	require.NoError(t, err)
	require.Equal(t, int64(57), route.GroupID)
	require.Equal(t, "gpt-5.6-luna", route.Model)
	require.Equal(t, thread, route.ThreadID)
	for _, endpoint := range []string{"/v1/images/generations", "/v1/images/edits"} {
		w = request(endpoint, `{"model":"gpt-image-2","prompt":"pig"}`, turn)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "57", gjson.Get(w.Body.String(), "group").String())
		require.Equal(t, "gpt-5.6-luna", gjson.Get(w.Body.String(), "driver").String())
		require.Equal(t, "gpt-image-2", gjson.Get(w.Body.String(), "model").String())
		require.False(t, gjson.Get(w.Body.String(), "pure").Bool())
		require.True(t, gjson.Get(w.Body.String(), "delivery").Bool())
	}
	group.AllowImageGeneration = false
	w = request("/v1/images/generations", `{"model":"gpt-image-2","prompt":"pig"}`, turn)
	require.Equal(t, 409, w.Code, "revoked image permission must reject stale native routes")
	require.Nil(t, key.Group, "cached global KEY must not be mutated")
}

func TestTextImageDeliveryMissingMetadataDoesNotBreakText(t *testing.T) {
	for _, body := range []string{`{"model":"gpt-5.6-luna"}`, `{"model":"gpt-image-2"}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		groupID := int64(57)
		key := &service.APIKey{KeyType: service.APIKeyTypeGlobal, GroupID: &groupID,
			Group: &service.Group{Platform: service.PlatformOpenAI, AllowImageGeneration: true}}
		c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{
			Model: gjson.Get(body, "model").String(), ValidationErr: service.ErrImageTurnMissing})
		require.NoError(t, service.BindTokenProImageTurn(c, key))
		require.False(t, service.TokenProNativeImages(c))
		require.False(t, service.TokenProNativeImageDelivery(c))
	}
}
