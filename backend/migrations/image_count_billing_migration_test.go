package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageCountBillingMigrationFollowsGeminiImageGate(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	require.NoError(t, err)

	var sqlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}
	index := map[string]int{}
	for i, name := range sqlFiles {
		index[name] = i
	}
	gate, ok := index["246_enable_gemini_image_generation_groups.sql"]
	require.True(t, ok)
	adminKeys, ok := index["247_scoped_admin_api_keys.sql"]
	require.True(t, ok)
	countBilling, ok := index["248_group_image_count_billing.sql"]
	require.True(t, ok)
	require.Less(t, gate, adminKeys)
	require.Less(t, adminKeys, countBilling)

	body, err := FS.ReadFile("248_group_image_count_billing.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))
	require.Contains(t, sql, "image_count_billing")
	require.Contains(t, sql, "default false")
	require.NotContains(t, strings.ToLower(mustReadMigration(t, "246_enable_gemini_image_generation_groups.sql")), "image_count_billing")
}

func mustReadMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	require.NoError(t, err)
	return string(body)
}
