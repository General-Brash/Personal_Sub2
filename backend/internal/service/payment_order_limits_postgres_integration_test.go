//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPaymentOrderGlobalLimitsAreSerializedOnPostgres(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("payment_order_limits_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(16)
	require.NoError(t, db.PingContext(ctx))

	drv := entsql.OpenDB(entdialect.Postgres, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(ctx))

	svc := &PaymentService{entClient: client}
	pendingUser := createPaymentOrderLimitTestUser(t, ctx, client, "pending")
	pendingResults := runConcurrentPaymentOrderCreations(ctx, svc, pendingUser, &PaymentConfig{
		MaxPendingOrders: 1,
	}, 8, 3)
	assertExactlyOnePaymentLimitSuccess(t, pendingResults, "TOO_MANY_PENDING")

	dailyUser := createPaymentOrderLimitTestUser(t, ctx, client, "daily")
	_, err = client.PaymentOrder.Create().
		SetUserID(dailyUser.ID).
		SetUserEmail(dailyUser.Email).
		SetUserName(dailyUser.Username).
		SetAmount(7).
		SetPayAmount(7).
		SetFeeRate(0).
		SetRechargeCode("DAILY-SEED").
		SetOutTradeNo("sub2_daily_seed").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("daily-seed-trade").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	dailyResults := runConcurrentDailyLimitChecks(ctx, svc, dailyUser, 8, 3, 10)
	assertExactlyOnePaymentLimitSuccess(t, dailyResults, "DAILY_LIMIT_EXCEEDED")
}

func createPaymentOrderLimitTestUser(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail("payment-limit-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername("payment-limit-" + suffix).
		Save(ctx)
	require.NoError(t, err)
	return user
}

func runConcurrentPaymentOrderCreations(ctx context.Context, svc *PaymentService, user *dbent.User, cfg *PaymentConfig, workers, amount int) []error {
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.createOrderInTxWithProduct(
				ctx,
				CreateOrderRequest{
					UserID:      user.ID,
					PaymentType: payment.TypeAlipay,
					OrderType:   payment.OrderTypeBalance,
					ClientIP:    "127.0.0.1",
					SrcHost:     "api.example.com",
				},
				&User{ID: user.ID, Email: user.Email, Username: user.Username},
				nil,
				nil,
				cfg,
				float64(amount),
				float64(amount),
				0,
				float64(amount),
				nil,
			)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	out := make([]error, 0, workers)
	for err := range results {
		out = append(out, err)
	}
	return out
}

func runConcurrentDailyLimitChecks(ctx context.Context, svc *PaymentService, user *dbent.User, workers, amount int, limit float64) []error {
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for workerID := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-start
			tx, err := svc.entClient.Tx(ctx)
			if err == nil {
				err = lockPaymentOrderLimitUser(ctx, tx, user.ID, paymentAuditDialect(tx.Client()) == entdialect.Postgres)
			}
			if err == nil {
				err = svc.checkDailyLimit(ctx, tx, user.ID, float64(amount), limit)
			}
			if err == nil {
				_, err = tx.PaymentOrder.Create().
					SetUserID(user.ID).
					SetUserEmail(user.Email).
					SetUserName(user.Username).
					SetAmount(float64(amount)).
					SetPayAmount(float64(amount)).
					SetFeeRate(0).
					SetRechargeCode(fmt.Sprintf("DAILY-CONCURRENT-%d", workerID)).
					SetOutTradeNo(fmt.Sprintf("sub2_daily_concurrent_%d", workerID)).
					SetPaymentType(payment.TypeAlipay).
					SetPaymentTradeNo(fmt.Sprintf("daily-concurrent-trade-%d", workerID)).
					SetOrderType(payment.OrderTypeBalance).
					SetStatus(OrderStatusCompleted).
					SetExpiresAt(time.Now().Add(time.Hour)).
					SetPaidAt(time.Now()).
					SetClientIP("127.0.0.1").
					SetSrcHost("api.example.com").
					Save(ctx)
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			results <- err
		}(workerID)
	}
	close(start)
	wg.Wait()
	close(results)

	out := make([]error, 0, workers)
	for err := range results {
		out = append(out, err)
	}
	return out
}

func assertExactlyOnePaymentLimitSuccess(t *testing.T, results []error, limitReason string) {
	t.Helper()
	successes := 0
	limitFailures := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		if infraerrors.Reason(err) == limitReason {
			limitFailures++
			continue
		}
		t.Fatalf("unexpected payment order limit error: %v", err)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, len(results)-1, limitFailures)
}
