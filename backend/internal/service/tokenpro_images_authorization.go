package service

import (
	"context"

	"github.com/gin-gonic/gin"
)

// WithTokenProImagesAuthorization keeps the call site stable. A global key
// whose image route selected a text group is already bound to that group.
// Channel and group restrictions then see the image model that is forwarded
// and billed, the same as a group-scoped key. The conversation model is not
// substituted in.
func WithTokenProImagesAuthorization(ctx context.Context, _ *gin.Context, _ *APIKey, _ string) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func tokenProImagesChannelAuthorizationModel(_ context.Context, _ *int64, model string) string {
	return model
}
