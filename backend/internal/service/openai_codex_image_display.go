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
	// Chromium refuses very long data URLs. Keep enough headroom below its
	// practical 2 MB boundary for the SSE and JSON framing around the image.
	tokenProCodexImageResultMaxChars = 1_750_000
)

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
		if result == "" || len(result) <= tokenProCodexImageResultMaxChars {
			continue
		}
		displayResult, ok := buildTokenProCodexImageDisplayResult(result)
		if !ok {
			continue
		}
		next, err := sjson.SetBytes(updated, path+".result", displayResult)
		if err != nil {
			continue
		}
		next, err = sjson.SetBytes(next, path+".output_format", "jpeg")
		if err != nil {
			continue
		}
		updated = next
		changed = true
	}
	return updated, changed
}

func buildTokenProCodexImageDisplayResult(result string) (string, bool) {
	encoded := strings.TrimSpace(result)
	if comma := strings.IndexByte(encoded, ','); strings.HasPrefix(encoded, "data:image/") && comma >= 0 {
		encoded = encoded[comma+1:]
	}
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
		uri := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(output.Bytes())
		if len(uri) <= tokenProCodexImageResultMaxChars {
			return uri, true
		}

		ratio := math.Sqrt(float64(tokenProCodexImageResultMaxChars)/float64(len(uri))) * 0.90
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
