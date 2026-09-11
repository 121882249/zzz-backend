package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	xdraw "golang.org/x/image/draw"
)

const (
	tokenProImageDisplayContextKey = "tokenpro_codex_image_display"
	tokenProImageModelContextKey   = "tokenpro_preferred_image_model"
	// Chromium refuses very long data URLs. Keep enough headroom below its
	// practical 2 MB boundary for the data URL Codex builds from this Base64.
	tokenProCodexImageResultMaxChars = 1_750_000
)

// consumeTokenProPreferredImageModel removes TokenPro-only routing metadata
// from the upstream headers while retaining the selection across account
// failover attempts that reuse the same Gin context.
func consumeTokenProPreferredImageModel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	preferred := strings.TrimSpace(c.GetHeader(tokenProImageModelHeader))
	c.Request.Header.Del(tokenProImageModelHeader)
	if preferred != "" {
		c.Set(tokenProImageModelContextKey, preferred)
		return preferred
	}
	stored, exists := c.Get(tokenProImageModelContextKey)
	if !exists {
		return ""
	}
	preferred, _ = stored.(string)
	return strings.TrimSpace(preferred)
}

// normalizeTokenProCodexImageDisplayPayload makes oversized generated images
// displayable in Codex. The upstream image is preserved whenever it already
// fits; only oversized results receive a JPEG display copy.
func normalizeTokenProCodexImageDisplayPayload(c *gin.Context, data []byte) ([]byte, bool) {
	if c == nil || len(data) == 0 || !gjson.ValidBytes(data) {
		return data, false
	}
	if enabled, exists := c.Get(tokenProImageDisplayContextKey); !exists || enabled != true {
		return data, false
	}

	eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
	paths := make([]string, 0, 2)
	switch eventType {
	case "response.output_item.done":
		if gjson.GetBytes(data, "item.type").String() == "image_generation_call" {
			paths = append(paths, "item")
		}
	case "response.completed", "response.done":
		for index, item := range gjson.GetBytes(data, "response.output").Array() {
			if item.Get("type").String() == "image_generation_call" {
				paths = append(paths, "response.output."+strconv.Itoa(index))
			}
		}
	default:
		return data, false
	}

	updated := data
	changed := false
	for _, path := range paths {
		result := gjson.GetBytes(updated, path+".result").String()
		if result == "" {
			continue
		}

		displayResult, outputFormat, normalized := normalizeTokenProCodexImageResult(result)
		if len(displayResult) > tokenProCodexImageResultMaxChars {
			var ok bool
			displayResult, ok = buildTokenProCodexImageDisplayResult(displayResult)
			if !ok {
				continue
			}
			outputFormat = "jpeg"
			normalized = true
		}
		if !normalized {
			continue
		}
		next, err := sjson.SetBytes(updated, path+".result", displayResult)
		if err != nil {
			continue
		}
		if outputFormat != "" {
			next, err = sjson.SetBytes(next, path+".output_format", outputFormat)
			if err != nil {
				continue
			}
		}
		updated = next
		changed = true
	}
	return updated, changed
}

// normalizeTokenProCodexImageResult strips data-URL framing from otherwise
// valid Responses image output. Codex decodes image_generation_call.result as
// raw Base64; including "data:image/...;base64," makes even a small image render
// as a broken attachment and poisons the next turn's image input.
func normalizeTokenProCodexImageResult(result string) (encoded, outputFormat string, changed bool) {
	trimmed := strings.TrimSpace(result)
	comma := strings.IndexByte(trimmed, ',')
	if comma < 0 {
		return trimmed, "", trimmed != result
	}
	prefix := strings.ToLower(trimmed[:comma])
	if !strings.HasPrefix(prefix, "data:image/") || !strings.Contains(prefix, ";base64") {
		return trimmed, "", trimmed != result
	}

	format := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(prefix, "data:image/"), ";base64"))
	if semicolon := strings.IndexByte(format, ';'); semicolon >= 0 {
		format = format[:semicolon]
	}
	if format == "jpg" {
		format = "jpeg"
	}
	return strings.TrimSpace(trimmed[comma+1:]), format, true
}

func buildTokenProCodexImageDisplayResult(result string) (string, bool) {
	encoded, _, _ := normalizeTokenProCodexImageResult(result)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", false
	}

	current := flattenTokenProImage(source)
	for attempt := 0; attempt < 7; attempt++ {
		quality := 88 - attempt*8
		if quality < 55 {
			quality = 55
		}
		var output bytes.Buffer
		if err := jpeg.Encode(&output, current, &jpeg.Options{Quality: quality}); err != nil {
			return "", false
		}
		encodedResult := base64.StdEncoding.EncodeToString(output.Bytes())
		if len(encodedResult) <= tokenProCodexImageResultMaxChars {
			// Responses image_generation_call.result is raw Base64. Returning a
			// data: URI here makes Codex treat the prefix as image bytes and reject
			// the next turn with "Invalid image in your last message".
			return encodedResult, true
		}

		ratio := math.Sqrt(float64(tokenProCodexImageResultMaxChars)/float64(len(encodedResult))) * 0.90
		if ratio >= 1 {
			ratio = 0.88
		}
		bounds := current.Bounds()
		width := max(1, int(float64(bounds.Dx())*ratio))
		height := max(1, int(float64(bounds.Dy())*ratio))
		resized := image.NewRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), current, bounds, xdraw.Over, nil)
		current = resized
	}
	return "", false
}

func flattenTokenProImage(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	flattened := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	stdDraw.Draw(flattened, flattened.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stdDraw.Src)
	stdDraw.Draw(flattened, flattened.Bounds(), source, bounds.Min, stdDraw.Over)
	return flattened
}
