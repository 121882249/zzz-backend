package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Called only after Responses authentication, group resolution, moderation,
// image permission, concurrency, billing eligibility and session-block checks.
// Dispatch has no upstream inference and no usage charge. The subsequent native
// Images request is independently authorized, scheduled and billed as usual.
func (h *OpenAIGatewayHandler) dispatchTokenProNativeImage(c *gin.Context, apiKey *service.APIKey, model, platform string, body []byte, stream bool, streamStarted *bool) bool {
	if !service.TokenProNativeImages(c) || !service.IsGPTImageGenerationModel(model) ||
		!isBareOpenAIResponsesPath(c) || service.IsOpenAIResponsesCompactPath(c) ||
		platform != service.PlatformOpenAI || apiKey == nil || !apiKey.IsGlobal() ||
		!service.TokenProPureImageGroup(apiKey.Group) {
		return false
	}
	item, err := service.BuildTokenProNativeImageItem(body)
	if err != nil {
		h.handleStreamingAwareError(c, http.StatusBadRequest, "native_image_dispatch_error", err.Error(), *streamStarted)
		return true
	}
	id := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	response := gin.H{"id": id, "object": "response", "model": model, "status": "completed", "output": []any{item},
		"usage": gin.H{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}}
	c.Header("Cache-Control", "no-store")
	c.Header("X-TokenPro-Native-Dispatch", "v1")
	if !stream {
		c.JSON(http.StatusOK, response)
		return true
	}
	c.Header("Content-Type", "text/event-stream")
	*streamStarted = true
	for _, event := range []gin.H{{"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response}} {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return true
		}
	}
	c.Writer.Flush()
	return true
}
