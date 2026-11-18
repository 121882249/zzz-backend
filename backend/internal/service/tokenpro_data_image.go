package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"

	_ "image/gif"
	_ "image/png"
)

const (
	tokenProDataImageMaxEdge   = 1024
	tokenProDataImageQuality   = 80
	tokenProDataImageMaxBytes  = 384 * 1024
	tokenProDataImageMarker    = "[已生成图片]"
	TokenProCodexImageModeData = "data-v1"
	TokenProCodexImageModeURL  = "url-v1"
	// TokenProCodexDataImageContextKey is set by Forward from the gateway
	// config. Missing means data-v1, the default.
	TokenProCodexDataImageContextKey = "tokenpro_codex_data_image"
)

// tokenProDataImageMarkdown matches only the markdown image we embed for
// official Codex. Structured input_image parts do not use this syntax.
var tokenProDataImageMarkdown = regexp.MustCompile(`!\[[^\]\n]{0,200}\]\(data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=\r\n]+\)`)

// TokenProCodexDataImageEnabled reports whether official Codex should embed a
// compressed JPEG data URI. The default is data-v1. url-v1 turns it off.
func tokenProCodexDataImageFromConfig(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", TokenProCodexImageModeData:
		return true
	case TokenProCodexImageModeURL:
		return false
	default:
		return true
	}
}

func TokenProCodexDataImageEnabled(c *gin.Context) bool {
	if c == nil {
		return true
	}
	value, ok := c.Get(TokenProCodexDataImageContextKey)
	if !ok {
		return true
	}
	enabled, ok := value.(bool)
	return ok && enabled
}

// TokenProOfficialCodexDataImageText is the data-v1 assistant message.
// The markdown image is a compressed JPEG. The link still points at the
// original signed download.
func TokenProOfficialCodexDataImageText(dataURI, imageURL, revised string) string {
	url := strings.TrimSpace(imageURL)
	text := tokenProChatImageSuccessPrefix + "\n\n![](" + strings.TrimSpace(dataURI) + ")\n\n[原图链接](" + url + ")"
	revised = strings.TrimSpace(revised)
	if revised == "" {
		return text
	}
	revised = truncateRunes(revised, tokenProChatImageRevisedLimit)
	return text + "\n调整后的描述：" + revised
}

// BuildTokenProCodexDataImageURI shrinks one image for inline markdown.
// The long edge is about 1024px and the JPEG is quality 80, capped at a few
// hundred kilobytes. Decode or encode failure returns false so the caller
// can fall back to url-v1.
func BuildTokenProCodexDataImageURI(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || src == nil {
		return "", false
	}
	bounds := src.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return "", false
	}
	for _, edge := range []int{tokenProDataImageMaxEdge, 768, 512, 384} {
		for _, quality := range []int{tokenProDataImageQuality, 60, 40, 25} {
			jpegBytes, ok := encodeTokenProPreviewJPEG(src, edge, quality)
			if !ok || len(jpegBytes) == 0 || len(jpegBytes) > tokenProDataImageMaxBytes {
				continue
			}
			return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes), true
		}
	}
	return "", false
}

func encodeTokenProPreviewJPEG(src image.Image, maxEdge, quality int) ([]byte, bool) {
	scaled := scaleTokenProLongEdge(src, maxEdge)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func scaleTokenProLongEdge(src image.Image, maxEdge int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	long := width
	if height > long {
		long = height
	}
	if long <= maxEdge || maxEdge <= 0 {
		return src
	}
	newW, newH := maxEdge, 1
	if width >= height {
		newH = height * maxEdge / width
		if newH < 1 {
			newH = 1
		}
	} else {
		newW = width * maxEdge / height
		if newW < 1 {
			newW = 1
		}
		newH = maxEdge
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}

// StripTokenProDataImageMarkdown replaces our embedded data-URI markdown
// with a short placeholder before the body is forwarded upstream. User
// input_image parts are left alone because they are not this markdown.
func StripTokenProDataImageMarkdown(body []byte) ([]byte, bool) {
	if len(body) == 0 || !bytes.Contains(body, []byte("](data:image/")) {
		return body, false
	}
	out := tokenProDataImageMarkdown.ReplaceAll(body, []byte(tokenProDataImageMarker))
	if bytes.Equal(out, body) {
		return body, false
	}
	return out, true
}
