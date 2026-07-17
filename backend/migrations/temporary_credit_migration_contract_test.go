package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditMigrationPreservesLedgerAuditConstraints(t *testing.T) {
	content, err := fs.ReadFile(FS, "175_daily_checkin_temporary_credits.sql")
	require.NoError(t, err)
	sql := string(content)

	require.NotContains(t, sql, "ON DELETE CASCADE")
	require.NotContains(t, sql, "ON DELETE SET NULL")
	require.GreaterOrEqual(t, strings.Count(sql, "ON DELETE RESTRICT"), 6)
	require.Contains(t, sql, "granted_by BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT")
	require.Contains(t, sql, "source = 'checkin' AND checkin_id IS NOT NULL AND granted_by IS NULL AND notes = ''")
	require.Contains(t, sql, "source = 'admin_grant' AND checkin_id IS NULL AND granted_by IS NOT NULL")
	require.Contains(t, sql, "remaining_amount >= 0 AND remaining_amount <= amount")
	require.Contains(t, sql, "temporary_credit_consumptions_reference_check")

	dropTrigger := "DROP TRIGGER IF EXISTS temporary_credit_consumptions_request_id_immutable"
	createTrigger := "CREATE TRIGGER temporary_credit_consumptions_request_id_immutable"
	require.Contains(t, sql, dropTrigger)
	require.Contains(t, sql, createTrigger)
	require.Less(t, strings.Index(sql, dropTrigger), strings.Index(sql, createTrigger))
}

func TestUsageLogDecimalMigration177IsNotEmbedded(t *testing.T) {
	_, err := fs.Stat(FS, "177_usage_log_exact_decimal_amounts.sql")
	require.ErrorIs(t, err, fs.ErrNotExist)
}
