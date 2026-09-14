package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	tokenProDirectModelPrefix = "tp-g"
	tokenProGroupIDHeader     = "X-TokenPro-Group-Id"
	tokenProImageRouteHeader  = "X-TokenPro-Image-Route"
)

// TokenProDirectRoute decodes the group-qualified model slug emitted by the
// direct Codex client. It restores the public model name before allowlists,
// scheduling, upstream forwarding and billing inspect the request.
func TokenProDirectRoute(stores ...service.TokenProImageTurnStore) gin.HandlerFunc {
	var turnStore service.TokenProImageTurnStore
	if len(stores) > 0 {
		turnStore = stores[0]
	}
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}
		// This is client configuration, never an upstream provider header.
		imageRoute := strings.TrimSpace(c.GetHeader(tokenProImageRouteHeader))
		imageMode := strings.TrimSpace(c.GetHeader("X-TokenPro-Image-Mode"))
		imageTurnID := strings.TrimSpace(c.GetHeader("X-Codex-Image-Turn-Id"))
		imageTurnHeaders := c.Request.Header.Values("X-Codex-Image-Turn-Id")
		c.Request.Header.Del("X-Codex-Image-Turn-Id")
		c.Request.Header.Del(tokenProImageRouteHeader)
		c.Request.Header.Del("X-TokenPro-Image-Mode")
		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || !apiKey.IsGlobal() || c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}

		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(c.GetHeader("Content-Type")))
		if err != nil {
			c.Next()
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status := http.StatusBadRequest
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Cannot read request body"}})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		var groupID int64
		var rewritten []byte
		var routed bool
		// Native Codex image_gen uses a plain image model, independently of the
		// conversation's catalog slug. Only Images endpoints may consume its
		// explicitly configured default image route; text requests must ignore it.
		imageEndpoint := strings.HasSuffix(c.Request.URL.Path, "/images/generations") || strings.HasSuffix(c.Request.URL.Path, "/images/edits")
		turnMode := imageMode == "native-v2"
		if turnMode {
			// Provider-level defaults must never override a turn's explicit choice.
			imageRoute = ""
			if imageEndpoint {
				if len(imageTurnHeaders) != 1 || !validTokenProTurnID(imageTurnID) || turnStore == nil {
					abortImageTurn(c, http.StatusBadRequest, "native_image_turn_missing")
					return
				}
				ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
				route, lookupErr := turnStore.Lookup(ctx, service.TokenProImageTurnKey(apiKey, imageTurnID))
				cancel()
				if lookupErr != nil {
					status := http.StatusServiceUnavailable
					if errors.Is(lookupErr, service.ErrImageTurnMissing) {
						status = http.StatusConflict
					}
					abortImageTurn(c, status, "native_image_turn_unavailable")
					return
				}
				imageRoute = tokenProDirectModelPrefix + strconv.FormatInt(route.GroupID, 10) + "-" + base64.RawURLEncoding.EncodeToString([]byte(route.Model))
				c.Set(service.TokenProImageSessionContextKey, route.ThreadID)
			}
		}
		resolve := func(model string) (int64, string, bool, error) {
			group, public, matched := decodeTokenProDirectModel(model)
			if matched {
				if turnMode && imageEndpoint && model != imageRoute {
					return 0, "", false, service.ErrImageTurnConflict
				}
				return group, public, true, nil
			}
			if strings.HasPrefix(model, tokenProDirectModelPrefix) {
				return 0, "", false, errors.New("malformed model route")
			}
			if !imageEndpoint || imageRoute == "" {
				return 0, "", false, nil
			}
			group, public, matched = decodeTokenProDirectModel(imageRoute)
			if !matched {
				return 0, "", false, errors.New("malformed image route")
			}
			if (imageMode == "native-v1" || turnMode) && model == "gpt-image-2" && strings.HasPrefix(public, "gpt-") && !service.IsGPTImageGenerationModel(public) {
				c.Set(service.TokenProNativeImageDriverContextKey, public)
				return group, model, true, nil
			}
			if strings.HasPrefix(public, "gpt-") && !service.IsGPTImageGenerationModel(public) {
				return 0, "", false, errors.New("text image route requires native delivery mode")
			}
			// The native tool's fixed model is replaced by the user's selected
			// default. Other explicit model names must match the selected public model.
			if model != public && model != "gpt-image-2" {
				return 0, "", false, errors.New("image model does not match configured route")
			}
			return group, public, true, nil
		}
		switch {
		case strings.EqualFold(mediaType, "application/json"):
			if !gjson.ValidBytes(body) {
				err = errors.New("invalid JSON")
				break
			}
			models := 0
			gjson.ParseBytes(body).ForEach(func(key, value gjson.Result) bool {
				if key.String() == "model" {
					models++
				}
				return true
			})
			if models > 1 {
				err = errors.New("duplicate model fields")
				break
			}
			model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
			var publicModel string
			groupID, publicModel, routed, err = resolve(model)
			if err == nil && routed && turnMode && strings.HasSuffix(c.Request.URL.Path, "/responses") && strings.HasPrefix(publicModel, "gpt-") {
				turnID, threadID, metadataErr := tokenProTurnMetadata(c, body)
				if metadataErr != nil || turnStore == nil {
					abortImageTurn(c, http.StatusBadRequest, "native_image_turn_missing")
					return
				}
				c.Set(service.TokenProImageTurnContextKey, &service.TokenProPendingImageTurn{Store: turnStore, TurnID: turnID, ThreadID: threadID, Model: publicModel})
			}
			if err == nil && routed && imageMode == "native-v1" && strings.HasSuffix(c.Request.URL.Path, "/responses") && service.IsGPTImageGenerationModel(publicModel) {
				imageGroup, imageModel, valid := decodeTokenProDirectModel(imageRoute)
				if !valid || imageGroup != groupID || imageModel != publicModel {
					// Native image tools use the provider's default image route.
					// Never silently bill/render a different catalog image selection.
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "native_image_selection_mismatch", "message": "The selected image model differs from the applied native image route. Apply this image model in TokenPro before generating."}})
					return
				}
			}
			if routed {
				rewritten, err = sjson.SetBytes(body, "model", publicModel)
			}
			// A Responses call is routed as one upstream request. Never silently
			// move its text model into a different image tool's group.
			if err == nil && routed && strings.HasSuffix(c.Request.URL.Path, "/responses") {
				for i, tool := range gjson.GetBytes(rewritten, "tools").Array() {
					if tool.Get("type").String() != "image_generation" {
						continue
					}
					toolModel := tool.Get("model").String()
					if !strings.HasPrefix(toolModel, tokenProDirectModelPrefix) {
						continue
					}
					toolGroup, toolPublic, ok := decodeTokenProDirectModel(toolModel)
					if !ok || toolGroup != groupID {
						err = errors.New("image tool route conflicts with response route")
						break
					}
					rewritten, err = sjson.SetBytes(rewritten, "tools."+strconv.Itoa(i)+".model", toolPublic)
					if err != nil {
						break
					}
				}
			}
		case strings.EqualFold(mediaType, "multipart/form-data"):
			groupID, rewritten, routed, err = rewriteTokenProMultipartRoute(body, params["boundary"], resolve)
		default:
			c.Next()
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": "Invalid TokenPro model route",
			}})
			return
		}
		if !routed {
			c.Next()
			return
		}
		group := strconv.FormatInt(groupID, 10)
		if current := strings.TrimSpace(c.GetHeader(tokenProGroupIDHeader)); current != "" && current != group {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": "TokenPro model route does not match the group header",
			}})
			return
		}
		c.Request.Header.Set(tokenProGroupIDHeader, group)
		if (imageMode == "native-v1" || turnMode) && strings.HasSuffix(c.Request.URL.Path, "/responses") {
			// This request has an authenticated global key and a validated
			// explicit model route. Python/unmarked API callers remain unchanged.
			c.Set(service.TokenProNativeImagesContextKey, true)
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
		c.Request.ContentLength = int64(len(rewritten))
		c.Request.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
		c.Next()
	}
}

