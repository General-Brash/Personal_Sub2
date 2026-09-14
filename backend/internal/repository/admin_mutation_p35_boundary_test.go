package repository

import (
	"context"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"regexp"
	"testing"
)

func TestAdminEmptyUserMaskStillChecksTransactionAuthority(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := NewUserRepository(client, db)
	ctx := enforcedAdminMutationContext(&service.AdminPrincipal{ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1, Role: service.RoleAdmin, Version: 1, Explicit: true})
	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock, sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleAdmin, service.StatusActive, int64(1)).AddRow(int64(7), service.RoleUser, service.StatusActive, int64(0)), 1, 7)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}))
	mock.ExpectRollback()
	err = repo.Update(ctx, &service.User{ID: 7}, service.UserUpdateFields{})
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHumanSuperAdminCanAuthorizeSelfDemotionAndDeletion(t *testing.T) {
	for _, action := range []string{"demote_to_admin", "delete"} {
		t.Run(action, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			ctx := enforcedAdminMutationContext(&service.AdminPrincipal{ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1, Role: service.RoleSuperAdmin, Version: 3, Explicit: true})
			mock.ExpectBegin()
			expectAdminMutationLockUsers(mock, sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(3)), 1)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}))
			mock.ExpectRollback()
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			if action == "delete" {
				err = authorizeUserDeleteMutationTx(ctx, tx, 1, 1)
			} else {
				err = authorizeUserUpdateMutationTx(ctx, tx, 1, 1, &service.User{ID: 1, Role: service.RoleAdmin}, service.UserUpdateFields{Role: true})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLastSuperAdminGuardCountsActiveUndeletedRowsUnderLock(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
		mock.ExpectBegin()
		tx, err := client.Tx(context.Background())
		require.NoError(t, err)
		mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).WithArgs(superAdminGuardLockKey).WillReturnResult(sqlmock.NewResult(0, 1))
		rows := sqlmock.NewRows([]string{"id"})
		for i := 1; i <= count; i++ {
			rows.AddRow(int64(i))
		}
		mock.ExpectQuery(`SELECT .*FROM "users".*"role".*"status".*"deleted_at" IS NULL.*ORDER BY.*FOR UPDATE`).WithArgs(service.RoleSuperAdmin, service.StatusActive).WillReturnRows(rows)
		mock.ExpectRollback()
		err = ensureNotLastSuperAdminWithClient(context.Background(), tx.Client())
		if count <= 1 {
			require.ErrorIs(t, err, service.ErrLastSuperAdmin)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}
