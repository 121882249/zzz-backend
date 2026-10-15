package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTokenProLocalTitleKeepsUserInferenceUntouched(t *testing.T) {
	for _, source := range []string{"thread_title", "user", "memory_consolidation", ""} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("X-Codex-Turn-Metadata", `{"thread_source":"`+source+`"}`)
		body := []byte(`{"model":"tp-g69-gpt-6-astra","input":"User prompt: 画一只猫","text":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"}}}}}}`)
		handled := ServeTokenProLocalTitle(c, body)
		require.Equal(t, source == "thread_title", handled)
		if handled {
			require.Equal(t, "v1", rec.Header().Get("X-TokenPro-Local-Title"))
			require.Zero(t, gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
			require.Equal(t, "画一只猫", gjson.Get(gjson.Get(rec.Body.String(), "output.0.content.0.text").String(), "title").String())
		} else {
			require.Empty(t, rec.Body.String())
		}
	}
}

func TestTokenProLocalTitleStreamingAndMetadataFallback(t *testing.T) {
	body := []byte(`{"model":"tp-g69-gpt-6-astra","stream":true,"client_metadata":{"x-codex-turn-metadata":"{\"thread_source\":\"thread_title\"}"},"input":[{"role":"user","content":[{"type":"input_text","text":"User prompt: 修复生图工具"}]}],"text":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"}}}}}}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.True(t, ServeTokenProLocalTitle(c, body))
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	var sequence []string
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := gjson.Parse(strings.TrimPrefix(line, "data: "))
		sequence = append(sequence, event.Get("type").String())
		if event.Get("type").String() == "response.completed" {
			require.Zero(t, event.Get("response.usage.total_tokens").Int())
			require.Equal(t, "修复生图工具", gjson.Get(event.Get("response.output.0.content.0.text").String(), "title").String())
		}
	}
	require.Equal(t, []string{"response.created", "response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed"}, sequence)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"thread_source":"user"}`)
	require.False(t, ServeTokenProLocalTitle(c, body))
	c.Request.Header.Del("X-Codex-Turn-Metadata")
	require.False(t, ServeTokenProLocalTitle(c, []byte(strings.ReplaceAll(string(body), "json_schema", "json_object"))))
	c.Request.URL.Path = "/v1/responses/compact"
	require.False(t, ServeTokenProLocalTitle(c, body))
}

func TestTokenProLocalTitleTextHandlesUnicodeAndEmptyPrompts(t *testing.T) {
	for _, text := range []string{"", "## My request: 修复生图", "User request: " + strings.Repeat("猫", 50)} {
		body, err := json.Marshal(gin.H{"input": text})
		require.NoError(t, err)
		title := tokenProLocalTitleText(body)
		require.NotEmpty(t, title)
		require.LessOrEqual(t, len([]rune(title)), 36)
	}
}
