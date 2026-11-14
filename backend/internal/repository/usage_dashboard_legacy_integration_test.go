//go:build integration

package repository

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestDashboardLegacyHistoryRecovery(t *testing.T) {
	ctx := context.Background()
	previous := timezone.Name()
	require.NoError(t, timezone.Init("Asia/Shanghai"))
	t.Cleanup(func() { _ = timezone.Init(previous) })
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	_, err := tx.ExecContext(ctx, `SET LOCAL TIME ZONE 'Asia/Shanghai';
		DELETE FROM usage_dashboard_daily;
		DELETE FROM usage_dashboard_legacy_daily;
		DELETE FROM usage_dashboard_legacy_capture;`)
	require.NoError(t, err)
	user := mustCreateUser(t, client, &service.User{Email: "legacy-" + uuid.NewString() + "@fixture.invalid"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-legacy-" + uuid.NewString(), Name: "legacy"})
	account := mustCreateAccount(t, client, &service.Account{Name: "legacy-" + uuid.NewString()})
	oldDay := time.Date(2020, 5, 18, 0, 0, 0, 0, timezone.Location())
	overlapDay := time.Date(2020, 7, 19, 0, 0, 0, 0, timezone.Location())
	end := overlapDay.AddDate(0, 0, 1)
	logs := make([]*service.UsageLog, 3)
	for i := range logs {
		duration := 100
		logs[i] = &service.UsageLog{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
			RequestID: uuid.NewString(), Model: "legacy-fixture",
			InputTokens: 10, OutputTokens: 20, CacheCreationTokens: 30, CacheReadTokens: 40,
			TotalCost: 0.2000000001, ActualCost: 0.1000000001, DurationMs: &duration,
			CreatedAt: overlapDay.Add(time.Duration(i+1) * time.Hour),
		}
		_, err = repo.Create(ctx, logs[i])
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_usage_entries(usage_log_id,user_id,api_key_id,billing_type)
		VALUES($1,$2,$3,0)`, logs[2].ID, user.ID, key.ID)
	require.NoError(t, err)
	var processed, deleted, protected int64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT * FROM compact_usage_batch(ARRAY[$1::bigint,$2::bigint],'Asia/Shanghai')`, []any{logs[1].ID, logs[2].ID}, &processed, &deleted, &protected))
	require.Equal(t, int64(2), processed)
	require.Equal(t, int64(1), protected)
	var archivedRequests int64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT request_count FROM usage_history_daily WHERE (dimensions->>'user_id')::bigint=$1`, []any{user.ID}, &archivedRequests))
	require.Equal(t, int64(2), archivedRequests)
	before, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)

	// The two missing requests on July 19 share a day with one raw request,
	// a two-request archive bucket and its protected, already compacted raw row.
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(
		bucket_date,total_requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,
		total_cost,actual_cost,account_cost,total_duration_ms)
		VALUES ($1::date,126352,2500000000,100000000,100000000,9982544000,12.0000000001,10.0000000001,8.0000000001,12635200),
		       ($2::date,5,70,90,120,151,1.6000000004,0.8000000004,1.6000000004,500)`,
		oldDay.Format("2006-01-02"), overlapDay.Format("2006-01-02"))
	require.NoError(t, err)
	migration, err := fs.ReadFile(migrations.FS, "256_preserve_legacy_dashboard_usage.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var missingRequests, missingTokens int64
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT SUM(total_requests),
		SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens)
		FROM usage_dashboard_legacy_daily`, nil, &missingRequests, &missingTokens))
	require.Equal(t, int64(126354), missingRequests)
	require.Equal(t, int64(12682544131), missingTokens)
	after, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, before.TotalRequests+missingRequests, after.TotalRequests)
	require.Equal(t, before.TotalTokens+missingTokens, after.TotalTokens)
	require.InDelta(t, before.TotalCost+13.0000000002, after.TotalCost, 1e-9)
	require.InDelta(t, before.TotalActualCost+10.5000000002, after.TotalActualCost, 1e-9)
	require.InDelta(t, before.TotalAccountCost+9.0000000002, after.TotalAccountCost, 1e-9)
	require.Equal(t, before.TodayRequests, after.TodayRequests)
	require.Equal(t, before.TodayTokens, after.TodayTokens)

	ranged, err := repo.GetDashboardStatsWithRange(ctx, overlapDay, end)
	require.NoError(t, err)
	require.Equal(t, int64(5), ranged.TotalRequests)
	require.Equal(t, int64(431), ranged.TotalTokens)
	require.InDelta(t, 100, ranged.AverageDurationMs, 1e-9)
	partial, err := repo.GetDashboardStatsWithRange(ctx, overlapDay.Add(12*time.Hour), overlapDay.Add(13*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(4), partial.TotalRequests, "archive and legacy use whole intersecting days; raw timestamps remain exact")
	require.Equal(t, int64(331), partial.TotalTokens)
	outside, err := repo.GetDashboardStatsWithRange(ctx, end, end.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, outside.TotalRequests, "the legacy day must not leak across its exclusive end")
	userStats, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(3), userStats.TotalRequests)
	require.Equal(t, int64(300), userStats.TotalTokens)
	keyStats, err := repo.GetAPIKeyDashboardStats(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, int64(300), keyStats.TotalTokens, "dimensionless legacy history is site-only")

	// Replaying after the cache changes must never capture a second delta.
	_, err = tx.ExecContext(ctx, `UPDATE usage_dashboard_daily SET total_requests=total_requests+7,cache_read_tokens=cache_read_tokens+700 WHERE bucket_date=$1::date`, overlapDay.Format("2006-01-02"))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	replayed, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, after.TotalRequests, replayed.TotalRequests)
	require.Equal(t, after.TotalTokens, replayed.TotalTokens)
	late := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
		RequestID: uuid.NewString(), Model: "legacy-fixture", InputTokens: 17,
		ActualCost: 0.01, TotalCost: 0.01, CreatedAt: overlapDay.Add(12 * time.Hour)}
	_, err = repo.Create(ctx, late)
	require.NoError(t, err)
	withLate, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, after.TotalRequests+1, withLate.TotalRequests)
	require.Equal(t, after.TotalTokens+17, withLate.TotalTokens)
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT * FROM compact_usage_batch(ARRAY[$1::bigint,$2::bigint],'Asia/Shanghai')`, []any{logs[0].ID, late.ID}, &processed, &deleted, &protected))
	compacted, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, withLate.TotalRequests, compacted.TotalRequests)
	require.Equal(t, withLate.TotalTokens, compacted.TotalTokens)
	agg := newDashboardAggregationRepositoryWithSQL(tx)
	require.NoError(t, agg.AggregateRange(ctx, oldDay, end))
	require.NoError(t, agg.RecomputeRange(ctx, oldDay, end))
	require.NoError(t, agg.CleanupAggregates(ctx, end, end))
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	cleaned, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, withLate.TotalRequests, cleaned.TotalRequests)
	require.Equal(t, withLate.TotalTokens, cleaned.TotalTokens)
	require.Equal(t, withLate.TotalCost, cleaned.TotalCost)
}

