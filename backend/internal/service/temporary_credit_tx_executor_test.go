package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditAllocationExecutorUsesFEFOAndPreservesOriginalRequestID(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).
			AddRow(int64(11), "1.00000000").
			AddRow(int64(12), "2.00000000"))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs("1.00000000", int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("1.00000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(11), nil, "upstream-request-42", "1.00000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
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

func TestTemporaryCreditAllocationExecutorSkipsExpiredCredits(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT id, remaining_amount`).
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
