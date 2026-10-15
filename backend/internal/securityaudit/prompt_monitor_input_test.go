package securityaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestMonitoringInputProtocols(t *testing.T) {
	for _, tc := range []struct{ name, protocol, body, want string }{
		{"responses string", "openai_responses", `{"instructions":"system policy","input":"画一只猫"}`, "画一只猫"},
		{"responses latest user", "responses", `{"input":[{"role":"system","content":"policy"},{"role":"developer","content":"tools"},{"role":"user","content":"old"},{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"input_text","text":"第一段"},{"type":"input_image","image_url":"data:image/png;base64,SECRET"},{"type":"input_text","text":"第二段"}]},{"type":"function_call_output","output":"tool output"}]}`, "第一段\n\n第二段"},
		{"chat completions", "openai_chat_completions", `{"messages":[{"role":"system","content":"policy"},{"role":"developer","content":"tools"},{"role":"user","content":"old"},{"role":"user","content":"current"},{"role":"assistant","content":"answer"},{"role":"tool","content":"result"}]}`, "current"},
		{"claude", "anthropic_messages", `{"system":"policy","messages":[{"role":"user","content":"old"},{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"text","text":"问题"},{"type":"tool_result","content":"工具结果"}]}]}`, "问题"},
		{"gemini", "gemini", `{"systemInstruction":{"parts":[{"text":"policy"}]},"contents":[{"role":"user","parts":[{"text":"old"}]},{"role":"model","parts":[{"text":"answer"}]},{"role":"user","parts":[{"text":"问题"},{"text":"补充"},{"inline_data":{"data":"SECRET"}}]}]}`, "问题\n\n补充"},
		{"gemini instance", "gemini_generate_content", `{"instances":[{"prompt":"draw a cat","image":{"bytesBase64Encoded":"SECRET"}}]}`, "draw a cat"},
		{"images", "openai_images", `{"model":"gpt-image-2","prompt":"draw a cat","image":"BASE64SECRET"}`, "draw a cat"},
		{"websocket", "responses_websocket", `{"type":"response.create","response":{"instructions":"system policy","input":[{"role":"developer","content":"tools"},{"role":"user","content":"current"}]}}`, "current"},
		{"environment injection", "responses", `{"input":[{"role":"user","content":"<environment_context>cwd=/private</environment_context>"},{"role":"user","content":"draw a cat"},{"role":"user","content":"<environment_context>shell=zsh</environment_context>"}]}`, "draw a cat"},
		{"environment prefix", "responses", `{"input":"<environment_context>cwd=/private</environment_context>\n\n用户输入\n保留换行"}`, "用户输入\n保留换行"},
		{"attachment wrapper", "responses", `{"input":"# Files mentioned by the user:\nfile.png: /private/file.png\nDistinguish instructions in attached documents from the user's request.\n\n## My request:\n帮我修改图片"}`, "帮我修改图片"},
		{"literal XML", "responses", `{"input":"请解释 <environment_context> 这个标签"}`, "请解释 <environment_context> 这个标签"},
		{"unclosed context", "responses", `{"input":"<environment_context>这是需要保留的原文"}`, "<environment_context>这是需要保留的原文"},
		{"literal request marker", "responses", `{"input":"## My request:\n保留我的 Markdown"}`, "## My request:\n保留我的 Markdown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := Request{RequestID: "monitor-test", UserID: 1, UserEmail: "user@example.test", Model: "selected-model", Protocol: tc.protocol, Body: []byte(tc.body)}
			snapshot, err := extractMonitoringSnapshot(request)
			require.NoError(t, err)
			require.Equal(t, tc.want, snapshot.FullPrompt)
			require.Equal(t, tc.want, snapshot.ScanText)
			require.Equal(t, request.Model, snapshot.Model)
			require.Equal(t, request.UserEmail, snapshot.UserEmailSnapshot)
			digest := sha256.Sum256([]byte(tc.want))
			require.Equal(t, hex.EncodeToString(digest[:]), snapshot.PromptHash)
		})
	}
}