func abortImageTurn(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": code, "message": "Cannot resolve this turn's exact image model. Start a new turn with the selected model; no alternate route was used."}})
}

func validTokenProTurnID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func tokenProTurnMetadata(c *gin.Context, body []byte) (string, string, error) {
	turn := gjson.GetBytes(body, "client_metadata.turn_id").String()
	thread := gjson.GetBytes(body, "client_metadata.thread_id").String()
	if len(c.Request.Header.Values("X-Codex-Turn-Metadata")) > 1 {
		return "", "", service.ErrImageTurnConflict
	}
	header := c.GetHeader("X-Codex-Turn-Metadata")
	if header != "" {
		if !gjson.Valid(header) {
			return "", "", service.ErrImageTurnConflict
		}
		hTurn, hThread := gjson.Get(header, "turn_id").String(), gjson.Get(header, "thread_id").String()
		if turn != "" && hTurn != "" && turn != hTurn {
			return "", "", service.ErrImageTurnConflict
		}
		if thread != "" && hThread != "" && thread != hThread {
			return "", "", service.ErrImageTurnConflict
		}
		if turn == "" {
			turn = hTurn
		}
		if thread == "" {
			thread = hThread
		}
	}
	if !validTokenProTurnID(turn) || !validTokenProTurnID(thread) {
		return "", "", service.ErrImageTurnMissing
	}
	return turn, thread, nil
}

