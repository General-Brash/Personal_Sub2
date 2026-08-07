//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryGetAvailableCreditSnapshotIncludesOnlyCurrentTemporaryCreditAcrossSources(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	t.Cleanup(func() {
		cleanupExec := func(name, query string) {
			if _, err := integrationDB.ExecContext(ctx, query, user.ID); err != nil {
				t.Errorf("cleanup %s: %v", name, err)
			}
		}
		cleanupExec("temporary credit consumptions", `
DELETE FROM temporary_credit_consumptions
WHERE grant_id IN (SELECT id FROM temporary_credit_grants WHERE user_id = $1)`)
		cleanupExec("temporary credit grants", "DELETE FROM temporary_credit_grants WHERE user_id = $1")
		cleanupExec("daily check-ins", "DELETE FROM daily_checkins WHERE user_id = $1")
		cleanupExec("daily temporary-credit subscriptions", "DELETE FROM mall_daily_credit_subscriptions WHERE user_id = $1")
		cleanupExec("mall purchases", "DELETE FROM mall_purchases WHERE user_id = $1")

		cleanupChecks := []struct {
			name  string
			query string
		}{
			{
				name: "temporary credit consumptions",
				query: `
SELECT COUNT(*)
FROM temporary_credit_consumptions AS consumption
JOIN temporary_credit_grants AS grant_row ON grant_row.id = consumption.grant_id
WHERE grant_row.user_id = $1`,
			},
			{name: "temporary credit grants", query: "SELECT COUNT(*) FROM temporary_credit_grants WHERE user_id = $1"},
			{name: "daily check-ins", query: "SELECT COUNT(*) FROM daily_checkins WHERE user_id = $1"},
			{name: "daily temporary-credit subscriptions", query: "SELECT COUNT(*) FROM mall_daily_credit_subscriptions WHERE user_id = $1"},
			{name: "mall purchases", query: "SELECT COUNT(*) FROM mall_purchases WHERE user_id = $1"},
		}
		for _, check := range cleanupChecks {
			var remaining int
			if err := integrationDB.QueryRowContext(ctx, check.query, user.ID).Scan(&remaining); err != nil {
				t.Errorf("verify cleanup %s: %v", check.name, err)
				continue
			}
			if remaining != 0 {
				t.Errorf("verify cleanup %s: %d rows remain", check.name, remaining)
			}
		}
	})

	_, err := integrationDB.ExecContext(ctx, `
WITH checkin AS (
	INSERT INTO daily_checkins (user_id, checkin_date, streak_day, reward_day, reward_amount)
	VALUES ($1, CURRENT_DATE, 1, 1, 1.25)
	RETURNING id
), mall_purchase AS (
	INSERT INTO mall_purchases
		(user_id, product_type, product_id, product_name, idempotency_record_id,
		 payment_credit_type, price, credited_type, credited_amount, status)
	VALUES ($1, 'currency', 1, 'available-credit-test-currency', nextval('idempotency_records_id_seq'),
			'permanent', 1, 'temporary', 2.75, 'completed')
	RETURNING id
), subscription_purchase AS (
	INSERT INTO mall_purchases
		(user_id, product_type, product_id, product_name, idempotency_record_id,
		 payment_credit_type, price, benefit_type, benefit_days, daily_temporary_credit_amount, status)
	VALUES ($1, 'subscription', 1, 'available-credit-test-subscription', nextval('idempotency_records_id_seq'),
			'permanent', 1, 'daily_temporary_credit', 1, 5, 'completed')
	RETURNING id
), daily_subscription AS (
	INSERT INTO mall_daily_credit_subscriptions
		(user_id, plan_id, starts_at, last_grant_date, expires_at)
	VALUES ($1, 1, clock_timestamp(), CURRENT_DATE, clock_timestamp() + INTERVAL '2 hours')
	RETURNING id
)
INSERT INTO temporary_credit_grants
	(user_id, source, checkin_id, mall_purchase_id, daily_subscription_id, scheduled_date,
	 amount, remaining_amount, available_at, expires_at, notes, granted_by)
VALUES
	($1, 'checkin', (SELECT id FROM checkin), NULL, NULL, NULL,
		1.25, 1.25, clock_timestamp() - INTERVAL '1 minute', clock_timestamp() + INTERVAL '2 hours', '', NULL),
	($1, 'mall_product', NULL, (SELECT id FROM mall_purchase), NULL, NULL,
		2.75, 2.75, clock_timestamp() - INTERVAL '1 minute', clock_timestamp() + INTERVAL '1 hour', '', NULL),
	($1, 'subscription', NULL, (SELECT id FROM subscription_purchase), (SELECT id FROM daily_subscription), CURRENT_DATE,
		5.00, 5.00, clock_timestamp() + INTERVAL '1 hour', clock_timestamp() + INTERVAL '2 hours', '', NULL),
	($1, 'admin_grant', NULL, NULL, NULL, NULL,
		7.00, 7.00, clock_timestamp() - INTERVAL '2 hours', clock_timestamp() - INTERVAL '1 hour', 'expired', $1),
	($1, 'bank_advance', NULL, NULL, NULL, NULL,
		11.00, 0.00, clock_timestamp() - INTERVAL '1 hour', clock_timestamp() + INTERVAL '1 hour', '', NULL)`, user.ID)
	require.NoError(t, err)

	reader, ok := NewUserRepository(testEntClient(t), integrationDB).(service.AvailableCreditSnapshotReader)
	require.True(t, ok)
	snapshot, err := reader.GetAvailableCreditSnapshot(ctx, user.ID)

	require.NoError(t, err)
	require.InDelta(t, 4.0, snapshot.TemporaryCredit, 1e-12)
	require.NotNil(t, snapshot.EarliestTemporaryCreditExpiry)
}
