package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeTokenProCodexImageDisplayPayloadCompressesOversizedImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(tokenProImageDisplayContextKey, true)

	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	seed := uint32(1)
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			seed = seed*1664525 + 1013904223
			img.SetNRGBA(x, y, color.NRGBA{R: byte(seed >> 24), G: byte(seed >> 16), B: byte(seed >> 8), A: 255})
		}
	}
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, img))
	encoded := base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	require.Greater(t, len(encoded), tokenProCodexImageResultMaxChars)

	payload := []byte(`{"type":"response.output_item.done","item":{"type":"image_generation_call","status":"completed","result":"` + encoded + `","output_format":"png"}}`)
	updated, changed := normalizeTokenProCodexImageDisplayPayload(c, payload)
	require.True(t, changed)
	result := gjson.GetBytes(updated, "item.result").String()
	require.True(t, strings.HasPrefix(result, "data:image/jpeg;base64,"))
	require.LessOrEqual(t, len(result), tokenProCodexImageResultMaxChars)
	require.Equal(t, "jpeg", gjson.GetBytes(updated, "item.output_format").String())

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result, "data:image/jpeg;base64,"))
	require.NoError(t, err)
	decoded, format, err := image.Decode(bytes.NewReader(raw))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.NotZero(t, decoded.Bounds().Dx())
	require.NotZero(t, decoded.Bounds().Dy())
}

func TestNormalizeTokenProCodexImageDisplayPayloadRequiresTokenProSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	payload := []byte(`{"type":"response.output_item.done","item":{"type":"image_generation_call","result":"oversized"}}`)
	updated, changed := normalizeTokenProCodexImageDisplayPayload(c, payload)
	require.False(t, changed)
	require.Equal(t, payload, updated)
}
