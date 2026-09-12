package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestApplyEntitlementChangeCommitsTierVersionAndAuditAtomically(t *testing.T) {
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
	repo := NewEntitlementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock")).WithArgs("entitlement-change:1:req-premium").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WithArgs(int64(1), "req-premium").WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "result"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT enabled, version FROM entitlement_tiers")).WithArgs(service.EntitlementTierPremium).WillReturnRows(sqlmock.NewRows([]string{"enabled", "version"}).AddRow(true, int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT tier, version, expires_at FROM user_entitlements")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"tier", "version", "expires_at"}).AddRow(service.EntitlementTierStandard, int64(0), nil))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_entitlements")).
		WithArgs(int64(7), service.EntitlementTierPremium, int64(1), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO entitlement_audit_logs")).
		WithArgs(int64(1), int64(7), service.EntitlementTierPremium, int64(0), int64(1), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO entitlement_change_requests")).WithArgs(int64(1), "req-premium", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := repo.ApplyEntitlementChange(context.Background(), []int64{7}, service.EntitlementTierPremium, 1, "campaign", "req-premium")
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed != 1 || result.Version != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestEntitlementChangeFingerprintIncludesTargetsTierAndReason(t *testing.T) {
	original := entitlementChangeFingerprint([]int64{1, 2}, "premium", "reason")
	if original != entitlementChangeFingerprint([]int64{2, 1}, "premium", "reason") {
		t.Fatal("ID order must not change the request")
	}
	for _, value := range []string{entitlementChangeFingerprint([]int64{1, 3}, "premium", "reason"), entitlementChangeFingerprint([]int64{1, 2}, "standard", "reason"), entitlementChangeFingerprint([]int64{1, 2}, "premium", "other")} {
		if value == original {
			t.Fatal("different entitlement request shares a fingerprint")
		}
	}
}
