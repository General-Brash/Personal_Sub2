package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinancialLedgersAndBankExchangeTiersMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("190_financial_ledgers_and_bank_exchange_tiers.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "add column if not exists product_name varchar(100)")
	require.Contains(t, sql, "permanent_balance_before numeric(20,8)")
	require.Contains(t, sql, "temporary_balance_after numeric(20,8)")
	require.Contains(t, sql, "create table if not exists bank_exchange_daily_usage")
	require.Contains(t, sql, "primary key (user_id, usage_date)")
	require.Contains(t, sql, "at time zone 'asia/shanghai'")
	require.Contains(t, sql, "bank_exchange_tiers")
	require.Contains(t, sql, "capture_bank_ledger_balance_snapshot")
}
