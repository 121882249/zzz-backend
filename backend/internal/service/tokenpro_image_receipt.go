package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const TokenProImageReceiptContextKey = "tokenpro_image_receipt_context"
const TokenProImageReadyMarker = "TOKENPRO_IMAGE_READY:"

var ErrImageReceiptNotReady = errors.New("native image completion is not confirmed")

type TokenProImageCall struct {
	CallID      string `json:"call_id"`
	RequestHash string `json:"request_hash"`
	PromptHash  string `json:"prompt_hash"`
	State       string `json:"state"`
}

// Implemented by the existing Redis turn store. Kept separate so legacy clients
// and turn stores without receipt support retain the old native tool protocol.
type TokenProImageCallStore interface {
	PrepareCall(context.Context, string, TokenProImageCall) (TokenProImageCall, error)
	ClaimCall(context.Context, string, string) (string, error)
	FinishCall(context.Context, string, string, bool) error
	CallResult(context.Context, string, string) (string, error)
}

type TokenProImageReceiptContext struct {
	Store  TokenProImageTurnStore
	TurnID string
}

func TokenProImageReceiptHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

var receiptCallPattern = regexp.MustCompile(`^call_tp_receipt_[a-f0-9]{32}$`)
var receiptRunningPattern = regexp.MustCompile(`(?:Script running with cell ID|Cell still running with cell ID) ([A-Za-z0-9_-]{1,128})`)

func ValidTokenProImageReceiptCall(id string) bool { return receiptCallPattern.MatchString(id) }

func TokenProSupportsImageReceipt(body []byte) bool {
	exec, wait := false, false
	for _, namespace := range gjson.GetBytes(body, "tools").Array() {
		// Codex serializes the default functions namespace as flat tools.
		if ns := namespace.Get("namespace").String(); ns == "" || ns == "functions" {
			exec = exec || (namespace.Get("name").String() == "exec" && namespace.Get("type").String() == "custom")
			wait = wait || (namespace.Get("name").String() == "wait" && namespace.Get("type").String() == "function")
		}
		if namespace.Get("type").String() != "namespace" || namespace.Get("name").String() != "functions" {
			continue
		}
		for _, tool := range namespace.Get("tools").Array() {
			exec = exec || (tool.Get("name").String() == "exec" && tool.Get("type").String() == "custom")
			wait = wait || (tool.Get("name").String() == "wait" && tool.Get("type").String() == "function")
		}
	}
	return exec && wait
}

func TokenProImageReceiptStore(c *gin.Context, key *APIKey) (TokenProImageCallStore, string) {
	if c == nil || key == nil || !key.IsGlobal() || !TokenProPureImageGroup(key.Group) {
		return nil, ""
	}
	v, _ := c.Get(TokenProImageTurnContextKey)
	pending, _ := v.(*TokenProPendingImageTurn)
	if pending == nil || pending.Legacy || pending.ValidationErr != nil || pending.TurnID == "" {
		return nil, ""
	}
	store, _ := pending.Store.(TokenProImageCallStore)
	return store, TokenProImageTurnKey(key, pending.TurnID)
}

func PrepareTokenProImageReceipt(ctx context.Context, store TokenProImageCallStore, key string, body []byte, item gin.H) (gin.H, error) {
	args := gjson.Parse(fmt.Sprint(item["arguments"]))
	prompt := args.Get("prompt").String()
	call := TokenProImageCall{CallID: tokenProDispatchID("call_tp_receipt_"), RequestHash: TokenProImageReceiptHash(body),
		PromptHash: TokenProImageReceiptHash([]byte(prompt)), State: "pending"}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	call, err := store.PrepareCall(ctx, key, call)
	if err != nil {
		return nil, err
	}
	if !ValidTokenProImageReceiptCall(call.CallID) {
		return nil, ErrImageTurnConflict
	}
	encodedArgs, _ := json.Marshal(gin.H{"prompt": prompt})
	marker, _ := json.Marshal(TokenProImageReadyMarker + call.CallID)
	// The native handler emits the full image event and saves the original. Do
	// not call generatedImage(): that would append the image to model follow-up.
	script := "// @exec: {\"yield_time_ms\": 120000, \"max_output_tokens\": 512}\n" +
		"const result = await tools.image_gen__imagegen(" + string(encodedArgs) + "); " +
		"if (!result || typeof result.image_url !== \"string\" || !result.image_url.startsWith(\"data:image/\")) throw new Error(\"image did not return\"); " +
		"text(" + string(marker) + "); if (result.output_hint) text(result.output_hint);"
	return gin.H{"type": "custom_tool_call", "id": tokenProDispatchID("ctc_"), "call_id": call.CallID,
		"name": "exec", "namespace": "functions", "input": script}, nil
}

