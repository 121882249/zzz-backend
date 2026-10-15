package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Compatibility with the already-installed Skill catalog, including clients
// predating explicit CLI capability markers. Never inspect user/tool messages.
var tokenProImageSkillCatalogLine = regexp.MustCompile(`(?m)^- (imagegen|tokenpro-imagegen): [^\r\n]*TokenPro[^\r\n]*\(file: ([^\r\n]+)\)[ \t]*$`)
var tokenProImageSkillRoot = regexp.MustCompile("(?m)^- `(r[0-9]+)` = `([^`\\r\\n]+)`[ \\t]*$")

// The CLI already waits for the Images response and saves it before returning
// success. Short shell polls add a full text-model round trip between polls and
// can leave a completed image waiting locally. Change orchestration guidance
// only: never change the selected model, reasoning effort or admission checks.
const tokenProTextImageFastDeliveryInstructions = "[TokenPro fast image delivery v1] For a simple image-only request, after reading the selected Skill, invoke its documented CLI directly; do not inspect its launcher or call --help. Run the image command with exec_command yield_time_ms: 30000, and allow an enclosing functions.exec at least 30000 ms as well. If execution is still running, wait on that existing session or cell with a 30000 ms wait instead of repeated short polls; never rerun generation just to check completion. Only after the CLI reports exit code 0 and an actual saved absolute image path, immediately finish with exactly one Markdown image using that path. The successful CLI save is sufficient confirmation: do not add file inspection, view_image, hashing, a second image request, or extra verification commands before displaying it, unless the user requested inspection or validation. Do not perform unrelated work before displaying the saved image. Failed, incomplete or ambiguous outputs must not be treated as success. If the user also requested other work, complete that work normally; this shortcut does not discard it."

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

// This changes tool delivery only. It uses the already-authorized request's
// original group/model, before channel mappings, and adds no tool definitions.
func prepareTokenProImageTools(c *gin.Context, body []byte) ([]byte, error) {
	if tokenProHasNativeImageTool(body) || TokenProNativeImages(c) {
		return PrepareTokenProNativeImages(body)
	}
	skill := tokenProAdvertisedImageSkill(body)
	key := getAPIKeyFromContext(c)
	v, _ := c.Get(TokenProImageTurnContextKey)
	pending, _ := v.(*TokenProPendingImageTurn)
	if skill == "" || key == nil || !key.IsGlobal() || key.GroupID == nil || *key.GroupID <= 0 ||
		pending == nil || !strings.HasPrefix(pending.Model, "gpt-") || IsGPTImageGenerationModel(pending.Model) {
		// No known client implementation: retain the ordinary group-key path,
		// including its explicit tools and hosted-image policy.
		c.Set(TokenProTextImageDeliveryContextKey, false)
		return body, nil
	}
	route := fmt.Sprintf("tp-g%d-%s", *key.GroupID, base64.RawURLEncoding.EncodeToString([]byte(pending.Model)))
	quotedSkill, _ := json.Marshal(skill)
	quotedRoute, _ := json.Marshal(route)
	guidance := fmt.Sprintf("[TokenPro text image tool compatibility v1] For image generation, read and follow the installed TokenPro image Skill at %s through the client's existing tools. Pass --model %s to its image CLI so it uses this conversation's authorized text group/model, not a separately saved image selection. Do not change providers or routing configuration. Never call image_gen.imagegen or any other tool absent from the client's declared tools. Do not claim success unless the CLI returned an actual saved image; report failures accurately. Ordinary non-image requests must remain ordinary text requests.", quotedSkill, quotedRoute)
	guidance += "\n\n" + tokenProTextImageFastDeliveryInstructions
	instructions := strings.ReplaceAll(gjson.GetBytes(body, "instructions").String(), tokenProNativeImageInstructions, "")
	if !strings.Contains(instructions, guidance) {
		instructions = strings.TrimSpace(instructions + "\n\n" + guidance)
	}
	return sjson.SetBytes(body, "instructions", instructions)
}
