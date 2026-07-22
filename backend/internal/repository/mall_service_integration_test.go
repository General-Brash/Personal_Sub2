//go:build integration

package repository

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMallCurrencyPurchaseFourCreditCombinationsAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		paymentType, creditType service.MallCreditType
		wantPermanent, wantTemp string
	}{
		{name: "permanent_to_permanent", paymentType: service.MallCreditTypePermanent, creditType: service.MallCreditTypePermanent, wantPermanent: "11.00000000", wantTemp: "10.00000000"},
		{name: "permanent_to_temporary", paymentType: service.MallCreditTypePermanent, creditType: service.MallCreditTypeTemporary, wantPermanent: "9.00000000", wantTemp: "12.00000000"},
		{name: "temporary_to_permanent", paymentType: service.MallCreditTypeTemporary, creditType: service.MallCreditTypePermanent, wantPermanent: "12.00000000", wantTemp: "9.00000000"},
		{name: "temporary_to_temporary", paymentType: service.MallCreditTypeTemporary, creditType: service.MallCreditTypeTemporary, wantPermanent: "10.00000000", wantTemp: "11.00000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			user := newTemporaryCreditTestUser(t)
			registerMallUserCleanup(t, user.ID)
			require.NoError(t, setMallUserBalance(ctx, user.ID, "10.00000000"))
			createTemporaryCreditTestGrant(t, NewTemporaryCreditRepository(integrationDB), user.ID, "10.00000000")
			productID := createMallCurrencyProduct(t, tc.paymentType, tc.creditType, "1.00000000", "2.00000000", 0)
			mall, coordinator := newMallIntegrationServices()
			key := "mall-four-combos-" + uuid.NewString()
			req := service.MallPurchaseRequest{ProductType: service.MallProductTypeCurrency, ProductID: productID}

			first, err := executeMallAtomic(ctx, coordinator, mall, user.ID, key, req)
			require.NoError(t, err)
			require.False(t, first.Replayed)
			second, err := executeMallAtomic(ctx, coordinator, mall, user.ID, key, req)
			require.NoError(t, err)
			require.True(t, second.Replayed)

			balance, err := mall.GetBalance(ctx, user.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantPermanent, balance.PermanentBalance)
			require.Equal(t, tc.wantTemp, balance.TemporaryCreditAvailable)
			var purchases int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mall_purchases WHERE user_id = $1`, user.ID).Scan(&purchases))
			require.Equal(t, 1, purchases)
		})
	}
}

func TestMallTemporaryPaymentInsufficientRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "4.00000000"))
	grant := createTemporaryCreditTestGrant(t, NewTemporaryCreditRepository(integrationDB), user.ID, "1.00000000")
	productID := createMallCurrencyProduct(t, service.MallCreditTypeTemporary, service.MallCreditTypePermanent, "2.00000000", "10.00000000", 1)
	mall, coordinator := newMallIntegrationServices()
	req := service.MallPurchaseRequest{ProductType: service.MallProductTypeCurrency, ProductID: productID}

	_, err := executeMallAtomic(ctx, coordinator, mall, user.ID, "mall-insufficient-"+uuid.NewString(), req)
	require.Equal(t, "MALL_INSUFFICIENT_TEMPORARY_CREDIT", infraerrors.Reason(err))

	var remaining string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT remaining_amount::text FROM temporary_credit_grants WHERE id = $1`, grant.ID).Scan(&remaining))
	require.Equal(t, "1.00000000", remaining)
	var purchases, consumed int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mall_purchases WHERE user_id = $1`, user.ID).Scan(&purchases))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(consumed_count), 0) FROM payment_purchase_counters WHERE user_id = $1`, user.ID).Scan(&consumed))
	require.Zero(t, purchases)
	require.Zero(t, consumed)
}

