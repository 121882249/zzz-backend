package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestTokenProNativeImageAuthorizationUsesOnlySelectedDriver(t *testing.T) {
	for _, tc := range []struct{ path, model, want string }{
		{"/v1/images/generations", "gpt-image-2", "gpt-5.6-sol"},
		{"/v1/images/edits", "gpt-image-2", "gpt-5.6-sol"},
		{"/v1/images/generations", "gpt-image-2.5-flare", "gpt-image-2.5-flare"},
		{"/v1/responses", "gpt-image-2", "gpt-image-2"},
	} {
		t.Run(tc.path+tc.model, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.path, nil)
			c.Set(service.TokenProNativeImageDriverContextKey, "gpt-5.6-sol")
			require.Equal(t, tc.want, nativeImageAuthorizationModel(c, tc.model))
		})
	}
}
