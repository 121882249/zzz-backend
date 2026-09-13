//go:build unit

package middleware

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
	"testing"
)

func TestTokenProPureDispatchSimulation(t *testing.T) {
	marshal := func(input any) []byte {
		body, _ := json.Marshal(map[string]any{"model": "gpt-image-2.5-flare", "input": input})
		return body
	}
	user := func(text string) map[string]any {
		return map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}
	}
	first, err := service.BuildTokenProNativeImageItem(marshal([]any{user("cat")}))
	if err != nil || first["type"] != "function_call" {
		t.Fatalf("first dispatch: %v %v", first, err)
	}
	second, _ := service.BuildTokenProNativeImageItem(marshal([]any{user("dog")}))
	if first["call_id"] == second["call_id"] {
		t.Fatal("concurrent calls share an ID")
	}
	done := map[string]any{"type": "function_call_output", "call_id": first["call_id"], "output": "image result"}
	reply, err := service.BuildTokenProNativeImageItem(marshal([]any{user("cat"), first, done}))
	if err != nil || reply["type"] != "message" {
		t.Fatalf("continuation: %v %v", reply, err)
	}
	fresh, err := service.BuildTokenProNativeImageItem(marshal([]any{user("cat"), first, done, user("dog")}))
	if err != nil || fresh["type"] != "function_call" || !strings.Contains(fresh["arguments"].(string), "dog") {
		t.Fatal("old output consumed a new request")
	}
	wrong := map[string]any{"type": "function_call_output", "call_id": second["call_id"], "output": "image result"}
	if _, err := service.BuildTokenProNativeImageItem(marshal([]any{user("cat"), first, wrong})); err == nil {
		t.Fatal("mismatched call was accepted")
	}
	edits := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "https://example.invalid/input.png"}}}}
	if _, err := service.BuildTokenProNativeImageItem(marshal(edits)); err == nil {
		t.Fatal("silently dropped image input")
	}
}
