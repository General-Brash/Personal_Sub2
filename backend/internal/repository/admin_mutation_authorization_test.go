package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectAdminMutationLockUsers(mock sqlmock.Sqlmock, rows *sqlmock.Rows, ids ...int64) {
	args := make([]driver.Value, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).
		WithArgs(superAdminGuardLockKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.id, u.role, u.status, COALESCE(v.version, 0)")).
		WithArgs(args...).
		WillReturnRows(rows)
}

func enforcedAdminMutationContext(principal *service.AdminPrincipal) context.Context {
	ctx := context.Background()
	ctx = service.ContextWithAdminPrincipal(ctx, principal)
	ctx = service.ContextWithAdminAuthorization(ctx, service.NewAdminPermissionService(nil, service.AdminPermissionModeEnforce))
	return service.ContextWithAdminMutationActorID(ctx, principal.UserID)
}

func TestAuthorizeAdminMutationTxRejectsActorPermissionVersionChange(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	principal := &service.AdminPrincipal{
		ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1,
		Role: service.RoleAdmin, Version: 4, Explicit: true,
	}
	ctx := enforcedAdminMutationContext(principal)

	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock,
		sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleAdmin, service.StatusActive, int64(5)),
		int64(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}))
	mock.ExpectRollback()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{
		Permissions: []AdminMutationPermission{{Permission: "users.update"}},
	})
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxReturnsConflictWhenTargetChangesAfterPreflight(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	principal := &service.AdminPrincipal{
		ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1,
		Role: service.RoleAdmin, Version: 4, Explicit: true,
	}
	ctx := enforcedAdminMutationContext(principal)

	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock,
		sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleAdmin, service.StatusActive, int64(4)).
			AddRow(int64(2), service.RoleSuperAdmin, service.StatusActive, int64(8)),
		int64(1), int64(2))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}).AddRow("users.update", service.AdminGrantAllow, []byte(`{"*":"*"}`)))
	mock.ExpectRollback()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{
		TargetUserID:         2,
		TargetNotFound:       service.ErrUserNotFound,
		ExpectedTargetRole:   service.RoleAdmin,
		ExpectedTargetStatus: service.StatusActive,
		Permissions: []AdminMutationPermission{{
			Permission: "users.update",
			Scope:      userMutationScope(2),
		}},
	})
	require.ErrorIs(t, err, service.ErrAdminMutationConflict)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxReturnsNotFoundWithoutPreflightSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := service.ContextWithAdminAuthorization(context.Background(), service.NewAdminPermissionService(nil, service.AdminPermissionModeDisabled))
	ctx = service.ContextWithAdminMutationActorID(ctx, 1)

	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock,
		sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleAdmin, service.StatusActive, int64(4)),
		int64(1), int64(2))
	mock.ExpectRollback()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{
		TargetUserID:   2,
		TargetNotFound: service.ErrUserNotFound,
	})
	require.ErrorIs(t, err, service.ErrUserNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxNeverTreatsAPIKeyBoundToSuperAdminAsHuman(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	principal := &service.AdminPrincipal{
		ID: "admin-key:key-1", Kind: service.AdminPrincipalKindAPIKey, UserID: 1,
		Role: service.RoleAdmin, Version: 7, Explicit: true,
	}
	ctx := enforcedAdminMutationContext(principal)

	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock,
		sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(7)),
		int64(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT principal_user_id, scopes, enabled")).
		WithArgs("key-1").
		WillReturnRows(sqlmock.NewRows([]string{"principal_user_id", "scopes", "enabled"}).
			AddRow(int64(1), []byte(`["security.superadmin.assign"]`), true))
	mock.ExpectRollback()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{
		RequireHumanSuperAdmin: true,
		Permissions:            []AdminMutationPermission{{Permission: "security.superadmin.assign"}},
	})
	require.ErrorIs(t, err, service.ErrSuperAdminAssignmentDenied)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxReturnsConflictWhenTargetWasDeletedAfterPreflight(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := service.ContextWithAdminAuthorization(context.Background(), service.NewAdminPermissionService(nil, service.AdminPermissionModeDisabled))
	ctx = service.ContextWithAdminMutationActorID(ctx, 1)

	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock,
		sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(4)),
		int64(1), int64(2))
	mock.ExpectRollback()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{
		TargetUserID:         2,
		TargetNotFound:       service.ErrUserNotFound,
		ExpectedTargetRole:   service.RoleAdmin,
		ExpectedTargetStatus: service.StatusActive,
	})
	require.True(t, errors.Is(err, service.ErrAdminMutationConflict), "err=%v", err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxVersionChangeWithCurrentPermissionIsConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	principal := &service.AdminPrincipal{ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1, Role: service.RoleAdmin, Version: 4, Explicit: true}
	ctx := enforcedAdminMutationContext(principal)
	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock, sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleAdmin, service.StatusActive, int64(5)), 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}).AddRow("users.update", service.AdminGrantAllow, []byte(`{"*":"*"}`)))
	mock.ExpectRollback()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{Permissions: []AdminMutationPermission{{Permission: "users.update"}}})
	require.ErrorIs(t, err, service.ErrAdminMutationConflict)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxRejectsKeyAfterOwnerDemotion(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	principal := &service.AdminPrincipal{ID: "admin-key:key-1", Kind: service.AdminPrincipalKindAPIKey, UserID: 1, Role: service.RoleAdmin, Version: 4, Explicit: true}
	ctx := enforcedAdminMutationContext(principal)
	mock.ExpectBegin()
	expectAdminMutationLockUsers(mock, sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleUser, service.StatusActive, int64(4)), 1)
	mock.ExpectRollback()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 1, AdminMutationAuthorizationOptions{Permissions: []AdminMutationPermission{{Permission: "users.update"}}})
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthorizeAdminMutationTxMarkedMissingActorCannotBypassLegacyGate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := service.ContextWithAdminMutationActorID(context.Background(), 0)
	ctx = service.ContextWithAdminAuthorization(ctx, service.NewAdminPermissionService(nil, service.AdminPermissionModeDisabled))
	mock.ExpectBegin()
	mock.ExpectRollback()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = AuthorizeAdminMutationTx(ctx, tx, 0, AdminMutationAuthorizationOptions{})
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
