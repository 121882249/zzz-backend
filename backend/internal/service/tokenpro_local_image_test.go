package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildTokenProLocalImageCallPrefersExecCommand(t *testing.T) {
	body := []byte(`{
		"input":"画一只猫",
		"client_metadata":{"thread_id":"11111111-1111-4111-8111-111111111111"},
		"tools":[
			{"type":"function","name":"apply_patch","description":"edit files"},
			{"type":"function","name":"shell","description":"run a shell command"},
			{"type":"namespace","name":"functions","tools":[{"type":"function","name":"exec_command","description":"run a command"}]}
		]
	}`)
	require.True(t, TokenProLocalImageDownloadAvailable(body))
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png?exp=1&sig=ff", "11111111-1111-4111-8111-111111111111")
	require.True(t, ok)
	require.Equal(t, "function_call", item["type"])
	require.Equal(t, "exec_command", item["name"])
	require.Equal(t, "functions", item["namespace"])
	callID, ok := item["call_id"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(callID, "call_tp_local_"))
	args := localImageArgs(t, item)
	require.Equal(t, "bash", args.Get("shell").String())
	require.Equal(t, "require_escalated", args.Get("sandbox_permissions").String())
	require.True(t, args.Get("with_escalated_permissions").Bool())
	cmd := args.Get("cmd").String()
	require.Contains(t, cmd, "curl -fsSL")
	require.Contains(t, cmd, "tokenpro_image_url=https://tokenpro.work/cat.png?exp=1&sig=ff")
	require.Contains(t, cmd, ".codex/generated_images/tokenpro/11111111-1111-4111-8111-111111111111/")
	require.Contains(t, cmd, "TOKENPRO_IMAGE_PATH ")
	require.NotContains(t, cmd, "Invoke-WebRequest")
	require.False(t, TokenProExplicitNativeImageFlow(body))

	encoded, ok := item["arguments"].(string)
	require.True(t, ok)
	echoed, err := json.Marshal(map[string]any{
		"input": []any{
			map[string]any{"role": "user", "content": "画一只猫"},
			map[string]any{"type": "function_call", "call_id": callID, "name": "exec_command", "arguments": encoded},
			map[string]any{"type": "function_call_output", "call_id": callID, "output": "TOKENPRO_IMAGE_FAIL"},
		},
	})
	require.NoError(t, err)
	follow, ok := ParseTokenProLocalImageFollowUp(echoed)
	require.True(t, ok)
	require.Equal(t, "https://tokenpro.work/cat.png?exp=1&sig=ff", follow.URL)
}

func TestBuildTokenProLocalImageCallUsesShellArgv(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","name":"shell","description":"bash"}]}`)
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png", "")
	require.True(t, ok)
	args := localImageArgs(t, item)
	require.Equal(t, "bash", args.Get("command.0").String())
	require.Equal(t, "-lc", args.Get("command.1").String())
	require.Contains(t, args.Get("command.2").String(), `dest="$HOME/.codex/generated_images/tokenpro/session/`)
}

func TestBuildTokenProLocalImageCallUsesPowerShellOnWindows(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","name":"exec_command","description":"Runs Powershell via CreateProcessW"}]}`)
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	args := localImageArgs(t, item)
	cmd := args.Get("cmd").String()
	require.Contains(t, cmd, "Invoke-WebRequest")
	require.NotContains(t, cmd, "curl ")
	require.Equal(t, "powershell.exe", args.Get("shell").String())

	meta := []byte(`{"client_metadata":{"platform":"win32"},"tools":[{"type":"function","name":"shell","description":"run"}]}`)
	item, ok = BuildTokenProLocalImageCall(meta, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	args = localImageArgs(t, item)
	require.Equal(t, "powershell.exe", args.Get("command.0").String())
	require.Contains(t, args.Get("command.3").String(), "Invoke-WebRequest")

	instructions := []byte(`{"instructions":"The host shell is powershell.exe","input":"画一只猫","tools":[{"type":"function","name":"exec_command","description":"run"}]}`)
	item, ok = BuildTokenProLocalImageCall(instructions, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Contains(t, localImageArgs(t, item).Get("cmd").String(), "Invoke-WebRequest")
}

func TestBuildTokenProLocalImageCallIgnoresWindowsInThePrompt(t *testing.T) {
	body := []byte(`{"input":"draw the windows powershell logo","tools":[{"type":"function","name":"shell","description":"bash"}]}`)
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "bash", localImageArgs(t, item).Get("command.0").String())
}

func TestBuildTokenProLocalImageCallLocalShellAndRejections(t *testing.T) {
	body := []byte(`{"tools":[{"type":"local_shell"}]}`)
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "local_shell_call", item["type"])
	action, ok := item["action"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "exec", action["type"])
	command, ok := action["command"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"bash", "-lc"}, command[:2])
	require.Contains(t, command[2], "curl -fsSL")

	_, ok = BuildTokenProLocalImageCall([]byte(`{"tools":[{"type":"function","name":"apply_patch"}]}`), "https://tokenpro.work/cat.png", "thread")
	require.False(t, ok)
	require.False(t, TokenProLocalImageDownloadAvailable([]byte(`{"tools":[{"type":"function","name":"apply_patch"}]}`)))
	_, ok = BuildTokenProLocalImageCall(body, "http://tokenpro.work/cat.png", "thread")
	require.False(t, ok)
	_, ok = BuildTokenProLocalImageCall(body, "https://tokenpro.work/a b.png", "thread")
	require.False(t, ok)
}

