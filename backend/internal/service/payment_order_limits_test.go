//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestLockPaymentOrderLimitUserUsesPostgresRowLock(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT id\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectCommit()

	require.NoError(t, lockPaymentOrderLimitUser(ctx, tx, 42, true))
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLockPaymentOrderLimitUserReturnsUserNotFound(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT id\s+FROM users\s+WHERE id = \$1`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	require.ErrorIs(t, lockPaymentOrderLimitUser(ctx, tx, 42, false), ErrUserNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
