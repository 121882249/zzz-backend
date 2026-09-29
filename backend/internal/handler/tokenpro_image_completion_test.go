//go:build unit

package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFinishTokenProImageContinuationShortCircuitsTextTail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": "draw a donkey"},
		map[string]any{"type": "function_call", "namespace": "image_gen", "name": "imagegen", "call_id": "call_image"},
		map[string]any{"type": "function_call_output", "call_id": "call_image", "output": []any{
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,ZmFrZQ=="},
		}},
	}})
	require.NoError(t, err)
	started := false
	h := &OpenAIGatewayHandler{}
	require.True(t, h.finishTokenProImageContinuation(c, "gpt-5.6-sol", body, false, &started))
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), "图片生成好了")
	require.NotContains(t, recorder.Body.String(), "image_url")
	require.False(t, started)
}
