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

func newUsageServiceAvailableCreditInvalidationTestClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func expectUsageServiceTemporaryOnlyCommit(mock sqlmock.Sqlmock, requestID, amount string) {
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(requestID).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(11), amount))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs(amount, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow(amount))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(11), int64(100), nil, amount).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

func TestUsageServiceCreateInvalidatesAvailableCreditOnlyAfterCommittedCharge(t *testing.T) {
	t.Run("temporary-only committed charge invalidates", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		expectUsageServiceTemporaryOnlyCommit(mock, "usage-available-credit-success", "1.00000000")
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err := svc.Create(context.Background(), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-available-credit-success",
			ActualCost: 1.0,
		})

		require.NoError(t, err)
		require.Equal(t, 1, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("legacy float is rounded once before temporary allocation", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		expectUsageServiceTemporaryOnlyCommit(mock, "usage-available-credit-rounds-float-once", "1.23456789")
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err := svc.Create(context.Background(), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-available-credit-rounds-float-once",
			ActualCost: 1.234567891,
		})

		require.NoError(t, err)
		require.Equal(t, 1, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rollback does not invalidate", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO usage_logs`).
			WithArgs("usage-available-credit-rollback").
			WillReturnResult(sqlmock.NewResult(100, 1))
		mock.ExpectQuery(`SELECT id, remaining_amount`).
			WithArgs(int64(42)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(11), "0.25000000"))
		mock.ExpectQuery(`UPDATE temporary_credit_grants`).
			WithArgs("0.25000000", int64(11)).
			WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("0.25000000"))
		mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
			WithArgs(int64(11), int64(100), nil, "0.25000000").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`(?s)UPDATE.*users`).WillReturnError(errors.New("permanent balance write failed"))
		mock.ExpectRollback()
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err := svc.Create(context.Background(), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-available-credit-rollback",
			ActualCost: 1.0,
		})

		require.Error(t, err)
		require.Equal(t, 0, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cache failure does not reverse committed charge", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		expectUsageServiceTemporaryOnlyCommit(mock, "usage-available-credit-cache-failure", "1.00000000")
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{err: errors.New("redis unavailable")}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err := svc.Create(context.Background(), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-available-credit-cache-failure",
			ActualCost: 1.0,
		})

		require.NoError(t, err)
		require.Equal(t, 1, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
