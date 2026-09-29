//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveGlobalAPIKeyDoesNotTrustPersistedLegacyGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	groupID := int64(16)
	key := &service.APIKey{
		ID: 1, UserID: 7, KeyType: service.APIKeyTypeGlobal,
		GroupID: &groupID, Group: &service.Group{ID: groupID},
	}

	_, err := resolveGlobalAPIKeyForModel(c, nil, key, 7, "gpt-5.6-sol")
	require.ErrorIs(t, err, service.ErrGlobalGroupRequired)
}

func TestResolveGlobalAPIKeyReusesOnlyRequestScopedResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	groupID := int64(16)
	key := &service.APIKey{
		ID: 1, UserID: 7, KeyType: service.APIKeyTypeGlobal,
		GroupID: &groupID, Group: &service.Group{ID: groupID},
	}
	c.Set(tokenProGlobalGroupResolvedContextKey, true)

	resolved, err := resolveGlobalAPIKeyForModel(c, nil, key, 7, "gpt-5.6-sol")
	require.NoError(t, err)
	require.Same(t, key, resolved)
}
