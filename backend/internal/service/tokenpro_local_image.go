package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	tokenProLocalImageCallPrefix = "call_tp_local_"
	tokenProLocalImageMarker     = "TOKENPRO_IMAGE_PATH "
	tokenProLocalImageFail       = "TOKENPRO_IMAGE_FAIL"
	tokenProLocalImageURLMarker  = "tokenpro_image_url="
	tokenProLocalImageDir        = ".codex/generated_images/tokenpro"
	// TokenProLocalImageTTL keeps the signed URL long enough for Codex to run
	// the download and send the tool output back. The image was already billed.
	TokenProLocalImageTTL = 24 * time.Hour
)

// TokenProLocalImageRecord is the signed URL for one desktop download call.
// The shell command also carries the URL, so a missed Redis write still works
// when Codex echoes the function_call on the next turn.
type TokenProLocalImageRecord struct {
	URL string `json:"url"`
}

// TokenProLocalImageStore is optional Redis state for a download call whose
// original function_call is not echoed on the follow-up turn.
type TokenProLocalImageStore interface {
	RememberLocalImage(context.Context, string, TokenProLocalImageRecord) error
	LookupLocalImage(context.Context, string) (TokenProLocalImageRecord, error)
}

// TokenProLocalImageKey isolates one download by the caller's global key.
func TokenProLocalImageKey(key *APIKey, callID string) string {
	if key == nil {
		return ""
	}
	sum := sha256Sum(key.Key)
	return fmt.Sprintf("tokenpro:image-local:v1:%d:%d:%s:%s", key.UserID, key.ID, sum, callID)
}

func sha256Sum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// TokenProLocalImageFollowUp is a Codex turn that is answering our download.
type TokenProLocalImageFollowUp struct {
	CallID string
	Output string
	URL    string
}

// ParseTokenProLocalImageFollowUp reports a turn whose newest item is the
// shell output for a download we issued. A later user message is a new image.
func ParseTokenProLocalImageFollowUp(body []byte) (TokenProLocalImageFollowUp, bool) {
	items := gjson.GetBytes(body, "input").Array()
	lastUser := -1
	lastOutput := -1
	var follow TokenProLocalImageFollowUp
	calls := map[string]string{}
	for index, item := range items {
		if item.Get("role").String() == "user" {
			lastUser = index
		}
		callID := item.Get("call_id").String()
		if !strings.HasPrefix(callID, tokenProLocalImageCallPrefix) {
			continue
		}
		switch item.Get("type").String() {
		case "function_call", "custom_tool_call", "local_shell_call":
			calls[callID] = localImageCommandText(item)
		case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
			lastOutput = index
			follow.CallID = callID
			follow.Output = localImageOutputText(item)
		}
	}
	if lastOutput < 0 || lastOutput < lastUser {
		return TokenProLocalImageFollowUp{}, false
	}
	follow.URL = localImageURLFromCommand(calls[follow.CallID])
	return follow, true
}

// TokenProLocalImageAssistantText is the desktop markdown image, or the HTTPS
// link when the download did not print an absolute path.
func TokenProLocalImageAssistantText(output, imageURL string) string {
	if path, ok := tokenProLocalImagePath(output); ok {
		return "![](" + path + ")\n图片已保存，可在对话里直接查看。"
	}
	if strings.HasPrefix(strings.TrimSpace(imageURL), "https://") {
		return TokenProChatImageSuccessText(imageURL, "")
	}
	return "图片已生成，但没有保存到本机。请再试一次。"
}

// TokenProLocalImageDownloadAvailable reports a shell or exec tool Codex can
// run. apply_patch does not count. The header is chosen before image bytes exist.
func TokenProLocalImageDownloadAvailable(body []byte) bool {
	_, ok := selectTokenProLocalImageTool(body)
	return ok
}

// BuildTokenProLocalImageCall returns a shell/exec function_call Codex 0.163
// desktop can run. apply_patch is ignored. ok is false when no shell tool is
// advertised, and the caller keeps the HTTPS link text.
func BuildTokenProLocalImageCall(body []byte, imageURL, threadID string) (map[string]any, bool) {
	tool, ok := selectTokenProLocalImageTool(body)
	if !ok || !safeTokenProImageURL(imageURL) {
		return nil, false
	}
	fileID := strings.ReplaceAll(uuid.NewString(), "-", "")
	callID := tokenProLocalImageCallPrefix + fileID
	thread := sanitizeTokenProPathSegment(threadID)
	if thread == "" {
		thread = "session"
	}
	script := tokenProLocalImageScript(imageURL, thread, fileID, tool.windows)
	switch tool.name {
	case "local_shell":
		command := tokenProLocalImageArgv(script, tool.windows)
		return map[string]any{
			"type":    "local_shell_call",
			"id":      "lsh_" + fileID,
			"call_id": callID,
			"status":  "in_progress",
			"action": map[string]any{
				"type":       "exec",
				"command":    command,
				"timeout_ms": 60000,
			},
		}, true
	case "shell":
		args := map[string]any{
			"command":    tokenProLocalImageArgv(script, tool.windows),
			"timeout_ms": 60000,
		}
		return localImageFunctionCall(tool, callID, fileID, tokenProLocalImageEscalation(body, args)), true
	default:
		shell := "bash"
		if tool.windows {
			shell = "powershell.exe"
		}
		args := map[string]any{
			"cmd":               script,
			"shell":             shell,
			"login":             false,
			"tty":               false,
			"yield_time_ms":     60000,
			"max_output_tokens": 2000,
		}
		return localImageFunctionCall(tool, callID, fileID, tokenProLocalImageEscalation(body, args)), true
	}
}

