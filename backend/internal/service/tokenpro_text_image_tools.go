package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Compatibility with the already-installed Skill catalog, including clients
// predating explicit CLI capability markers. Never inspect user/tool messages.
var tokenProImageSkillCatalogLine = regexp.MustCompile(`(?m)^- (imagegen|tokenpro-imagegen): [^\r\n]*TokenPro[^\r\n]*\(file: ([^\r\n]+)\)[ \t]*$`)
var tokenProImageSkillCatalogLineDrop = regexp.MustCompile(`(?m)^- (imagegen|tokenpro-imagegen): [^\r\n]*TokenPro[^\r\n]*\(file: [^\r\n]+\)[ \t]*\r?\n?`)
var tokenProImageSkillRoot = regexp.MustCompile("(?m)^- `(r[0-9]+)` = `([^`\\r\\n]+)`[ \\t]*$")

// Retained only so a replayed request that already carries the retired CLI
// guidance is stripped. New turns must not be steered at the client skill.
const tokenProTextImageFastDeliveryInstructions = "[TokenPro fast image delivery v1] For a simple image-only request, after reading the selected Skill, invoke its documented CLI directly; do not inspect its launcher or call --help. Run the image command with exec_command yield_time_ms: 30000, and allow an enclosing functions.exec at least 30000 ms as well. If execution is still running, wait on that existing session or cell with a 30000 ms wait instead of repeated short polls; never rerun generation just to check completion. Only after the CLI reports exit code 0 and an actual saved absolute image path, immediately finish with exactly one Markdown image using that path. The successful CLI save is sufficient confirmation: do not add file inspection, view_image, hashing, a second image request, or extra verification commands before displaying it, unless the user requested inspection or validation. Do not perform unrelated work before displaying the saved image. Failed, incomplete or ambiguous outputs must not be treated as success. If the user also requested other work, complete that work normally; this shortcut does not discard it."
const tokenProTextImageSkillGuidanceMarker = "[TokenPro text image tool compatibility v1]"

// Replaces an advertised image skill so the model does not launch its CLI.
// The hosted image_generation tool, injected later by the Codex bridge, is the
// only image path. Title turns use the shorter line and stay text-only.
const tokenProImageSkillNeutralLine = "- imagegen: unavailable in this session. Do not read or run imagegen or tokenpro-imagegen. Raster images are produced only by the hosted image_generation tool on this request."
const tokenProImageSkillNeutralTitleLine = "- imagegen: unavailable in this session. Do not read or run imagegen or tokenpro-imagegen."

// Set only when this text turn must use the hosted image_generation bridge.
// Passthrough returns before the ordinary bridge, so it reads this flag.
const tokenProHostedImageBridgeContextKey = "tokenpro_hosted_image_bridge"

func tokenProHostedImageBridgeRequested(c *gin.Context) bool {
	return c != nil && c.GetBool(tokenProHostedImageBridgeContextKey)
}

func setTokenProHostedImageBridge(c *gin.Context, enabled bool) {
	if c == nil {
		return
	}
	c.Set(tokenProHostedImageBridgeContextKey, enabled)
}

func tokenProDeveloperTexts(body []byte) []string {
	var texts []string
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() != "developer" {
			continue
		}
		if content := item.Get("content"); content.Type == gjson.String {
			texts = append(texts, content.String())
		}
		for _, part := range item.Get("content").Array() {
			if part.Get("type").String() == "input_text" {
				texts = append(texts, part.Get("text").String())
			}
		}
	}
	return texts
}

func tokenProAdvertisedImageSkill(body []byte) string {
	texts := tokenProDeveloperTexts(body)
	roots := make(map[string]string)
	conflicts := make(map[string]bool)
	for _, text := range texts {
		for _, root := range tokenProImageSkillRoot.FindAllStringSubmatch(text, -1) {
			if previous, exists := roots[root[1]]; exists && previous != root[2] {
				conflicts[root[1]] = true
			}
			roots[root[1]] = root[2]
		}
	}
	var selected string
	for _, text := range texts {
		for _, match := range tokenProImageSkillCatalogLine.FindAllStringSubmatch(text, -1) {
			skill := match[2]
			if alias, suffix, found := strings.Cut(skill, "/"); found && suffix == match[1]+"/SKILL.md" && !conflicts[alias] && roots[alias] != "" {
				skill = strings.TrimRight(roots[alias], "/\\") + "/" + suffix
			}
			normalized := strings.ReplaceAll(skill, "\\", "/")
			absolute := strings.HasPrefix(normalized, "/") || (len(normalized) > 3 && normalized[1] == ':' && normalized[2] == '/')
			if !absolute || strings.ContainsAny(skill, "\r\n\x00") || !strings.HasSuffix(normalized, "/skills/"+match[1]+"/SKILL.md") {
				continue
			}
			if selected == "" || match[1] == "tokenpro-imagegen" {
				selected = skill
			}
		}
	}
	return selected
}

