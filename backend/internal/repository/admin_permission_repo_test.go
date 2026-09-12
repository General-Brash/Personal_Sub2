package repository

import (
	"context"
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT role FROM users")).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow(service.RoleAdmin))
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
