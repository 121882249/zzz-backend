package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProPureDispatchRejectsInvalidInputs(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"input":" "}`, `{"input":false}`, `{"input":[],"previous_response_id":"resp_other"}`, `{"input":[{"role":"user","content":[{"type":"input_image"}]}]}`} {
		_, err := BuildTokenProNativeImageItem([]byte(body))
		require.Error(t, err, body)
	}
}

func TestTokenProPureDispatchBuildsNativeImageEdit(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[` +
		`{"type":"input_text","text":"# Files mentioned by the user:\n\n## source.png: /tmp/source.png\n\n## My request:\n带上墨镜\n"},` +
		`{"type":"input_text","text":"<image name=[Image #1] path=\"/tmp/source.png\">"},` +
		`{"type":"input_image","image_url":"data:image/png;base64,AQID"},` +
		`{"type":"input_text","text":"</image>"}` +
		`]}]}`)
	item, err := BuildTokenProNativeImageItem(body)
	require.NoError(t, err)
	arguments, ok := item["arguments"].(string)
	require.True(t, ok)
	args := gjson.Parse(arguments)
	require.Equal(t, "带上墨镜", args.Get("prompt").String())
	require.Equal(t, "/tmp/source.png", args.Get("referenced_image_paths.0").String())
	require.False(t, args.Get("num_last_images_to_include").Exists())
}

func TestTokenProPureDispatchUsesRecentImageWhenNoLocalPathExists(t *testing.T) {
	item, err := BuildTokenProNativeImageItem([]byte(`{"input":[{"role":"user","content":[` +
		`{"type":"input_text","text":"change the background"},` +
		`{"type":"input_image","image_url":"https://example.com/source.png"}` +
		`]}]}`))
	require.NoError(t, err)
	arguments, ok := item["arguments"].(string)
	require.True(t, ok)
	args := gjson.Parse(arguments)
	require.Equal(t, "change the background", args.Get("prompt").String())
	require.Equal(t, int64(1), args.Get("num_last_images_to_include").Int())
	require.False(t, args.Get("referenced_image_paths").Exists())
}

func TestTokenProPureDispatchCanvasFollowUpUsesPreviousReceiptImage(t *testing.T) {
	callID := "call_tp_receipt_" + strings.Repeat("a", 32)
	threadID := "01a0a36b-bb2f-7763-a432-d9906e51cb19"
	imagePath := "/Users/test/.codex/generated_images/" + threadID + "/exec-image.png"
	body, err := json.Marshal(map[string]any{"client_metadata": map[string]any{"thread_id": threadID}, "input": []any{
		map[string]any{"role": "user", "content": "画一只马"},
		map[string]any{"type": "custom_tool_call", "name": "exec", "namespace": "functions", "call_id": callID},
		map[string]any{"type": "custom_tool_call_output", "call_id": callID, "output": []any{
			map[string]any{"type": "input_text", "text": TokenProImageReadyMarker + callID},
			map[string]any{"type": "input_text", "text": "Generated images are saved to /Users/test/.codex/generated_images/" + threadID + " as " + imagePath + " by default.\nThe generated image is already displayed to the user."},
		}},
		map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "output_text", "text": "图片生成好了 ✨"},
		}},
		map[string]any{"role": "user", "content": "再骑上2个人"},
	}})
	require.NoError(t, err)
	item, err := BuildTokenProNativeImageItem(body)
	require.NoError(t, err)
	args := gjson.Parse(item["arguments"].(string))
	require.Equal(t, "再骑上2个人", args.Get("prompt").String())
	require.Equal(t, imagePath, args.Get("referenced_image_paths.0").String())
	require.False(t, args.Get("num_last_images_to_include").Exists())
}

func TestTokenProPureDispatchFailedReceiptDoesNotReuseCanvasImage(t *testing.T) {
	callID := "call_tp_receipt_" + strings.Repeat("b", 32)
	body, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": "画一只马"},
		map[string]any{"type": "custom_tool_call", "name": "exec", "namespace": "functions", "call_id": callID},
		map[string]any{"type": "custom_tool_call_output", "call_id": callID, "output": "Error: generation failed"},
		map[string]any{"role": "user", "content": "再骑上2个人"},
	}})
	require.NoError(t, err)
	item, err := BuildTokenProNativeImageItem(body)
	require.NoError(t, err)
	args := gjson.Parse(item["arguments"].(string))
	require.False(t, args.Get("num_last_images_to_include").Exists())
}

func TestTokenProPureDispatchConcurrentCorrelation(t *testing.T) {
	const count = 64
	ids := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := BuildTokenProNativeImageItem([]byte(`{"input":"cat"}`))
			if err != nil {
				t.Error(err)
				return
			}
			id, ok := item["call_id"].(string)
			if !ok {
				t.Error("missing string call_id")
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		require.False(t, seen[id])
		seen[id] = true
	}
	require.Len(t, seen, count)
}

func TestTokenProPureDispatchFailureContinuationDoesNotClaimSuccess(t *testing.T) {
	item, err := BuildTokenProNativeImageItem([]byte(`{"input":"cat"}`))
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": "cat"}, item,
		map[string]any{"type": "function_call_output", "call_id": item["call_id"], "output": "Error: image generation failed"},
	}})
	require.NoError(t, err)
	result, err := BuildTokenProNativeImageItem(body)
	require.NoError(t, err)
	require.Equal(t, "message", result["type"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "已生成")
	require.NotContains(t, string(encoded), "已展示")
	require.Contains(t, string(encoded), "这次没能生成图片")
}

func TestTokenProPureDispatchSuccessContinuationIsFriendly(t *testing.T) {
	item, err := BuildTokenProNativeImageItem([]byte(`{"input":"cat"}`))
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": "cat"}, item,
		map[string]any{"type": "function_call_output", "call_id": item["call_id"], "output": []any{
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,AQID"},
		}},
	}})
	require.NoError(t, err)
	result, err := BuildTokenProNativeImageItem(body)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "图片生成好了 ✨")
	require.NotContains(t, string(encoded), "图片工具")
}

func TestTokenProNativeImageFailureMessages(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{`Error: request timed out`, "图片生成超时了，请重新发起。"},
		{`Error: service overloaded`, "生图服务有点忙，请稍后再试。"},
		{`Error: content policy violation`, "这次请求未通过检查，请调整图片描述后再试。"},
		{`unexpected empty result`, "图片未能正常返回，请重新生成。"},
	}
	for _, tt := range tests {
		output := gjson.Get(`{"output":`+strconv.Quote(tt.output)+`}`, "output")
		require.Equal(t, tt.want, tokenProNativeImageResultText(output))
	}
}
