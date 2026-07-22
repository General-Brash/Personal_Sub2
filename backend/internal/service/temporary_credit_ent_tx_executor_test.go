package service

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

var _ TemporaryCreditSQLExecutor = (*entTemporaryCreditExecutor)(nil)

func TestTemporaryCreditExecutorFromEntTxUsesCurrentTransactionDriver(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)

	executor, err := temporaryCreditExecutorFromEntTx(tx)
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(1))
	rows, err := executor.QueryContext(ctx, "SELECT 1")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var value int
	require.NoError(t, rows.Scan(&value))
	require.Equal(t, 1, value)
	require.NoError(t, rows.Close())

	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEntTxTemporaryCreditExecutorRollsBackUsageAndCreditWritesWhenPermanentBalanceWriteFails(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	executor, err := temporaryCreditExecutorFromEntTx(tx)
	require.NoError(t, err)

	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs("usage-request-42").
		WillReturnResult(sqlmock.NewResult(100, 1))
	_, err = executor.ExecContext(ctx, "INSERT INTO usage_logs (request_id) VALUES ($1)", "usage-request-42")
	require.NoError(t, err)

	expectTemporaryCreditUserLock(mock, 42)
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(11), "0.25000000"))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs("0.25000000", int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("0.25000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(11), int64(100), nil, "0.25000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	usageLogID := int64(100)
	remaining, err := NewTemporaryCreditAllocationExecutor().Allocate(
		ctx,
		executor,
		42,
		1.0,
		TemporaryCreditConsumptionReference{UsageLogID: &usageLogID},
	)
	require.NoError(t, err)
	require.Equal(t, 0.75, remaining)

	mock.ExpectExec(`UPDATE users`).
		WithArgs("-0.75000000", int64(42)).
		WillReturnError(errors.New("permanent balance write failed"))
	_, err = executor.ExecContext(ctx, "UPDATE users SET balance = balance + $1 WHERE id = $2", "-0.75000000", int64(42))
	require.ErrorContains(t, err, "permanent balance write failed")

	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
