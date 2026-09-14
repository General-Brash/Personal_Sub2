package repository

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestUserGroupRatesJoinExistingAdminTransaction(t *testing.T) {
	fallback, fallbackMock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = fallback.Close() })
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	ctx := dbent.NewTxContext(context.Background(), tx)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_group_rate_multipliers")).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM user_group_rate_multipliers")).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, NewUserGroupRateRepository(fallback).SyncUserGroupRates(ctx, 7, map[int64]*float64{}))
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
	require.NoError(t, fallbackMock.ExpectationsWereMet())
}
