package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinancialLookupIndexesMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("191_financial_lookup_indexes.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "payment_orders_completed_mall_event_idx")
	require.Contains(t, sql, "coalesce(completed_at, paid_at, created_at)")
	require.Contains(t, sql, "status = 'completed'")
	require.Contains(t, sql, "currency_product_id is not null")
	require.Contains(t, sql, "payment_orders_completed_mall_user_event_idx")
	require.Contains(t, sql, "payment_orders_completed_currency_product_idx")
	require.Contains(t, sql, "payment_orders_completed_subscription_plan_idx")
	require.Contains(t, sql, "mall_purchases_completed_product_idx")
	require.Contains(t, sql, "on mall_purchases (product_type, product_id)")
}
