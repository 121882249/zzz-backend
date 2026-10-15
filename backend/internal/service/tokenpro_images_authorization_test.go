//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func textImagesAuthorizationFixture(path string) (*gin.Context, *APIKey, context.Context) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", path, nil)
	c.Set(TokenProTextImageDeliveryContextKey, true)
	c.Set(TokenProNativeImageDriverContextKey, "gpt-6.1-sol")
	groupID := int64(69)
	key := &APIKey{KeyType: APIKeyTypeGlobal, GroupID: &groupID,
		Group: &Group{ID: groupID, Platform: PlatformOpenAI, AllowImageGeneration: true}}
	return c, key, WithOpenAIImagesEndpoint(WithOpenAIImageGenerationIntent(context.Background()))
}

func TestTokenProImagesAuthorizationScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*gin.Context, *APIKey, *context.Context, *string)
	}{
		{"group key", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.KeyType = APIKeyTypeGroup }},
		{"image permission revoked", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.Group.AllowImageGeneration = false }},
		{"pure image group", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.Group.Description = "生图" }},
		{"wrong platform", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.Group.Platform = PlatformAnthropic }},
		{"missing group", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.Group = nil }},
		{"mismatched group", func(_ *gin.Context, k *APIKey, _ *context.Context, _ *string) { k.Group.ID++ }},
		{"not bound", func(c *gin.Context, _ *APIKey, _ *context.Context, _ *string) {
			c.Set(TokenProTextImageDeliveryContextKey, false)
		}},
		{"missing driver", func(c *gin.Context, _ *APIKey, _ *context.Context, _ *string) {
			c.Set(TokenProNativeImageDriverContextKey, "")
		}},
		{"image driver", func(c *gin.Context, _ *APIKey, _ *context.Context, _ *string) {
			c.Set(TokenProNativeImageDriverContextKey, "gpt-image-2")
		}},
		{"other image model", func(_ *gin.Context, _ *APIKey, _ *context.Context, m *string) { *m = "gpt-image-2.5-flare" }},
		{"text endpoint", func(c *gin.Context, _ *APIKey, _ *context.Context, _ *string) { c.Request.URL.Path = "/v1/responses" }},
		{"no image endpoint context", func(_ *gin.Context, _ *APIKey, ctx *context.Context, _ *string) {
			*ctx = WithOpenAIImageGenerationIntent(context.Background())
		}},
		{"no image intent", func(_ *gin.Context, _ *APIKey, ctx *context.Context, _ *string) {
			*ctx = WithOpenAIImagesEndpoint(context.Background())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, key, ctx := textImagesAuthorizationFixture("/v1/images/generations")
			model := "gpt-image-2"
			tc.edit(c, key, &ctx, &model)
			ctx = WithTokenProImagesAuthorization(ctx, c, key, model)
			require.Equal(t, model, tokenProImagesChannelAuthorizationModel(ctx, key.GroupID, model))
		})
	}
	c, key, ctx := textImagesAuthorizationFixture("/v1/images/edits")
	ctx = WithTokenProImagesAuthorization(ctx, c, key, "gpt-image-2")
	require.Equal(t, "gpt-6.1-sol", tokenProImagesChannelAuthorizationModel(ctx, key.GroupID, "gpt-image-2"))
	otherGroup := int64(57)
	require.Equal(t, "gpt-image-2", tokenProImagesChannelAuthorizationModel(ctx, &otherGroup, "gpt-image-2"))
	require.Equal(t, "gpt-6-astra", tokenProImagesChannelAuthorizationModel(ctx, key.GroupID, "gpt-6-astra"))
}

func TestTokenProImagesAuthorizationChannelRestriction(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceResponse, BillingModelSourceUpstream} {
		t.Run(source, func(t *testing.T) {
			c, key, ctx := textImagesAuthorizationFixture("/v1/images/generations")
			ch := Channel{ID: 14, Status: StatusActive, GroupIDs: []int64{69, 57}, RestrictModels: true, BillingModelSource: source,
				ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-6.1-sol", "gpt-6.1-sol-mapped"}}},
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"gpt-6.1-sol": "gpt-6.1-sol-mapped"}}}
			svc := &OpenAIGatewayService{channelService: newTestChannelService(makeStandardRepo(ch, map[int64]string{69: PlatformOpenAI, 57: PlatformOpenAI}))}
			ctx = WithTokenProImagesAuthorization(ctx, c, key, "gpt-image-2")
			if source != BillingModelSourceUpstream {
				require.True(t, svc.checkChannelPricingRestriction(context.Background(), key.GroupID, "gpt-image-2"), "reproduce R80 restriction")
				require.False(t, svc.checkChannelPricingRestriction(ctx, key.GroupID, "gpt-image-2"), "check the authorized text selection")
				require.True(t, svc.checkChannelPricingRestriction(ctx, key.GroupID, "gpt-image-2.5-flare"), "no arbitrary model escape")
			}
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol-mapped"}}}
			require.False(t, svc.isUpstreamModelRestrictedByChannel(ctx, 69, account, "gpt-image-2", false))
			require.True(t, svc.isUpstreamModelRestrictedByChannel(context.Background(), 69, account, "gpt-image-2", false))
			require.True(t, svc.isUpstreamModelRestrictedByChannel(ctx, 57, account, "gpt-image-2", false), "context is group scoped")
			mapping, _ := svc.ResolveChannelMappingAndRestrict(ctx, key.GroupID, "gpt-image-2")
			require.Equal(t, "gpt-image-2", mapping.MappedModel, "actual image mapping stays untouched")
			c.Set(TokenProNativeImageDriverContextKey, "gpt-6-astra")
			denied := WithTokenProImagesAuthorization(WithOpenAIImagesEndpoint(WithOpenAIImageGenerationIntent(context.Background())), c, key, "gpt-image-2")
			if source != BillingModelSourceUpstream {
				require.True(t, svc.checkChannelPricingRestriction(denied, key.GroupID, "gpt-image-2"), "unlisted driver remains denied")
			}
			require.True(t, svc.isUpstreamModelRestrictedByChannel(denied, 69, account, "gpt-image-2", false))
		})
	}
}

func TestTokenProImagesAuthorizationSchedulerKeepsImageModel(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			c, key, ctx := textImagesAuthorizationFixture("/v1/images/generations")
			ch := Channel{ID: 14, Status: StatusActive, GroupIDs: []int64{69}, RestrictModels: true,
				BillingModelSource: BillingModelSourceChannelMapped,
				ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-6.1-sol"}}}}
			svc := &OpenAIGatewayService{channelService: newTestChannelService(makeStandardRepo(ch, map[int64]string{69: PlatformOpenAI})),
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 47, Platform: PlatformOpenAI, Type: accountType, Status: StatusActive, Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": "gpt-image-2"}}}}}}
			_, _, err := svc.SelectAccountWithSchedulerForImages(ctx, key.GroupID, "", "gpt-image-2", nil, OpenAIImagesCapabilityNative)
			require.ErrorContains(t, err, "channel pricing restriction")
			ctx = WithTokenProImagesAuthorization(ctx, c, key, "gpt-image-2")
			selected, _, err := svc.SelectAccountWithSchedulerForImages(ctx, key.GroupID, "", "gpt-image-2", nil, OpenAIImagesCapabilityNative)
			require.NoError(t, err)
			require.Equal(t, int64(47), selected.Account.ID, "schedule gpt-image-2, never the text driver")
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
}
