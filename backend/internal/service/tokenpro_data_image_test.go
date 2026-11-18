package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildTokenProCodexDataImageURICapsSize(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1800, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1800; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var raw bytes.Buffer
	require.NoError(t, png.Encode(&raw, src))

	uri, ok := BuildTokenProCodexDataImageURI(raw.Bytes())
	require.True(t, ok)
	require.True(t, strings.HasPrefix(uri, "data:image/jpeg;base64,"))
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/jpeg;base64,"))
	require.NoError(t, err)
	require.LessOrEqual(t, len(payload), tokenProDataImageMaxBytes)
	decoded, err := jpeg.Decode(bytes.NewReader(payload))
	require.NoError(t, err)
	long := decoded.Bounds().Dx()
	if decoded.Bounds().Dy() > long {
		long = decoded.Bounds().Dy()
	}
	require.LessOrEqual(t, long, tokenProDataImageMaxEdge)

	text := TokenProOfficialCodexDataImageText(uri, "https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff", "一只猫")
	require.Contains(t, text, "图片已生成：\n\n![]("+uri+")")
	require.Contains(t, text, "[原图链接](https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff)")
	require.Contains(t, text, "调整后的描述：一只猫")
	require.Less(t, len(text), 600*1024)
	t.Logf("data-v1 example message bytes=%d jpeg bytes=%d edge=%d", len(text), len(payload), long)
}

func TestBuildTokenProCodexDataImageURIRejectsGarbage(t *testing.T) {
	_, ok := BuildTokenProCodexDataImageURI(nil)
	require.False(t, ok)
	_, ok = BuildTokenProCodexDataImageURI([]byte("not an image"))
	require.False(t, ok)
}

func TestStripTokenProDataImageMarkdownKeepsInputImage(t *testing.T) {
	body := []byte(`{"model":"gpt-6-sol","input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"图片已生成：\n\n![](data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString([]byte("jpeg-bytes")) + `)\n\n[原图链接](https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff)"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"再改一下"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]}`)
	out, changed := StripTokenProDataImageMarkdown(body)
	require.True(t, changed)
	text := gjson.GetBytes(out, "input.0.content.0.text").String()
	require.Contains(t, text, "[已生成图片]")
	require.NotContains(t, text, "data:image/jpeg;base64,")
	require.Contains(t, text, "[原图链接](https://tokenpro.work/v1/tokenpro/images/abc?exp=1&sig=ff)")
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(out, "input.1.content.1.image_url").String())

	chat := []byte(`{"model":"gpt-6-sol","messages":[{"role":"assistant","content":"图片已生成：\n\n![](data:image/png;base64,QUFBQQ==)\n\n[原图链接](https://tokenpro.work/cat.png)"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB"}}]}]}`)
	out, changed = StripTokenProDataImageMarkdown(chat)
	require.True(t, changed)
	require.Contains(t, gjson.GetBytes(out, "messages.0.content").String(), "[已生成图片]")
	require.NotContains(t, gjson.GetBytes(out, "messages.0.content").String(), "base64,")
	require.Equal(t, "data:image/png;base64,BBBB", gjson.GetBytes(out, "messages.1.content.0.image_url.url").String())

	plain := []byte(`{"input":"hello"}`)
	out, changed = StripTokenProDataImageMarkdown(plain)
	require.False(t, changed)
	require.Equal(t, plain, out)
}