func TestMallLastDailyLimitSlotIsAtomicOnPostgres(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "10.00000000"))
	productID := createMallCurrencyProduct(t, service.MallCreditTypePermanent, service.MallCreditTypeTemporary, "1.00000000", "1.00000000", 1)
	mall, coordinator := newMallIntegrationServices()
	req := service.MallPurchaseRequest{ProductType: service.MallProductTypeCurrency, ProductID: productID}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := executeMallAtomic(ctx, coordinator, mall, user.ID, "mall-limit-"+uuid.NewString(), req)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes, limited := 0, 0
	for err := range results {
		switch infraerrors.Reason(err) {
		case "":
			successes++
		case "DAILY_PURCHASE_LIMIT_EXCEEDED":
			limited++
		default:
			t.Fatalf("unexpected concurrent mall purchase error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	var purchases int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mall_purchases WHERE user_id = $1`, user.ID).Scan(&purchases))
	require.Equal(t, 1, purchases)
}

func TestMallDailyTemporaryCreditPrecreatesAndRenewsAfterLastPlannedDay(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "100.00000000"))
	planID := createMallDailyPlan(t, "5.00000000", "10.00000000", 3)
	mall, coordinator := newMallIntegrationServices()
	req := service.MallPurchaseRequest{ProductType: service.MallProductTypeSubscription, ProductID: planID}

	first, err := executeMallAtomic(ctx, coordinator, mall, user.ID, "mall-daily-first-"+uuid.NewString(), req)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	assertMallDailyGrantShape(t, user.ID, 3, 1, 2, "10.00000000")

	secondKey := "mall-daily-renew-" + uuid.NewString()
	second, err := executeMallAtomic(ctx, coordinator, mall, user.ID, secondKey, req)
	require.NoError(t, err)
	require.False(t, second.Replayed)
	replay, err := executeMallAtomic(ctx, coordinator, mall, user.ID, secondKey, req)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	assertMallDailyGrantShape(t, user.ID, 6, 1, 5, "10.00000000")

	var firstDate, lastDate time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT MIN(scheduled_date), MAX(scheduled_date)
FROM temporary_credit_grants
WHERE user_id = $1 AND source = 'subscription'`, user.ID).Scan(&firstDate, &lastDate))
	require.Equal(t, 5, int(lastDate.Sub(firstDate).Hours()/24))
	balance, err := mall.GetBalance(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "90.00000000", balance.PermanentBalance)
	require.Equal(t, "10.00000000", balance.TemporaryCreditAvailable)
}

func TestMallBalanceSummaryIsReadOnlyEvenWhenBankDebtIsDue(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "4.00000000"))
	_, err := integrationDB.ExecContext(ctx, `
UPDATE users
SET temporary_credit_debt = 3, temporary_credit_debt_due_at = clock_timestamp() - INTERVAL '1 hour'
WHERE id = $1`, user.ID)
	require.NoError(t, err)
	mall, _ := newMallIntegrationServices()
	summary, err := mall.GetBalance(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "4.00000000", summary.PermanentBalance)
	var debt string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT temporary_credit_debt::text FROM users WHERE id = $1`, user.ID).Scan(&debt))
	require.Equal(t, "3.00000000", debt)
}

func TestMallSub2PlanRemainsCompatibleAndExtendsExistingSubscription(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	registerMallUserCleanup(t, user.ID)
	require.NoError(t, setMallUserBalance(ctx, user.ID, "20.00000000"))
	groupID := createMallSubscriptionGroup(t)
	planID := createMallSub2Plan(t, groupID, "5.00000000", 7)
	mall, coordinator := newMallIntegrationServices()
	req := service.MallPurchaseRequest{ProductType: service.MallProductTypeSubscription, ProductID: planID}
	_, err := executeMallAtomic(ctx, coordinator, mall, user.ID, "mall-sub2-first-"+uuid.NewString(), req)
	require.NoError(t, err)
	_, err = executeMallAtomic(ctx, coordinator, mall, user.ID, "mall-sub2-renew-"+uuid.NewString(), req)
	require.NoError(t, err)
	var count int
	var expiresAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*), MAX(expires_at)
FROM user_subscriptions WHERE user_id = $1 AND group_id = $2 AND deleted_at IS NULL`, user.ID, groupID).Scan(&count, &expiresAt))
	require.Equal(t, 1, count)
	require.True(t, expiresAt.After(time.Now().AddDate(0, 0, 13)))
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id = $1`, user.ID).Scan(&balance))
	require.Equal(t, "10.00000000", balance)
}

func TestMallSub2ValidityUnitsFreezeConvertedDaysAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, validityUnit string
		wantDays           int
	}{
		{name: "weeks", validityUnit: "weeks", wantDays: 7},
		{name: "months", validityUnit: "months", wantDays: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			user := newTemporaryCreditTestUser(t)
			registerMallUserCleanup(t, user.ID)
			require.NoError(t, setMallUserBalance(ctx, user.ID, "20.00000000"))
			groupID := createMallSubscriptionGroup(t)
			planID := createMallSub2PlanWithUnit(t, groupID, "5.00000000", 1, tc.validityUnit)
			mall, coordinator := newMallIntegrationServices()
			req := service.MallPurchaseRequest{ProductType: service.MallProductTypeSubscription, ProductID: planID}
			key := "mall-sub2-validity-unit-" + uuid.NewString()

			first, err := executeMallAtomic(ctx, coordinator, mall, user.ID, key, req)
			require.NoError(t, err)
			require.False(t, first.Replayed)
			replay, err := executeMallAtomic(ctx, coordinator, mall, user.ID, key, req)
			require.NoError(t, err)
			require.True(t, replay.Replayed)

			var purchaseCount, benefitDays int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*), MAX(benefit_days)
FROM mall_purchases
WHERE user_id = $1 AND product_type = 'subscription' AND product_id = $2`, user.ID, planID).Scan(&purchaseCount, &benefitDays))
			require.Equal(t, 1, purchaseCount)
			require.Equal(t, tc.wantDays, benefitDays)

			var grantedDays float64
			require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT EXTRACT(EPOCH FROM (expires_at - starts_at)) / 86400
FROM user_subscriptions
WHERE user_id = $1 AND group_id = $2 AND deleted_at IS NULL`, user.ID, groupID).Scan(&grantedDays))
			require.Equal(t, float64(tc.wantDays), grantedDays)

			var balance string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id = $1`, user.ID).Scan(&balance))
			require.Equal(t, "15.00000000", balance)
		})
	}
}

func newMallIntegrationServices() (*service.MallService, *service.IdempotencyCoordinator) {
	credit := service.NewTemporaryCreditService(NewTemporaryCreditRepository(integrationDB))
	mall := service.NewMallService(integrationDB, credit, nil)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	return mall, service.NewIdempotencyCoordinator(NewIdempotencyRepository(nil, integrationDB), cfg)
}

func executeMallAtomic(ctx context.Context, coordinator *service.IdempotencyCoordinator, mall *service.MallService, userID int64, key string, req service.MallPurchaseRequest) (*service.IdempotencyExecuteResult, error) {
	return coordinator.ExecuteAtomic(ctx, service.IdempotencyExecuteOptions{
		Scope: "user.mall.purchase", ActorScope: "user:" + strconv.FormatInt(userID, 10),
		Method: "POST", Route: "/api/v1/mall/purchases", IdempotencyKey: key,
		Payload: req, RequireKey: true,
	}, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return mall.PurchaseAtomic(ctx, userID, req, claim)
	})
}

func createMallCurrencyProduct(t *testing.T, paymentType, creditedType service.MallCreditType, price, credited string, dailyLimit int) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO currency_products
    (name, description, payment_price, payment_credit_type, credited_type, credited_amount,
     credited_permanent_amount, is_active, for_sale, daily_purchase_limit, total_purchase_limit)
VALUES ($1, '', $2, $3, $4, $5, $5, TRUE, TRUE, $6, 0)
RETURNING id`, "mall-integration-"+uuid.NewString(), price, paymentType, creditedType, credited, dailyLimit).Scan(&id))
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM currency_products WHERE id = $1", id) })
	return id
}

