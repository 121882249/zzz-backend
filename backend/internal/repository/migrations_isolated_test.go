//go:build isolateddb

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestApplyMigrationsIsolatedPostgres(t *testing.T) {
	dsn := os.Getenv("TOKENPRO_ISOLATED_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TOKENPRO_ISOLATED_DATABASE_URL is required")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	require.NoError(t, db.PingContext(ctx))

	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "reapplying the same files must be a no-op")

	var imageCountDefault string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT column_default FROM information_schema.columns
		WHERE table_name = 'groups' AND column_name = 'image_count_billing'`).Scan(&imageCountDefault))
	require.Contains(t, imageCountDefault, "false")

	for _, table := range []string{"mcp_oauth_grants", "mcp_oauth_codes", "mcp_oauth_credentials", "api_keys", "users"} {
		var exists bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists))
		require.True(t, exists, table)
	}

	// Binary rollback leaves the additive MCP tables in place. A data rollback
	// is only safe when MCP was never enabled, and drops the child tables first.
	_, err = db.ExecContext(ctx, `DROP TABLE mcp_oauth_credentials, mcp_oauth_codes, mcp_oauth_grants`)
	require.NoError(t, err)
	var groupsRemain bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.groups') IS NOT NULL`).Scan(&groupsRemain))
	require.True(t, groupsRemain)

	_, err = db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE filename = '249_mcp_oauth.sql'`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db), "249 must recreate cleanly after the tables are removed")
	var grants bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass('public.mcp_oauth_grants') IS NOT NULL`).Scan(&grants))
	require.True(t, grants)
}