// tokenProLocalImageEscalation asks Codex to leave the sandbox when the
// request is still sandboxed. danger-full-access, sandbox none, and approval
// never already allow the download, and an extra approval field makes exec
// wait instead of running.
func tokenProLocalImageEscalation(body []byte, args map[string]any) map[string]any {
	if tokenProLocalImageUnrestricted(body) {
		return args
	}
	args["with_escalated_permissions"] = true
	args["sandbox_permissions"] = "require_escalated"
	args["justification"] = "把生成的图片保存到本机，以便在对话里直接显示。"
	return args
}

var tokenProLocalImageUnrestrictedPattern = regexp.MustCompile(`(?i)danger-full-access|(?:sandbox(?:_mode)?|sandbox mode)\s*(?:["']?\s*[:=]\s*["']?|\bis\s+)none\b|sandbox\s+none\b|approval(?:_policy|\s+policy)?\s*(?:["']?\s*[:=]\s*["']?|\bis\s+)never\b|approval\s+never\b`)

func tokenProLocalImageUnrestricted(body []byte) bool {
	// The user prompt is not a signal. Codex records the live sandbox in
	// instructions and client_metadata, including the nested turn-metadata JSON.
	blob := gjson.GetBytes(body, "instructions").String() + "\n" + gjson.GetBytes(body, "client_metadata").Raw
	if nested := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String(); gjson.Valid(nested) {
		blob += "\n" + nested
	}
	return tokenProLocalImageUnrestrictedPattern.MatchString(blob)
}

type tokenProLocalImageTool struct {
	name      string
	namespace string
	windows   bool
}

func selectTokenProLocalImageTool(body []byte) (tokenProLocalImageTool, bool) {
	found := map[string]tokenProLocalImageTool{}
	var walk func(tool gjson.Result, namespace string)
	walk = func(tool gjson.Result, namespace string) {
		kind := tool.Get("type").String()
		name := tool.Get("name").String()
		if kind == "namespace" {
			ns := name
			for _, child := range tool.Get("tools").Array() {
				walk(child, ns)
			}
			return
		}
		if kind == "local_shell" {
			name = "local_shell"
		}
		switch name {
		case "exec_command", "shell", "local_shell":
			if _, exists := found[name]; exists {
				return
			}
			description := tool.Get("description").String()
			found[name] = tokenProLocalImageTool{
				name:      name,
				namespace: namespace,
				windows:   tokenProLocalImageWindows(body, description),
			}
		}
	}
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		walk(tool, "")
	}
	for _, name := range []string{"exec_command", "shell", "local_shell"} {
		if tool, ok := found[name]; ok {
			return tool, true
		}
	}
	return tokenProLocalImageTool{}, false
}

func tokenProLocalImageWindows(body []byte, description string) bool {
	// The user prompt is not a signal: a drawing request can mention Windows.
	// Codex puts the host shell in the tool description, client metadata, or
	// the developer instructions.
	blob := strings.ToLower(description + "\n" + gjson.GetBytes(body, "instructions").String() + "\n" + gjson.GetBytes(body, "client_metadata").Raw)
	return strings.Contains(blob, "powershell") || strings.Contains(blob, "createprocessw") || strings.Contains(blob, "win32")
}

func localImageFunctionCall(tool tokenProLocalImageTool, callID, fileID string, args map[string]any) map[string]any {
	encoded, _ := json.Marshal(args)
	item := map[string]any{
		"type":      "function_call",
		"id":        "fc_" + fileID,
		"call_id":   callID,
		"name":      tool.name,
		"arguments": string(encoded),
	}
	if tool.namespace != "" {
		item["namespace"] = tool.namespace
	}
	return item
}

