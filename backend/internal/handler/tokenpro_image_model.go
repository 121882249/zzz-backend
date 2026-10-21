package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const tokenProImageModelHeader = "X-TokenPro-Image-Model"

// applyTokenProSelectedImageModel honors X-TokenPro-Image-Model on the Images
// endpoints. Absent header leaves the body alone. A present header must name
// an image model allowed by the X-TokenPro-Group-Id image group; the body is
// rewritten to that catalog name before global-key resolution, scheduling and
// billing. The header is consumed so it is never forwarded upstream.
func (h *OpenAIGatewayHandler) applyTokenProSelectedImageModel(c *gin.Context, body []byte) ([]byte, *service.OpenAIImagesRequest, error) {
	selected := ""
	if c != nil && c.Request != nil {
		selected = strings.TrimSpace(c.GetHeader(tokenProImageModelHeader))
		c.Request.Header.Del(tokenProImageModelHeader)
	}
	if selected == "" {
		parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
		return body, parsed, err
	}
	groupID, err := tokenProSelectedImageGroupID(c)
	if err != nil {
		return nil, nil, err
	}
	if h == nil || h.globalGroupResolver == nil {
		return nil, nil, fmt.Errorf("%w: resolver unavailable", service.ErrTokenProImageModelInternal)
	}
	canonical, err := h.globalGroupResolver.CanonicalTokenProImageModel(c.Request.Context(), groupID, selected)
	if err != nil {
		return nil, nil, err
	}
	contentType := ""
	if c != nil {
		contentType = c.GetHeader("Content-Type")
	}
	rewritten, rewrittenType, err := service.RewriteOpenAIImagesModel(body, contentType, canonical)
	if err != nil {
		return nil, nil, &service.TokenProImageModelError{Message: "无法写入所选生图模型，没有改用其他模型。"}
	}
	if c != nil && c.Request != nil {
		c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
		c.Request.ContentLength = int64(len(rewritten))
		if strings.TrimSpace(rewrittenType) != "" {
			c.Request.Header.Set("Content-Type", rewrittenType)
		}
		c.Request.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	}
	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, rewritten)
	if err != nil {
		return nil, nil, err
	}
	return rewritten, parsed, nil
}

func tokenProSelectedImageGroupID(c *gin.Context) (int64, error) {
	raw := ""
	if c != nil {
		raw = strings.TrimSpace(c.GetHeader(tokenProGroupIDHeader))
	}
	groupID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || groupID <= 0 {
		return 0, &service.TokenProImageModelError{Message: "生图请求缺少有效分组，请重新连接 TokenPro。没有改用其他模型。"}
	}
	return groupID, nil
}

func (h *OpenAIGatewayHandler) respondTokenProImageModelError(c *gin.Context, err error) {
	var modelErr *service.TokenProImageModelError
	if errors.As(err, &modelErr) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", modelErr.Error())
		return
	}
	if errors.Is(err, service.ErrTokenProImageModelInternal) {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "无法确认生图模型，没有改用其他模型。")
		return
	}
	h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
}
