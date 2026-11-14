package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

const usageBillingMaxTransactionAttempts = 3

// apply returns whether to commit. A dedup replay can discard tentative writes
// without treating the replay as an error. All callbacks must be transaction-only.
func runUsageBillingTransaction(ctx context.Context, db *sql.DB, apply func(*sql.Tx) (bool, error)) error {
	for attempt := 1; ; attempt++ {
		err, retryable := attemptUsageBillingTransaction(ctx, db, apply)
		if err == nil || !retryable || attempt >= usageBillingMaxTransactionAttempts {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func attemptUsageBillingTransaction(ctx context.Context, db *sql.DB, apply func(*sql.Tx) (bool, error)) (error, bool) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err, false
	}
	defer func() { _ = tx.Rollback() }()

	commit, err := apply(tx)
	if err == nil && commit {
		err = tx.Commit()
		if err == nil {
			return nil, false
		}
	}
	rollbackErr := tx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return errors.Join(err, fmt.Errorf("rollback usage billing transaction: %w", rollbackErr)), false
	}
	// Only these PostgreSQL responses establish that the entire transaction
	// aborted, including at COMMIT. Transport/timeout/ambiguous commit errors
	// and other SQLSTATEs must never cause another charge attempt.
	return err, isRetryableUsageBillingTransactionError(err)
}

func isRetryableUsageBillingTransactionError(err error) bool {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) || pgErr == nil {
		return false
	}
	return pgErr.Code == "40P01" || pgErr.Code == "40001"
}
