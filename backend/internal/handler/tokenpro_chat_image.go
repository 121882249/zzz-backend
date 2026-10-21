package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type tokenProChatImageGenerated struct {
	Bytes       []byte
	ContentType string
	Revised     string
	Result      *service.OpenAIForwardResult
	Account     *service.Account
}

type tokenProChatImageGenerator func(ctx context.Context, c *gin.Context, apiKey *service.APIKey, model string, prompt *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error)

type tokenProChatImagePublisher func(ctx context.Context, contentType string, data []byte) (string, error)

type tokenProChatImageUsageRecorder func(input *service.OpenAIRecordUsageInput)

// TokenProChatImageDownload serves a signed chat image. The signature is the
// access control; this route is registered outside API-key authentication.
func (h *OpenAIGatewayHandler) TokenProChatImageDownload(c *gin.Context) {
	files := h.tokenProChatImageFiles()
	if files == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	contentType, data, err := files.Open(c.Param("id"), c.Query("exp"), c.Query("sig"))
	if err != nil {
		if errors.Is(err, service.ErrTokenProChatImageExpired) {
			c.String(http.StatusGone, "链接已过期")
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(http.StatusOK, contentType, data)
}

func (h *OpenAIGatewayHandler) dispatchTokenProChatImage(c *gin.Context, apiKey *service.APIKey, model string, body []byte, stream bool, streamStarted *bool) bool {
	if follow, ok := service.ParseTokenProLocalImageFollowUp(body); ok {
		imageURL := follow.URL
		if imageURL == "" {
			imageURL = h.lookupTokenProLocalImageURL(c, apiKey, follow.CallID)
		}
		text := service.TokenProLocalImageAssistantText(follow.Output, imageURL)
		mode := "url-v1"
		if strings.HasPrefix(text, "![") {
			mode = "local-v1"
		}
		var sse *tokenProChatImageSSE
		if stream {
			sse = h.startTokenProChatImageSSE(c, model, streamStarted, mode)
			defer sse.stop()
		}
		h.finishTokenProChatImage(c, sse, model, text, nil, stream, mode)
		return true
	}
	if text, ok := service.TokenProChatImageCheapReply(body); ok {
		var sse *tokenProChatImageSSE
		if stream {
			sse = h.startTokenProChatImageSSE(c, model, streamStarted, "text-v1")
			defer sse.stop()
		}
		h.finishTokenProChatImage(c, sse, model, text, nil, stream, "text-v1")
		return true
	}
	prompt, err := service.ExtractTokenProChatImage(body)
	if err != nil {
		h.writeTokenProChatImageClientError(c, err, *streamStarted)
		return true
	}
	if !h.tokenProChatImageReady() {
		h.writeTokenProChatImageClientError(c, service.ErrTokenProChatImageStorage, *streamStarted)
		return true
	}
	store, turnKey := service.TokenProImageReceiptStore(c, apiKey)
	finishReceipt, err := service.BeginTokenProChatImageReceipt(c.Request.Context(), store, turnKey, body, prompt.Prompt, prompt.Edit)
	if err != nil {
		h.writeTokenProChatImageClientError(c, err, *streamStarted)
		return true
	}
	succeeded := false
	if finishReceipt != nil {
		defer func() { _ = finishReceipt(succeeded) }()
	}

	mode := "url-v1"
	if service.TokenProLocalImageDownloadAvailable(body) {
		mode = "local-v1"
	}
	var sse *tokenProChatImageSSE
	if stream {
		sse = h.startTokenProChatImageSSE(c, model, streamStarted, mode)
		defer sse.stop()
	}
	generated, genErr := h.generateTokenProChatImage(c.Request.Context(), c, apiKey, model, prompt)
	if genErr != nil {
		service.LogTokenProChatImageUpstreamError(genErr)
		h.finishTokenProChatImage(c, sse, model, service.TokenProChatImageFailureText(genErr), nil, stream, mode)
		return true
	}
	if generated == nil || len(generated.Bytes) == 0 {
		h.finishTokenProChatImage(c, sse, model, service.TokenProChatImageFailureText(service.ErrTokenProChatImageEmpty), nil, stream, mode)
		return true
	}
	imageURL, pubErr := h.publishTokenProChatImage(c.Request.Context(), generated.ContentType, generated.Bytes)
	if pubErr != nil || !strings.HasPrefix(imageURL, "https://") || strings.Contains(imageURL, "data:") {
		service.LogTokenProChatImageUpstreamError(pubErr)
		h.finishTokenProChatImage(c, sse, model, service.TokenProChatImageFailureText(service.ErrTokenProChatImagePublish), nil, stream, mode)
		return true
	}
	if generated.Result != nil && generated.Result.ImageCount > 0 {
		h.recordTokenProChatImageUsage(c, apiKey, generated.Account, generated.Result, body)
	}
	if item, ok := service.BuildTokenProLocalImageCall(body, imageURL, tokenProChatImageThreadID(c, body)); ok {
		h.rememberTokenProLocalImage(c, apiKey, item, imageURL)
		h.writeTokenProChatImageOutput(c, sse, model, item, generated.Result, stream, "local-v1")
		succeeded = true
		return true
	}
	text := service.TokenProChatImageSuccessText(imageURL, generated.Revised)
	h.finishTokenProChatImage(c, sse, model, text, generated.Result, stream, "url-v1")
	succeeded = true
	return true
}

func (h *OpenAIGatewayHandler) writeTokenProChatImageClientError(c *gin.Context, err error, streamStarted bool) {
	status, message, ok := service.TokenProChatImageClientError(err)
	if !ok {
		status, message = http.StatusBadGateway, service.TokenProChatImageFailureText(err)
	}
	errType := "invalid_request_error"
	if status == http.StatusConflict {
		errType = "conflict_error"
	} else if status >= 500 {
		errType = "api_error"
	}
	h.handleStreamingAwareError(c, status, errType, message, streamStarted)
}

func (h *OpenAIGatewayHandler) tokenProChatImageReady() bool {
	if h == nil {
		return false
	}
	if h.chatImagePublish != nil {
		return true
	}
	if h.tokenProChatImageObjectStorage() != nil {
		return true
	}
	return h.tokenProChatImageFiles() != nil
}

func (h *OpenAIGatewayHandler) tokenProChatImageKeepaliveInterval() time.Duration {
	if h != nil && h.chatImageKeepalive > 0 {
		return h.chatImageKeepalive
	}
	seconds := 10
	if h != nil && h.cfg != nil && h.cfg.Gateway.TokenProChatImageKeepaliveSeconds > 0 {
		seconds = h.cfg.Gateway.TokenProChatImageKeepaliveSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (h *OpenAIGatewayHandler) publishTokenProChatImage(ctx context.Context, contentType string, data []byte) (string, error) {
	if h != nil && h.chatImagePublish != nil {
		return h.chatImagePublish(ctx, contentType, data)
	}
	if storage := h.tokenProChatImageObjectStorage(); storage != nil {
		imageURL, err := h.saveTokenProChatImageObject(ctx, storage, contentType, data)
		if err == nil && strings.HasPrefix(strings.TrimSpace(imageURL), "https://") {
			return imageURL, nil
		}
		if err != nil {
			service.LogTokenProChatImageUpstreamError(fmt.Errorf("object storage: %w", err))
		}
	}
	files := h.tokenProChatImageFiles()
	if files == nil {
		return "", service.ErrTokenProChatImageStorage
	}
	return files.Save(contentType, data)
}

func (h *OpenAIGatewayHandler) saveTokenProChatImageObject(ctx context.Context, storage service.ImageStorage, contentType string, data []byte) (string, error) {
	prefix := "images/"
	if h != nil && h.cfg != nil && strings.TrimSpace(h.cfg.ImageStorage.Prefix) != "" {
		prefix = strings.TrimSpace(h.cfg.ImageStorage.Prefix)
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}
	ext := ".png"
	switch serviceContentType(contentType) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	key := prefix + "tokenpro-chat/" + strings.ReplaceAll(uuid.NewString(), "-", "") + ext
	return storage.Save(ctx, key, serviceContentType(contentType), data)
}

func serviceContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return contentType
	default:
		return "image/png"
	}
}

func (h *OpenAIGatewayHandler) tokenProChatImageObjectStorage() service.ImageStorage {
	if h == nil || h.cfg == nil || !h.cfg.ImageStorage.Active() || h.chatImageStorageFactory == nil {
		return nil
	}
	h.chatImageObjectOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		storage, err := h.chatImageStorageFactory(ctx, &h.cfg.ImageStorage)
		if err != nil {
			h.chatImageObjectErr = err
			service.LogTokenProChatImageUpstreamError(fmt.Errorf("object storage init: %w", err))
			return
		}
		h.chatImageObjectStorage = storage
	})
	return h.chatImageObjectStorage
}

