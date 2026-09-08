//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigration233InvalidIndexRecoveryOnPostgres exercises the real PostgreSQL
// recovery path. The fixture must provide an INVALID idx_usage_logs_upstream_request_id
// left by a failed CREATE INDEX CONCURRENTLY; this test is compile-only in the
// code-first phase and must not be replaced with Skip.
func TestMigration233InvalidIndexRecoveryOnPostgres(t *testing.T) {
	ctx := context.Background()
	require.NotNil(t, integrationDB)
	invalid, err := indexIsInvalid(ctx, integrationDB, usageLogsUpstreamRequestIDIndex)
	require.NoError(t, err)
	require.True(t, invalid, "the approved PostgreSQL fixture must contain the INVALID 233 index")
	require.NoError(t, prepareNonTransactionalMigration(ctx, integrationDB, usageLogsUpstreamRequestIDIndexMigration))
	invalid, err = indexIsInvalid(ctx, integrationDB, usageLogsUpstreamRequestIDIndex)
	require.NoError(t, err)
	require.False(t, invalid, "233 recovery must remove the invalid index before retry")
}
