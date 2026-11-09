package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPOAuthMigrationIsAdditiveAfterImageCountBilling(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	require.NoError(t, err)
	index := map[string]int{}
	for i, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			index[entry.Name()] = i
		}
	}
	countBilling := index["248_group_image_count_billing.sql"]
	mcp, ok := index["249_mcp_oauth.sql"]
	require.True(t, ok)
	require.Less(t, countBilling, mcp)

	body, err := FS.ReadFile("249_mcp_oauth.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))
	for _, table := range []string{"mcp_oauth_grants", "mcp_oauth_codes", "mcp_oauth_credentials"} {
		require.Contains(t, sql, "create table if not exists "+table)
	}
	require.NotContains(t, sql, "alter table api_keys")
	require.NotContains(t, sql, "alter table users")
	require.NotContains(t, sql, "alter table payment_orders")
	require.NotContains(t, sql, "drop table")
}