func createMallDailyPlan(t *testing.T, price, dailyAmount string, days int) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO subscription_plans
    (group_id, name, description, price, benefit_type, payment_credit_type,
     daily_temporary_credit_amount, validity_days, validity_unit, for_sale)
VALUES (0, $1, '', $2, 'daily_temporary_credit', 'permanent', $3, $4, 'day', TRUE)
RETURNING id`, "daily-integration-"+uuid.NewString(), price, dailyAmount, days).Scan(&id))
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM subscription_plans WHERE id = $1", id) })
	return id
}

func createMallSubscriptionGroup(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO groups (name, description, platform, subscription_type, status)
VALUES ($1, '', 'anthropic', 'subscription', 'active')
RETURNING id`, "mall-group-"+uuid.NewString()).Scan(&id))
	t.Cleanup(func() {
		_, _ = integrationDB.Exec("DELETE FROM user_subscriptions WHERE group_id = $1", id)
		_, _ = integrationDB.Exec("DELETE FROM groups WHERE id = $1", id)
	})
	return id
}

func createMallSub2Plan(t *testing.T, groupID int64, price string, days int) int64 {
	return createMallSub2PlanWithUnit(t, groupID, price, days, "day")
}

func createMallSub2PlanWithUnit(t *testing.T, groupID int64, price string, days int, validityUnit string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`
INSERT INTO subscription_plans
    (group_id, name, description, price, benefit_type, payment_credit_type,
     daily_temporary_credit_amount, validity_days, validity_unit, for_sale)
VALUES ($1, $2, '', $3, 'sub2', 'permanent', 0, $4, $5, TRUE)
RETURNING id`, groupID, "sub2-integration-"+uuid.NewString(), price, days, validityUnit).Scan(&id))
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM subscription_plans WHERE id = $1", id) })
	return id
}

func setMallUserBalance(ctx context.Context, userID int64, amount string) error {
	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, amount, userID)
	return err
}

func assertMallDailyGrantShape(t *testing.T, userID int64, total, active, future int, amount string) {
	t.Helper()
	var gotTotal, gotActive, gotFuture int
	var minAmount, maxAmount string
	require.NoError(t, integrationDB.QueryRow(`
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE available_at <= clock_timestamp() AND expires_at > clock_timestamp()),
       COUNT(*) FILTER (WHERE available_at > clock_timestamp()),
       MIN(amount)::text, MAX(amount)::text
FROM temporary_credit_grants
WHERE user_id = $1 AND source = 'subscription'`, userID).Scan(&gotTotal, &gotActive, &gotFuture, &minAmount, &maxAmount))
	require.Equal(t, total, gotTotal)
	require.Equal(t, active, gotActive)
	require.Equal(t, future, gotFuture)
	require.Equal(t, amount, minAmount)
	require.Equal(t, amount, maxAmount)
}

func registerMallUserCleanup(t *testing.T, userID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM temporary_credit_consumptions WHERE grant_id IN (SELECT id FROM temporary_credit_grants WHERE user_id = $1)`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM temporary_credit_grants WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM mall_daily_credit_subscriptions WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM mall_purchases WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM payment_purchase_counters WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM idempotency_records WHERE actor_scope = $1`, fmt.Sprintf("user:%d", userID))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM user_subscriptions WHERE user_id = $1`, userID)
	})
}
