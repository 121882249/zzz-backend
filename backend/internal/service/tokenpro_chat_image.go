package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

const (
	tokenProChatImageSuccessPrefix = "图片已生成："
	tokenProChatImageRevisedLimit  = 120
	tokenProChatImageLogLimit      = 200
)

var (
	ErrTokenProChatImageInvalidJSON = errors.New("invalid json")
	ErrTokenProChatImagePrompt      = errors.New("missing image prompt")
	ErrTokenProChatImageHistory     = errors.New("full history required")
	ErrTokenProChatImageTooMany     = errors.New("too many image inputs")
	ErrTokenProChatImageUnsupported = errors.New("unsupported image input")
	ErrTokenProChatImageSource      = errors.New("image source unavailable")
	ErrTokenProChatImageNoAccount   = errors.New("no image account")
	ErrTokenProChatImageStorage     = errors.New("image url storage is not configured")
	ErrTokenProChatImageEmpty       = errors.New("image bytes missing")
	ErrTokenProChatImagePublish     = errors.New("image url publish failed")
)

// TokenProChatImageRequest is the latest user turn, reduced to an Images call.
type TokenProChatImageRequest struct {
	Prompt    string
	Edit      bool
	ImageURLs []string
}

// TokenProExplicitNativeImageFlow reports requests that must keep the
// function_call / receipt protocol. A passive missing tool list is not enough:
// the tools array has to contain image_gen.imagegen, or the history has to
// carry one of our native call ids.
func TokenProExplicitNativeImageFlow(body []byte) bool {
	if tokenProHasNativeImageTool(body) {
		return true
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		id := item.Get("call_id").String()
		if strings.HasPrefix(id, "call_tp_pure_") || strings.HasPrefix(id, "call_tp_receipt_") || strings.HasPrefix(id, "call_tp_wait_") {
			return true
		}
	}
	return false
}

// ExtractTokenProChatImage reads the latest user turn. A bare text prompt is a
// generation even when older images exist. Referenced images become an edit;
// if their bytes cannot be resolved, the caller must not generate a new image.
func ExtractTokenProChatImage(body []byte) (*TokenProChatImageRequest, error) {
	if !gjson.ValidBytes(body) {
		return nil, ErrTokenProChatImageInvalidJSON
	}
	if strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()) != "" {
		return nil, ErrTokenProChatImageHistory
	}
	input := gjson.GetBytes(body, "input")
	if input.Type == gjson.String {
		return tokenProChatImageFromText(input.String(), nil)
	}
	if !input.IsArray() {
		return nil, ErrTokenProChatImagePrompt
	}
	items := input.Array()
	lastUser := -1
	for index, item := range items {
		if item.Get("role").String() == "user" {
			lastUser = index
		}
	}
	if lastUser < 0 {
		return nil, ErrTokenProChatImagePrompt
	}
	prompt, _, urls, referenced, err := tokenProLatestUserImage(items[lastUser])
	if err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(prompt)
	if referenced > 5 {
		return nil, ErrTokenProChatImageTooMany
	}
	if prompt == "" {
		return nil, ErrTokenProChatImagePrompt
	}
	if referenced == 0 {
		return &TokenProChatImageRequest{Prompt: prompt}, nil
	}
	urls = uniqueTokenProImagePaths(urls)
	if len(urls) > referenced {
		urls = urls[:referenced]
	}
	if len(urls) < referenced {
		prior := tokenProPriorChatImageURLs(items[:lastUser])
		need := referenced - len(urls)
		if len(prior) < need {
			return nil, ErrTokenProChatImageSource
		}
		urls = append(urls, prior[len(prior)-need:]...)
	}
	return &TokenProChatImageRequest{Prompt: prompt, Edit: true, ImageURLs: urls}, nil
}

func tokenProLatestUserImage(item gjson.Result) (prompt string, paths, urls []string, referenced int, err error) {
	imageInputs := 0
	if item.Get("content").Type == gjson.String {
		prompt = tokenProNativeImagePromptText(item.Get("content").String())
		paths = tokenProImagePaths(item.Get("content").String())
	}
	if item.Get("content").IsArray() {
		for _, content := range item.Get("content").Array() {
			switch content.Get("type").String() {
			case "input_text":
				text := content.Get("text").String()
				paths = append(paths, tokenProImagePaths(text)...)
				if cleaned := tokenProNativeImagePromptText(text); cleaned != "" {
					prompt += cleaned + "\n"
				}
			case "input_image":
				imageInputs++
				if raw := tokenProContentImageURL(content); tokenProFetchableImageURL(raw) {
					urls = append(urls, raw)
				}
			default:
				return "", nil, nil, 0, fmt.Errorf("%w: %s", ErrTokenProChatImageUnsupported, content.Get("type").String())
			}
		}
	}
	for _, path := range paths {
		if tokenProFetchableImageURL(path) {
			urls = append(urls, path)
		}
	}
	referenced = imageInputs
	if pathCount := len(uniqueTokenProImagePaths(paths)); pathCount > referenced {
		referenced = pathCount
	}
	return prompt, paths, urls, referenced, nil
}

