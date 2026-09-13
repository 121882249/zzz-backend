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