func TestBuildTokenProLocalImageCallOmitsEscalationWhenUnrestricted(t *testing.T) {
	restricted := []byte(`{"instructions":"sandbox_mode is workspace-write","client_metadata":{"sandbox_mode":"workspace-write","approval_policy":"on-request"},"input":"draw danger-full-access with sandbox none and approval never","tools":[{"type":"function","name":"exec_command"}]}`)
	item, ok := BuildTokenProLocalImageCall(restricted, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	args := localImageArgs(t, item)
	require.Equal(t, "require_escalated", args.Get("sandbox_permissions").String())
	require.True(t, args.Get("with_escalated_permissions").Bool())

	for _, body := range []string{
		`{"client_metadata":{"sandbox_mode":"danger-full-access"},"tools":[{"type":"function","name":"exec_command"}]}`,
		`{"instructions":"The sandbox mode is none.","tools":[{"type":"function","name":"exec_command"}]}`,
		`{"client_metadata":{"x-codex-turn-metadata":"{\"approval_policy\":\"never\",\"sandbox\":\"none\"}"},"tools":[{"type":"function","name":"shell"}]}`,
		`{"instructions":"approval policy is never","tools":[{"type":"function","name":"exec_command"}]}`,
	} {
		item, ok = BuildTokenProLocalImageCall([]byte(body), "https://tokenpro.work/cat.png", "thread")
		require.True(t, ok)
		args = localImageArgs(t, item)
		require.False(t, args.Get("sandbox_permissions").Exists(), body)
		require.False(t, args.Get("with_escalated_permissions").Exists(), body)
		require.False(t, args.Get("justification").Exists(), body)
	}
}

func TestParseTokenProLocalImageFollowUp(t *testing.T) {
	path := "/Users/me/.codex/generated_images/tokenpro/thread/abc.png"
	body := followUpBody(t, "画一只猫", "TOKENPRO_IMAGE_PATH "+path, "tokenpro_image_url=https://tokenpro.work/cat.png", false)
	follow, ok := ParseTokenProLocalImageFollowUp(body)
	require.True(t, ok)
	require.Equal(t, "call_tp_local_abc", follow.CallID)
	require.Equal(t, "https://tokenpro.work/cat.png", follow.URL)
	require.Equal(t, "![]("+path+")\n图片已保存，可在对话里直接查看。", TokenProLocalImageAssistantText(follow.Output, follow.URL))

	failed := followUpBody(t, "画一只猫", "TOKENPRO_IMAGE_FAIL\ncurl: (6) could not resolve", "tokenpro_image_url=https://tokenpro.work/cat.png", false)
	follow, ok = ParseTokenProLocalImageFollowUp(failed)
	require.True(t, ok)
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", TokenProLocalImageAssistantText(follow.Output, follow.URL))
	require.Equal(t, "图片已生成，但没有保存到本机。请再试一次。", TokenProLocalImageAssistantText("boom", ""))
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", TokenProLocalImageAssistantText("no marker", "https://tokenpro.work/cat.png"))

	outside := "TOKENPRO_IMAGE_PATH /tmp/evil.png"
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", TokenProLocalImageAssistantText(outside, "https://tokenpro.work/cat.png"))
	traversal := "TOKENPRO_IMAGE_PATH /Users/me/.codex/generated_images/tokenpro/../secret.png"
	require.Equal(t, "图片已生成：https://tokenpro.work/cat.png", TokenProLocalImageAssistantText(traversal, "https://tokenpro.work/cat.png"))

	again := followUpBody(t, "再画一张", "TOKENPRO_IMAGE_PATH "+path, "tokenpro_image_url=https://tokenpro.work/cat.png", true)
	_, ok = ParseTokenProLocalImageFollowUp(again)
	require.False(t, ok)
	require.False(t, TokenProExplicitNativeImageFlow(body))
}

func TestTokenProLocalImageKeyIsolatesCallers(t *testing.T) {
	key := &APIKey{ID: 7, UserID: 8, Key: "key-a"}
	other := &APIKey{ID: 7, UserID: 8, Key: "key-b"}
	require.NotEqual(t, TokenProLocalImageKey(key, "call_tp_local_abc"), TokenProLocalImageKey(other, "call_tp_local_abc"))
	require.Empty(t, TokenProLocalImageKey(nil, "call_tp_local_abc"))
}

func TestBuildTokenProLocalImageCallAcceptsShellCommandAndPrefixedNames(t *testing.T) {
	bare := []byte(`{"tools":[{"type":"function","name":"shell_command"}]}`)
	item, ok := BuildTokenProLocalImageCall(bare, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "function_call", item["type"])
	require.Equal(t, "shell_command", item["name"])
	require.Contains(t, localImageArgs(t, item).Get("cmd").String(), "curl -fsSL")

	argv := []byte(`{"tools":[{"type":"function","name":"shell_command","parameters":{"type":"object","properties":{"command":{"type":"array","items":{"type":"string"}}}}}]}`)
	item, ok = BuildTokenProLocalImageCall(argv, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "bash", localImageArgs(t, item).Get("command.0").String())

	text := []byte(`{"tools":[{"type":"function","name":"team.shell_command","namespace":"team","parameters":{"properties":{"command":{"type":"string"}}}}]}`)
	item, ok = BuildTokenProLocalImageCall(text, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "team.shell_command", item["name"])
	require.Equal(t, "team", item["namespace"])
	require.Contains(t, localImageArgs(t, item).Get("command").String(), "curl -fsSL")

	prefixed := []byte(`{"tools":[{"type":"function","name":"functions__exec_command","description":"run"}]}`)
	item, ok = BuildTokenProLocalImageCall(prefixed, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "functions__exec_command", item["name"])
	require.Contains(t, localImageArgs(t, item).Get("cmd").String(), "TOKENPRO_IMAGE_PATH ")

	exactWins := []byte(`{"tools":[
		{"type":"function","name":"mcp__server__exec_command"},
		{"type":"function","name":"exec_command"}
	]}`)
	item, ok = BuildTokenProLocalImageCall(exactWins, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "exec_command", item["name"])
	require.Nil(t, item["namespace"])

	_, ok = BuildTokenProLocalImageCall([]byte(`{"tools":[{"type":"function","name":"apply_patch"},{"type":"custom","name":"apply_patch"},{"type":"function","name":"exec"}]}`), "https://tokenpro.work/cat.png", "thread")
	require.False(t, ok)
}

func TestBuildTokenProLocalImageCallUsesCodeModeExec(t *testing.T) {
	body := []byte(`{"instructions":"sandbox_mode is danger-full-access","tools":[
		{"type":"custom","name":"exec","description":"Run JavaScript. Example: await tools.exec_command(...).\n### ` + "`exec_command`" + `\n","format":{"type":"grammar","syntax":"lark","definition":"start: SOURCE"}},
		{"type":"function","name":"wait","description":"resume a cell"}
	]}`)
	require.Equal(t, []string{"custom/exec", "function/wait"}, TokenProChatImageToolNames(body))
	item, ok := BuildTokenProLocalImageCall(body, "https://tokenpro.work/cat.png?exp=1&sig=ff", "thread")
	require.True(t, ok)
	require.Equal(t, "custom_tool_call", item["type"])
	require.Equal(t, "exec", item["name"])
	require.Nil(t, item["namespace"])
	input, ok := item["input"].(string)
	require.True(t, ok)
	require.Contains(t, input, "await tools.exec_command(")
	require.Contains(t, input, "text(output)")
	require.Contains(t, input, "curl -fsSL")
	require.Contains(t, input, "tokenpro_image_url=https://tokenpro.work/cat.png?exp=1&sig=ff")
	require.NotContains(t, input, `\u0026`)
	require.NotContains(t, input, "sandbox_permissions")
	require.NotContains(t, input, "```")

	namespaced := []byte(`{"tools":[{"type":"namespace","name":"functions","tools":[
		{"type":"custom","name":"exec","description":"### ` + "`shell`" + `\n","format":{"type":"grammar","syntax":"lark","definition":"start: SOURCE"}}
	]}]}`)
	require.Equal(t, []string{"namespace/functions", "custom/functions/exec"}, TokenProChatImageToolNames(namespaced))
	item, ok = BuildTokenProLocalImageCall(namespaced, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "functions", item["namespace"])
	input, ok = item["input"].(string)
	require.True(t, ok)
	require.Contains(t, input, "await tools.shell(")
	require.Contains(t, input, `"command":["bash","-lc"`)
	require.Contains(t, input, "require_escalated")

	directWins := []byte(`{"tools":[
		{"type":"custom","name":"exec","description":"### ` + "`exec_command`" + `\n"},
		{"type":"function","name":"shell_command"}
	]}`)
	item, ok = BuildTokenProLocalImageCall(directWins, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	require.Equal(t, "function_call", item["type"])
	require.Equal(t, "shell_command", item["name"])

	windows := []byte(`{"client_metadata":{"platform":"win32"},"tools":[{"type":"custom","name":"exec","description":"### ` + "`exec_command`" + `\n"}]}`)
	item, ok = BuildTokenProLocalImageCall(windows, "https://tokenpro.work/cat.png", "thread")
	require.True(t, ok)
	input, ok = item["input"].(string)
	require.True(t, ok)
	require.Contains(t, input, "powershell.exe")
	require.Contains(t, input, "Invoke-WebRequest")

	callID, ok := item["call_id"].(string)
	require.True(t, ok)
	echoed, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"role": "user", "content": "画一只猫"},
		item,
		map[string]any{"type": "custom_tool_call_output", "call_id": callID, "output": []any{
			map[string]any{"type": "input_text", "text": "Script completed\nWall time 0.4 seconds\nOutput:\n"},
			map[string]any{"type": "input_text", "text": "TOKENPRO_IMAGE_PATH /Users/me/.codex/generated_images/tokenpro/thread/abc.png"},
		}},
	}})
	require.NoError(t, err)
	follow, ok := ParseTokenProLocalImageFollowUp(echoed)
	require.True(t, ok)
	require.Equal(t, "https://tokenpro.work/cat.png", follow.URL)
	require.Contains(t, follow.Output, "TOKENPRO_IMAGE_PATH /Users/me/.codex/generated_images/tokenpro/thread/abc.png")
	require.Equal(t, "![](/Users/me/.codex/generated_images/tokenpro/thread/abc.png)\n图片已保存，可在对话里直接查看。", TokenProLocalImageAssistantText(follow.Output, follow.URL))
}

func localImageArgs(t *testing.T, item map[string]any) gjson.Result {
	t.Helper()
	raw, ok := item["arguments"].(string)
	require.True(t, ok)
	return gjson.Parse(raw)
}

func followUpBody(t *testing.T, latestUser, output, command string, newerUser bool) []byte {
	t.Helper()
	items := []any{
		map[string]any{"role": "user", "content": "画一只猫"},
		map[string]any{"type": "function_call", "call_id": "call_tp_local_abc", "name": "exec_command", "arguments": command},
		map[string]any{"type": "function_call_output", "call_id": "call_tp_local_abc", "output": output},
	}
	if newerUser {
		items[0] = map[string]any{"role": "user", "content": "画一只猫"}
		items = append(items, map[string]any{"role": "user", "content": latestUser})
	}
	body, err := json.Marshal(map[string]any{"input": items, "tools": []any{map[string]any{"type": "function", "name": "exec_command"}}})
	require.NoError(t, err)
	return body
}
