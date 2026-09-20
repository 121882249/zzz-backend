//go:build unit

package middleware

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProDirectRouteRestoresModelAndPinsGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	model := "gpt-5.6-sol"
	slug := "tp-g16-" + base64.RawURLEncoding.EncodeToString([]byte(model))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
		c.Next()
	})
	r.Use(TokenProDirectRoute())
	r.POST("/v1/responses", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, model, gjson.GetBytes(body, "model").String())
		require.Equal(t, "16", c.GetHeader(tokenProGroupIDHeader))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"`+slug+`","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestDecodeTokenProDirectModelSupportsLegacyAndReadableRoutes(t *testing.T) {
	const model = "gpt-5.6-luna"
	legacy := "tp-g57-" + base64.RawURLEncoding.EncodeToString([]byte(model))
	for _, route := range []string{legacy, "tp-g57-" + model} {
		groupID, publicModel, matched := decodeTokenProDirectModel(route)
		require.True(t, matched)
		require.Equal(t, int64(57), groupID)
		require.Equal(t, model, publicModel)
	}

	for _, route := range []string{
		"tp-g0-gpt-5.6-luna",
		"tp-g57-",
		"tp-g57-model with spaces",
		"tp-g57-model?unsafe=true",
	} {
		_, _, matched := decodeTokenProDirectModel(route)
		require.False(t, matched, route)
	}
}

func TestTokenProNativeModeRequiresRoutedGlobalRequest(t *testing.T) {
	for _, tc := range []struct {
		name, model, keyType, path string
		enabled                    bool
	}{
		{"new Codex", "tp-g16-Z3B0LTUuNi1zb2w", service.APIKeyTypeGlobal, "/v1/responses", true},
		{"ordinary SDK", "gpt-5.6-sol", service.APIKeyTypeGlobal, "/v1/responses", false},
		{"group key", "tp-g16-Z3B0LTUuNi1zb2w", "group", "/v1/responses", false},
		{"compact", "tp-g16-Z3B0LTUuNi1zb2w", service.APIKeyTypeGlobal, "/v1/responses/compact", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: tc.keyType}); c.Next() })
			r.Use(TokenProDirectRoute())
			r.POST(tc.path, func(c *gin.Context) {
				require.False(t, service.TokenProNativeImages(c), "native mode requires the trusted group")
				groupID := int64(16)
				require.NoError(t, service.BindTokenProImageTurn(c, &service.APIKey{KeyType: tc.keyType, GroupID: &groupID,
					Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Description: "生图"}}))
				require.Equal(t, tc.enabled, service.TokenProNativeImages(c))
				require.Empty(t, c.GetHeader("X-TokenPro-Image-Mode"))
				c.Status(204)
			})
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"model":"`+tc.model+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-TokenPro-Image-Mode", "native-v1")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, 204, w.Code)
		})
	}
}

func TestTokenProNativeTextImageRouteIsExplicitAndScoped(t *testing.T) {
	for _, tc := range []struct {
		mode, model string
		status      int
		driver      string
	}{
		{"native-v1", "gpt-image-2", 204, "gpt-5.6-sol"},
		{"", "gpt-image-2", 400, ""},
		{"native-v1", "gpt-image-2.5-flare", 400, ""},
	} {
		t.Run(tc.mode+tc.model, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
				c.Next()
			})
			r.Use(TokenProDirectRoute())
			r.POST("/v1/images/generations", func(c *gin.Context) {
				require.Equal(t, tc.driver, service.TokenProNativeImageDriver(c))
				require.Equal(t, "16", c.GetHeader(tokenProGroupIDHeader))
				c.Status(204)
			})
			req := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"`+tc.model+`","prompt":"cat"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-TokenPro-Image-Mode", tc.mode)
			req.Header.Set(tokenProImageRouteHeader, "tp-g16-Z3B0LTUuNi1zb2w")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
		})
	}
}

