package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// ErrTokenProTextGroupNoImageModel is returned when a global text-group image
// route has no gpt-image model the group can actually run.
var ErrTokenProTextGroupNoImageModel = errors.New("当前分组没有可用的生图模型")

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

// ResolveTokenProTextRouteImageModel keeps a global text-group image request
// inside that group. The requested model is used when the group's channel
// whitelist and model allowlist already accept it. Otherwise the request is
// rewritten to one image model from that whitelist. gpt-image-2 wins when it
// is available; remaining candidates keep channel pricing order, then sorted
// mapping keys, then the group allowlist order. An empty request means
// gpt-image-2. The error is only returned when no image model is available.
func (s *OpenAIGatewayService) ResolveTokenProTextRouteImageModel(ctx context.Context, group *Group, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "gpt-image-2"
	}
	if group == nil || group.ID <= 0 {
		return requested, nil
	}
	if s.tokenProTextGroupAllowsImageModel(ctx, group, requested) {
		return requested, nil
	}
	candidates := s.tokenProTextGroupImageCandidates(ctx, group)
	for _, candidate := range candidates {
		if strings.EqualFold(candidate, "gpt-image-2") && s.tokenProTextGroupAllowsImageModel(ctx, group, candidate) {
			return candidate, nil
		}
	}
	for _, candidate := range candidates {
		if s.tokenProTextGroupAllowsImageModel(ctx, group, candidate) {
			return candidate, nil
		}
	}
	if !s.tokenProTextGroupImageWhitelistActive(ctx, group) {
		return requested, nil
	}
	return "", ErrTokenProTextGroupNoImageModel
}

func (s *OpenAIGatewayService) tokenProTextGroupImageWhitelistActive(ctx context.Context, group *Group) bool {
	if group != nil && group.ModelAllowlistEnabled() {
		return true
	}
	if s == nil || s.channelService == nil || group == nil {
		return false
	}
	channel, err := s.channelService.GetChannelForGroup(ctx, group.ID)
	return err == nil && channel != nil && channel.RestrictModels
}

func (s *OpenAIGatewayService) tokenProTextGroupAllowsImageModel(ctx context.Context, group *Group, model string) bool {
	if group != nil && group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(model) {
		return false
	}
	if s == nil || s.channelService == nil || group == nil || group.ID <= 0 {
		return true
	}
	channel, err := s.channelService.GetChannelForGroup(ctx, group.ID)
	if err != nil || channel == nil || !channel.RestrictModels {
		return true
	}
	if s.checkChannelPricingRestriction(ctx, &group.ID, model) {
		return false
	}
	if !s.channelService.IsModelRestricted(ctx, group.ID, model) {
		return true
	}
	mapped := strings.TrimSpace(s.channelService.ResolveChannelMapping(ctx, group.ID, model).MappedModel)
	return mapped != "" && !strings.EqualFold(mapped, model) && !s.channelService.IsModelRestricted(ctx, group.ID, mapped)
}

func (s *OpenAIGatewayService) tokenProTextGroupImageCandidates(ctx context.Context, group *Group) []string {
	seen := map[string]struct{}{}
	var candidates []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, "*") || !IsGPTImageGenerationModel(name) {
			return
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, name)
	}
	if s != nil && s.channelService != nil && group != nil {
		if channel, err := s.channelService.GetChannelForGroup(ctx, group.ID); err == nil && channel != nil {
			for _, pricing := range channel.ModelPricing {
				for _, model := range pricing.Models {
					add(model)
				}
			}
			var mapped []string
			for _, mapping := range channel.ModelMapping {
				for model := range mapping {
					mapped = append(mapped, model)
				}
			}
			sort.Strings(mapped)
			for _, model := range mapped {
				add(model)
			}
		}
	}
	if group != nil && group.ModelAllowlistEnabled() {
		for _, model := range group.ModelAllowlist.Models {
			add(model)
		}
	}
	return candidates
}
