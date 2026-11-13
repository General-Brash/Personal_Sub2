//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestUsageCompactionPreservesTotalsReplayAndLateRows(t *testing.T) {
	ctx := context.Background()
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init(previous) })
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "compaction-" + uuid.NewString() + "@fixture.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-compact-" + uuid.NewString(), Name: "compaction"})
	account := mustCreateAccount(t, client, &service.Account{Name: "compaction-" + uuid.NewString()})
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	month := timezone.StartOfMonth(time.Now()).AddDate(0, -1, 0)
	end := month.AddDate(0, 1, 0)
	logs := make([]*service.UsageLog, 3)
	for i := range logs {
		logs[i] = &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "compaction-model", InputTokens: 10 + i, OutputTokens: 3, CacheReadTokens: 7, ActualCost: 0.1000000001, TotalCost: 0.2000000001, CreatedAt: month.Add(36 * time.Hour)}
		inserted, err := repo.Create(ctx, logs[i])
		require.NoError(t, err)
		require.True(t, inserted)
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO billing_usage_entries(usage_log_id,user_id,api_key_id,billing_type) VALUES($1,$2,$3,0)`, logs[2].ID, user.ID, key.ID)
	require.NoError(t, err)
	before, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	cleanup := newUsageCleanupRepositoryWithSQL(client, integrationDB)
	filters := service.UsageCleanupFilters{StartTime: month, EndTime: end, UserID: &user.ID}
	result, err := cleanup.CompactUsageLogsBatch(ctx, filters, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.ProcessedRows)
	result, err = cleanup.CompactUsageLogsBatch(ctx, filters, 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ProcessedRows)
	require.Equal(t, int64(1), result.ProtectedRows)
	result, err = cleanup.CompactUsageLogsBatch(ctx, filters, 2)
	require.NoError(t, err)
	require.Zero(t, result.ProcessedRows)
	after, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, before.TotalRequests, after.TotalRequests)
	require.Equal(t, before.TotalTokens, after.TotalTokens)
	require.Equal(t, before.TotalActualCost, after.TotalActualCost)
	stats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{UserID: user.ID})
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
	keyStats, err := repo.GetAPIKeyDashboardStats(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, int64(3), keyStats.TotalRequests)
	for _, log := range logs {
		duplicate := *log
		inserted, err := repo.Create(ctx, &duplicate)
		require.NoError(t, err)
		require.False(t, inserted)
		require.Equal(t, log.ID, duplicate.ID)
	}
	late := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "compaction-model", InputTokens: 1, ActualCost: 0.01, TotalCost: 0.01, CreatedAt: month.Add(48 * time.Hour)}
	_, err = repo.Create(ctx, late)
	require.NoError(t, err)
	result, err = cleanup.CompactUsageLogsBatch(ctx, filters, 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ProcessedRows)
	after, err = repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(4), after.TotalRequests)
	var monthly int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT request_count FROM usage_user_monthly WHERE user_id=$1 AND month_start=$2::date", user.ID, month.Format("2006-01-02")).Scan(&monthly))
	require.Equal(t, int64(4), monthly)
	current := *late
	current.RequestID = uuid.NewString()
	current.CreatedAt = end.Add(time.Hour)
	_, err = repo.Create(ctx, &current)
	require.NoError(t, err)
	result, err = cleanup.CompactUsageLogsBatch(ctx, service.UsageCleanupFilters{All: true, UserID: &user.ID}, 100)
	require.NoError(t, err)
	require.Zero(t, result.ProcessedRows)
	var raw int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE user_id=$1", user.ID).Scan(&raw))
	require.Equal(t, 2, raw)
	t.Log(fmt.Sprintf("verified user=%d requests=5; protected credential and current month retained", user.ID))
}

func TestUsageCompactionQueuedJobAndCancellation(t *testing.T) {
	ctx := context.Background()
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init(previous) })
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "queued-" + uuid.NewString() + "@fixture.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-queued-" + uuid.NewString(), Name: "queued"})
	account := mustCreateAccount(t, client, &service.Account{Name: "queued-" + uuid.NewString()})
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	month := timezone.StartOfMonth(time.Now()).AddDate(0, -2, 0)
	for i := 0; i < 3; i++ {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "queued", InputTokens: 7, ActualCost: 0.2, TotalCost: 0.3, CreatedAt: month.Add(time.Hour)})
		require.NoError(t, err)
	}
	cleanup := service.NewDataCleanupService(integrationDB, nil, nil)
	filter := service.DataCleanupFilter{Category: "usage_logs", Mode: service.DataCleanupModeRange, Month: month.Format("2006-01")}
	preview, err := cleanup.Preview(ctx, filter)
	require.NoError(t, err)
	require.GreaterOrEqual(t, preview.MatchedRows, int64(3))
	job, err := cleanup.Execute(ctx, filter, preview.MatchedRows, preview.Confirmation, " "+preview.PreviewToken+" ", service.DataCleanupOperator{UserID: user.ID, AuthMethod: "jwt"})
	require.NoError(t, err)
	require.Equal(t, "pending", job.Status)
	cleanup.Start()
	defer cleanup.Stop()
	require.Eventually(t, func() bool {
		var status string
		return integrationDB.QueryRowContext(ctx, "SELECT status FROM data_cleanup_audits WHERE id=$1", job.AuditID).Scan(&status) == nil && status == "succeeded"
	}, 15*time.Second, 200*time.Millisecond)
	var processed int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT (filters->>'processed_rows')::bigint FROM data_cleanup_audits WHERE id=$1", job.AuditID).Scan(&processed))
	require.Equal(t, preview.MatchedRows, processed)
	overview, err := cleanup.Overview(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, overview.Requests, int64(3))
	require.NotEmpty(t, overview.Months)
	require.NotEmpty(t, overview.Years)
	stats, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
	preview, err = cleanup.Preview(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, preview.MatchedRows)
	job, err = cleanup.Execute(ctx, filter, preview.MatchedRows, preview.Confirmation, preview.PreviewToken, service.DataCleanupOperator{UserID: user.ID, AuthMethod: "jwt"})
	require.NoError(t, err)
	require.NoError(t, cleanup.CancelTask(ctx, job.AuditID))
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM data_cleanup_audits WHERE id=$1", job.AuditID).Scan(&status))
	require.Equal(t, "canceled", status)
}

func TestUsageCompactionPrecisionRollbackAndLeaseRecovery(t *testing.T) {
	ctx := context.Background()
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init(previous) })
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "precision-" + uuid.NewString() + "@fixture.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-precision-" + uuid.NewString(), Name: "precision"})
	account := mustCreateAccount(t, client, &service.Account{Name: "precision-" + uuid.NewString()})
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	month := timezone.StartOfMonth(time.Now()).AddDate(0, -3, 0)
	for i := 0; i < 2; i++ {
		log := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "precision", InputTokens: 1, ActualCost: 0.0000000001, TotalCost: 0.0000000001, CreatedAt: month.Add(time.Hour)}
		_, err := repo.Create(ctx, log)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "UPDATE usage_logs SET account_rate_multiplier=0.5,image_count=NULL WHERE id=$1", log.ID)
		require.NoError(t, err)
	}
	filters := service.UsageCleanupFilters{StartTime: month, EndTime: month.AddDate(0, 1, 0), UserID: &user.ID}
	compactor := newUsageCleanupRepositoryWithSQL(client, integrationDB)
	for i := 0; i < 2; i++ {
		result, err := compactor.CompactUsageLogsBatch(ctx, filters, 1)
		require.NoError(t, err)
		require.Equal(t, int64(1), result.ProcessedRows)
	}
	var cost string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT SUM(account_stats_cost)::text FROM usage_statistics WHERE user_id=$1", user.ID).Scan(&cost))
	require.True(t, decimal.RequireFromString("0.00000000010").Equal(decimal.RequireFromString(cost)))
	late := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "precision", InputTokens: 9, ActualCost: 0.1, TotalCost: 0.1, CreatedAt: month.Add(48 * time.Hour)}
	_, err := repo.Create(ctx, late)
	require.NoError(t, err)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SELECT * FROM compact_usage_batch(ARRAY[$1::bigint],'Asia/Shanghai')", late.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	var compacted bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT compacted_at IS NOT NULL FROM usage_logs WHERE id=$1", late.ID).Scan(&compacted))
	require.False(t, compacted)
	cfg := &config.Config{}
	cfg.JWT.Secret = "durable-compaction-lease-test"
	settings := service.NewSettingService(nil, cfg)
	svc := service.NewDataCleanupService(integrationDB, settings, nil)
	filter := service.DataCleanupFilter{Category: "usage_logs", Mode: "range", Month: month.Format("2006-01")}
	preview, err := svc.Preview(ctx, filter)
	require.NoError(t, err)
	job, err := svc.Execute(ctx, filter, preview.MatchedRows, preview.Confirmation, preview.PreviewToken, service.DataCleanupOperator{UserID: user.ID})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE data_cleanup_audits SET status='running',filters=filters||jsonb_build_object('heartbeat',(NOW()-interval '3 minutes')::text,'worker','dead-process') WHERE id=$1`, job.AuditID)
	require.NoError(t, err)
	resumed := service.NewDataCleanupService(integrationDB, settings, nil)
	resumed.Start()
	defer resumed.Stop()
	require.Eventually(t, func() bool {
		var status string
		return integrationDB.QueryRowContext(ctx, "SELECT status FROM data_cleanup_audits WHERE id=$1", job.AuditID).Scan(&status) == nil && status == "succeeded"
	}, 15*time.Second, 200*time.Millisecond)
	stats, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
}
