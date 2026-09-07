package service

import "testing"

func TestAPIKeyScopeUsesExplicitType(t *testing.T) {
	global := &APIKey{KeyType: APIKeyTypeGlobal, Name: "TokenPro", GroupID: nil}
	if !global.IsGlobal() || global.IsGroupScoped() {
		t.Fatal("global key must be identified only by key_type")
	}

	// A normal key can use the same display name and must remain group-scoped.
	group := &APIKey{KeyType: APIKeyTypeGroup, Name: "TokenPro"}
	if group.IsGlobal() || !group.IsGroupScoped() {
		t.Fatal("group key named TokenPro must remain group-scoped")
	}

	legacy := &APIKey{Name: "legacy", GroupID: ptrInt64(42)}
	if legacy.IsGlobal() || !legacy.IsGroupScoped() {
		t.Fatal("missing key_type must preserve legacy group behavior")
	}
}

func TestAPIKeyScopeNilReceiver(t *testing.T) {
	var key *APIKey
	if key.IsGlobal() || !key.IsGroupScoped() {
		t.Fatal("nil receiver should not be global and should preserve legacy behavior")
	}
}