func tokenProChatImageFromText(text string, prior []string) (*TokenProChatImageRequest, error) {
	paths := tokenProImagePaths(text)
	prompt := tokenProNativeImagePromptText(text)
	if strings.TrimSpace(prompt) == "" {
		return nil, ErrTokenProChatImagePrompt
	}
	urls := make([]string, 0, len(paths))
	for _, path := range paths {
		if tokenProFetchableImageURL(path) {
			urls = append(urls, path)
		}
	}
	urls = uniqueTokenProImagePaths(urls)
	if len(paths) == 0 {
		return &TokenProChatImageRequest{Prompt: prompt}, nil
	}
	if len(paths) > 5 {
		return nil, ErrTokenProChatImageTooMany
	}
	if len(urls) < len(uniqueTokenProImagePaths(paths)) {
		need := len(uniqueTokenProImagePaths(paths)) - len(urls)
		if len(prior) < need {
			return nil, ErrTokenProChatImageSource
		}
		urls = append(urls, prior[len(prior)-need:]...)
	}
	if len(urls) > 5 {
		urls = urls[:5]
	}
	return &TokenProChatImageRequest{Prompt: prompt, Edit: true, ImageURLs: urls}, nil
}

func tokenProImagePaths(text string) []string {
	matches := tokenProAttachedImagePathPattern.FindAllStringSubmatch(text, -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
			paths = append(paths, strings.TrimSpace(match[1]))
		}
	}
	return paths
}

func tokenProContentImageURL(content gjson.Result) string {
	value := content.Get("image_url")
	if value.Type == gjson.String {
		return strings.TrimSpace(value.String())
	}
	return strings.TrimSpace(value.Get("url").String())
}

func tokenProFetchableImageURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:image/")
}

func tokenProPriorChatImageURLs(items []gjson.Result) []string {
	var urls []string
	for _, item := range items {
		role := item.Get("role").String()
		kind := item.Get("type").String()
		if role != "assistant" && kind != "message" {
			continue
		}
		if item.Get("content").Type == gjson.String {
			urls = append(urls, tokenProIssuedImageURLs(item.Get("content").String())...)
		}
		for _, content := range item.Get("content").Array() {
			if content.Get("type").String() == "output_text" {
				urls = append(urls, tokenProIssuedImageURLs(content.Get("text").String())...)
			}
		}
	}
	return uniqueTokenProImagePaths(urls)
}

func tokenProIssuedImageURLs(text string) []string {
	var urls []string
	for _, match := range tokenProIssuedImagePattern.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			urls = append(urls, tokenProTrimURL(match[1]))
		}
	}
	for _, match := range tokenProHTTPSURLPattern.FindAllString(text, -1) {
		if strings.Contains(match, "/v1/tokenpro/images/") {
			urls = append(urls, tokenProTrimURL(match))
		}
	}
	return urls
}

func tokenProTrimURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,);]>\"'")
}

// BuildTokenProChatImagesPayload is the JSON body accepted by ParseOpenAIImagesRequest.
func BuildTokenProChatImagesPayload(model string, req *TokenProChatImageRequest) (string, []byte, error) {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return "", nil, ErrTokenProChatImagePrompt
	}
	payload := map[string]any{
		"model":           strings.TrimSpace(model),
		"prompt":          req.Prompt,
		"n":               1,
		"response_format": "b64_json",
		"stream":          false,
	}
	endpoint := openAIImagesGenerationsEndpoint
	if req.Edit {
		if len(req.ImageURLs) == 0 {
			return "", nil, ErrTokenProChatImageSource
		}
		images := make([]map[string]string, 0, len(req.ImageURLs))
		for _, imageURL := range req.ImageURLs {
			if !tokenProFetchableImageURL(imageURL) {
				return "", nil, ErrTokenProChatImageSource
			}
			images = append(images, map[string]string{"image_url": imageURL})
		}
		payload["images"] = images
		endpoint = openAIImagesEditsEndpoint
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	return endpoint, body, nil
}

// TokenProChatImageClientError maps request problems to a status and Chinese text.
// The boolean is false when the error should be an assistant message instead.
func TokenProChatImageClientError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, ErrTokenProChatImageInvalidJSON):
		return 400, "请求格式不正确。", true
	case errors.Is(err, ErrTokenProChatImagePrompt):
		return 400, "请先描述要生成的图片。", true
	case errors.Is(err, ErrTokenProChatImageHistory):
		return 400, "请带上完整对话后再生成图片。", true
	case errors.Is(err, ErrTokenProChatImageTooMany):
		return 400, "一次最多参考 5 张图片。", true
	case errors.Is(err, ErrTokenProChatImageUnsupported):
		return 400, "这种图片输入暂时不支持。", true
	case errors.Is(err, ErrTokenProChatImageSource):
		return 400, "这次引用了图片，但没能读取到原图，请重新附上图片后再试。", true
	case errors.Is(err, ErrImageTurnConflict):
		return 409, "上一张图片还在生成或已经完成，请稍后再试。", true
	case errors.Is(err, ErrImageReceiptNotReady), errors.Is(err, ErrImageTurnMissing):
		return 503, "生图状态暂时不可用，请稍后再试。", true
	case errors.Is(err, ErrTokenProChatImageStorage):
		return 503, "图片链接还没配好：请设置 image_storage（并返回 HTTPS 地址），或同时设置 gateway.tokenpro_image_public_base_url 与 gateway.tokenpro_image_url_secret。", true
	default:
		return 0, "", false
	}
}

