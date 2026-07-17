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

type usageServiceTemporaryCreditRollbackUserRepo struct {
	UserRepository
}

func (r *usageServiceTemporaryCreditRollbackUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id}, nil
}

type usageServiceTemporaryCreditRollbackUsageRepo struct {
	UsageLogRepository
}

func (r *usageServiceTemporaryCreditRollbackUsageRepo) Create(ctx context.Context, log *UsageLog) (bool, error) {
	executor, err := temporaryCreditExecutorFromEntTx(dbent.TxFromContext(ctx))
	if err != nil {
		return false, err
	}
	if _, err := executor.ExecContext(ctx, "INSERT INTO usage_logs (request_id) VALUES ($1)", log.RequestID); err != nil {
		return false, err
	}
	log.ID = 100
	return true, nil
}

func TestUsageServiceCreateRollsBackUsageAndTemporaryCreditWhenPermanentBalanceWriteFails(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	entClient := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = entClient.Close() })

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO usage_logs`).
		WithArgs("usage-request-42").
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

	svc := NewUsageService(
		&usageServiceTemporaryCreditRollbackUsageRepo{},
		&usageServiceTemporaryCreditRollbackUserRepo{},
		entClient,
		nil,
	)

	_, err = svc.Create(ctx, CreateUsageLogRequest{
		UserID:     42,
		RequestID:  "usage-request-42",
		ActualCost: 1.0,
	})
	require.ErrorContains(t, err, "update user balance")
	require.NoError(t, mock.ExpectationsWereMet())
}
