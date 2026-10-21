package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const TokenProImageTurnContextKey = "tokenpro_image_turn_v2"
const TokenProImageSessionContextKey = "tokenpro_image_session_v2"
const TokenProImageThreadBoundContextKey = "tokenpro_image_thread_bound"
const TokenProImageTurnTTL = 30 * time.Minute
const TokenProImageThreadTTL = 24 * time.Hour

// ErrTokenProImageThreadUnbound means a Codex turn named a thread but the
// latest image model could not be recorded before the function_call returned.
var ErrTokenProImageThreadUnbound = errors.New("无法记录本次对话的生图模型，没有改用其他模型。")

var ErrImageTurnMissing = errors.New("image turn route missing or expired")
var ErrImageTurnConflict = errors.New("image turn route conflicts with existing selection")

// Turn IDs are correlation hints, never credentials. Credential fingerprints
// isolate rotations even though regenerating a global key preserves its row ID.
func TokenProImageTurnKey(key *APIKey, turn string) string {
	sum := sha256.Sum256([]byte(key.Key))
	return fmt.Sprintf("tokenpro:image-turn:v2:%d:%d:%s:%s", key.UserID, key.ID, hex.EncodeToString(sum[:]), turn)
}

type TokenProImageTurnRoute struct {
	GroupID  int64  `json:"group_id"`
	Model    string `json:"model"`
	ThreadID string `json:"thread_id"`
}

type TokenProImageTurnStore interface {
	Bind(context.Context, string, TokenProImageTurnRoute) error
	Lookup(context.Context, string) (TokenProImageTurnRoute, error)
}

// TokenProImageThreadStore remembers the latest image-group chat model for one
// Codex thread. Unlike the per-turn record, a later turn overwrites it.
type TokenProImageThreadStore interface {
	RememberThread(context.Context, string, TokenProImageTurnRoute) error
	LookupThread(context.Context, string) (TokenProImageTurnRoute, error)
}

// TokenProImageThreadKey isolates a thread binding by the caller's global key.
// Thread IDs are correlation hints, never credentials.
func TokenProImageThreadKey(key *APIKey, thread string) string {
	sum := sha256.Sum256([]byte(key.Key))
	return fmt.Sprintf("tokenpro:image-thread:v1:%d:%d:%s:%s", key.UserID, key.ID, hex.EncodeToString(sum[:]), thread)
}

// ValidTokenProCorrelationID accepts the canonical UUID Codex puts in
// client_metadata.thread_id and x-codex-turn-metadata.thread_id.
func ValidTokenProCorrelationID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

// Pending metadata is installed by ingress but bound only after the handler
// validates the user's selected group. Store before any tool call is emitted.
type TokenProPendingImageTurn struct {
	Store         TokenProImageTurnStore
	TurnID        string
	ThreadID      string
	Model         string
	ValidationErr error
	Legacy        bool
}

func BindTokenProImageTurn(c *gin.Context, key *APIKey) error {
	// Resolve the trusted group before enforcing any native-only metadata.
	// Old clients may still send a provider-wide native header for text groups.
	pure := key != nil && TokenProPureImageGroup(key.Group)
	textDelivery := key != nil && tokenProTextImageGroup(key.Group) && !pure
	eligible := key != nil && key.IsGlobal() && key.GroupID != nil && (pure || textDelivery)
	if !eligible {
		imageRequest := TokenProNativeImages(c) && c.Request != nil &&
			(strings.HasSuffix(c.Request.URL.Path, "/images/generations") || strings.HasSuffix(c.Request.URL.Path, "/images/edits"))
		c.Set(TokenProNativeImagesContextKey, false)
		c.Set(TokenProTextImageDeliveryContextKey, false)
		c.Set(TokenProNativeImageDriverContextKey, "")
		if imageRequest {
			// Reject stale native turn routes after a group's policy changes.
			return ErrImageTurnConflict
		}
		return nil
	}
	v, ok := c.Get(TokenProImageTurnContextKey)
	if !ok {
		// Images already resolved this driver through the authenticated turn
		// store in ingress. Do not discard it merely because this is a text group.
		if textDelivery && TokenProNativeImages(c) {
			c.Set(TokenProNativeImagesContextKey, false)
			if driver := TokenProNativeImageDriver(c); !strings.HasPrefix(driver, "gpt-") || IsGPTImageGenerationModel(driver) {
				c.Set(TokenProNativeImageDriverContextKey, "")
				return ErrImageTurnConflict
			}
			c.Set(TokenProTextImageDeliveryContextKey, true)
		}
		return nil
	}
	pending, ok := v.(*TokenProPendingImageTurn)
	if !ok || pending == nil {
		if textDelivery {
			return nil
		}
		return ErrImageTurnMissing
	}
	if textDelivery {
		c.Set(TokenProNativeImagesContextKey, false)
		c.Set(TokenProTextImageDeliveryContextKey, false)
		// Missing/invalid metadata must not break an ordinary text request.
		// Other platforms/models and old provider-wide mode headers do not opt in.
		if pending.Legacy || pending.ValidationErr != nil || pending.Store == nil ||
			pending.TurnID == "" || pending.ThreadID == "" ||
			!strings.HasPrefix(pending.Model, "gpt-") || IsGPTImageGenerationModel(pending.Model) {
			return nil
		}
	}
	if pending.ValidationErr != nil {
		return pending.ValidationErr
	}
	if pending.Legacy {
		c.Set(TokenProNativeImagesContextKey, true)
		return nil
	}
	if pending.Store == nil || pending.TurnID == "" || pending.ThreadID == "" {
		return ErrImageTurnMissing
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := pending.Store.Bind(ctx, TokenProImageTurnKey(key, pending.TurnID), TokenProImageTurnRoute{GroupID: *key.GroupID, Model: pending.Model, ThreadID: pending.ThreadID}); err != nil {
		return err
	}
	c.Set(TokenProNativeImagesContextKey, pure)
	c.Set(TokenProTextImageDeliveryContextKey, textDelivery)
	return nil
}

// RememberTokenProImageThread records (api key, thread) -> this turn's image
// model. Call it before any byte of the image_gen.imagegen function_call is
// written. A turn without a Codex thread id is unchanged. A turn that names a
// thread must be stored or the function_call is not returned.
func RememberTokenProImageThread(c *gin.Context, key *APIKey, model string, body []byte) error {
	if c == nil || c.Request == nil {
		return ErrTokenProImageThreadUnbound
	}
	threadID := ""
	var store TokenProImageThreadStore
	if v, ok := c.Get(TokenProImageTurnContextKey); ok {
		if pending, _ := v.(*TokenProPendingImageTurn); pending != nil {
			threadID = strings.TrimSpace(pending.ThreadID)
			store, _ = pending.Store.(TokenProImageThreadStore)
		}
	}
	if threadID == "" {
		threadID = strings.TrimSpace(gjson.GetBytes(body, "client_metadata.thread_id").String())
	}
	if !ValidTokenProCorrelationID(threadID) {
		return nil
	}
	if key == nil || key.GroupID == nil || *key.GroupID <= 0 || strings.TrimSpace(model) == "" || store == nil {
		return ErrTokenProImageThreadUnbound
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := store.RememberThread(ctx, TokenProImageThreadKey(key, threadID), TokenProImageTurnRoute{
		GroupID: *key.GroupID, Model: strings.TrimSpace(model), ThreadID: threadID,
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrTokenProImageThreadUnbound, err)
	}
	return nil
}
