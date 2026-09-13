package service

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenProPureDispatchRejectsInvalidInputs(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"input":" "}`, `{"input":false}`, `{"input":[],"previous_response_id":"resp_other"}`, `{"input":[{"role":"user","content":[{"type":"input_image"}]}]}`} {
		_, err := BuildTokenProNativeImageItem([]byte(body))
		require.Error(t, err, body)
	}
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
}
