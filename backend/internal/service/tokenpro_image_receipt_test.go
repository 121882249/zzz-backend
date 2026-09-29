package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageReceiptContinuationWaitReadyFailureAndSteering(t *testing.T) {
	root := "call_tp_receipt_" + strings.Repeat("a", 32)
	start := gin.H{"type": "custom_tool_call", "name": "exec", "namespace": "functions", "call_id": root}
	user := gin.H{"role": "user", "content": "pig"}
	output := func(value string) gin.H {
		return gin.H{"type": "custom_tool_call_output", "call_id": root, "output": []any{gin.H{"type": "input_text", "text": value}}}
	}
	body := func(items ...any) []byte {
		b, err := json.Marshal(gin.H{"input": items})
		require.NoError(t, err)
		return b
	}
	ready := output("Script completed\n" + TokenProImageReadyMarker + root + "\nSaved to /local/image.png")
	called := 0
	verify := func(id string) error { require.Equal(t, root, id); called++; return nil }
	item, handled, err := BuildTokenProImageReceiptContinuation(body(user, start, ready), verify)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "message", item["type"])
	require.Equal(t, 1, called)
	_, _, err = BuildTokenProImageReceiptContinuation(body(user, start, ready), nil)
	require.ErrorIs(t, err, ErrImageReceiptNotReady, "client text is not proof of generation")
	item, handled, err = BuildTokenProImageReceiptContinuation(body(user, start, output("Error: generation failed")), nil)
	require.NoError(t, err)
	require.True(t, handled)
	encoded, _ := json.Marshal(item)
	require.NotContains(t, string(encoded), "生成好了")
	running := output("Script running with cell ID cell_123\nOutput:")
	item, handled, err = BuildTokenProImageReceiptContinuation(body(user, start, running, user), nil)
	require.NoError(t, err)
	require.True(t, handled, "a newly queued user must not orphan the running image")
	require.Equal(t, "wait", item["name"])
	waitOutput := gin.H{"type": "function_call_output", "call_id": item["call_id"], "output": TokenProImageReadyMarker + root}
	_, handled, err = BuildTokenProImageReceiptContinuation(body(user, start, running, user, item, waitOutput), verify)
	require.NoError(t, err)
	require.False(t, handled, "after the old image finishes, dispatch the newer user prompt")
	item, handled, err = BuildTokenProImageReceiptContinuation(body(user, start, running, item, waitOutput), verify)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "message", item["type"])
	_, handled, err = BuildTokenProImageReceiptContinuation(body(user, start, ready, user), nil)
	require.NoError(t, err, "completed historical images need no receipt lookup for a new turn")
	require.False(t, handled)
	_, handled, err = BuildTokenProImageReceiptContinuation(body(user, start), nil)
	require.True(t, handled)
	require.Error(t, err)
}

func TestImageReceiptRequiresExecAndWaitCapability(t *testing.T) {
	full := `{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"},{"type":"function","name":"wait"}]}]}`
	require.True(t, TokenProSupportsImageReceipt([]byte(full)))
	require.True(t, TokenProSupportsImageReceipt([]byte(`{"tools":[{"type":"custom","name":"exec"},{"type":"function","name":"wait"}]}`)))
	require.False(t, TokenProSupportsImageReceipt([]byte(strings.Replace(full, `"wait"`, `"other"`, 1))))
	require.False(t, TokenProSupportsImageReceipt([]byte(strings.Replace(full, `"functions"`, `"user_namespace"`, 1))))
	require.False(t, TokenProSupportsImageReceipt([]byte(`{"tools":[]}`)))
	require.False(t, ValidTokenProImageReceiptCall("call_A"))
}
