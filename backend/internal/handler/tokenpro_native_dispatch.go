package handler

import (
	"encoding/json"
	"errors"
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
	store, turnKey := service.TokenProImageReceiptStore(c, apiKey)
	item, handled, err := service.BuildTokenProImageReceiptContinuation(body, func(callID string) error {
		return service.VerifyTokenProImageReceipt(c.Request.Context(), store, turnKey, callID)
	})
	if !handled {
		item, err = service.BuildTokenProNativeImageItem(body)
		if err == nil && item["type"] == "function_call" && store != nil && service.TokenProSupportsImageReceipt(body) {
			item, err = service.PrepareTokenProImageReceipt(c.Request.Context(), store, turnKey, body, item)
		}
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, service.ErrImageReceiptNotReady) {
			status = http.StatusServiceUnavailable
		} else if errors.Is(err, service.ErrImageTurnConflict) {
			status = http.StatusConflict
		}
		h.handleStreamingAwareError(c, status, "native_image_dispatch_error", err.Error(), *streamStarted)
		return true
	}
	c.Header("X-TokenPro-Native-Dispatch", "v1")
	if handled || item["type"] == "custom_tool_call" {
		c.Header("X-TokenPro-Image-Receipt", "text-v1")
	}
	h.writeTokenProSyntheticResponse(c, model, item, stream, streamStarted)
	return true
}

// A normal GPT turn has already paid for its planning response and the native
// Images endpoint has already billed the delivered image. When the client sends
// that successful tool result back, finish locally instead of selecting another
// text account and charging a second inference for a fixed acknowledgement.
func (h *OpenAIGatewayHandler) dispatchTokenProCompletedImage(c *gin.Context, model string, body []byte, stream bool, streamStarted *bool) bool {
	if !service.TokenProNativeImageDelivery(c) || !isBareOpenAIResponsesPath(c) || service.IsOpenAIResponsesCompactPath(c) {
		return false
	}
	item, ok := service.BuildTokenProCompletedImageMessage(body)
	if !ok {
		return false
	}
	c.Header("X-TokenPro-Image-Completion", "text-v1")
	h.writeTokenProSyntheticResponse(c, model, item, stream, streamStarted)
	return true
}

func (h *OpenAIGatewayHandler) writeTokenProSyntheticResponse(c *gin.Context, model string, item gin.H, stream bool, streamStarted *bool) {
	if _, exists := item["id"]; !exists {
		item["id"] = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	id := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	response := gin.H{"id": id, "object": "response", "model": model, "status": "completed", "output": []any{item},
		"usage": gin.H{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}}
	c.Header("Cache-Control", "no-store")
	if !stream {
		c.JSON(http.StatusOK, response)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	*streamStarted = true
	for _, event := range []gin.H{{"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response}} {
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return
		}
	}
	c.Writer.Flush()
}
