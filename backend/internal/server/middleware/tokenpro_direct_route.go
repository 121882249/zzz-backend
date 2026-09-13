package middleware

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	tokenProDirectModelPrefix = "tp-g"
	tokenProGroupIDHeader     = "X-TokenPro-Group-Id"
)

// TokenProDirectRoute decodes the group-qualified model slug emitted by the
// direct Codex client. It restores the public model name before allowlists,
// scheduling, upstream forwarding and billing inspect the request.
func TokenProDirectRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
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
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		var groupID int64
		var rewritten []byte
		var routed bool
		switch {
		case strings.EqualFold(mediaType, "application/json"):
			model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
			var publicModel string
			groupID, publicModel, routed = decodeTokenProDirectModel(model)
			if routed {
				rewritten, err = sjson.SetBytes(body, "model", publicModel)
			}
		case strings.EqualFold(mediaType, "multipart/form-data"):
			groupID, rewritten, routed, err = rewriteTokenProMultipartModel(body, params["boundary"])
		default:
			c.Next()
			return
		}
		if !routed {
			c.Next()
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": "Invalid TokenPro model route",
			}})
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
		c.Request.Body = io.NopCloser(bytes.NewReader(rewritten))
		c.Request.ContentLength = int64(len(rewritten))
		c.Request.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
		c.Next()
	}
}

// rewriteTokenProMultipartModel restores the public model field used by image
// edits while preserving uploaded files and the caller's multipart boundary.
func rewriteTokenProMultipartModel(body []byte, boundary string) (int64, []byte, bool, error) {
	boundary = strings.TrimSpace(boundary)
	if boundary == "" {
		return 0, nil, false, nil
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var output bytes.Buffer
	writer := multipart.NewWriter(&output)
	if err := writer.SetBoundary(boundary); err != nil {
		return 0, nil, false, err
	}

	var routedGroupID int64
	routed := false
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
			groupID, publicModel, isRouted := decodeTokenProDirectModel(strings.TrimSpace(string(data)))
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