// TokenProChatImageFailureText is the assistant text for a failed generation.
func TokenProChatImageFailureText(err error) string {
	if err == nil {
		return "这次没能生成图片，请稍后再试。"
	}
	if errors.Is(err, context.Canceled) {
		return "图片生成已取消，请重新发起。"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTokenProChatImageEmpty) {
		if errors.Is(err, ErrTokenProChatImageEmpty) {
			return "图片未能正常返回，请重新生成。"
		}
		return "图片生成超时了，请重新发起。"
	}
	if errors.Is(err, ErrTokenProChatImageNoAccount) {
		return "当前没有可用的生图账号，请稍后再试。"
	}
	if errors.Is(err, ErrTokenProChatImageStorage) || errors.Is(err, ErrTokenProChatImagePublish) {
		return "图片已经生成，但暂时无法给出访问链接，请稍后再试。"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "timed out"), strings.Contains(message, "超时"):
		return "图片生成超时了，请重新发起。"
	case strings.Contains(message, "rate limit"), strings.Contains(message, "429"), strings.Contains(message, "overload"), strings.Contains(message, "busy"), strings.Contains(message, "繁忙"):
		return "生图服务有点忙，请稍后再试。"
	case strings.Contains(message, "content policy"), strings.Contains(message, "moderation"), strings.Contains(message, "safety"), strings.Contains(message, "审核"):
		return "这次请求未通过检查，请调整图片描述后再试。"
	default:
		return "这次没能生成图片，请稍后再试。"
	}
}

// TokenProChatImageSuccessText is the assistant text Codex can display.
// The URL is plain HTTPS text, never a data URI.
func TokenProChatImageSuccessText(imageURL, revised string) string {
	text := tokenProChatImageSuccessPrefix + strings.TrimSpace(imageURL)
	revised = strings.TrimSpace(revised)
	if revised == "" {
		return text
	}
	revised = truncateRunes(revised, tokenProChatImageRevisedLimit)
	return text + "\n调整后的描述：" + revised
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

// tokenProChatImageUpstreamLogHook is optional and used by tests.
var tokenProChatImageUpstreamLogHook func(detail string)

// LogTokenProChatImageUpstreamError records a sanitized upstream failure.
// Only the first 200 characters are kept, and credential query parameters are redacted.
func LogTokenProChatImageUpstreamError(err error) {
	if err == nil {
		return
	}
	detail := tokenProLogSnippet(tokenProChatImageErrorDetail(err))
	if tokenProChatImageUpstreamLogHook != nil {
		tokenProChatImageUpstreamLogHook(detail)
	}
	logger.LegacyPrintf("service.tokenpro_chat_image", "chat image upstream error: %s", detail)
}

func tokenProChatImageErrorDetail(err error) string {
	var imageErr *OpenAIImagesUpstreamError
	if errors.As(err, &imageErr) && strings.TrimSpace(imageErr.Message) != "" {
		return imageErr.Message
	}
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) && len(failover.ResponseBody) > 0 {
		if message := strings.TrimSpace(extractUpstreamErrorMessage(failover.ResponseBody)); message != "" {
			return message
		}
	}
	return err.Error()
}

func tokenProLogSnippet(text string) string {
	text = strings.TrimSpace(sanitizeUpstreamErrorMessage(text))
	text = strings.Join(strings.Fields(text), " ")
	return truncateRunes(text, tokenProChatImageLogLimit)
}

// BeginTokenProChatImageReceipt claims the turn receipt before upstream work.
// A nil store means this request has no receipt context and should proceed.
func BeginTokenProChatImageReceipt(ctx context.Context, store TokenProImageCallStore, turnKey string, body []byte, prompt string, edit bool) (func(bool) error, error) {
	if store == nil || strings.TrimSpace(turnKey) == "" {
		return nil, nil
	}
	action := "generate"
	if edit {
		action = "edit"
	}
	call := TokenProImageCall{
		CallID:      tokenProDispatchID("call_tp_receipt_"),
		RequestHash: TokenProImageReceiptHash(body),
		PromptHash:  TokenProImageReceiptHash([]byte(prompt)),
		Action:      action,
		State:       "pending",
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	prepared, err := store.PrepareCall(ctx, turnKey, call)
	if err != nil {
		return nil, err
	}
	callID, err := store.ClaimCall(ctx, turnKey, prepared.PromptHash, prepared.Action)
	if err != nil {
		return nil, err
	}
	if callID == "" {
		return nil, ErrImageTurnConflict
	}
	return func(success bool) error {
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer finishCancel()
		return store.FinishCall(finishCtx, turnKey, callID, success)
	}, nil
}
