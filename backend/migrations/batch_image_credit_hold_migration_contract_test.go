package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageCreditHoldMigrationPreservesSplitLedgerContract(t *testing.T) {
	content, err := fs.ReadFile(FS, "183_personal_batch_image_credit_holds.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS batch_image_credit_holds")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS batch_image_credit_hold_allocations")
	require.Contains(t, sql, "status IN ('reserved', 'captured', 'released')")
	require.Contains(t, sql, "group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL")
	require.Contains(t, sql, "idx_batch_image_credit_holds_group_reserved_at")
	require.Contains(t, sql, "WHERE group_id IS NOT NULL")
	require.Contains(t, sql, "temporary_reserved_amount + permanent_reserved_amount = hold_amount")
	require.Contains(t, sql, "temporary_captured_amount + permanent_captured_amount = captured_amount")
	require.Contains(t, sql, "expired_unrestored_amount <= temporary_reserved_amount - temporary_captured_amount")
	require.Contains(t, sql, "captured_amount + refunded_amount + expired_amount = reserved_amount")
	require.Contains(t, sql, "UNIQUE (hold_id, grant_id)")
	require.Contains(t, sql, "FOREIGN KEY (hold_id, batch_id)")
	require.Contains(t, sql, "REFERENCES batch_image_credit_holds(id, batch_id)")
	require.Contains(t, sql, "grant_id BIGINT NOT NULL REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT")
	require.GreaterOrEqual(t, strings.Count(strings.ToUpper(sql), "NUMERIC(20,8)"), 11)
	require.GreaterOrEqual(t, strings.Count(strings.ToUpper(sql), "ON DELETE RESTRICT"), 5)

	// Legacy users.frozen_balance cannot be decomposed into per-grant rows
	// without fabricating history, so this migration must remain additive-only.
	require.NotContains(t, strings.ToUpper(sql), "INSERT INTO BATCH_IMAGE_CREDIT_HOLDS")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE BATCH_IMAGE_JOBS")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE BATCH_IMAGE_JOBS")
}
