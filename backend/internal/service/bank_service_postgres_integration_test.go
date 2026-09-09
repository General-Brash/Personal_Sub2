//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"

	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
	"github.com/Wei-Shaw/sub2api/internal/testutil/integrationenv"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSettleUnusedAdvanceLockedPostgresQuery(t *testing.T) {
	cfg, cleanup, err := integrationenv.Load(context.Background(), false)
	if err != nil {
		t.Fatal(fmt.Errorf("bank service integration target rejected: %w", err))
	}
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	dsn := validation.DatabaseDSN(cfg)
	if dsn == "" {
		t.Fatal("dedicated validation config resolved an empty database DSN")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(ctx))

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, `
CREATE TEMP TABLE bank_loans (
    id BIGINT PRIMARY KEY,
    grant_id BIGINT NOT NULL,
    debt_remaining NUMERIC NOT NULL,
    user_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    unused_credit_settled_at TIMESTAMPTZ,
    grant_expires_at TIMESTAMPTZ NOT NULL
) ON COMMIT DROP;
CREATE TEMP TABLE temporary_credit_grants (
    id BIGINT PRIMARY KEY,
    remaining_amount NUMERIC NOT NULL
) ON COMMIT DROP;`)
	require.NoError(t, err)

	debtAfter, processed, err := settleUnusedAdvanceLocked(
		ctx,
		tx,
		42,
		10,
		DefaultBankPolicy(),
		time.Now().UTC(),
	)
	require.NoError(t, err)
	require.False(t, processed)
	require.Equal(t, float64(10), debtAfter)
}
