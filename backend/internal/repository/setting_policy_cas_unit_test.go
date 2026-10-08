//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"
)

func TestSettingPolicyCASConcurrentInitialization(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "committed winner returns conflict", value: "winner"},
		{name: "existing empty row can still be initialized", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := NewSettingRepository(client).(*settingRepository)

			const key = "test_cas_initialization"
			const selectLocked = `SELECT .* FROM "settings" WHERE "settings"\."key" = \$1 .*FOR UPDATE`
			columns := []string{"id", "key", "value", "updated_at"}
			mock.ExpectBegin()
			mock.ExpectQuery(selectLocked).WithArgs(key).
				WillReturnRows(sqlmock.NewRows(columns))
			// Ent's single-row create still scans RETURNING id. DO NOTHING
			// returns no row when a concurrent initializer wins the insert.
			mock.ExpectQuery(`INSERT INTO "settings" .*ON CONFLICT \("key"\) DO NOTHING RETURNING "id"`).
				WithArgs(key, "", sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(selectLocked).WithArgs(key).
				WillReturnRows(sqlmock.NewRows(columns).AddRow(int64(1), key, tc.value, time.Now()))
			if tc.want {
				mock.ExpectQuery(`INSERT INTO "settings" .*ON CONFLICT \("key"\) DO UPDATE SET .*RETURNING "id"`).
					WithArgs(key, "candidate", sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}

			ok, err := repo.CompareAndSetMultiple(context.Background(), map[string]string{key: ""}, map[string]string{key: "candidate"})
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSettingPolicyCASInitializationInsertFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewSettingRepository(client).(*settingRepository)
	insertErr := errors.New("initialization insert failed")
	const key = "test_cas_insert_failure"
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "settings" .*FOR UPDATE`).WithArgs(key).
		WillReturnRows(sqlmock.NewRows([]string{"id", "key", "value", "updated_at"}))
	mock.ExpectQuery(`INSERT INTO "settings" .*DO NOTHING RETURNING "id"`).
		WithArgs(key, "", sqlmock.AnyArg()).WillReturnError(insertErr)
	mock.ExpectRollback()

	ok, err := repo.CompareAndSetMultiple(context.Background(), map[string]string{key: ""}, map[string]string{key: "candidate"})
	require.False(t, ok)
	require.ErrorIs(t, err, insertErr)
	require.NoError(t, mock.ExpectationsWereMet())
}
