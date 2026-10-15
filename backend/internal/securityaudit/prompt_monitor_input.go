package securityaudit

import (
	"encoding/json"
	"errors"
	"strings"
)

const monitoringUserInputVersion = "user-input-v2"

// Monitoring is a user-input view, not the security scanner's transcript.
// Keep the complete scanner extraction unchanged and select only the latest
// user message here, preserving all of that message's text parts in order.
func extractMonitoringSnapshot(req Request) (PromptSnapshot, error) {
	var root map[string]any
	if err := json.Unmarshal(req.Body, &root); err != nil {
		return PromptSnapshot{}, errors.New("content monitor request JSON is invalid")
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	var texts []string
	switch protocol {
	case "openai_responses", "responses", "responses_websocket":
		if frame := stringValue(root["type"]); frame != "" || protocol == "responses_websocket" {
			if frame != "response.create" {
				return PromptSnapshot{}, ErrNoPromptText
			}
			if root["input"] == nil {
				root, _ = root["response"].(map[string]any)
			}
		}
		texts = latestMonitoringUserTexts(root["input"], false, true)
	case "gemini", "gemini_generate_content":
		value := root["contents"]
		if value == nil {
			value = root["content"]
		}
		texts = latestMonitoringUserTexts(value, true, true)
		if len(texts) == 0 {
			texts = extractGeminiInstanceInput(root["instances"])
		}
	case "openai_images", "grok_media", "media", "images":
		texts = extractMediaPrompts(root)
	case "typesafe_systemone":
		texts = promptSegmentTexts(extractSystemOneSegments(root))
	default:
		texts = latestMonitoringUserTexts(root["messages"], false, false)
	}
	if len(texts) == 0 {
		return PromptSnapshot{}, ErrNoPromptText
	}
	// One segment avoids the audit's latest-part-first prioritization changing
	// the order of a multi-part user message in the monitoring UI.
	return snapshotFromSegments(req, []string{strings.Join(texts, "\n\n")})
}

func latestMonitoringUserTexts(value any, gemini, allowImplicitRole bool) []string {
	var items []any
	switch typed := value.(type) {
	case string:
		return monitoringTextParts([]string{typed})
	case []any:
		items = typed
	case map[string]any:
		items = []any{typed}
	}
	for index := len(items) - 1; index >= 0; index-- {
		if text, ok := items[index].(string); ok && allowImplicitRole {
			if texts := monitoringTextParts([]string{text}); len(texts) > 0 {
				return texts
			}
			continue
		}
		item, ok := items[index].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringValue(item["role"])))
		if role != "user" && (!allowImplicitRole || role != "") {
			continue
		}
		if !gemini {
			if kind := stringValue(item["type"]); kind != "" && kind != "message" {
				continue // function calls/results are not implicit user input
			}
		}
		content := item["content"]
		if gemini {
			content = item["parts"]
		}
		raw := contentTexts(content)
		texts := monitoringTextParts(raw)
		if len(raw) > 0 && len(texts) == 0 {
			continue // client-injected environment-only message, not a user turn
		}
		return texts // image-only input must not accidentally repeat an old turn
	}
	return nil
}

func monitoringTextParts(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		for strings.HasPrefix(text, "<environment_context>") {
			_, rest, closed := strings.Cut(text, "</environment_context>")
			if !closed {
				break
			}
			text = strings.TrimSpace(rest)
		}
		if strings.HasPrefix(text, "# AGENTS.md instructions for ") && strings.HasSuffix(text, "</INSTRUCTIONS>") {
			continue
		}
		if strings.HasPrefix(text, "# Files mentioned by the user:") {
			if _, request, ok := strings.Cut(text, "## My request:"); ok {
				text = strings.TrimSpace(request)
			}
		}
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func extractGeminiInstanceInput(value any) []string {
	segments := extractGeminiInstances(value)
	return monitoringTextParts(promptSegmentTexts(segments))
}
