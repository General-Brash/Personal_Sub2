package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBankTemporaryDebtMigrationContract(t *testing.T) {
	content, err := fs.ReadFile(FS, "185_bank_temporary_debt.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "temporary_credit_debt numeric(20,8)")
	require.Contains(t, sql, "temporary_credit_debt_due_at timestamptz")
	require.Contains(t, sql, "create table if not exists bank_loans")
	require.Contains(t, sql, "create unique index if not exists bank_loans_one_active_per_user")
	require.Contains(t, sql, "create table if not exists bank_ledger")
	require.Contains(t, sql, "bank_advance")
	require.Contains(t, sql, "bank_exchange")
	require.Contains(t, sql, "bank_exchange_rate")
}

func TestBankRepaymentAndTemporaryAvailabilityMigrationContract(t *testing.T) {
	content, err := fs.ReadFile(FS, "186_bank_repayment_and_available_at.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "add column if not exists available_at timestamptz")
	require.Contains(t, sql, "set available_at = created_at")
	require.Contains(t, sql, "unused_credit_settled_at timestamptz")
	require.Contains(t, sql, "unused_credit_amount numeric(20,8)")
	require.Contains(t, sql, "unused_debt_repaid numeric(20,8)")
	require.Contains(t, sql, "bank_unused_advance_debt_reduction_ratio")
	require.Contains(t, sql, "0.75000000")
	require.Contains(t, sql, "bank_early_repay_temporary_ratio")
	require.Contains(t, sql, "bank_early_repay_permanent_ratio")
	require.Contains(t, sql, "on conflict (key) do nothing")
}
