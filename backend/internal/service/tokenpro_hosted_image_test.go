package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const hostedTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestCodexDesktopTextModelHostedImageStreamsLocalExec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clientBody := desktopCodeModeBody(t, nil)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(clientBody, &decoded))
	require.True(t, ensureOpenAIResponsesImageGenerationTool(decoded), "bridge injects hosted image_generation when Desktop has no image_gen tool")
	require.False(t, ensureOpenAIResponsesImageGenerationTool(decoded), "a client that already has image_generation is not armed again")

	var publishes atomic.Int32
	var published []byte
	c, rec, mem := armDesktopHostedImage(t, clientBody, func(_ context.Context, contentType string, data []byte) (string, error) {
		publishes.Add(1)
		require.Equal(t, "image/png", contentType)
		published = append([]byte(nil), data...)
		return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
	})
	value, ok := c.Get("api_key")
	require.True(t, ok)
	apiKey, ok := value.(*APIKey)
	require.True(t, ok)
	require.True(t, apiKey.IsGlobal())
	require.True(t, GroupAllowsImageGeneration(apiKey.Group))
	require.Equal(t, int64(16), apiKey.Group.ID)
	require.Equal(t, "Codex Desktop/0.163.0-alpha.10.2 (Codex Desktop; 26.1013.40934)", c.Request.UserAgent())
	require.Equal(t, "codex_chatgpt_desktop", c.GetHeader("originator"))

	counter := newOpenAIImageOutputCounter()
	writeHostedSSE(t, c, counter, `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"画好啦","annotations":[]}]}}`)
	writeHostedSSE(t, c, counter, `{"type":"response.image_generation_call.partial_image","output_index":1,"item_id":"ig_cat","partial_image_b64":"PARTIAL_IMAGE_MARKER"}`)
	imageDone := `{"type":"response.output_item.done","output_index":1,"item":{"id":"ig_cat","type":"image_generation_call","status":"completed","output_format":"png","result":"` + hostedTestPNG + `"}}`
	counter.AddSSEData([]byte(imageDone))
	raw := []byte("data: " + imageDone + "\n\n")
	_, err := c.Writer.Write(raw[:12])
	require.NoError(t, err)
	_, err = c.Writer.Write(raw[12:])
	require.NoError(t, err)
	completed := `{"type":"response.completed","response":{"id":"resp_cat","object":"response","status":"completed","model":"gpt-6-sol","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"画好啦","annotations":[]}]},{"id":"ig_cat","type":"image_generation_call","status":"completed","output_format":"png","result":"` + hostedTestPNG + `"}],"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33}}}`
	writeHostedSSE(t, c, counter, completed)
	c.Writer.Flush()

	require.Equal(t, 1, counter.Count(), "upstream image billing still sees the hosted result once")
	require.Equal(t, int32(1), publishes.Load())
	png, err := base64.StdEncoding.DecodeString(hostedTestPNG)
	require.NoError(t, err)
	require.Equal(t, png, published)
	body := rec.Body.String()
	require.NotContains(t, body, "image_generation_call")
	require.NotContains(t, body, hostedTestPNG)
	require.NotContains(t, body, "PARTIAL_IMAGE_MARKER")
	require.Contains(t, body, "画好啦")

	var inputDone gjson.Result
	var completedEvent gjson.Result
	for _, event := range gjsonEvents(body) {
		switch event.Get("type").String() {
		case "response.custom_tool_call_input.done":
			inputDone = event
		case "response.completed":
			completedEvent = event
		}
	}
	require.Equal(t, "exec", inputDone.Get("name").String())
	require.Equal(t, int64(1), inputDone.Get("output_index").Int())
	input := inputDone.Get("input").String()
	require.Contains(t, input, "await tools.exec_command(")
	require.Contains(t, input, "tokenpro_image_url=https://tokenpro.work/cat.png?exp=1&sig=ff")
	require.NotContains(t, input, `\u0026`)
	require.True(t, completedEvent.Exists())
	require.Equal(t, "message", completedEvent.Get("response.output.0.type").String())
	require.Equal(t, "画好啦", completedEvent.Get("response.output.0.content.0.text").String())
	require.Equal(t, "custom_tool_call", completedEvent.Get("response.output.1.type").String())
	require.Equal(t, "exec", completedEvent.Get("response.output.1.name").String())
	require.Equal(t, int64(11), completedEvent.Get("response.usage.input_tokens").Int())
	require.Equal(t, int64(22), completedEvent.Get("response.usage.output_tokens").Int())
	require.Len(t, mem.snapshot(), 1)
	for _, record := range mem.snapshot() {
		require.Equal(t, "https://tokenpro.work/cat.png?exp=1&sig=ff", record.URL)
	}
}

func TestCodexDesktopTextModelHostedImageJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clientBody := desktopCodeModeBody(t, nil)
	var publishes atomic.Int32
	c, rec, _ := armDesktopHostedImage(t, clientBody, func(context.Context, string, []byte) (string, error) {
		publishes.Add(1)
		return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
	})
	c.Writer.Header().Set("Content-Type", "application/json")
	payload := []byte(`{"id":"resp_cat","object":"response","model":"gpt-6-sol","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"画好啦"}]},{"id":"ig_cat","type":"image_generation_call","status":"completed","output_format":"png","result":"` + hostedTestPNG + `"}],"usage":{"input_tokens":11,"output_tokens":22,"total_tokens":33}}`)
	counter := newOpenAIImageOutputCounter()
	counter.AddJSONResponse(payload)
	_, err := c.Writer.Write(payload)
	require.NoError(t, err)

	require.Equal(t, 1, counter.Count())
	require.Equal(t, int32(1), publishes.Load())
	body := rec.Body.String()
	require.NotContains(t, body, "image_generation_call")
	require.NotContains(t, body, hostedTestPNG)
	require.Equal(t, "画好啦", gjson.Get(body, "output.0.content.0.text").String())
	require.Equal(t, "custom_tool_call", gjson.Get(body, "output.1.type").String())
	require.Contains(t, gjson.Get(body, "output.1.input").String(), "tools.exec_command")
	require.Equal(t, int64(11), gjson.Get(body, "usage.input_tokens").Int())
}

func TestCodexDesktopHostedImageFallsBackToLink(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tools := []any{
		map[string]any{"type": "web_search"},
		map[string]any{"type": "function", "name": "wait"},
		map[string]any{"type": "function", "name": "request_user_input"},
	}
	c, rec, mem := armDesktopHostedImage(t, desktopCodeModeBody(t, tools), func(context.Context, string, []byte) (string, error) {
		return "https://tokenpro.work/cat.png?exp=1&sig=ff", nil
	})
	counter := newOpenAIImageOutputCounter()
	writeHostedSSE(t, c, counter, `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","content":[{"type":"output_text","text":"画好啦"}]}}`)
	writeHostedSSE(t, c, counter, `{"type":"response.completed","response":{"id":"resp_cat","output":[{"type":"message","content":[{"type":"output_text","text":"画好啦"}]},{"id":"ig_cat","type":"image_generation_call","status":"completed","result":"`+hostedTestPNG+`"}],"usage":{"input_tokens":3,"output_tokens":4}}}`)
	c.Writer.Flush()

	require.Equal(t, 1, counter.Count())
	body := rec.Body.String()
	require.NotContains(t, body, "image_generation_call")
	require.NotContains(t, body, hostedTestPNG)
	require.NotContains(t, body, "custom_tool_call")
	var link string
	for _, event := range gjsonEvents(body) {
		if event.Get("type").String() == "response.output_item.done" && event.Get("output_index").Int() == 1 {
			link = event.Get("item.content.0.text").String()
		}
	}
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png?exp=1&sig=ff", link)
	require.Empty(t, mem.snapshot())
}

func TestCodexDesktopHostedImagePublishFailureStripsCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, rec, _ := armDesktopHostedImage(t, desktopCodeModeBody(t, nil), func(context.Context, string, []byte) (string, error) {
		return "", errTokenProHostedImageURL
	})
	counter := newOpenAIImageOutputCounter()
	writeHostedSSE(t, c, counter, `{"type":"response.completed","response":{"id":"resp_cat","output":[{"type":"message","content":[{"type":"output_text","text":"画好啦"}]},{"id":"ig_cat","type":"image_generation_call","status":"completed","result":"`+hostedTestPNG+`"}],"usage":{"input_tokens":3,"output_tokens":4}}}`)
	c.Writer.Flush()

	require.Equal(t, 1, counter.Count())
	body := rec.Body.String()
	require.NotContains(t, body, "image_generation_call")
	require.NotContains(t, body, hostedTestPNG)
	require.NotContains(t, body, "图片已生成")
	require.NotContains(t, body, "custom_tool_call")
	require.Contains(t, body, "画好啦")
	require.Contains(t, body, "图片没有保存下来，请再试一次。")
}

func TestHostedImageWriterLeavesTextOnlyStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var publishes atomic.Int32
	c, rec, _ := armDesktopHostedImage(t, desktopCodeModeBody(t, nil), func(context.Context, string, []byte) (string, error) {
		publishes.Add(1)
		return "", nil
	})
	payload := `{"type":"response.completed","response":{"id":"resp_text","output":[{"type":"message","content":[{"type":"output_text","text":"你好"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}`
	writeHostedSSE(t, c, nil, payload)
	c.Writer.Flush()
	require.Equal(t, int32(0), publishes.Load())
	body := rec.Body.String()
	require.NotContains(t, body, "custom_tool_call")
	require.NotContains(t, body, "image_generation_call")
	require.Contains(t, body, payload)
}

