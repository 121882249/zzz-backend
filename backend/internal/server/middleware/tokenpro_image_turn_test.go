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

func TestNativeDeliveryRequiresAuthorizedImageGroup(t *testing.T) {
	for _, tc := range []struct {
		name, platform, description, mode, keyType string
		metadata                                   bool
		wantStatus                                 int
		wantNative                                 bool
	}{
		{"image group without mode header", service.PlatformOpenAI, "生图", "", service.APIKeyTypeGlobal, true, 200, true},
		{"image group legacy v2", service.PlatformOpenAI, "生图", "native-v2", service.APIKeyTypeGlobal, true, 200, true},
		{"trimmed description", service.PlatformOpenAI, " 生图 ", "", service.APIKeyTypeGlobal, true, 200, true},
		{"ordinary text", service.PlatformOpenAI, "普通", "", service.APIKeyTypeGlobal, false, 200, false},
		{"old client ordinary text", service.PlatformOpenAI, "普通", "native-v2", service.APIKeyTypeGlobal, false, 200, false},
		{"old v1 ordinary text", service.PlatformOpenAI, "普通", "native-v1", service.APIKeyTypeGlobal, false, 200, false},
		{"name is not description", service.PlatformOpenAI, "", "native-v2", service.APIKeyTypeGlobal, true, 200, false},
		{"description must equal", service.PlatformOpenAI, "生图专用", "", service.APIKeyTypeGlobal, true, 200, false},
		{"other platform", service.PlatformAnthropic, "生图", "native-v2", service.APIKeyTypeGlobal, true, 200, false},
		{"composite platform", service.PlatformComposite, "生图", "", service.APIKeyTypeGlobal, true, 200, false},
		{"ordinary key", service.PlatformOpenAI, "生图", "native-v2", "group", true, 200, false},
		{"image requires metadata", service.PlatformOpenAI, "生图", "", service.APIKeyTypeGlobal, false, 409, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rdb := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: rdb.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			store := repository.NewTokenProImageTurnStore(client)
			key := &service.APIKey{ID: 1, UserID: 2, Key: "fixture", KeyType: tc.keyType}
			groupID := int64(65)
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
			r.Use(TokenProDirectRoute(store))
			r.POST("/v1/responses", func(c *gin.Context) {
				resolved := *key
				resolved.GroupID = &groupID
				resolved.Group = &service.Group{ID: groupID, Name: "生图", Platform: tc.platform, Description: tc.description}
				if err := service.BindTokenProImageTurn(c, &resolved); err != nil {
					c.Status(409)
					return
				}
				require.Equal(t, tc.wantNative, service.TokenProNativeImages(c))
				require.Empty(t, c.GetHeader("X-TokenPro-Image-Mode"))
				c.Status(200)
			})
			turn, thread := uuid.NewString(), uuid.NewString()
			payload := map[string]any{"model": "tp-g65-Z3B0LTUuNi1zb2w", "input": "hello"}
			if tc.metadata {
				payload["client_metadata"] = map[string]string{"turn_id": turn, "thread_id": thread}
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.mode != "" {
				req.Header.Set("X-TokenPro-Image-Mode", tc.mode)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			route, lookupErr := store.Lookup(context.Background(), service.TokenProImageTurnKey(key, turn))
			if tc.wantNative {
				require.NoError(t, lookupErr)
				require.Equal(t, groupID, route.GroupID)
				require.Equal(t, thread, route.ThreadID)
			} else {
				require.ErrorIs(t, lookupErr, service.ErrImageTurnMissing)
			}
			require.Nil(t, key.Group, "cached global key must remain unchanged")
		})
	}
}

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
			group, _ := strconv.ParseInt(c.GetHeader(tokenProGroupIDHeader), 10, 64)
			resolved := *key
			resolved.GroupID = &group
			resolved.Group = &service.Group{ID: group, Platform: service.PlatformOpenAI}
			if group == 65 || group == 66 {
				resolved.Group.Description = "生图"
			}
			if err := service.BindTokenProImageTurn(c, &resolved); err != nil {
				c.Status(409)
				return
			}
			data, _ := io.ReadAll(c.Request.Body)
			c.JSON(200, gin.H{"body": string(data), "group": c.GetHeader(tokenProGroupIDHeader), "driver": service.TokenProNativeImageDriver(c), "turn_header": c.GetHeader("X-Codex-Image-Turn-Id")})
		})
	}
	request := func(path, contentType, body, turn string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		// New providers omit native mode. Retain the old missing-turn check.
		if strings.Contains(path, "/images/") && turn == "" {
			req.Header.Set("X-TokenPro-Image-Mode", "native-v2")
		}
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
	require.Equal(t, 409, w.Code, "stale ordinary-group bindings must not enable native images")
	turn = uuid.NewString()
	require.NoError(t, store.Bind(context.Background(), service.TokenProImageTurnKey(key, turn), service.TokenProImageTurnRoute{GroupID: 65, Model: "gpt-image-2.5-flare", ThreadID: uuid.NewString()}))
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