func (h *OpenAIGatewayHandler) tokenProChatImageFiles() *service.TokenProChatImageFiles {
	if h == nil {
		return nil
	}
	h.chatImageFilesOnce.Do(func() {
		cfg := service.TokenProChatImageFileConfig{TTL: 7 * 24 * time.Hour, Dir: "data/tokenpro-chat-images"}
		if h.cfg != nil {
			if dir := strings.TrimSpace(h.cfg.Gateway.TokenProImageStoreDir); dir != "" {
				cfg.Dir = dir
			}
			cfg.Secret = h.cfg.Gateway.TokenProImageURLSecret
			cfg.PublicBase = h.cfg.Gateway.TokenProImagePublicBaseURL
			if h.cfg.Gateway.TokenProImageURLTTLHours > 0 {
				cfg.TTL = time.Duration(h.cfg.Gateway.TokenProImageURLTTLHours) * time.Hour
			}
		}
		h.chatImageFiles = service.NewTokenProChatImageFiles(cfg)
	})
	return h.chatImageFiles
}

func (h *OpenAIGatewayHandler) recordTokenProChatImageUsage(c *gin.Context, apiKey *service.APIKey, account *service.Account, result *service.OpenAIForwardResult, body []byte) {
	if result == nil || result.ImageCount <= 0 || apiKey == nil || account == nil {
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	upstreamModel := result.UpstreamModel
	input := &service.OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             apiKey,
		User:               apiKey.User,
		Account:            account,
		Subscription:       subscription,
		InboundEndpoint:    GetInboundEndpoint(c),
		UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
		UserAgent:          c.GetHeader("User-Agent"),
		IPAddress:          ip.GetClientIP(c),
		RequestPayloadHash: service.HashUsageRequestPayload(body),
		APIKeyService:      h.apiKeyService,
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
		SessionID:          service.ExtractClientSessionID(c),
		ChannelUsageFields: clientRequestedUsageFields(c, service.ChannelMappingResult{}, result.Model, upstreamModel),
	}
	if h.chatImageRecordUsage != nil {
		h.chatImageRecordUsage(input)
		return
	}
	subjectUserID := int64(0)
	if apiKey.User != nil {
		subjectUserID = apiKey.User.ID
	}
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, input); err != nil {
			requestLogger(c, "handler.tokenpro_chat_image", zap.Int64("user_id", subjectUserID), zap.Int64("api_key_id", apiKey.ID)).Error("tokenpro.chat_image.record_usage_failed", zap.Error(err))
		}
	})
}

