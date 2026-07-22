package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalMallSettlementMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("189_internal_mall_settlement.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "payment_credit_type varchar(20)")
	require.Contains(t, sql, "credited_type varchar(20)")
	require.Contains(t, sql, "credited_amount numeric(20,8)")
	require.Contains(t, sql, "benefit_type varchar(40)")
	require.Contains(t, sql, "daily_temporary_credit_amount numeric(20,8)")
	require.Contains(t, sql, "create table if not exists mall_purchases")
	require.Contains(t, sql, "idempotency_record_id bigint not null unique")
	require.Contains(t, sql, "benefit_type = 'sub2' and daily_temporary_credit_amount is null")
	require.Contains(t, sql, "benefit_type = 'daily_temporary_credit' and daily_temporary_credit_amount > 0")
	require.Contains(t, sql, "create table if not exists mall_daily_credit_subscriptions")
	require.Contains(t, sql, "unique (user_id, plan_id)")
	require.Contains(t, sql, "temporary_credit_grants_daily_subscription_date_key")
	require.Contains(t, sql, "source = 'mall_product'")
	require.Contains(t, sql, "source = 'subscription'")
}
