package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobalAPIKeyRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("237_global_api_key_repair.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "INSERT INTO api_keys (user_id, key, name, key_type, status)")
	require.Contains(t, sql, "k.key_type = 'global'")
	require.Contains(t, sql, "k.deleted_at IS NULL")
	require.Contains(t, sql, "u.deleted_at IS NULL")
	require.Contains(t, sql, "ON CONFLICT DO NOTHING")
	require.NotContains(t, sql, "UPDATE api_keys")
}
