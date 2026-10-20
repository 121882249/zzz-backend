package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiNativeGlobalKeyWithoutGroupReturnsGroupIDRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &GatewayHandler{}
	for _, action := range []string{"generateContent", "streamGenerateContent", "countTokens"} {
		t.Run(action, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := newGeminiGlobalKeyContext(t, rec, http.MethodPost, "/v1beta/models/gemini-2.5-flash:"+action, "")
			c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:" + action}}
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})

			h.GeminiV1BetaModels(c)

			assertGeminiRoutingError(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT", "group_id_required",
				"全局 Key 请求必须携带有效的分组 ID。请更新 TokenPro 客户端并重新连接。")
		})
	}

	t.Run("get model", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c := newGeminiGlobalKeyContext(t, rec, http.MethodGet, "/v1beta/models/gemini-2.5-flash", "")
		c.Params = gin.Params{{Key: "model", Value: "gemini-2.5-flash"}}

		h.GeminiV1BetaGetModel(c)

		assertGeminiRoutingError(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT", "group_id_required",
			"全局 Key 请求必须携带有效的分组 ID。请更新 TokenPro 客户端并重新连接。")
	})
}

func TestGeminiNativeGlobalKeyInvalidGroupStaysUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &GatewayHandler{}

	rec := httptest.NewRecorder()
	c := newGeminiGlobalKeyContext(t, rec, http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", "57")
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:generateContent"}}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})

	h.GeminiV1BetaModels(c)

	assertGeminiRoutingError(t, rec, http.StatusServiceUnavailable, "INTERNAL", "no_available_group",
		"当前模型没有可用分组或有效订阅。")
}

func TestGeminiNativeGlobalKeyPlatformMismatchStaysBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &GatewayHandler{}
	groupID := int64(4)

	rec := httptest.NewRecorder()
	c := newGeminiGlobalKeyContext(t, rec, http.MethodPost, "/v1beta/models/claude-sonnet-5-5:generateContent", "")
	c.Params = gin.Params{{Key: "modelAction", Value: "/claude-sonnet-5-5:generateContent"}}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: 1, UserID: 7, KeyType: service.APIKeyTypeGlobal,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})
	c.Set(tokenProGlobalGroupResolvedContextKey, true)
	c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformAntigravity))

	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, http.StatusBadRequest, body.Error.Code)
	require.Equal(t, "INVALID_ARGUMENT", body.Error.Status)
	require.Equal(t, "API key group platform is not gemini", body.Error.Message)
	require.NotContains(t, rec.Body.String(), "group_id_required")
}

func newGeminiGlobalKeyContext(t *testing.T, rec *httptest.ResponseRecorder, method, path, groupID string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, path, nil)
	if groupID != "" {
		req.Header.Set(tokenProGroupIDHeader, groupID)
	}
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: 1, UserID: 7, KeyType: service.APIKeyTypeGlobal,
	})
	return c
}

func assertGeminiRoutingError(t *testing.T, rec *httptest.ResponseRecorder, status int, googleStatus, reason, message string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
			Details []struct {
				Type   string `json:"@type"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, status, body.Error.Code)
	require.Equal(t, googleStatus, body.Error.Status)
	require.Equal(t, message, body.Error.Message)
	require.Len(t, body.Error.Details, 1)
	require.Equal(t, "type.googleapis.com/google.rpc.ErrorInfo", body.Error.Details[0].Type)
	require.Equal(t, reason, body.Error.Details[0].Reason)
}