func tokenProLocalImageScript(imageURL, thread, fileID string, windows bool) string {
	quoted := "'" + strings.ReplaceAll(imageURL, "'", "") + "'"
	if windows {
		dir := strings.ReplaceAll(tokenProLocalImageDir, "/", `\`)
		return fmt.Sprintf(`$dir = Join-Path $env:USERPROFILE '%s\%s'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir '%s.png'
# %s%s
try {
  Invoke-WebRequest -Uri %s -OutFile $dest -UseBasicParsing
  $shown = ((Resolve-Path -LiteralPath $dest).Path) -replace '\\','/'
  Write-Output ('%s' + $shown)
} catch {
  Write-Output '%s'
  exit 1
}
`, dir, thread, fileID, tokenProLocalImageURLMarker, imageURL, quoted, tokenProLocalImageMarker, tokenProLocalImageFail)
	}
	return fmt.Sprintf(`set -eu
dest="$HOME/%s/%s/%s.png"
mkdir -p "$(dirname "$dest")"
# %s%s
if curl -fsSL --retry 2 --max-time 60 -o "$dest" %s; then
  printf '%s%%s\n' "$dest"
else
  printf '%s\n'
  exit 1
fi
`, tokenProLocalImageDir, thread, fileID, tokenProLocalImageURLMarker, imageURL, quoted, tokenProLocalImageMarker, tokenProLocalImageFail)
}

func tokenProLocalImageArgv(script string, windows bool) []string {
	if windows {
		return []string{"powershell.exe", "-NoProfile", "-Command", script}
	}
	return []string{"bash", "-lc", script}
}

func sanitizeTokenProPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			_, _ = b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func safeTokenProImageURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "https://") || len(raw) > 2048 {
		return false
	}
	return !strings.ContainsAny(raw, " \t\r\n'\"`$\\;")
}

func localImageCommandText(item gjson.Result) string {
	if args := item.Get("arguments").String(); args != "" {
		return decodeLocalImageCommand(args)
	}
	if input := item.Get("input").String(); input != "" {
		return decodeLocalImageCommand(input)
	}
	return decodeLocalImageCommand(item.Get("action.command").Raw)
}

// decodeLocalImageCommand reads the shell text out of a function_call.
// encoding/json writes & as \u0026 and newlines as \n, so searching the raw
// arguments string would not find the signed URL.
func decodeLocalImageCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !gjson.Valid(raw) {
		return raw
	}
	parsed := gjson.Parse(raw)
	switch parsed.Type {
	case gjson.String:
		return parsed.String()
	case gjson.JSON:
		if cmd := parsed.Get("cmd").String(); cmd != "" {
			return cmd
		}
		if parsed.Get("command").IsArray() {
			parts := parsed.Get("command").Array()
			texts := make([]string, 0, len(parts))
			for _, part := range parts {
				texts = append(texts, part.String())
			}
			return strings.Join(texts, "\n")
		}
		if command := parsed.Get("command").String(); command != "" {
			return command
		}
	}
	return raw
}

func localImageOutputText(item gjson.Result) string {
	output := item.Get("output")
	switch output.Type {
	case gjson.String:
		return output.String()
	case gjson.JSON:
		if text := output.Get("output").String(); text != "" {
			return text
		}
		return output.Raw
	default:
		return output.Raw
	}
}

var tokenProLocalImagePathPattern = regexp.MustCompile(`TOKENPRO_IMAGE_PATH\s+([A-Za-z]:/[A-Za-z0-9._~/-]+|/[A-Za-z0-9._~/-]+)`)

func tokenProLocalImagePath(output string) (string, bool) {
	if strings.Contains(output, tokenProLocalImageFail) {
		return "", false
	}
	match := tokenProLocalImagePathPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return "", false
	}
	path := match[1]
	if strings.Contains(path, "..") || strings.Contains(path, ")") {
		return "", false
	}
	if !strings.Contains(path, "/"+tokenProLocalImageDir+"/") {
		return "", false
	}
	return path, true
}

func localImageURLFromCommand(command string) string {
	index := strings.Index(command, tokenProLocalImageURLMarker)
	if index < 0 {
		return ""
	}
	rest := command[index+len(tokenProLocalImageURLMarker):]
	end := strings.IndexAny(rest, " \t\r\n'\"")
	if end >= 0 {
		rest = rest[:end]
	}
	if !safeTokenProImageURL(rest) {
		return ""
	}
	return rest
}

// TokenProRequestThreadID reads the Codex thread id from the request body.
func TokenProRequestThreadID(body []byte) string {
	if id := strings.TrimSpace(gjson.GetBytes(body, "client_metadata.thread_id").String()); ValidTokenProCorrelationID(id) {
		return id
	}
	raw := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String()
	if gjson.Valid(raw) {
		if id := strings.TrimSpace(gjson.Get(raw, "thread_id").String()); ValidTokenProCorrelationID(id) {
			return id
		}
	}
	return ""
}
