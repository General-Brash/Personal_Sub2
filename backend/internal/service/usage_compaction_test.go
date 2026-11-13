package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestUsageCompactionMonthBoundaryAndProtectedCategories(t *testing.T) {
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init(previous) })
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, timezone.Location())
	require.Equal(t, "2025-12-31T16:00:00Z", UsageCompactionEnd(now).Format(time.RFC3339))
	for _, category := range []string{"usage_history_daily", "usage_request_archive", "usage_billing_dedup", "bank_ledger", "payment_orders", "mall_purchases"} {
		_, _, err := validateDataCleanupFilter(DataCleanupFilter{Category: category, Mode: DataCleanupModeAll})
		require.Error(t, err, category)
	}
	_, _, err := validateDataCleanupFilter(DataCleanupFilter{Category: "usage_logs", Mode: DataCleanupModeAll})
	require.Error(t, err)
	start := timezone.StartOfMonth(time.Now()).AddDate(0, -1, 0)
	end := start.AddDate(0, 1, 0)
	_, _, err = validateDataCleanupFilter(DataCleanupFilter{Category: "usage_logs", Mode: DataCleanupModeRange, StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	partial := start.Add(time.Hour)
	_, _, err = validateDataCleanupFilter(DataCleanupFilter{Category: "usage_logs", Mode: DataCleanupModeRange, StartTime: &partial, EndTime: &end})
	require.Error(t, err)
}

func TestQueuedCleanupPreviewSurvivesSubmissionExpiryButNotTampering(t *testing.T) {
	svc := testDataCleanupService(nil)
	start := time.Now().Add(-48 * time.Hour)
	end := start.Add(time.Hour)
	filter := DataCleanupFilter{Category: "ops_system_logs", Mode: DataCleanupModeRange, StartTime: &start, EndTime: &end}
	preview := &DataCleanupPreview{MatchedRows: 1, snapshotMaxID: 1, snapshotBatchSize: 1, snapshotDigests: []string{UsageCleanupRowIdentityDigest([]int64{1})}}
	token, err := svc.signPreviewToken(filter, preview)
	require.NoError(t, err)
	_, err = svc.decodeQueuedPreview(token)
	require.NoError(t, err)
	_, err = svc.decodeQueuedPreview(token + "corrupted")
	require.Error(t, err)
	_, err = svc.Execute(context.Background(), filter, 1, "DELETE ops_system_logs 1", token, DataCleanupOperator{})
	require.Error(t, err)
}

type auditRetentionProbe struct {
	AuditLogRepository
	calls int
}

func (p *auditRetentionProbe) DeleteBefore(context.Context, time.Time, int) (int64, error) {
	p.calls++
	return 0, nil
}

func TestAuditLogRetentionHonorsUnifiedCleanupSwitch(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyAuditLogRetentionDays] = "1"
	probe := &auditRetentionProbe{}
	svc := NewAuditLogService(probe, NewSettingService(repo, nil))
	defer svc.Stop()
	svc.runRetentionOnce()
	require.Zero(t, probe.calls, "missing master setting defaults to protected")
	repo.values[SettingKeyOpsAdvancedSettings] = `{"data_retention":{"cleanup_enabled":false}}`
	svc.runRetentionOnce()
	require.Zero(t, probe.calls)
	repo.values[SettingKeyOpsAdvancedSettings] = `{"data_retention":{"cleanup_enabled":true}}`
	svc.runRetentionOnce()
	require.Equal(t, 1, probe.calls)
}
