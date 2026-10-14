//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProLocalTitleOnlyAfterGlobalAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, keyType, source string
		authenticated, local  bool
	}{
		{"global title", service.APIKeyTypeGlobal, "thread_title", true, true},
		{"global conversation", service.APIKeyTypeGlobal, "user", true, false},
		{"group title", "group", "thread_title", true, false},
		{"unauthenticated title", "", "thread_title", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.authenticated {
					c.Set(string(ContextKeyAPIKey), &service.APIKey{KeyType: tc.keyType})
				}
				c.Next()
			})
			router.Use(TokenProDirectRoute())
			called := false
			router.POST("/v1/responses", func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) })
			body := `{"model":"tp-g69-gpt-6-astra","input":"User prompt: 画一只猫","text":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"}}}}}}`
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", `{"thread_source":"`+tc.source+`"}`)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, !tc.local, called, "title must not invoke upstream routing or usage recording")
			if tc.local {
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, "v1", rec.Header().Get("X-TokenPro-Local-Title"))
				require.Zero(t, gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
			} else {
				require.Equal(t, http.StatusNoContent, rec.Code)
			}
		})
	}
}
