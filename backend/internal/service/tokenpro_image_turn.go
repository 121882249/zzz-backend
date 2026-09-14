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
)

const TokenProImageTurnContextKey = "tokenpro_image_turn_v2"
const TokenProImageSessionContextKey = "tokenpro_image_session_v2"
const TokenProImageTurnTTL = 30 * time.Minute

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
	eligible := key != nil && key.IsGlobal() && key.GroupID != nil && TokenProPureImageGroup(key.Group)
	if !eligible {
		imageRequest := TokenProNativeImages(c) && c.Request != nil &&
			(strings.HasSuffix(c.Request.URL.Path, "/images/generations") || strings.HasSuffix(c.Request.URL.Path, "/images/edits"))
		c.Set(TokenProNativeImagesContextKey, false)
		c.Set(TokenProNativeImageDriverContextKey, "")
		if imageRequest {
			// Reject stale native turn routes after a group's policy changes.
			return ErrImageTurnConflict
		}
		return nil
	}
	v, ok := c.Get(TokenProImageTurnContextKey)
	if !ok {
		return nil
	}
	pending, ok := v.(*TokenProPendingImageTurn)
	if !ok || pending == nil {
		return ErrImageTurnMissing
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
	c.Set(TokenProNativeImagesContextKey, true)
	return nil
}
