package service

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const bankServicePostgresTestEnv = "BANK_SERVICE_TEST_POSTGRES_DSN"

func TestSettleUnusedAdvanceLockedPostgresQuery(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(bankServicePostgresTestEnv))
	if dsn == "" {
		t.Skip(bankServicePostgresTestEnv + " is not set")
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
