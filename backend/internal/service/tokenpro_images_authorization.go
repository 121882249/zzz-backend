package service

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

type tokenProImagesAuthorizationKey struct{}

type tokenProImagesAuthorization struct {
	groupID int64
	model   string
}

// WithTokenProImagesAuthorization preserves the authenticated text selection
// for channel allow-list checks on its fixed native image tool call. The tool's
// gpt-image-2 is an implementation detail, not a second catalog selection.
// This does not change image scheduling, model mappings, forwarding or billing.
func WithTokenProImagesAuthorization(ctx context.Context, c *gin.Context, key *APIKey, imageModel string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil || !c.GetBool(TokenProTextImageDeliveryContextKey) || key == nil || !key.IsGlobal() ||
		key.GroupID == nil || *key.GroupID <= 0 || key.Group == nil || key.Group.ID != *key.GroupID ||
		!tokenProTextImageGroup(key.Group) || TokenProPureImageGroup(key.Group) || imageModel != "gpt-image-2" {
		return ctx
	}
	driver := TokenProNativeImageDriver(c)
	if !strings.HasPrefix(driver, "gpt-") || IsGPTImageGenerationModel(driver) {
		return ctx
	}
	return context.WithValue(ctx, tokenProImagesAuthorizationKey{}, tokenProImagesAuthorization{
		groupID: *key.GroupID, model: driver,
	})
}

func tokenProImagesChannelAuthorizationModel(ctx context.Context, groupID *int64, model string) string {
	if ctx == nil || groupID == nil || model != "gpt-image-2" ||
		!OpenAIImagesEndpointFromContext(ctx) || !OpenAIImageGenerationIntentFromContext(ctx) {
		return model
	}
	authorization, ok := ctx.Value(tokenProImagesAuthorizationKey{}).(tokenProImagesAuthorization)
	if !ok || authorization.groupID != *groupID {
		return model
	}
	return authorization.model
}
