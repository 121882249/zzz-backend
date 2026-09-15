package service

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenProDisplayedImagePathsScope(t *testing.T) {
	path := "/Users/a/.codex/generated_images/thread/call.png"
	call := map[string]any{"type": "function_call", "name": "imagegen", "namespace": "image_gen", "call_id": "call"}
	output := map[string]any{"type": "function_call_output", "call_id": "call", "output": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,YQ=="}, map[string]any{"type": "input_text", "text": "Saved to " + path}}}
	for _, tc := range []struct {
		name  string
		items []any
		want  bool
	}{
		{"success", []any{call, output}, true},
		{"historical", []any{call, output, map[string]any{"role": "user", "content": "new request"}}, false},
		{"unmatched", []any{output}, false},
		{"failed", []any{call, map[string]any{"type": "function_call_output", "call_id": "call", "output": "failed " + path}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"input": tc.items})
			require.NoError(t, err)
			paths := tokenProDisplayedImagePaths(body)
			if tc.want {
				require.Equal(t, []string{path}, paths)
			} else {
				require.Empty(t, paths)
			}
		})
	}
}

func TestTokenProImageReplySplitStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	w := &tokenProImageReplyWriter{ResponseWriter: c.Writer, paths: []string{"/x/generated_images/t/a.png"}, deltas: map[string]string{}}
	w.Header().Set("Content-Type", "text/event-stream")
	events := []map[string]any{
		{"type": "response.output_text.delta", "item_id": "m", "content_index": 0, "delta": "![dog](/x/generated_"},
		{"type": "response.output_text.delta", "item_id": "m", "content_index": 0, "delta": "images/t/a.png)"},
		{"type": "response.output_text.done", "item_id": "m", "content_index": 0, "text": "![dog](/x/generated_images/t/a.png)"},
		{"type": "response.completed", "response": map[string]any{"usage": map[string]any{"total_tokens": 42}, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "![dog](/x/generated_images/t/a.png)"}}}}}},
	}
	for i, e := range events {
		b, _ := json.Marshal(e)
		frame := "data: " + string(b) + "\n\n"
		for _, ch := range []byte(frame) {
			_, err := w.Write([]byte{ch})
			require.NoError(t, err)
		}
		if i < 2 {
			require.Empty(t, rec.Body.String())
		}
	}
	require.NotContains(t, rec.Body.String(), "generated_images")
	require.Contains(t, rec.Body.String(), "图片生成好了")
	require.Contains(t, rec.Body.String(), `"total_tokens":42`)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "response.output_text.delta"))
}
func TestTokenProCleanImageReplyPreservesOtherContent(t *testing.T) {
	require.Empty(t, tokenProCleanImageReply("", []string{"/x/generated_images/t/a.png"}))
	p := "/x/generated_images/t/a.png"
	require.Equal(t, "说明\n\n![upload](/upload.png)", tokenProCleanImageReply("说明\n![dog]("+p+")\n![upload](/upload.png)", []string{p}))
	require.Equal(t, "![old](/x/generated_images/t/b.png)", tokenProCleanImageReply("![old](/x/generated_images/t/b.png)", []string{p}))
}

func TestTokenProImageReplyJSONAndCRLF(t *testing.T) {
	for _, stream := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		w := &tokenProImageReplyWriter{ResponseWriter: c.Writer, paths: []string{"/x/generated_images/t/a.png"}, deltas: map[string]string{}}
		raw := `{"type":"response.completed","response":{"id":9007199254740993,"output":[{"type":"message","content":[{"type":"output_text","text":"![dog](/x/generated_images/t/a.png)"}]}]}}`
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			raw = "data: " + raw + "\r\n\r\n"
		}
		_, err := w.Write([]byte(raw))
		require.NoError(t, err)
		require.Contains(t, rec.Body.String(), "9007199254740993")
		require.NotContains(t, rec.Body.String(), "generated_images")
		require.Contains(t, rec.Body.String(), "图片生成好了")
	}
	require.Equal(t, "图片生成好了 ✨", tokenProCleanImageReply(`<img src="/x/generated_images/t/a.png">`, []string{"/x/generated_images/t/a.png"}))
}