func tokenProReceiptOutputText(output gjson.Result) string {
	if output.Type == gjson.String {
		return output.String()
	}
	var text strings.Builder
	for _, part := range output.Array() {
		if part.Get("type").String() == "input_text" {
			_, _ = text.WriteString(part.Get("text").String())
			_ = text.WriteByte('\n')
		}
	}
	return text.String()
}

// Recognizes our own exec/wait history. A queued user message must not orphan
// a still-running image execution; finish waiting before starting its image.
func BuildTokenProImageReceiptContinuation(body []byte, verify func(string) error) (gin.H, bool, error) {
	items := gjson.GetBytes(body, "input").Array()
	lastUser, rootIndex := -1, -1
	root, cell, outputText := "", "", ""
	hasOutput := false
	for i, item := range items {
		if item.Get("role").String() == "user" {
			lastUser = i
		}
		id := item.Get("call_id").String()
		kind := item.Get("type").String()
		if kind == "custom_tool_call" && ValidTokenProImageReceiptCall(id) && item.Get("name").String() == "exec" && item.Get("namespace").String() == "functions" {
			root, rootIndex, cell, outputText, hasOutput = id, i, "", "", false
		}
		if root == "" {
			continue
		}
		waitPrefix := "call_tp_wait_" + strings.TrimPrefix(root, "call_tp_receipt_") + "_"
		if (kind == "custom_tool_call_output" && id == root) || (kind == "function_call_output" && strings.HasPrefix(id, waitPrefix)) {
			hasOutput = true
			outputText = tokenProReceiptOutputText(item.Get("output"))
			cell = ""
			if match := receiptRunningPattern.FindStringSubmatch(outputText); len(match) == 2 {
				cell = match[1]
			}
		}
	}
	if root == "" {
		return nil, false, nil
	}
	if !hasOutput {
		return nil, true, fmt.Errorf("missing matching native image execution output")
	}
	if cell != "" {
		args, _ := json.Marshal(gin.H{"cell_id": cell, "yield_time_ms": 120000, "max_tokens": 512})
		return gin.H{"type": "function_call", "id": tokenProDispatchID("fc_"),
			"call_id":   tokenProDispatchID("call_tp_wait_" + strings.TrimPrefix(root, "call_tp_receipt_") + "_"),
			"namespace": "functions", "name": "wait", "arguments": string(args)}, true, nil
	}
	// Historical completed executions do not decide the outcome of a new user
	// request (nor require a still-live receipt for an older turn).
	if rootIndex < lastUser {
		return nil, false, nil
	}
	var text string
	if strings.Contains(outputText, TokenProImageReadyMarker+root) {
		if verify == nil {
			return nil, true, ErrImageReceiptNotReady
		}
		if err := verify(root); err != nil {
			return nil, true, err
		}
		text = "图片生成好了 ✨"
	} else {
		encoded, _ := json.Marshal(outputText)
		text = tokenProNativeImageResultText(gjson.ParseBytes(encoded))
	}
	return gin.H{"type": "message", "id": tokenProDispatchID("msg_"), "role": "assistant", "status": "completed",
		"content": []any{gin.H{"type": "output_text", "text": text, "annotations": []any{}}}}, true, nil
}

func VerifyTokenProImageReceipt(ctx context.Context, store TokenProImageCallStore, key, callID string) error {
	if store == nil || !ValidTokenProImageReceiptCall(callID) {
		return ErrImageReceiptNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		state, err := store.CallResult(ctx, key, callID)
		if err != nil {
			return ErrImageReceiptNotReady
		}
		if state == "succeeded" {
			return nil
		}
		if state != "running" && state != "pending" {
			return ErrImageReceiptNotReady
		}
		select {
		case <-ctx.Done():
			return ErrImageReceiptNotReady
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Claim only a prepared receipt execution. Ordinary native images have no
// receipt record and keep their existing behavior. Duplicate claims are denied.
func ClaimTokenProImageReceipt(c *gin.Context, key *APIKey, parsed *OpenAIImagesRequest) (func(bool) error, error) {
	if key == nil || !key.IsGlobal() || !TokenProPureImageGroup(key.Group) || parsed == nil ||
		parsed.Endpoint != "/v1/images/generations" || parsed.Multipart || parsed.Stream {
		return nil, nil
	}
	v, _ := c.Get(TokenProImageReceiptContextKey)
	binding, _ := v.(*TokenProImageReceiptContext)
	if binding == nil {
		return nil, nil
	}
	store, ok := binding.Store.(TokenProImageCallStore)
	if !ok {
		return nil, nil
	}
	turnKey := TokenProImageTurnKey(key, binding.TurnID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	callID, err := store.ClaimCall(ctx, turnKey, TokenProImageReceiptHash([]byte(parsed.Prompt)))
	cancel()
	if err != nil || callID == "" {
		return nil, err
	}
	done := false
	return func(success bool) error {
		if done {
			return nil
		}
		done = true
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
		defer cancel()
		return store.FinishCall(ctx, turnKey, callID, success)
	}, nil
}