func legacyCaptureTestTx(t *testing.T, zone string) (*sql.Tx, string) {
	t.Helper()
	tx := testTx(t)
	_, err := tx.ExecContext(context.Background(), `SELECT set_config('TimeZone',$1,true),set_config('sub2api.legacy_dashboard_timezone','',true)`, zone)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), `DELETE FROM usage_dashboard_daily;
		DELETE FROM usage_dashboard_legacy_daily;
		DELETE FROM usage_dashboard_legacy_capture;`)
	require.NoError(t, err)
	migration, err := fs.ReadFile(migrations.FS, "256_preserve_legacy_dashboard_usage.sql")
	require.NoError(t, err)
	return tx, string(migration)
}

func TestDashboardLegacyCaptureSafety(t *testing.T) {
	ctx := context.Background()
	t.Run("covered_stale_and_open_days", func(t *testing.T) {
		tx, migration := legacyCaptureTestTx(t, "UTC")
		_, err := tx.ExecContext(ctx, `INSERT INTO usage_history_daily(
			bucket_start,month_start,timezone,dimensions,dimension_key,request_count,input_tokens,first_created_at,last_created_at)
			VALUES ('2019-07-19T00:00Z','2019-07-01','UTC','{}','legacy-safety',3,30,'2019-07-19T00:00Z','2019-07-19T01:00Z');
			INSERT INTO usage_dashboard_daily(bucket_date,total_requests,input_tokens) VALUES
			('2019-07-19',3,30),('2019-07-20',0,40),(CURRENT_DATE,9,90),(CURRENT_DATE+1,9,90);`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.NoError(t, err)
		var captured int64
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_dashboard_legacy_daily").Scan(&captured))
		require.Zero(t, captured)
		_, err = tx.ExecContext(ctx, "UPDATE usage_dashboard_daily SET total_requests=total_requests+1,input_tokens=input_tokens+10")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.NoError(t, err)
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_dashboard_legacy_daily").Scan(&captured))
		require.Zero(t, captured, "an empty initial capture is also immutable")
	})
	t.Run("timezone_alias_and_signed_cost", func(t *testing.T) {
		tx, migration := legacyCaptureTestTx(t, "UTC")
		_, err := tx.ExecContext(ctx, `INSERT INTO usage_history_daily(
			bucket_start,month_start,timezone,dimensions,dimension_key,request_count,input_tokens,
			total_cost,actual_cost,account_cost,duration_ms,first_created_at,last_created_at)
			VALUES ('2019-07-19T00:00Z','2019-07-01','Etc/UTC','{}','legacy-safety',3,30,2,2,2,300,'2019-07-19T00:00Z','2019-07-19T01:00Z');
			INSERT INTO usage_dashboard_daily(bucket_date,total_requests,input_tokens,total_cost,actual_cost,account_cost,total_duration_ms)
			VALUES ('2019-07-19',5,50,1,1,1,500);`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.NoError(t, err)
		var requests, tokens int64
		var totalCost, actualCost, accountCost float64
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT total_requests,input_tokens,total_cost,actual_cost,account_cost
			FROM usage_dashboard_legacy_daily`).Scan(&requests, &tokens, &totalCost, &actualCost, &accountCost))
		require.Equal(t, int64(2), requests)
		require.Equal(t, int64(20), tokens)
		require.Equal(t, float64(-1), totalCost)
		require.Equal(t, float64(-1), actualCost)
		require.Equal(t, float64(-1), accountCost)
	})
	t.Run("original_timezone_override", func(t *testing.T) {
		tx, migration := legacyCaptureTestTx(t, "UTC")
		_, err := tx.ExecContext(ctx, `SET LOCAL sub2api.legacy_dashboard_timezone='Asia/Shanghai';
			INSERT INTO usage_history_daily(
			bucket_start,month_start,timezone,dimensions,dimension_key,request_count,input_tokens,first_created_at,last_created_at)
			VALUES ('2019-07-18T16:00Z','2019-07-01','Asia/Shanghai','{}','legacy-safety',3,30,'2019-07-18T16:30Z','2019-07-18T17:00Z');
			INSERT INTO usage_dashboard_daily(bucket_date,total_requests,input_tokens) VALUES ('2019-07-19',5,50);`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.NoError(t, err)
		var requests int64
		var zone string
		var start time.Time
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT total_requests,timezone,bucket_start
			FROM usage_dashboard_legacy_daily_buckets`).Scan(&requests, &zone, &start))
		require.Equal(t, int64(2), requests)
		require.Equal(t, "Asia/Shanghai", zone)
		require.True(t, start.Equal(time.Date(2019, 7, 18, 16, 0, 0, 0, time.UTC)))
	})
	t.Run("mismatched_boundaries_and_invalid_tokens_rollback", func(t *testing.T) {
		tx, migration := legacyCaptureTestTx(t, "UTC")
		_, err := tx.ExecContext(ctx, `INSERT INTO usage_history_daily(
			bucket_start,month_start,timezone,dimensions,dimension_key,request_count,input_tokens,first_created_at,last_created_at)
			VALUES ('2019-07-18T16:00Z','2019-07-01','Asia/Shanghai','{}','legacy-safety',3,30,'2019-07-18T16:30Z','2019-07-18T17:00Z');
			INSERT INTO usage_dashboard_daily(bucket_date,total_requests,input_tokens) VALUES ('2019-07-19',5,50);
			SAVEPOINT legacy_capture;`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.ErrorContains(t, err, "day boundaries differ")
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT legacy_capture")
		require.NoError(t, err)
		var captures int64
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_dashboard_legacy_capture").Scan(&captures))
		require.Zero(t, captures)
		_, err = tx.ExecContext(ctx, `SET LOCAL sub2api.legacy_dashboard_timezone='Asia/Shanghai';
			UPDATE usage_dashboard_daily SET input_tokens=29;
			SAVEPOINT invalid_tokens;`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		var pgErr *pq.Error
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, pq.ErrorCode("23514"), pgErr.Code, "inconsistent deltas must not be silently clamped")
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT invalid_tokens")
		require.NoError(t, err)
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_dashboard_legacy_capture").Scan(&captures))
		require.Zero(t, captures, "failed capture must remain retryable")
	})
	t.Run("daylight_saving_boundaries", func(t *testing.T) {
		tx, migration := legacyCaptureTestTx(t, "America/New_York")
		_, err := tx.ExecContext(ctx, `INSERT INTO usage_dashboard_daily(bucket_date,total_requests,input_tokens)
			VALUES ('2020-03-08',1,10),('2020-11-01',1,10);`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, migration)
		require.NoError(t, err)
		var springHours, fallHours float64
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM (bucket_end-bucket_start))/3600
			FROM usage_dashboard_legacy_daily_buckets WHERE bucket_date='2020-03-08'`).Scan(&springHours))
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM (bucket_end-bucket_start))/3600
			FROM usage_dashboard_legacy_daily_buckets WHERE bucket_date='2020-11-01'`).Scan(&fallHours))
		require.Equal(t, float64(23), springHours)
		require.Equal(t, float64(25), fallHours)
	})
}