func (h *OpenAIGatewayHandler) generateTokenProChatImage(ctx context.Context, c *gin.Context, apiKey *service.APIKey, model string, prompt *service.TokenProChatImageRequest) (*tokenProChatImageGenerated, error) {
	if h != nil && h.chatImageGenerate != nil {
		return h.chatImageGenerate(ctx, c, apiKey, model, prompt)
	}
	if h == nil || h.gatewayService == nil {
		return nil, fmt.Errorf("image gateway is not configured")
	}
	endpoint, body, err := service.BuildTokenProChatImagesPayload(model, prompt)
	if err != nil {
		return nil, err
	}
	routingModel := model
	if resolved, ok := service.ResolvedUpstreamModelFromContext(ctx); ok && strings.TrimSpace(resolved) != "" {
		routingModel = resolved
	}
	requestCtx := service.WithOpenAIImagesEndpoint(service.WithOpenAIImageGenerationIntent(ctx))
	requestCtx = service.WithTokenProImagesAuthorization(requestCtx, c, apiKey, model)
	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(requestCtx, apiKey.GroupID, routingModel)
	capture, _ := cloneGinForImages(c, endpoint, body)
	parsed, err := h.gatewayService.ParseOpenAIImagesRequest(capture, body)
	if err != nil {
		return nil, err
	}
	parsed.Stream = false

	maxSwitches := h.maxAccountSwitches
	if maxSwitches <= 0 {
		maxSwitches = 3
	}
	failed := map[int64]struct{}{}
	sameAccountRetries := map[int64]int{}
	switchCount := 0
	vetoes := 0
	var lastFailover *service.UpstreamFailoverError
	reqLog := requestLogger(c, "handler.tokenpro_chat_image", zap.String("model", model))
	for {
		if err := requestCtx.Err(); err != nil {
			return nil, err
		}
		selection, _, selErr := h.gatewayService.SelectAccountWithSchedulerForImages(
			requestCtx,
			apiKey.GroupID,
			sessionHash,
			routingModel,
			failed,
			parsed.RequiredCapabilityForModel(channelMapping.MappedModel),
		)
		if selErr != nil || selection == nil || selection.Account == nil {
			if len(failed) == 0 {
				if selErr != nil {
					return nil, fmt.Errorf("%w: %v", service.ErrTokenProChatImageNoAccount, selErr)
				}
				return nil, service.ErrTokenProChatImageNoAccount
			}
			if lastFailover != nil {
				return nil, lastFailover
			}
			return nil, service.ErrTokenProChatImageNoAccount
		}
		account := selection.Account
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		slotCapture, _ := cloneGinForImages(c, endpoint, body)
		streamStarted := false
		release, slot := h.acquireResponsesAccountSlot(slotCapture, apiKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slot == openAISlotAcquireProfitVetoed {
			failed[account.ID] = struct{}{}
			vetoes++
			if vetoes > 10 {
				return nil, service.ErrTokenProChatImageNoAccount
			}
			continue
		}
		if slot != openAISlotAcquireOK {
			return nil, service.ErrTokenProChatImageNoAccount
		}
		forwardCapture, recorder := cloneGinForImages(c, endpoint, body)
		result, forwardErr := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if release != nil {
					release()
				}
			}()
			return h.gatewayService.ForwardImages(requestCtx, forwardCapture, account, body, parsed, channelMapping.MappedModel)
		}()
		if forwardErr != nil {
			var failover *service.UpstreamFailoverError
			if errors.As(forwardErr, &failover) {
				h.gatewayService.ReportOpenAIAccountScheduleResult(account, routingModel, false, nil, forwardErr)
				if failover.RetryableOnSameAccount && sameAccountRetryAllowed(failover, sameAccountRetries[account.ID], effectiveSameAccountRetryLimit(failover, account)) {
					sameAccountRetries[account.ID]++
					delay := sameAccountRetryDelayFor(failover, sameAccountRetries[account.ID])
					select {
					case <-requestCtx.Done():
						return nil, requestCtx.Err()
					case <-time.After(delay):
					}
					continue
				}
				failed[account.ID] = struct{}{}
				lastFailover = failover
				if switchCount >= maxSwitches {
					return nil, failover
				}
				switchCount++
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, routingModel, false, nil, forwardErr)
			return nil, forwardErr
		}
		h.gatewayService.ReportOpenAIAccountScheduleResult(account, routingModel, true, nil)
		imageBytes, contentType, revised, parseErr := parseTokenProChatImageResponse(requestCtx, recorder.Body.Bytes())
		if parseErr != nil {
			return &tokenProChatImageGenerated{Result: result, Account: account, Revised: revised}, parseErr
		}
		if result != nil && result.ImageCount == 0 {
			result.ImageCount = 1
		}
		if result != nil && result.Model == "" {
			result.Model = model
		}
		return &tokenProChatImageGenerated{Bytes: imageBytes, ContentType: contentType, Revised: revised, Result: result, Account: account}, nil
	}
}

