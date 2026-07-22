//go:build integration

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentpurchasereservation"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type postgresPaidTransitionResult struct {
	updated        bool
	previousStatus string
	err            error
}

func TestPurchaseLimitCancelCallbackInterleavingOnPostgres(t *testing.T) {
	client, db, ctx := newPaymentLifecyclePostgresClient(t)
	svc := &PaymentService{entClient: client}
	order := createPostgresLimitedPaymentOrder(t, ctx, client, svc, 501, "PG-CANCEL-CALLBACK")
	stale := order
	stale.PaymentType = ""

	holder := holdPostgresPurchaseReservation(t, ctx, db, order.ID)
	cancelDone := make(chan error, 1)
	go func() {
		_, err := svc.cancelCore(ctx, stale, OrderStatusCancelled, "test", "interleaved cancel")
		cancelDone <- err
	}()
	waitForPostgresPaymentOrderLock(t, ctx, db, order.ID)

	paidDone := make(chan postgresPaidTransitionResult, 1)
	go func() {
		updated, previousStatus, err := svc.transitionOrderToPaidWithPurchase(
			ctx,
			order,
			"pg-trade-cancel-callback",
			order.PayAmount,
			time.Now(),
			time.Now().Add(-paymentGraceMinutes*time.Minute),
		)
		paidDone <- postgresPaidTransitionResult{updated: updated, previousStatus: previousStatus, err: err}
	}()
	// The callback is now queued behind the order lock held by cancellation.
	time.Sleep(75 * time.Millisecond)
	require.NoError(t, holder.Commit())

	require.NoError(t, waitPostgresInterleaveError(t, ctx, cancelDone))
	paid := waitPostgresPaidTransition(t, ctx, paidDone)
	require.NoError(t, paid.err)
	require.True(t, paid.updated)
	require.Equal(t, OrderStatusCancelled, paid.previousStatus)
	assertPostgresPaidPurchaseConsumed(t, ctx, client, order.ID, order.UserID, 501)
}

func TestPurchaseLimitProviderFailureLateCallbackInterleavingOnPostgres(t *testing.T) {
	client, db, ctx := newPaymentLifecyclePostgresClient(t)
	svc := &PaymentService{entClient: client}
	order := createPostgresLimitedPaymentOrder(t, ctx, client, svc, 502, "PG-FAILED-CALLBACK")

	holder := holdPostgresPurchaseReservation(t, ctx, db, order.ID)
	failureDone := make(chan error, 1)
	go func() {
		failureDone <- svc.failPendingOrderAndReleasePurchase(ctx, order.ID)
	}()
	waitForPostgresPaymentOrderLock(t, ctx, db, order.ID)

	paidDone := make(chan postgresPaidTransitionResult, 1)
	go func() {
		updated, previousStatus, err := svc.transitionOrderToPaidWithPurchase(
			ctx,
			order,
			"pg-trade-failed-callback",
			order.PayAmount,
			time.Now(),
			time.Now().Add(-paymentGraceMinutes*time.Minute),
		)
		paidDone <- postgresPaidTransitionResult{updated: updated, previousStatus: previousStatus, err: err}
	}()
	// Provider setup owns the order row and is blocked on reservation release;
	// the callback must wait for that order state before deciding what to do.
	time.Sleep(75 * time.Millisecond)
	require.NoError(t, holder.Commit())

	require.NoError(t, waitPostgresInterleaveError(t, ctx, failureDone))
	paid := waitPostgresPaidTransition(t, ctx, paidDone)
	require.NoError(t, paid.err)
	require.True(t, paid.updated)
	require.Equal(t, OrderStatusFailed, paid.previousStatus)
	assertPostgresPaidPurchaseConsumed(t, ctx, client, order.ID, order.UserID, 502)
}

func newPaymentLifecyclePostgresClient(t *testing.T) (*dbent.Client, *sql.DB, context.Context) {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, err := tcpostgres.Run(
		ctx,
		"postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("payment_lifecycle_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(ctx))
	return client, db, ctx
}

func createPostgresLimitedPaymentOrder(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	svc *PaymentService,
	productID int64,
	code string,
) *dbent.PaymentOrder {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(fmt.Sprintf("%s@example.com", code)).
		SetPasswordHash("hash").
		SetUsername("postgres-race-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode(code).
		SetOutTradeNo("sub2_" + code).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetCurrencyProductID(productID).
		SetDailyPurchaseLimitSnapshot(1).
		SetTotalPurchaseLimitSnapshot(1).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, order.ID, user.ID, &purchaseLimitSpec{
		productType: purchaseProductCurrency,
		productID:   productID,
		dailyLimit:  1,
		totalLimit:  1,
	}, time.Now()))
	require.NoError(t, tx.Commit())
	return order
}

func holdPostgresPurchaseReservation(t *testing.T, ctx context.Context, db *sql.DB, orderID int64) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	var id int64
	err = tx.QueryRowContext(ctx, `
SELECT id FROM payment_purchase_reservations WHERE order_id = $1 FOR UPDATE`, orderID).Scan(&id)
	require.NoError(t, err)
	return tx
}

func waitForPostgresPaymentOrderLock(t *testing.T, ctx context.Context, db *sql.DB, orderID int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		probe, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		var id int64
		err = probe.QueryRowContext(ctx, `
SELECT id FROM payment_orders WHERE id = $1 FOR UPDATE NOWAIT`, orderID).Scan(&id)
		_ = probe.Rollback()
		if err == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "55P03" {
			return
		}
		require.NoError(t, err)
	}
	t.Fatalf("payment order %d was not locked by the competing transaction", orderID)
}

func waitPostgresInterleaveError(t *testing.T, ctx context.Context, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatalf("postgres interleaving timed out: %v", ctx.Err())
		return ctx.Err()
	}
}

func waitPostgresPaidTransition(t *testing.T, ctx context.Context, done <-chan postgresPaidTransitionResult) postgresPaidTransitionResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		t.Fatalf("postgres paid transition timed out: %v", ctx.Err())
		return postgresPaidTransitionResult{err: ctx.Err()}
	}
}

func assertPostgresPaidPurchaseConsumed(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	orderID, userID, productID int64,
) {
	t.Helper()
	order, err := client.PaymentOrder.Get(ctx, orderID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, order.Status)
	require.NotNil(t, order.PaidAt)
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(orderID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationConsumed, reservation.Status)
	rows, err := client.QueryContext(ctx, `
SELECT reserved_count, consumed_count
FROM payment_purchase_counters
WHERE user_id = $1 AND product_type = $2 AND product_id = $3
ORDER BY period_type`, userID, purchaseProductCurrency, productID)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var reserved, consumed int
		require.NoError(t, rows.Scan(&reserved, &consumed))
		require.Zero(t, reserved)
		require.Equal(t, 1, consumed)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 2, count)
}