func TestTokenProDirectRouteRejectsConflictingGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	slug := "tp-g16-" + base64.RawURLEncoding.EncodeToString([]byte("gpt-5.6-sol"))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
		c.Next()
	})
	r.Use(TokenProDirectRoute())
	r.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"`+slug+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenProGroupIDHeader, "65")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTokenProDirectRouteRestoresMultipartImageEditModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		model       = "gpt-image-2"
		imageBytes  = "not-a-real-png-but-must-stay-identical"
		promptValue = "make it blue"
	)
	slug := "tp-g65-" + base64.RawURLEncoding.EncodeToString([]byte(model))
	var requestBody bytes.Buffer
	requestWriter := multipart.NewWriter(&requestBody)
	require.NoError(t, requestWriter.WriteField("model", slug))
	require.NoError(t, requestWriter.WriteField("prompt", promptValue))
	file, err := requestWriter.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = file.Write([]byte(imageBytes))
	require.NoError(t, err)
	require.NoError(t, requestWriter.Close())

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
		c.Next()
	})
	r.Use(TokenProDirectRoute())
	r.POST("/v1/images/edits", func(c *gin.Context) {
		mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		form, err := reader.ReadForm(1 << 20)
		require.NoError(t, err)
		defer form.RemoveAll()
		require.Equal(t, []string{model}, form.Value["model"])
		require.Equal(t, []string{promptValue}, form.Value["prompt"])
		require.Equal(t, "65", c.GetHeader(tokenProGroupIDHeader))
		upload, err := form.File["image"][0].Open()
		require.NoError(t, err)
		defer upload.Close()
		gotImage, err := io.ReadAll(upload)
		require.NoError(t, err)
		require.Equal(t, []byte(imageBytes), gotImage)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(requestBody.Bytes()))
	req.Header.Set("Content-Type", requestWriter.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestTokenProDirectRouteConcurrentRequestsStayIsolated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const model = "gpt-5.6-sol"
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
		c.Next()
	})
	r.Use(TokenProDirectRoute())
	r.POST("/v1/responses", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || gjson.GetBytes(body, "model").String() != model ||
			c.GetHeader(tokenProGroupIDHeader) != c.Query("expected_group") {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	for i := 0; i < 100; i++ {
		groupID := int64(16)
		if i%2 == 1 {
			groupID = 41
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			group := strconv.FormatInt(groupID, 10)
			slug := "tp-g" + group + "-" + base64.RawURLEncoding.EncodeToString([]byte(model))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses?expected_group="+group,
				bytes.NewBufferString(`{"model":"`+slug+`","input":"hi"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusNoContent {
				errors <- fmt.Errorf("group %s returned HTTP %d", group, w.Code)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
}

func TestTokenProDirectNativeImageRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	slug := func(group int, model string) string {
		return fmt.Sprintf("tp-g%d-%s", group, base64.RawURLEncoding.EncodeToString([]byte(model)))
	}
	image := slug(65, "gpt-image-2.5-flare")
	chat := slug(16, "gpt-6-astra")
	for _, tc := range []struct {
		name, path, body, header, group, model string
		status                                 int
	}{
		{"native image", "/v1/images/generations", `{"model":"gpt-image-2","prompt":"blue"}`, image, "65", "gpt-image-2.5-flare", 200},
		{"image edit JSON", "/v1/images/edits", `{"model":"gpt-image-2","images":[{"image_url":"data:image/png;base64,fixture"}]}`, image, "65", "gpt-image-2.5-flare", 200},
		{"text ignores image route", "/v1/responses", `{"model":"` + chat + `","input":"hello"}`, image, "16", "gpt-6-astra", 200},
		{"explicit slug wins", "/v1/images/generations", `{"model":"` + slug(41, "gpt-image-2") + `"}`, image, "41", "gpt-image-2", 200},
		{"unrelated image rejected", "/v1/images/generations", `{"model":"other-image"}`, image, "", "", 400},
		{"duplicate model rejected", "/v1/responses", `{"model":"` + chat + `","model":"other"}`, image, "", "", 400},
		{"malformed slug rejected", "/v1/responses", `{"model":"tp-g0-invalid"}`, "", "", "", 400},
		{"cross-group hosted tool rejected", "/v1/responses", `{"model":"` + chat + `","tools":[{"type":"image_generation","model":"` + image + `"}]}`, "", "", "", 400},
		{"same-group hosted tool", "/v1/responses", `{"model":"` + chat + `","tools":[{"type":"image_generation","model":"` + slug(16, "gpt-image-2") + `"}]}`, "", "16", "gpt-6-astra", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
				c.Next()
			})
			r.Use(TokenProDirectRoute())
			r.POST(tc.path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, tc.group, c.GetHeader(tokenProGroupIDHeader))
				require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
				require.Empty(t, c.GetHeader(tokenProImageRouteHeader))
				require.NotContains(t, string(body), "tp-g")
				c.Data(200, "application/json", []byte(`{"id":"resp_fixture","output":[{"id":"ig_fixture","type":"image_generation_call","result":"bytes+/="}]}`))
			})
			req := httptest.NewRequest("POST", tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(tokenProImageRouteHeader, tc.header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			if tc.status == 200 {
				require.Equal(t, `{"id":"resp_fixture","output":[{"id":"ig_fixture","type":"image_generation_call","result":"bytes+/="}]}`, w.Body.String())
			}
		})
	}
}

func TestTokenProImageReturnsStayWithRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Deliberately share the auth-cache key across requests. Routing must never mutate it.
	key := &service.APIKey{KeyType: service.APIKeyTypeGlobal}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
	r.Use(TokenProDirectRoute())
	r.POST("/v1/images/generations", func(c *gin.Context) {
		group := c.GetHeader(tokenProGroupIDHeader)
		request := c.GetHeader("X-Request-Id")
		c.Header("X-Request-Id", request)
		c.Data(200, "text/event-stream", []byte("event: image_generation.completed\ndata: "+fmt.Sprintf(`{"id":%q,"b64_json":%q}`, request, group+"-"+request)+"\n\n"))
	})
	var wg sync.WaitGroup
	failures := make(chan string, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			group := strconv.Itoa(65 + i%2)
			id := fmt.Sprintf("image-request-%d", i)
			req := httptest.NewRequest("POST", "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-2","prompt":"fixture"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-Id", id)
			req.Header.Set(tokenProImageRouteHeader, "tp-g"+group+"-"+base64.RawURLEncoding.EncodeToString([]byte("gpt-image-2")))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := "event: image_generation.completed\ndata: " + fmt.Sprintf(`{"id":%q,"b64_json":%q}`, id, group+"-"+id) + "\n\n"
			if w.Code != 200 || w.Header().Get("X-Request-Id") != id || w.Body.String() != want {
				failures <- id
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for id := range failures {
		t.Errorf("image response crossed request boundary: %s", id)
	}
	require.Nil(t, key.GroupID)
	require.Nil(t, key.Group)
}

func TestTokenProMultipartRejectsDuplicateModel(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(t, w.WriteField("model", "tp-g65-"+base64.RawURLEncoding.EncodeToString([]byte("gpt-image-2"))))
	require.NoError(t, w.WriteField("model", "plain-other-model"))
	require.NoError(t, w.Close())
	_, _, _, err := rewriteTokenProMultipartRoute(body.Bytes(), w.Boundary(), func(model string) (int64, string, bool, error) {
		group, public, routed := decodeTokenProDirectModel(model)
		return group, public, routed, nil
	})
	require.Error(t, err)
}

func TestTokenProPythonSDKEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ path, body, model, group, response string }{
		{"/v1/chat/completions", `{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"Hello"}]}`, "gpt-5.6-sol", "16", `{"id":"chat_fixture","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"}}]}`},
		{"/v1/images/generations", `{"model":"gpt-image-2.5-flare","prompt":"一只在键盘上打字的橘猫，插画风格"}`, "gpt-image-2.5-flare", "65", `{"created":1,"data":[{"b64_json":"fixture+/="}]}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: service.APIKeyTypeGlobal})
				c.Next()
			})
			r.Use(TokenProDirectRoute())
			r.POST(tc.path, func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, tc.body, string(body))
				require.Equal(t, tc.group, c.GetHeader(tokenProGroupIDHeader))
				c.Data(200, "application/json", []byte(tc.response))
			})
			req := httptest.NewRequest("POST", tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(tokenProGroupIDHeader, tc.group)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, 200, w.Code)
			require.Equal(t, tc.response, w.Body.String())
		})
	}
}