func parseTokenProChatImageResponse(ctx context.Context, body []byte) ([]byte, string, string, error) {
	revised := strings.TrimSpace(gjson.GetBytes(body, "data.0.revised_prompt").String())
	if encoded := strings.TrimSpace(gjson.GetBytes(body, "data.0.b64_json").String()); encoded != "" {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, "", revised, fmt.Errorf("%w: %v", service.ErrTokenProChatImageEmpty, err)
		}
		return data, http.DetectContentType(data), revised, nil
	}
	if imageURL := strings.TrimSpace(gjson.GetBytes(body, "data.0.url").String()); strings.HasPrefix(imageURL, "https://") {
		data, contentType, err := downloadTokenProChatImage(ctx, imageURL)
		if err != nil {
			return nil, "", revised, err
		}
		return data, contentType, revised, nil
	}
	return nil, "", revised, service.ErrTokenProChatImageEmpty
}

func downloadTokenProChatImage(ctx context.Context, imageURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("image redirect is not https")
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many image redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("image download status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > 20<<20 {
		return nil, "", service.ErrTokenProChatImageEmpty
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

func cloneGinForImages(parent *gin.Context, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if parent != nil && parent.Request != nil {
		req = req.WithContext(parent.Request.Context())
	}
	c.Request = req
	if parent != nil {
		for key, value := range parent.Keys {
			c.Set(key, value)
		}
	}
	return c, recorder
}

type tokenProChatImageSSE struct {
	mu       sync.Mutex
	c        *gin.Context
	stopOnce sync.Once
	stopCh   chan struct{}
	done     sync.WaitGroup
	id       string
	model    string
}

func (h *OpenAIGatewayHandler) startTokenProChatImageSSE(c *gin.Context, model string, streamStarted *bool, mode string) *tokenProChatImageSSE {
	if mode == "" {
		mode = "url-v1"
	}
	id := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	writer := &tokenProChatImageSSE{c: c, stopCh: make(chan struct{}), id: id, model: model}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Header("X-TokenPro-Chat-Image", mode)
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()
	if streamStarted != nil {
		*streamStarted = true
	}
	_ = writer.write(gin.H{"type": "response.created", "response": gin.H{
		"id": id, "object": "response", "created_at": time.Now().Unix(), "model": model, "status": "in_progress", "output": []any{},
	}})
	_ = writer.write(gin.H{"type": "response.in_progress"})
	writer.done.Add(1)
	interval := h.tokenProChatImageKeepaliveInterval()
	go func() {
		defer writer.done.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-writer.stopCh:
				return
			case <-ticker.C:
				if err := writer.write(gin.H{"type": "response.in_progress"}); err != nil {
					return
				}
			}
		}
	}()
	return writer
}