func tokenProHostedImageEligible(c *gin.Context) bool {
	if c == nil {
		return false
	}
	key := getAPIKeyFromContext(c)
	v, _ := c.Get(TokenProImageTurnContextKey)
	pending, _ := v.(*TokenProPendingImageTurn)
	return key != nil && key.IsGlobal() && key.GroupID != nil && *key.GroupID > 0 &&
		pending != nil && strings.HasPrefix(pending.Model, "gpt-") && !IsGPTImageGenerationModel(pending.Model)
}

// Title generation is metadata. Codex sends it with no tools and a json_schema
// answer, or labels the turn thread_title. It must not gain an image tool or a
// second image bill.
func tokenProTitleGenerationRequest(c *gin.Context, body []byte) bool {
	if c != nil {
		if meta := c.GetHeader("X-Codex-Turn-Metadata"); gjson.Valid(meta) && gjson.Get(meta, "thread_source").String() == "thread_title" {
			return true
		}
	}
	if !gjson.ValidBytes(body) {
		return false
	}
	if tokenProChatImageThreadSource(body) == "thread_title" {
		return true
	}
	tools := gjson.GetBytes(body, "tools")
	noTools := !tools.Exists() || (tools.IsArray() && len(tools.Array()) == 0)
	if !noTools {
		return false
	}
	for _, path := range []string{"text.format.type", "response_format.type"} {
		if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, path).String()), "json_schema") {
			return true
		}
	}
	return false
}

func stripTokenProLegacyImageSkillInstructions(instructions string) string {
	if !strings.Contains(instructions, tokenProTextImageSkillGuidanceMarker) &&
		!strings.Contains(instructions, tokenProTextImageFastDeliveryInstructions) {
		return instructions
	}
	instructions = strings.ReplaceAll(instructions, "\n\n"+tokenProTextImageFastDeliveryInstructions, "")
	instructions = strings.ReplaceAll(instructions, tokenProTextImageFastDeliveryInstructions, "")
	for {
		index := strings.Index(instructions, tokenProTextImageSkillGuidanceMarker)
		if index < 0 {
			break
		}
		end := strings.Index(instructions[index:], "\n\n")
		if end < 0 {
			instructions = instructions[:index]
			break
		}
		instructions = instructions[:index] + instructions[index+end+2:]
	}
	return strings.TrimSpace(instructions)
}

func neutralizeTokenProImageSkillText(text string, title bool) string {
	text = stripTokenProLegacyImageSkillInstructions(text)
	if !tokenProImageSkillCatalogLine.MatchString(text) {
		return text
	}
	line := tokenProImageSkillNeutralLine
	if title {
		line = tokenProImageSkillNeutralTitleLine
	}
	cleaned := strings.TrimLeft(tokenProImageSkillCatalogLineDrop.ReplaceAllString(text, ""), "\r\n")
	if strings.Contains(cleaned, line) {
		return cleaned
	}
	if strings.TrimSpace(cleaned) == "" {
		return line
	}
	return line + "\n" + cleaned
}

