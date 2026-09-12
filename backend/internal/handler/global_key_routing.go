package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const tokenProGroupIDHeader = "X-TokenPro-Group-Id"

// ResolveGlobalKeyForRoute resolves a global key before route-level platform
// dispatch. The regular handlers repeat this request-scoped resolution after
// parsing the body; doing it here ensures /messages, /responses and
// /chat/completions reach the correct protocol adapter instead of defaulting
// to the unbound-key branch.
func (h *GatewayHandler) ResolveGlobalKeyForRoute(c *gin.Context, model string) bool {
	if h == nil || c == nil {
		return false
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || !apiKey.IsGlobal() {
		return true
	}
	if model == "" {
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return false
	}
	if _, err := resolveGlobalAPIKeyForModel(c, h.gatewayService, apiKey, subject.UserID, model); err != nil {
		respondGlobalKeyRoutingError(c, err, h.errorResponse)
		return false
	}
	return true
}

func respondGlobalKeyRoutingError(c *gin.Context, err error, respond func(*gin.Context, int, string, string)) {
	if errors.Is(err, service.ErrNoAvailableAccounts) {
		respond(c, http.StatusServiceUnavailable, "no_available_group", "当前模型没有可用分组或有效订阅。")
		return
	}
	respond(c, http.StatusServiceUnavailable, "routing_error", "Failed to resolve an available group for this model")
}

// resolveGlobalAPIKeyForModel attaches a request-scoped group to a global key
// and leaves all downstream routing and billing code unchanged. Ordinary
// group-scoped keys are returned untouched. The authenticated key in the
// middleware cache is never mutated.
func resolveGlobalAPIKeyForModel(
	c *gin.Context,
	gatewayService *service.GatewayService,
	apiKey *service.APIKey,
	userID int64,
	model string,
) (*service.APIKey, error) {
	rawGroupID := strings.TrimSpace(c.GetHeader(tokenProGroupIDHeader))
	// This is an internal routing hint, not an upstream provider header.
	// Consume it once so downstream adapters can never forward it.
	c.Request.Header.Del(tokenProGroupIDHeader)
	if apiKey == nil || !apiKey.IsGlobal() {
		return apiKey, nil
	}
	// Route-level dispatch and the concrete handler may both resolve a global
	// key. Reuse the already validated request-scoped group on the second pass.
	if apiKey.Group != nil && apiKey.GroupID != nil {
		return apiKey, nil
	}
	if gatewayService == nil {
		return nil, service.ErrNoAvailableAccounts
	}
	var preferredGroupID *int64
	if rawGroupID != "" {
		parsed, parseErr := strconv.ParseInt(rawGroupID, 10, 64)
		if parseErr != nil || parsed <= 0 {
			return nil, service.ErrNoAvailableAccounts
		}
		preferredGroupID = &parsed
	}
	resolved, err := gatewayService.ResolveGlobalGroupForModelWithUserAndGroup(
		c.Request.Context(), apiKey.User, userID, "", model, preferredGroupID, nil,
	)
	if err != nil {
		return nil, err
	}
	if resolved == nil || resolved.Group == nil {
		return nil, service.ErrNoAvailableAccounts
	}
	requestKey := cloneAPIKeyWithGroup(apiKey, resolved.Group)
	c.Set(string(middleware2.ContextKeyAPIKey), requestKey)
	// Downstream pricing/profit-control code reads the authenticated group from
	// ctxkey.Group, so keep the resolved group request-scoped as well. The
	// middleware's cached API key is never mutated.
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, resolved.Group))
	if resolved.Subscription != nil {
		c.Set(string(middleware2.ContextKeySubscription), resolved.Subscription)
	}
	return requestKey, nil
}