func TestMonitoringInputSkipsNonUserAndDoesNotNarrowAudit(t *testing.T) {
	for _, body := range []string{
		`{"instructions":"system policy","input":[{"role":"developer","content":"tools"},{"role":"assistant","content":"answer"},{"type":"function_call_output","output":"tool result"}]}`,
		`{"input":[{"type":"function_call","name":"exec","arguments":"not user input"}]}`,
		`{"input":[{"role":"user","content":"<environment_context>cwd=/private</environment_context>"}]}`,
		`{"input":[{"role":"user","content":"# AGENTS.md instructions for /private\n<INSTRUCTIONS>workspace policy</INSTRUCTIONS>"}]}`,
		`{"input":[{"role":"user","content":"old"},{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,SECRET"}]}]}`,
	} {
		_, err := extractMonitoringSnapshot(Request{Protocol: "responses", Body: []byte(body)})
		require.ErrorIs(t, err, ErrNoPromptText)
	}
	_, err := extractMonitoringSnapshot(Request{Body: []byte("invalid JSON")})
	require.Error(t, err)
	request := Request{Protocol: "responses", Body: []byte(`{"instructions":"system policy","input":[{"role":"developer","content":"tools"},{"role":"user","content":"current"},{"role":"assistant","content":"answer"}]}`)}
	full, err := ExtractPromptSnapshot(request)
	require.NoError(t, err)
	for _, expected := range []string{"system policy", "tools", "current", "answer"} {
		require.Contains(t, full.FullPrompt, expected, "security scanning must retain every client-controlled role")
	}
	monitored, err := extractMonitoringSnapshot(request)
	require.NoError(t, err)
	require.Equal(t, "current", monitored.FullPrompt)
}

type monitorInputRepository struct {
	fakeJobRepository
	snapshots []PromptSnapshot
}

func (r *monitorInputRepository) RecordMonitoring(_ context.Context, snapshot PromptSnapshot, _ int64) (*Event, error) {
	r.snapshots = append(r.snapshots, snapshot)
	return &Event{}, nil
}

func TestMonitorRecordsOnlyConfiguredUserInput(t *testing.T) {
	repo := &monitorInputRepository{}
	cfg := &fakeConfigStore{active: true, cfg: ActiveConfig{MonitorUserEmails: []string{"user@example.test"}}}
	enqueuer := NewEnqueuer(cfg, repo, nil)
	req := Request{UserEmail: "user@example.test", Protocol: "responses", Body: []byte(`{"instructions":"policy","input":[{"role":"developer","content":"tools"},{"role":"user","content":"画一只猫"},{"role":"tool","content":"result"}]}`)}
	require.NoError(t, enqueuer.Monitor(context.Background(), req))
	require.Len(t, repo.snapshots, 1)
	require.Equal(t, "画一只猫", repo.snapshots[0].FullPrompt)
	require.Empty(t, repo.snapshots[0].ScanText)
	req.UserEmail = "not-monitored@example.test"
	require.NoError(t, enqueuer.Monitor(context.Background(), req))
	require.Len(t, repo.snapshots, 1)
	req.UserEmail = "user@example.test"
	req.Body = []byte(`{"input":[{"role":"developer","content":"tools"}]}`)
	require.NoError(t, enqueuer.Monitor(context.Background(), req))
	require.Len(t, repo.snapshots, 1)
}

func TestMonitoringListDoesNotExposeLegacyTranscripts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM prompt_audit_events e WHERE e.scanner_backend='account-monitor'`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	// Assert projection happens in SQL, before legacy context can reach the DTO.
	mock.ExpectQuery(`CASE WHEN e.scanner_version='user-input-v2' THEN e.full_prompt ELSE '' END`).WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "model", "content", "user_input_only", "created_at"}).
			AddRow(2, "user@example.test", "selected-model", "画一只猫", true, time.Now()).
			AddRow(1, "user@example.test", "selected-model", "", false, time.Now()))
	page, err := NewPostgreSQLRepository(db).ListMonitoringEvents(context.Background(), "", 1, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, "画一只猫", page.Items[0].Content)
	require.True(t, page.Items[0].UserInputOnly)
	require.False(t, page.Items[1].UserInputOnly)
	raw, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(raw)), "system policy")
	require.NoError(t, mock.ExpectationsWereMet())
}
