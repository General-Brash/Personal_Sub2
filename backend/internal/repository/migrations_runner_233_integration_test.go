//go:build integration

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository/validation"
	"github.com/Wei-Shaw/sub2api/internal/testutil/integrationenv"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestMigration233InvalidIndexRecoveryOnPostgres exercises the real PostgreSQL
// recovery path. The fixture must provide an INVALID idx_usage_logs_upstream_request_id
// left by a failed CREATE INDEX CONCURRENTLY. Hosted CI owns and creates that
// fixture; a dedicated target must provide it explicitly. Never replace with Skip.
func TestMigration233InvalidIndexRecoveryOnPostgres(t *testing.T) {
	ctx := context.Background()
	require.NotNil(t, integrationDB)
	db := integrationDB
	if os.Getenv(validation.ModeEnv) == validation.ModeCIContainer {
		cfg, cleanup, err := integrationenv.Load(ctx, false)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, cleanup()) })
		db, err = sql.Open("postgres", validation.DatabaseDSN(cfg))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		_, err = db.ExecContext(ctx, "CREATE TABLE usage_logs (upstream_request_id TEXT NOT NULL)")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "INSERT INTO usage_logs VALUES ('duplicate'), ('duplicate')")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+pq.QuoteIdentifier(usageLogsUpstreamRequestIDIndex)+" ON usage_logs (upstream_request_id)")
		var pgErr *pq.Error
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23505"), pgErr.Code)
	}
	invalid, err := indexIsInvalid(ctx, db, usageLogsUpstreamRequestIDIndex)
	require.NoError(t, err)
	require.True(t, invalid, "the approved PostgreSQL fixture must contain the INVALID 233 index")
	require.NoError(t, prepareNonTransactionalMigration(ctx, db, usageLogsUpstreamRequestIDIndexMigration))
	invalid, err = indexIsInvalid(ctx, db, usageLogsUpstreamRequestIDIndex)
	require.NoError(t, err)
	require.False(t, invalid, "233 recovery must remove the invalid index before retry")
}
