//go:build integration

package service

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPurchaseLimitLastSlotIsAtomicOnPostgres(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(
		ctx,
		"postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("purchase_limit_test"),
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
	require.NoError(t, db.PingContext(ctx))
	_, err = db.ExecContext(ctx, `
CREATE TABLE payment_purchase_counters (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    product_type VARCHAR(20) NOT NULL,
    product_id BIGINT NOT NULL,
    period_type VARCHAR(10) NOT NULL,
    period_start DATE NOT NULL,
    reserved_count INT NOT NULL DEFAULT 0,
    consumed_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, product_type, product_id, period_type, period_start)
)`)
	require.NoError(t, err)

	const workers = 12
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := db.BeginTx(ctx, nil)
			if err == nil {
				err = reservePurchaseCounter(ctx, tx, 7, purchaseProductCurrency, 9, purchasePeriodDaily, purchaseDailyPeriodStart(time.Now()), 1)
			}
			if err == nil {
				err = tx.Commit()
			} else if tx != nil {
				_ = tx.Rollback()
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	limitFailures := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if infraerrors.Reason(err) == "DAILY_PURCHASE_LIMIT_EXCEEDED" {
			limitFailures++
			continue
		}
		t.Fatalf("unexpected concurrent reservation error: %v", err)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, workers-1, limitFailures)
	var reserved int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT reserved_count FROM payment_purchase_counters`).Scan(&reserved))
	require.Equal(t, 1, reserved)
}
