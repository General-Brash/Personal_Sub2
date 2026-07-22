package service

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectTemporaryCreditUserLock(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectQuery(`(?s)SELECT id\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR UPDATE`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
}

func TestTemporaryCreditAllocationExecutorUsesFEFOAndPreservesOriginalRequestID(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	expectTemporaryCreditUserLock(mock, 42)
	mock.ExpectQuery(`(?s)SELECT id, remaining_amount.*available_at <= clock_timestamp\(\).*expires_at > clock_timestamp\(\).*ORDER BY expires_at ASC, id ASC`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).
			AddRow(int64(11), "1.00000000").
			AddRow(int64(12), "2.00000000"))
	mock.ExpectQuery(`(?s)UPDATE temporary_credit_grants.*available_at <= clock_timestamp\(\).*expires_at > clock_timestamp\(\)`).
		WithArgs("1.00000000", int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("1.00000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(11), nil, "upstream-request-42", "1.00000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`(?s)UPDATE temporary_credit_grants.*available_at <= clock_timestamp\(\).*expires_at > clock_timestamp\(\)`).
		WithArgs("0.50000000", int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("0.50000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(12), nil, "upstream-request-42", "0.50000000").
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectRollback()

	remaining, err := NewTemporaryCreditAllocationExecutor().Allocate(
		ctx,
		tx,
		42,
		1.5,
		TemporaryCreditConsumptionReference{RequestID: "upstream-request-42"},
	)

	require.NoError(t, err)
	require.Zero(t, remaining)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTemporaryCreditAllocationExecutorSkipsFutureAndExpiredCredits(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	expectTemporaryCreditUserLock(mock, 42)
	mock.ExpectQuery(`(?s)SELECT id, remaining_amount.*available_at <= clock_timestamp\(\).*expires_at > clock_timestamp\(\)`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}))
	mock.ExpectRollback()

	remaining, err := NewTemporaryCreditAllocationExecutor().Allocate(
		ctx,
		tx,
		42,
		1.5,
		TemporaryCreditConsumptionReference{RequestID: "upstream-request-42"},
	)

	require.NoError(t, err)
	require.Equal(t, 1.5, remaining)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTemporaryCreditAllocationExecutorConcurrentCallsLockUserBeforeGrants(t *testing.T) {
	const workers = 2
	type worker struct {
		db   *sql.DB
		mock sqlmock.Sqlmock
		tx   *sql.Tx
	}
	items := make([]worker, 0, workers)
	for range workers {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		expectTemporaryCreditUserLock(mock, 42)
		mock.ExpectQuery(`SELECT id, remaining_amount`).
			WithArgs(int64(42)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}))
		mock.ExpectRollback()
		items = append(items, worker{db: db, mock: mock, tx: tx})
	}

	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewTemporaryCreditAllocationExecutor().Allocate(
				context.Background(), item.tx, 42, 1,
				TemporaryCreditConsumptionReference{RequestID: "concurrent-lock-order"},
			)
			if err == nil {
				err = item.tx.Rollback()
			}
			if err == nil {
				err = item.mock.ExpectationsWereMet()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for _, item := range items {
		_ = item.db.Close()
	}
}

func TestTemporaryCreditAllocationExecutorRequiresExactlyOneConsumptionReference(t *testing.T) {
	executor := NewTemporaryCreditAllocationExecutor()
	amount := 1.0

	_, err := executor.Allocate(context.Background(), nil, 42, amount, TemporaryCreditConsumptionReference{})
	require.ErrorIs(t, err, ErrTemporaryCreditRequestReferenceRequired)

	usageLogID := int64(9)
	_, err = executor.Allocate(context.Background(), nil, 42, amount, TemporaryCreditConsumptionReference{
		UsageLogID: &usageLogID,
		RequestID:  "upstream-request-42",
	})
	require.ErrorIs(t, err, ErrTemporaryCreditRequestReferenceRequired)
}

var _ *sql.Tx