func rewriteTokenProMultipartRoute(body []byte, boundary string, resolve func(string) (int64, string, bool, error)) (int64, []byte, bool, error) {
	boundary = strings.TrimSpace(boundary)
	if boundary == "" {
		return 0, nil, false, errors.New("missing multipart boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	if err := writer.SetBoundary(boundary); err != nil {
		return 0, nil, false, err
	}

	var routedGroupID int64
	routed := false
	models := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, nil, routed, err
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return 0, nil, routed, err
		}
		if part.FileName() == "" && strings.TrimSpace(part.FormName()) == "model" {
			models++
			if models > 1 {
				return 0, nil, routed, errors.New("duplicate multipart model fields")
			}
			groupID, publicModel, isRouted, routeErr := resolve(strings.TrimSpace(string(data)))
			if routeErr != nil {
				return 0, nil, routed, routeErr
			}
			if isRouted {
				if routed && groupID != routedGroupID {
					return 0, nil, true, errors.New("conflicting TokenPro multipart model routes")
				}
				routed = true
				routedGroupID = groupID
				data = []byte(publicModel)
			}
		}
		destination, err := writer.CreatePart(part.Header)
		if err != nil {
			return 0, nil, routed, err
		}
		if _, err := destination.Write(data); err != nil {
			return 0, nil, routed, err
		}
	}
	if err := writer.Close(); err != nil {
		return 0, nil, routed, err
	}
	if !routed {
		return 0, body, false, nil
	}
	return routedGroupID, output.Bytes(), true, nil
}

func decodeTokenProDirectModel(model string) (int64, string, bool) {
	if !strings.HasPrefix(model, tokenProDirectModelPrefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(model, tokenProDirectModelPrefix)
	separator := strings.IndexByte(rest, '-')
	if separator <= 0 || separator == len(rest)-1 {
		return 0, "", false
	}
	groupID, err := strconv.ParseInt(rest[:separator], 10, 64)
	if err != nil || groupID <= 0 {
		return 0, "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rest[separator+1:])
	if err != nil || !utf8.Valid(decoded) {
		return 0, "", false
	}
	publicModel := strings.TrimSpace(string(decoded))
	if publicModel == "" || len(publicModel) > 512 {
		return 0, "", false
	}
	return groupID, publicModel, true
}
