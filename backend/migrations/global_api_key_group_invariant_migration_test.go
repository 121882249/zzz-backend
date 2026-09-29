package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobalAPIKeyGroupInvariantMigration(t *testing.T) {
	content, err := FS.ReadFile("238_global_api_key_group_invariant.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "SET group_id = NULL")
	require.Contains(t, sql, "WHERE key_type = 'global'")
	require.Contains(t, sql, "CHECK (key_type <> 'global' OR group_id IS NULL)")
}
