package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPurchaseLimitPeriodsMigrationContract(t *testing.T) {
	entries, err := FS.ReadDir(".")
	require.NoError(t, err)

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.Contains(t, names, "193_purchase_limit_periods.sql")

	content, err := FS.ReadFile("193_purchase_limit_periods.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "alter table currency_products")
	require.Contains(t, sql, "purchase_limit_unit varchar(10) not null default 'day'")
	require.Contains(t, sql, "purchase_limit_mode varchar(10) not null default 'calendar'")
	require.Contains(t, sql, "purchase_limit_window_size int not null default 1")
	require.Contains(t, sql, "purchase_limit_unit_snapshot varchar(10) not null default 'day'")
	require.Contains(t, sql, "alter table payment_purchase_reservations")
	require.Contains(t, sql, "add column if not exists period_type varchar(10) not null default 'daily'")

	require.Contains(t, sql, "drop constraint payment_purchase_counters_period_type_check")
	require.Contains(t, sql, "'weekly', 'monthly', 'rolling', 'total'")
	require.Contains(t, sql, "create table if not exists payment_purchase_limit_events")
	require.Contains(t, sql, "source_type varchar(20) not null")
	require.Contains(t, sql, "source_id bigint not null")
	require.Contains(t, sql, "status in ('reserved', 'consumed', 'released')")
	require.Contains(t, sql, "create unique index if not exists payment_purchase_limit_events_source_key")
	require.Contains(t, sql, "payment_purchase_limit_events_user_product_status_time_idx")
	require.Contains(t, sql, "status, occurred_at")
	require.Contains(t, sql, "status in ('reserved', 'consumed')")

	old188, err := FS.ReadFile("188_mall_and_purchase_limits.sql")
	require.NoError(t, err)
	old189, err := FS.ReadFile("189_internal_mall_settlement.sql")
	require.NoError(t, err)
	require.NotContains(t, string(old188), "purchase_limit_unit")
	require.NotContains(t, string(old189), "purchase_limit_unit")
}
