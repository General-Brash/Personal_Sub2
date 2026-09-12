package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func TestDynamicRateCaptureWritesFrozenWindowAndActualWalletDelta(t *testing.T) {
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
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &service.DynamicRatePricingSnapshot{UserID: 1, GroupID: 2, Metric: service.DynamicRateMetricWalletSpend, Mode: service.DynamicRateModeBatchImage, WindowID: "previous-window", TierID: "base", PolicyVersion: 1, StaticFactor: 1, PeakFactor: 1, DynamicFactor: 0.8, FinalFactor: 0.8, PricedAt: time.Now()}
	snapshot.PricingSnapshotID = service.BuildDynamicRateSnapshotID(snapshot)
	groupID := int64(2)
	cmd := &service.UsageBillingCommand{RequestID: "batch_image_capture:one", APIKeyID: 3, UserID: 1, DynamicRateSnapshot: snapshot, UsageLog: &service.UsageLog{GroupID: &groupID}}
	mock.ExpectExec("UPDATE usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, sqlmock.AnyArg(), "previous-window", int64(0), "5").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_group_usage_periods").WithArgs(int64(1), int64(2), "previous-window", int64(0), "5").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := incrementDynamicRateUsage(context.Background(), tx, cmd, decimal.NewFromInt(5)); err != nil {
		t.Fatal(err)
	}
	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func TestDynamicRateTokensAreDisjointAndRejectNegativeBuckets(t *testing.T) {
	cmd := &service.UsageBillingCommand{InputTokens: 4, OutputTokens: 3, CacheCreationTokens: 2, CacheReadTokens: 1}
	if got := normalizedDynamicRateTokenDelta(cmd); got != 10 {
		t.Fatalf("tokens: %d", got)
	}
	cmd.InputTokens = -1
	if got := normalizedDynamicRateTokenDelta(cmd); got >= 0 {
		t.Fatal("negative usage accepted")
	}
}
