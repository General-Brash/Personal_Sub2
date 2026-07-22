package service

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestUsageServiceCreateDefersAvailableCreditInvalidationUntilOuterTransactionCommits(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		mock.ExpectBegin()
		expectUsageServiceTemporaryCreditWrites(mock, "usage-outer-transaction-commit", "1.00000000")
		mock.ExpectCommit()

		outerTx, err := client.Tx(context.Background())
		require.NoError(t, err)
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err = svc.Create(dbent.NewTxContext(context.Background(), outerTx), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-outer-transaction-commit",
			ActualCost: 1,
		})

		require.NoError(t, err)
		require.Zero(t, invalidator.calls)
		require.NoError(t, outerTx.Commit())
		require.Equal(t, 1, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rollback", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		mock.ExpectBegin()
		expectUsageServiceTemporaryCreditWrites(mock, "usage-outer-transaction-rollback", "1.00000000")
		mock.ExpectRollback()

		outerTx, err := client.Tx(context.Background())
		require.NoError(t, err)
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err = svc.Create(dbent.NewTxContext(context.Background(), outerTx), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-outer-transaction-rollback",
			ActualCost: 1,
		})

		require.NoError(t, err)
		require.Zero(t, invalidator.calls)
		require.NoError(t, outerTx.Rollback())
		require.Zero(t, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("commit failure", func(t *testing.T) {
		client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
		mock.ExpectBegin()
		expectUsageServiceTemporaryCreditWrites(mock, "usage-outer-transaction-commit-failure", "1.00000000")
		mock.ExpectCommit().WillReturnError(errors.New("outer transaction commit failed"))

		outerTx, err := client.Tx(context.Background())
		require.NoError(t, err)
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewUsageService(&usageServiceTemporaryCreditRollbackUsageRepo{}, &usageServiceTemporaryCreditRollbackUserRepo{}, client, nil)
		svc.SetAvailableCreditInvalidator(invalidator)

		_, err = svc.Create(dbent.NewTxContext(context.Background(), outerTx), CreateUsageLogRequest{
			UserID:     42,
			RequestID:  "usage-outer-transaction-commit-failure",
			ActualCost: 1,
		})

		require.NoError(t, err)
		require.Zero(t, invalidator.calls)
		require.ErrorContains(t, outerTx.Commit(), "outer transaction commit failed")
		require.Zero(t, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUsageServiceCreateRejectsTransactionalClientWithoutTransactionContextBeforeRepositoryCalls(t *testing.T) {
	client, mock := newUsageServiceAvailableCreditInvalidationTestClient(t)
	mock.ExpectBegin()
	outerTx, err := client.Tx(context.Background())
	require.NoError(t, err)
	mock.ExpectRollback()

	userRepo := &usageServiceCreateCallTrackingUserRepo{}
	usageRepo := &usageServiceCreateCallTrackingUsageRepo{}
	svc := NewUsageService(usageRepo, userRepo, outerTx.Client(), nil)

	_, err = svc.Create(context.Background(), CreateUsageLogRequest{
		UserID:     42,
		RequestID:  "usage-transactional-client-without-context",
		ActualCost: 1,
	})

	require.ErrorContains(t, err, "usage billing transaction context is required")
	require.Zero(t, userRepo.calls)
	require.Zero(t, usageRepo.calls)
	require.NoError(t, outerTx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectUsageServiceTemporaryCreditWrites(mock interface {
	ExpectExec(string) *sqlmock.ExpectedExec
	ExpectQuery(string) *sqlmock.ExpectedQuery
}, requestID, amount string) {
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs(requestID).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectQuery(`(?s)SELECT id\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(11), amount))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs(amount, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow(amount))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(11), int64(100), nil, amount).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

type usageServiceCreateCallTrackingUserRepo struct {
	UserRepository
	calls int
}

func (r *usageServiceCreateCallTrackingUserRepo) GetByID(context.Context, int64) (*User, error) {
	r.calls++
	return nil, errors.New("unexpected user repository call")
}

type usageServiceCreateCallTrackingUsageRepo struct {
	UsageLogRepository
	calls int
}

func (r *usageServiceCreateCallTrackingUsageRepo) Create(context.Context, *UsageLog) (bool, error) {
	r.calls++
	return false, errors.New("unexpected usage repository call")
}