func TestHostedImageWriterReturnsJSONAfterKeepalive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, rec, _ := armDesktopHostedImage(t, desktopCodeModeBody(t, nil), func(context.Context, string, []byte) (string, error) {
		t.Fatal("publish")
		return "", nil
	})
	_, err := c.Writer.Write([]byte(": ping\n\n"))
	require.NoError(t, err)
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	payload := []byte(`{"error":{"type":"api_error","message":"upstream failed"}}`)
	_, err = c.Writer.Write(payload)
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"upstream failed"`)
	require.NotContains(t, rec.Body.String(), "custom_tool_call")
}

type hostedImageMem struct {
	mu      sync.Mutex
	records map[string]TokenProLocalImageRecord
}

func (m *hostedImageMem) Bind(context.Context, string, TokenProImageTurnRoute) error { return nil }
func (m *hostedImageMem) Lookup(context.Context, string) (TokenProImageTurnRoute, error) {
	return TokenProImageTurnRoute{}, nil
}
func (m *hostedImageMem) RememberLocalImage(_ context.Context, key string, record TokenProLocalImageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string]TokenProLocalImageRecord{}
	}
	m.records[key] = record
	return nil
}
func (m *hostedImageMem) LookupLocalImage(_ context.Context, key string) (TokenProLocalImageRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.records[key], nil
}
func (m *hostedImageMem) snapshot() map[string]TokenProLocalImageRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]TokenProLocalImageRecord, len(m.records))
	for key, record := range m.records {
		out[key] = record
	}
	return out
}

func armDesktopHostedImage(t *testing.T, clientBody []byte, publish TokenProHostedImagePublisher) (*gin.Context, *httptest.ResponseRecorder, *hostedImageMem) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("User-Agent", "Codex Desktop/0.163.0-alpha.10.2 (Codex Desktop; 26.1013.40934)")
	req.Header.Set("originator", "codex_chatgpt_desktop")
	c.Request = req
	groupID := int64(16)
	c.Set("api_key", &APIKey{
		ID: 9, UserID: 7, Key: "global-desktop", KeyType: APIKeyTypeGlobal, GroupID: &groupID,
		User:  &User{ID: 7},
		Group: &Group{ID: 16, Platform: PlatformOpenAI, AllowImageGeneration: true, Description: "对话"},
	})
	mem := &hostedImageMem{records: map[string]TokenProLocalImageRecord{}}
	c.Set(TokenProImageTurnContextKey, &TokenProPendingImageTurn{
		Store: mem, ThreadID: "11111111-1111-4111-8111-111111111111",
	})
	SetTokenProHostedImagePublisher(c, publish)
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	armTokenProHostedImageDelivery(c, clientBody)
	armTokenProHostedImageDelivery(c, clientBody)
	_, wrapped := c.Writer.(*tokenProHostedImageWriter)
	require.True(t, wrapped)
	return c, rec, mem
}

func desktopCodeModeBody(t *testing.T, tools []any) []byte {
	t.Helper()
	if tools == nil {
		tools = []any{
			map[string]any{
				"type": "custom", "name": "exec",
				"description": "### `exec_command`\nRun a command.\n",
				"format":      map[string]any{"type": "grammar", "syntax": "lark", "definition": "start: SOURCE"},
			},
			map[string]any{"type": "function", "name": "wait"},
			map[string]any{"type": "function", "name": "request_user_input"},
			map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "send_message"},
			}},
			map[string]any{"type": "function", "name": "mcp__linear__list"},
			map[string]any{"type": "web_search"},
		}
	}
	body, err := json.Marshal(map[string]any{
		"model":  "gpt-6-sol",
		"stream": true,
		"input":  "画一只猫",
		"tools":  tools,
	})
	require.NoError(t, err)
	require.NotContains(t, string(body), "image_gen")
	require.NotContains(t, string(body), "image_generation")
	return body
}

func writeHostedSSE(t *testing.T, c *gin.Context, counter *openAIImageOutputCounter, payload string) {
	t.Helper()
	if counter != nil {
		counter.AddSSEData([]byte(payload))
	}
	_, err := c.Writer.Write([]byte("data: " + payload + "\n"))
	require.NoError(t, err)
	_, err = c.Writer.Write([]byte("\n"))
	require.NoError(t, err)
}

func gjsonEvents(body string) []gjson.Result {
	var events []gjson.Result
	for _, block := range bytesSplitSSE(body) {
		events = append(events, gjson.Parse(block))
	}
	return events
}

func bytesSplitSSE(body string) []string {
	parts := []string{}
	rest := body
	for {
		index := indexNN(rest)
		if index < 0 {
			if trim := trimSpace(rest); trim != "" && len(trim) > 6 && trim[:6] == "data: " {
				parts = append(parts, trim[6:])
			}
			return parts
		}
		block := trimSpace(rest[:index])
		rest = rest[index+2:]
		if len(block) > 6 && block[:6] == "data: " {
			parts = append(parts, block[6:])
		}
	}
}

func indexNN(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\n' && s[i+1] == '\n' {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