// Developer-catalog lines are the only skill hint this gateway trusts. User and
// tool messages are left untouched. A no-op returns the original bytes.
func neutralizeTokenProImageSkillHints(body []byte, title bool) ([]byte, error) {
	if !gjson.ValidBytes(body) {
		return body, nil
	}
	updated := body
	changed := false
	if instructions := gjson.GetBytes(body, "instructions"); instructions.Exists() && instructions.Type == gjson.String {
		cleaned := neutralizeTokenProImageSkillText(instructions.String(), title)
		if cleaned != instructions.String() {
			var err error
			updated, err = sjson.SetBytes(updated, "instructions", cleaned)
			if err != nil {
				return nil, err
			}
			changed = true
		}
	}
	input := gjson.GetBytes(body, "input")
	if input.IsArray() {
		for i, item := range input.Array() {
			if item.Get("role").String() != "developer" {
				continue
			}
			content := item.Get("content")
			if content.Type == gjson.String {
				cleaned := neutralizeTokenProImageSkillText(content.String(), title)
				if cleaned == content.String() {
					continue
				}
				var err error
				updated, err = sjson.SetBytes(updated, "input."+strconv.Itoa(i)+".content", cleaned)
				if err != nil {
					return nil, err
				}
				changed = true
				continue
			}
			if !content.IsArray() {
				continue
			}
			for j, part := range content.Array() {
				if part.Get("type").String() != "input_text" {
					continue
				}
				text := part.Get("text").String()
				cleaned := neutralizeTokenProImageSkillText(text, title)
				if cleaned == text {
					continue
				}
				var err error
				updated, err = sjson.SetBytes(updated, "input."+strconv.Itoa(i)+".content."+strconv.Itoa(j)+".text", cleaned)
				if err != nil {
					return nil, err
				}
				changed = true
			}
		}
	}
	if !changed {
		return body, nil
	}
	return updated, nil
}

// prepareTokenProImageTools never routes a text model through the client
// tokenpro-imagegen skill. Clearing text-image delivery lets the existing Codex
// hosted image_generation bridge inject the tool and bill that single text-row
// forward. Title turns keep delivery set so the bridge stays off.
func prepareTokenProImageTools(c *gin.Context, body []byte) ([]byte, error) {
	setTokenProHostedImageBridge(c, false)
	if tokenProHasNativeImageTool(body) || TokenProNativeImages(c) {
		return PrepareTokenProNativeImages(body)
	}
	if !tokenProHostedImageEligible(c) {
		// No known client implementation: retain the ordinary group-key path,
		// including its explicit tools and hosted-image policy.
		c.Set(TokenProTextImageDeliveryContextKey, false)
		return body, nil
	}
	title := tokenProTitleGenerationRequest(c, body)
	neutralized, err := neutralizeTokenProImageSkillHints(body, title)
	if err != nil {
		return nil, err
	}
	if title {
		return neutralized, nil
	}
	c.Set(TokenProTextImageDeliveryContextKey, false)
	setTokenProHostedImageBridge(c, true)
	return neutralized, nil
}

// applyTokenProPassthroughHostedImage injects the Codex hosted image tool on
// the passthrough path. That path returns before the ordinary bridge, so a
// global text turn would otherwise lose the client skill and still have no
// image tool. The same bridge gate applies: Codex CLI, group permission,
// explicit-tool policy, and the account/channel/config switch. Title turns
// never set the request flag. The pre-injection body is what the local
// download call uses, so the client still sees its own shell tool.
func (s *OpenAIGatewayService) applyTokenProPassthroughHostedImage(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	isCodexCLI bool,
	explicitToolPolicy string,
) ([]byte, bool, error) {
	if !tokenProHostedImageBridgeRequested(c) || c == nil || account == nil || !isCodexCLI {
		return body, false, nil
	}
	if isOpenAIResponsesCompactPath(c) || isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader)) {
		return body, false, nil
	}
	if explicitToolPolicy == codexImageGenerationExplicitToolPolicyStrip {
		return body, false, nil
	}
	apiKey := getAPIKeyFromContext(c)
	if !GroupAllowsImageGeneration(apiKeyGroup(apiKey)) || !s.isCodexImageGenerationBridgeEnabled(ctx, account, apiKey) {
		return body, false, nil
	}
	var decoded map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &decoded); err != nil {
		return nil, false, err
	}
	if !ensureOpenAIResponsesImageGenerationTool(decoded) {
		return body, false, nil
	}
	ensureOpenAIResponsesImageGenerationToolChoiceAuto(decoded)
	normalizeOpenAIResponsesImageGenerationTools(decoded)
	applyCodexImageGenerationBridgeInstructions(decoded)
	injected, err := marshalOpenAIUpstreamJSON(decoded)
	if err != nil {
		return nil, false, err
	}
	armTokenProHostedImageDelivery(c, body)
	logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Injected /responses image_generation tool for Codex passthrough text model")
	return injected, true, nil
}
