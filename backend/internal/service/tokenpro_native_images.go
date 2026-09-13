package service

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
)

// Set only by authenticated TokenPro direct-route middleware, not from an
// upstream response. It changes tool transport, never group authorization.
const TokenProNativeImagesContextKey = "tokenpro_native_images_v1"
const TokenProNativeImageDriverContextKey = "tokenpro_native_image_driver"

// A text-only selection invokes the image tool through its authorized text
// model, just as a direct Responses image_generation request does. This is not
// permission to select another group or an arbitrary native image model.
func TokenProNativeImageDriver(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	path := c.Request.URL.Path
	if !strings.HasSuffix(path, "/images/generations") && !strings.HasSuffix(path, "/images/edits") {
		return ""
	}
	return c.GetString(TokenProNativeImageDriverContextKey)
}

func TokenProNativeImages(c *gin.Context) bool {
	return c != nil && c.GetBool(TokenProNativeImagesContextKey)
}

// Use the trusted resolved group's description field, never its display name
// or a client-supplied label. Text models retain their own routing/authorization.
func TokenProPureImageGroup(group *Group) bool {
	return group != nil && group.Platform == PlatformOpenAI && strings.TrimSpace(group.Description) == "生图"
}

// Pure-image groups outside this explicit policy must keep their legacy path,
// including downstream hosted-image detection. Merely skipping dispatch is not enough.
func RestrictTokenProNativeImages(c *gin.Context, group *Group, model string) {
	if TokenProNativeImages(c) && IsGPTImageGenerationModel(model) && !TokenProPureImageGroup(group) {
		c.Set(TokenProNativeImagesContextKey, false)
	}
}

const tokenProNativeImageInstructions = "[TokenPro native image delivery v1] For image generation or editing, call the client's image_gen.imagegen function. Its result is delivered by the client's native Images API tool. Do not use the hosted image_generation tool: this client cannot display its result. Do not claim an image is generated or displayed unless the native tool returned an actual image. On tool failure, report the error; do not retry through hosted image_generation or replace it with a text-only success message."

// PrepareTokenProNativeImages adapts only explicitly opted-in Codex requests.
// It never changes history, emits a tool result, or manufactures image success.
// Pure-image dispatch is handled before upstream forwarding. It must never
// borrow a text model as a fallback here.
func PrepareTokenProNativeImages(body []byte) ([]byte, error) {
	var request map[string]any
	// Preserve large numeric metadata fields rather than round-tripping float64.
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&request); err != nil {
		return nil, err
	}
	if request == nil {
		return body, nil
	}
	model, _ := request["model"].(string)
	if isOpenAIImageGenerationModel(model) {
		return body, nil
	}
	tools, _ := request["tools"].([]any)
	kept := make([]any, 0, len(tools)+1)
	hasNative := false
	var nativeNamespace map[string]any
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		if tool["type"] == "image_generation" {
			continue
		}
		if tool["type"] == "namespace" && tool["name"] == "image_gen" {
			nativeNamespace = tool
			children, _ := tool["tools"].([]any)
			for _, child := range children {
				if nested, ok := child.(map[string]any); ok && nested["name"] == "imagegen" {
					hasNative = true
				}
			}
		}
		kept = append(kept, raw)
	}
	if !hasNative {
		native := map[string]any{
			"type": "namespace", "name": "image_gen", "description": "Generate and edit images using the Codex client's native image tool.",
			"tools": []any{map[string]any{
				"type": "function", "name": "imagegen", "description": "Generate an image or edit existing images and return the real image to the current conversation.",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{
					"prompt":                     map[string]any{"type": "string"},
					"referenced_image_paths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"num_last_images_to_include": map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
				}, "required": []string{"prompt"}, "additionalProperties": false}, "strict": false,
			}},
		}
		if nativeNamespace == nil {
			kept = append(kept, native)
		} else {
			children, _ := nativeNamespace["tools"].([]any)
			nativeNamespace["tools"] = append(children, native["tools"].([]any)...)
		}
	}
	request["tools"] = kept
	if choice, ok := request["tool_choice"].(map[string]any); ok && choice["type"] == "image_generation" {
		// Never force another generation on the tool-result continuation.
		request["tool_choice"] = "auto"
	}
	instructions, _ := request["instructions"].(string)
	if !strings.Contains(instructions, tokenProNativeImageInstructions) {
		request["instructions"] = strings.TrimSpace(instructions + "\n\n" + tokenProNativeImageInstructions)
	}
	return json.Marshal(request)
}
