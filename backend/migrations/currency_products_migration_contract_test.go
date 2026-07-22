package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrencyProductsMigrationKeepsProductDeleteAndOrderSnapshotContract(t *testing.T) {
	sql, err := FS.ReadFile("187_currency_products.sql")
	require.NoError(t, err)
	text := string(sql)
	require.Contains(t, text, "CREATE TABLE IF NOT EXISTS currency_products")
	require.Contains(t, text, "payment_price NUMERIC(20,2)")
	require.Contains(t, text, "credited_permanent_amount NUMERIC(20,8)")
	require.Contains(t, text, "SET LOCAL lock_timeout = '5s'")
	require.Contains(t, text, "SET LOCAL statement_timeout = '15min'")
	require.Contains(t, text, "currency_product_id BIGINT")
	require.Contains(t, text, "currency_product_credited_amount NUMERIC(20,8)")
	require.Contains(t, text, "ALTER COLUMN amount TYPE NUMERIC(20,8)")
	require.Contains(t, text, "ALTER COLUMN refund_amount TYPE NUMERIC(20,8)")
	require.NotContains(t, text, "REFERENCES currency_products")
}
