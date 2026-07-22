package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMallAndPurchaseLimitsMigrationContract(t *testing.T) {
	sql, err := FS.ReadFile("188_mall_and_purchase_limits.sql")
	require.NoError(t, err)
	text := string(sql)
	require.Contains(t, text, "'mall_enabled'")
	require.Contains(t, text, "SELECT value FROM settings WHERE key = 'payment_enabled'")
	require.Contains(t, text, "COALESCE(")
	require.Contains(t, text, "'false'")
	require.Contains(t, text, "daily_purchase_limit INT NOT NULL DEFAULT 0")
	require.Contains(t, text, "total_purchase_limit INT NOT NULL DEFAULT 0")
	require.Contains(t, text, "daily_purchase_limit_snapshot INT NOT NULL DEFAULT 0")
	require.Contains(t, text, "CREATE TABLE IF NOT EXISTS payment_purchase_counters")
	require.Contains(t, text, "CREATE TABLE IF NOT EXISTS payment_purchase_reservations")
	require.Contains(t, text, "ON CONFLICT (user_id, product_type, product_id, period_type, period_start)")
	require.Contains(t, text, "AT TIME ZONE 'Asia/Shanghai'")
	require.Contains(t, text, "WHEN paid_at IS NOT NULL AND status <> 'REFUNDED' THEN 'consumed'")
}