func (w *tokenProChatImageSSE) stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.done.Wait()
	})
}

func (w *tokenProChatImageSSE) write(event gin.H) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.c.Writer, "data: %s\n\n", data); err != nil {
		return err
	}
	w.c.Writer.Flush()
	return nil
}

func (h *OpenAIGatewayHandler) finishTokenProChatImage(c *gin.Context, sse *tokenProChatImageSSE, model, text string, result *service.OpenAIForwardResult, stream bool, mode string) {
	h.writeTokenProChatImageOutput(c, sse, model, tokenProChatImageMessage(text), result, stream, mode)
}

func tokenProChatImageMessage(text string) map[string]any {
	return map[string]any{
		"type": "message", "id": "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"role": "assistant", "status": "completed",
		"content": []any{gin.H{"type": "output_text", "text": text, "annotations": []any{}}},
	}
}

func (h *OpenAIGatewayHandler) writeTokenProChatImageOutput(c *gin.Context, sse *tokenProChatImageSSE, model string, item map[string]any, result *service.OpenAIForwardResult, stream bool, mode string) {
	if sse != nil {
		sse.stop()
	}
	if mode == "" {
		mode = "url-v1"
	}
	usage := tokenProChatImageUsage(result)
	id := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if sse != nil {
		id = sse.id
	}
	response := gin.H{
		"id": id, "object": "response", "created_at": time.Now().Unix(), "model": model,
		"status": "completed", "output": []any{item}, "usage": usage,
	}
	if !stream || sse == nil {
		c.Header("Cache-Control", "no-store")
		c.Header("X-TokenPro-Chat-Image", mode)
		c.JSON(http.StatusOK, response)
		return
	}
	if itemType, ok := item["type"].(string); ok && itemType == "function_call" {
		added := make(map[string]any, len(item))
		for key, value := range item {
			added[key] = value
		}
		added["arguments"] = ""
		_ = sse.write(gin.H{"type": "response.output_item.added", "output_index": 0, "item": added})
		_ = sse.write(gin.H{
			"type": "response.function_call_arguments.delta", "output_index": 0,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"delta": item["arguments"],
		})
		_ = sse.write(gin.H{
			"type": "response.function_call_arguments.done", "output_index": 0,
			"item_id": item["id"], "call_id": item["call_id"], "name": item["name"],
			"arguments": item["arguments"],
		})
	} else {
		_ = sse.write(gin.H{"type": "response.output_item.added", "output_index": 0, "item": item})
	}
	_ = sse.write(gin.H{"type": "response.output_item.done", "output_index": 0, "item": item})
	_ = sse.write(gin.H{"type": "response.completed", "response": response})
}

