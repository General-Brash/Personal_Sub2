package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestApplyAdminPermissionChangeCommitsGrantVersionAndAuditAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Errorf("close sqlmock database: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
	})
	repo := NewAdminPermissionRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).
		WithArgs(superAdminGuardLockKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.id, u.role, u.status, COALESCE(v.version, 0)")).
		WithArgs(int64(1), int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(7)).
			AddRow(int64(5), service.RoleAdmin, service.StatusActive, int64(2)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO admin_principal_grants")).
		WithArgs(int64(5), "users.read", service.AdminGrantAllow, `{"*":"*"}`, int64(1), "test reason").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO admin_permission_versions")).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(3)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO admin_permission_audit_logs")).
		WithArgs(int64(1), int64(5), "grant", "users.read", "null", `{"effect":"allow"}`, "req-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	version, err := repo.ApplyAdminPermissionChange(context.Background(), service.AdminPermissionChange{
		ActorUserID: 1, ActorIsSuper: true, TargetUserID: 5,
		Action: "grant", Permission: "users.read", Effect: service.AdminGrantAllow,
		Scope: map[string]any{"*": "*"}, Reason: "test reason", RequestID: "req-1",
		NewValue: map[string]any{"effect": "allow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("version = %d, want 3", version)
	}
}

func newAdminPermissionRepoMock(t *testing.T) (service.AdminPermissionRepository, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("ADMIN_PERMISSIONS_MODE", "")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Errorf("close sqlmock database: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
	})
	return NewAdminPermissionRepository(db), mock
}

func expectSelfAdminPermissionLock(mock sqlmock.Sqlmock, role string) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).
		WithArgs(superAdminGuardLockKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.id, u.role, u.status, COALESCE(v.version, 0)")).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role", "status", "version"}).
			AddRow(int64(1), role, service.StatusActive, int64(7)))
}

// D-1: the final transaction guard lets a human super administrator self-grant
// oidc.* only; the audit row records the actor as its own target.
func TestApplyAdminPermissionChangeAllowsHumanSuperAdminOIDCSelfGrant(t *testing.T) {
	repo, mock := newAdminPermissionRepoMock(t)
	expectSelfAdminPermissionLock(mock, service.RoleSuperAdmin)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO admin_principal_grants")).
		WithArgs(int64(1), "oidc.keys.rotate", service.AdminGrantAllow, `{"*":"*"}`, int64(1), "bootstrap").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO admin_permission_versions")).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(8)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO admin_permission_audit_logs")).
		WithArgs(int64(1), int64(1), "grant", "oidc.keys.rotate", "null", "null", "").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	version, err := repo.ApplyAdminPermissionChange(context.Background(), service.AdminPermissionChange{
		ActorUserID: 1, ActorIsSuper: true, TargetUserID: 1,
		Action: "grant", Permission: "oidc.keys.rotate", Effect: service.AdminGrantAllow,
		Scope: map[string]any{"*": "*"}, Reason: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != 8 {
		t.Fatalf("version = %d, want 8", version)
	}
}

func TestApplyAdminPermissionChangeRejectsOtherSelfGrants(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		permission string
	}{
		{name: "super admin non-oidc", role: service.RoleSuperAdmin, permission: "security.permissions.grant"},
		{name: "ordinary admin oidc", role: service.RoleAdmin, permission: "oidc.keys.rotate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newAdminPermissionRepoMock(t)
			expectSelfAdminPermissionLock(mock, tc.role)
			mock.ExpectRollback()

			_, err := repo.ApplyAdminPermissionChange(context.Background(), service.AdminPermissionChange{
				ActorUserID: 1, TargetUserID: 1,
				Action: "grant", Permission: tc.permission, Effect: service.AdminGrantAllow,
				Scope: map[string]any{"*": "*"}, Reason: "self",
			})
			if !errors.Is(err, service.ErrAdminPermissionSelfGrant) {
				t.Fatalf("self grant error = %v", err)
			}
		})
	}
}
