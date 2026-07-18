package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffiliateRebateJobsMigrationIsAdditiveAndIdempotent(t *testing.T) {
	content, err := fs.ReadFile(FS, "184_affiliate_rebate_jobs.sql")
	require.NoError(t, err)
	sql := strings.ToUpper(string(content))

	require.Contains(t, sql, "ALTER TABLE USER_AFFILIATE_LEDGER")
	require.Contains(t, sql, "SOURCE_REDEEM_CODE_ID")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS IDX_USER_AFFILIATE_LEDGER_SOURCE_REDEEM_ACCRUE_UNIQ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS AFFILIATE_REBATE_JOBS")
	require.Contains(t, sql, "STATUS IN ('PENDING', 'PROCESSING', 'SUCCEEDED', 'SKIPPED', 'FAILED')")
	require.Contains(t, sql, "SOURCE_KIND IN ('REDEEM', 'ADMIN_RECHARGE')")
	require.Contains(t, sql, "UNIQUE (SOURCE_REDEEM_CODE_ID)")
	require.Contains(t, sql, "NEXT_RETRY_AT")
	require.Contains(t, sql, "ATTEMPTS")
	require.Contains(t, sql, "LAST_ERROR_AT")
	require.Contains(t, sql, "PROCESSING_STARTED_AT")
	require.Contains(t, sql, "SUCCEEDED_AT")
	require.Contains(t, sql, "SKIPPED_AT")
	require.Contains(t, sql, "FAILED_AT")
	require.Contains(t, sql, "ON DELETE RESTRICT")
	require.Contains(t, sql, "IF NOT EXISTS")

	// The outbox must not silently replay historical credits during migration.
	require.NotContains(t, sql, "INSERT INTO AFFILIATE_REBATE_JOBS")
	require.NotContains(t, sql, "UPDATE USERS")
}