func tokenProChatImageThreadID(c *gin.Context, body []byte) string {
	if c != nil {
		v, ok := c.Get(service.TokenProImageTurnContextKey)
		if ok {
			pending, pendingOK := v.(*service.TokenProPendingImageTurn)
			if pendingOK && pending != nil && strings.TrimSpace(pending.ThreadID) != "" {
				return pending.ThreadID
			}
		}
	}
	return service.TokenProRequestThreadID(body)
}

func (h *OpenAIGatewayHandler) tokenProLocalImageStore(c *gin.Context) service.TokenProLocalImageStore {
	if c == nil {
		return nil
	}
	v, ok := c.Get(service.TokenProImageTurnContextKey)
	if !ok {
		return nil
	}
	pending, ok := v.(*service.TokenProPendingImageTurn)
	if !ok || pending == nil || pending.Store == nil {
		return nil
	}
	store, ok := pending.Store.(service.TokenProLocalImageStore)
	if !ok {
		return nil
	}
	return store
}

func (h *OpenAIGatewayHandler) lookupTokenProLocalImageURL(c *gin.Context, apiKey *service.APIKey, callID string) string {
	store := h.tokenProLocalImageStore(c)
	if store == nil || apiKey == nil || c == nil || c.Request == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	record, err := store.LookupLocalImage(ctx, service.TokenProLocalImageKey(apiKey, callID))
	if err != nil {
		return ""
	}
	return record.URL
}

func (h *OpenAIGatewayHandler) rememberTokenProLocalImage(c *gin.Context, apiKey *service.APIKey, item map[string]any, imageURL string) {
	store := h.tokenProLocalImageStore(c)
	callID, ok := item["call_id"].(string)
	if store == nil || !ok || callID == "" || apiKey == nil || c == nil || c.Request == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := store.RememberLocalImage(ctx, service.TokenProLocalImageKey(apiKey, callID), service.TokenProLocalImageRecord{URL: imageURL}); err != nil {
		requestLogger(c, "handler.tokenpro_chat_image", zap.Int64("api_key_id", apiKey.ID)).Warn("tokenpro.chat_image.local_remember_failed", zap.Error(err))
	}
}

func tokenProChatImageUsage(result *service.OpenAIForwardResult) gin.H {
	inputTokens, outputTokens := 0, 0
	if result != nil {
		inputTokens = result.Usage.InputTokens
		outputTokens = result.Usage.OutputTokens
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	return gin.H{"input_tokens": inputTokens, "output_tokens": outputTokens, "total_tokens": inputTokens + outputTokens}
}
