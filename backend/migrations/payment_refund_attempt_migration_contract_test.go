package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentRefundAttemptsMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("224_payment_refund_attempts.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists payment_refund_attempts")
	for _, column := range []string{
		"attempt_id varchar(64)",
		"order_id bigint",
		"deduct_balance boolean",
		"force boolean",
		"held_balance_amount numeric(20,8)",
		"deduction_state varchar(20)",
		"provider_refund_id varchar(128)",
		"provider_state varchar(20)",
		"provider_result text",
		"manual_review boolean",
	} {
		require.Contains(t, sql, column)
	}
	require.Contains(t, sql, "references payment_orders(id)")
	require.Contains(t, sql, "deduction_state in ('none', 'held', 'consumed', 'returned')")
	require.Contains(t, sql, "provider_state in ('calling', 'pending', 'succeeded', 'failed', 'canceled', 'unknown')")
	require.Contains(t, sql, "payment_refund_attempts_order_created_idx")
	require.Contains(t, sql, "payment_refund_attempts_manual_review_idx")
}
