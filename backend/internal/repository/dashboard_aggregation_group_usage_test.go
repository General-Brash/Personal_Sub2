//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestDashboardAggregationRepositorySyncGroupUsageRollupsNoopsAtCurrentDate(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-14", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsRebuildsWhenTimezoneChanges(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "America/New_York")

	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)
	retainedFrom := time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-03-09", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`^SELECT MIN\(created_at\) FROM usage_statistics$`).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(retainedFrom))
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs("2026-03-01", "2026-03-01", "2026-03-09").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups \(bucket_date, group_id, actual_cost, computed_at\) SELECT \(created_at AT TIME ZONE \$3::text\)::date AS bucket_date, group_id, COALESCE\(SUM\(actual_cost\), 0\) AS actual_cost, NOW\(\) FROM usage_statistics WHERE group_id IS NOT NULL AND created_at >= \$1 AND created_at < \$2 GROUP BY 1, 2 ON CONFLICT \(bucket_date, group_id\) DO UPDATE SET actual_cost = EXCLUDED.actual_cost, computed_at = EXCLUDED.computed_at`).
		WithArgs(retainedFrom, todayStart, "America/New_York").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs("2026-03-09", retainedFrom, "America/New_York").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsPublishesWatermarkLast(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)
	retainedFrom := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	rebuildStart := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-13", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`^SELECT MIN\(created_at\) FROM usage_statistics$`).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(retainedFrom))
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs("2026-05-01", "2026-08-13", "2026-08-14").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups \(bucket_date, group_id, actual_cost, computed_at\) SELECT \(created_at AT TIME ZONE \$3::text\)::date AS bucket_date, group_id, COALESCE\(SUM\(actual_cost\), 0\) AS actual_cost, NOW\(\) FROM usage_statistics WHERE group_id IS NOT NULL AND created_at >= \$1 AND created_at < \$2 GROUP BY 1, 2 ON CONFLICT \(bucket_date, group_id\) DO UPDATE SET actual_cost = EXCLUDED.actual_cost, computed_at = EXCLUDED.computed_at`).
		WithArgs(rebuildStart, todayStart, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs("2026-08-14", retainedFrom, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsRejectsFutureWatermark(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-15", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectRollback()

	err := repo.SyncGroupUsageRollups(context.Background(), todayStart)
	require.ErrorContains(t, err, "未来")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryRecomputeRangeInvalidatesGroupRollupsBeforeDashboardRebuild(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(start, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM usage_dashboard_hourly`).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err := repo.RecomputeRange(context.Background(), start, end)
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryRecomputeRangeRebuildsGroupRollupsBeforeCommit(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }
	todayStart := service.GroupUsageTodayStart(fixedNow)
	startDate := service.GroupUsageDate(start)
	rebuildStart, err := service.ParseGroupUsageDate(startDate)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(start, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, query := range []string{
		`DELETE FROM usage_dashboard_hourly WHERE`,
		`DELETE FROM usage_dashboard_hourly_users WHERE`,
		`DELETE FROM usage_dashboard_daily WHERE`,
		`DELETE FROM usage_dashboard_daily_users WHERE`,
		`INSERT INTO usage_dashboard_hourly_users \(bucket_start, user_id\) SELECT DISTINCT date_trunc\('hour', created_at AT TIME ZONE \$3\) AT TIME ZONE \$3 AS bucket_start, user_id FROM usage_statistics WHERE created_at >= \$1 AND created_at < \$2 ON CONFLICT DO NOTHING`,
		`INSERT INTO usage_dashboard_daily_users`,
		`WITH hourly AS \( SELECT date_trunc\('hour', created_at AT TIME ZONE \$3\) AT TIME ZONE \$3 AS bucket_start, COALESCE\(SUM\(request_count\),\s*0\) AS total_requests, COALESCE\(SUM\(input_tokens\), 0\) AS input_tokens, COALESCE\(SUM\(output_tokens\), 0\) AS output_tokens, COALESCE\(SUM\(cache_creation_tokens\), 0\) AS cache_creation_tokens, COALESCE\(SUM\(cache_read_tokens\), 0\) AS cache_read_tokens, COALESCE\(SUM\(total_cost\), 0\) AS total_cost, COALESCE\(SUM\(actual_cost\), 0\) AS actual_cost, COALESCE\(SUM\(COALESCE\(account_stats_cost, total_cost\) \* COALESCE\(account_rate_multiplier, 1\)\), 0\) AS account_cost, COALESCE\(SUM\(COALESCE\(duration_ms, 0\)\), 0\) AS total_duration_ms FROM usage_statistics WHERE created_at >= \$1 AND created_at < \$2 GROUP BY 1`,
		`INSERT INTO usage_dashboard_daily`,
	} {
		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(startDate, time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`^SELECT MIN\(created_at\) FROM usage_statistics$`).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(start))
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs(startDate, startDate, service.GroupUsageDate(todayStart)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups \(bucket_date, group_id, actual_cost, computed_at\) SELECT \(created_at AT TIME ZONE \$3::text\)::date AS bucket_date, group_id, COALESCE\(SUM\(actual_cost\), 0\) AS actual_cost, NOW\(\) FROM usage_statistics WHERE group_id IS NOT NULL AND created_at >= \$1 AND created_at < \$2 GROUP BY 1, 2 ON CONFLICT \(bucket_date, group_id\) DO UPDATE SET actual_cost = EXCLUDED.actual_cost, computed_at = EXCLUDED.computed_at`).
		WithArgs(rebuildStart.UTC(), todayStart.UTC(), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(service.GroupUsageDate(todayStart), start, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.RecomputeRange(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsCompactsMultipleBatches(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }

	ids := make([]int64, 10000)
	rows := sqlmock.NewRows([]string{"id"})
	for i := range ids {
		ids[i] = int64(i + 1)
		rows.AddRow(ids[i])
	}
	expectDashboardUsageCompactionCandidates(t, mock, cutoff, rows)
	mock.ExpectQuery(`^SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch\(\$1,\$2\)$`).
		WithArgs(pq.Array(ids), "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{"processed_rows", "deleted_rows", "protected_rows"}).
			AddRow(int64(10000), int64(0), int64(10000)))
	mock.ExpectCommit()

	expectDashboardUsageCompactionCandidates(t, mock, cutoff, sqlmock.NewRows([]string{"id"}).AddRow(int64(10001)))
	mock.ExpectQuery(`^SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch\(\$1,\$2\)$`).
		WithArgs(pq.Array([]int64{10001}), "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{"processed_rows", "deleted_rows", "protected_rows"}).
			AddRow(int64(1), int64(1), int64(0)))
	mock.ExpectCommit()

	require.NoError(t, repo.CleanupUsageLogs(context.Background(), cutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsCapsCutoffWithoutDeletingPartitions(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }
	completedMonthEnd := service.UsageCompactionEnd(fixedNow)
	require.Equal(t, time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC), completedMonthEnd)

	expectDashboardUsageCompactionCandidates(t, mock, completedMonthEnd, sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectQuery(`^SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch\(\$1,\$2\)$`).
		WithArgs(pq.Array([]int64{42}), "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{"processed_rows", "deleted_rows", "protected_rows"}).
			AddRow(int64(1), int64(0), int64(1)))
	mock.ExpectCommit()

	require.NoError(t, repo.CleanupUsageLogs(context.Background(), cutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsCompactionFailureRollsBackAndStops(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }

	expectDashboardUsageCompactionCandidates(t, mock, cutoff, sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectQuery(`^SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch\(\$1,\$2\)$`).
		WithArgs(pq.Array([]int64{42}), "Asia/Shanghai").
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err := repo.CleanupUsageLogs(context.Background(), cutoff)
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsProcessedCountMismatchRollsBackAndStops(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }

	expectDashboardUsageCompactionCandidates(t, mock, cutoff, sqlmock.NewRows([]string{"id"}).AddRow(int64(42)).AddRow(int64(43)))
	mock.ExpectQuery(`^SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch\(\$1,\$2\)$`).
		WithArgs(pq.Array([]int64{42, 43}), "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{"processed_rows", "deleted_rows", "protected_rows"}).
			AddRow(int64(1), int64(1), int64(0)))
	mock.ExpectRollback()

	err := repo.CleanupUsageLogs(context.Background(), cutoff)
	require.ErrorIs(t, err, service.ErrUsageCleanupSnapshotMismatch)
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectDashboardUsageCompactionCandidates(t *testing.T, mock sqlmock.Sqlmock, cutoff time.Time, rows *sqlmock.Rows) {
	t.Helper()
	mock.ExpectBegin()
	mock.ExpectExec(`^SELECT pg_advisory_xact_lock\(\$1\)$`).
		WithArgs(usageCompactionLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`^SELECT id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE NOWAIT$`).
		WithArgs().
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`^SELECT id FROM usage_logs WHERE created_at >= \$1 AND created_at < \$2 AND compacted_at IS NULL AND created_at < \$3 ORDER BY created_at,id LIMIT \$4 FOR UPDATE NOWAIT$`).
		WithArgs(time.Unix(0, 0).UTC(), cutoff.UTC(), dashboardUsageCompactionEndMatcher{startedAt: time.Now()}, 10000).
		WillReturnRows(rows).
		RowsWillBeClosed()
}

type dashboardUsageCompactionEndMatcher struct {
	startedAt time.Time
}

func (m dashboardUsageCompactionEndMatcher) Match(value driver.Value) bool {
	end, ok := value.(time.Time)
	if !ok {
		return false
	}
	// Accept only the actual month boundary, including a rollover during the call.
	return end.Equal(service.UsageCompactionEnd(m.startedAt)) || end.Equal(service.UsageCompactionEnd(time.Now()))
}

func setGroupUsageRollupTestTimezone(t *testing.T) {
	t.Helper()
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
}
