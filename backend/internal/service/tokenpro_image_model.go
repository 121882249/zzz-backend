package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrTokenProImageModelInternal means the selected image model could not be
// confirmed. Handlers map it to a generic 500 and do not forward the cause.
var ErrTokenProImageModelInternal = errors.New("tokenpro image model unavailable")

// TokenProImageModelError is a client-visible rejection of X-TokenPro-Image-Model.
// The body model is left unchanged because the handler returns before scheduling.
type TokenProImageModelError struct {
	Message string
}

func (e *TokenProImageModelError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

const (
	tokenProImageModelMissingGroup     = "生图请求缺少有效分组，请重新连接 TokenPro。没有改用其他模型。"
	tokenProImageModelGroupUnavailable = "生图分组不存在或已停用，没有改用其他模型。"
	tokenProImageModelNotImageGroup    = "当前分组不是生图分组，没有改用其他模型。"
	tokenProImageModelNotImageModel    = "指定的模型不是生图模型，没有改用其他模型。"
	tokenProImageModelNotAllowed       = "该生图分组不支持这个图片模型，没有改用其他模型。"
)

// TokenProImageGroup matches any OpenAI group whose description contains 生图.
// Pure-image chat dispatch stays on the exact TokenProPureImageGroup check.
func TokenProImageGroup(group *Group) bool {
	return group != nil && group.Platform == PlatformOpenAI && strings.Contains(group.Description, "生图")
}

// CanonicalTokenProImageModel returns the catalog spelling of an image model
// that this group is allowed to serve. Empty catalogs and unknown names are
// rejected. The caller replaces the Images body before scheduling and billing.
func (s *GatewayService) CanonicalTokenProImageModel(ctx context.Context, groupID int64, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if groupID <= 0 {
		return "", &TokenProImageModelError{Message: tokenProImageModelMissingGroup}
	}
	if s == nil || s.groupRepo == nil || s.accountRepo == nil {
		return "", fmt.Errorf("%w: catalog unavailable", ErrTokenProImageModelInternal)
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			return "", &TokenProImageModelError{Message: tokenProImageModelGroupUnavailable}
		}
		return "", fmt.Errorf("%w: %v", ErrTokenProImageModelInternal, err)
	}
	if group == nil || !group.IsActive() {
		return "", &TokenProImageModelError{Message: tokenProImageModelGroupUnavailable}
	}
	if !TokenProImageGroup(group) {
		return "", &TokenProImageModelError{Message: tokenProImageModelNotImageGroup}
	}
	if !IsGPTImageGenerationModel(requested) {
		return "", &TokenProImageModelError{Message: tokenProImageModelNotImageModel}
	}
	if group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(requested) {
		return "", &TokenProImageModelError{Message: tokenProImageModelNotAllowed}
	}
	if _, err := s.accountRepo.ListSchedulableByGroupID(ctx, groupID); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenProImageModelInternal, err)
	}
	for _, name := range s.GetAvailableModels(ctx, &groupID, PlatformOpenAI) {
		if strings.EqualFold(name, requested) && IsGPTImageGenerationModel(name) {
			return name, nil
		}
	}
	return "", &TokenProImageModelError{Message: tokenProImageModelNotAllowed}
}
