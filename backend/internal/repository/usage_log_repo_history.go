package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/lib/pq"
)

func usageHistoryMetadata(ctx context.Context, db sqlExecutor, source string, args []any) (usagestats.HistoryMetadata, error) {
	var m usagestats.HistoryMetadata
	var first, last sql.NullTime
	var tz sql.NullString
	err := scanSingleRow(ctx, db, `SELECT COALESCE(BOOL_OR(is_archived), false),
		MIN(created_at) FILTER (WHERE is_archived), MAX(bucket_end) FILTER (WHERE is_archived),
		MIN(bucket_timezone) FILTER (WHERE is_archived) FROM `+source,
		args, &m.IncludesHistory, &first, &last, &tz)
	if err != nil {
		return m, err
	}
	if first.Valid {
		m.HistoryStart = &first.Time
	}
	if last.Valid {
		m.HistoryEnd = &last.Time
	}
	m.HistoryTimezone = tz.String
	m.SetHistoryPolicy()
	return m, nil
}

func (r *usageLogRepository) GetUsageHistoryMetadata(ctx context.Context, start, end time.Time, userID int64) (usagestats.HistoryMetadata, error) {
	source := "usage_statistics_range($1, $2)"
	args := []any{start, end}
	if userID > 0 {
		source += " WHERE user_id = $3"
		args = append(args, userID)
	}
	return usageHistoryMetadata(ctx, r.sql, source, args)
}

func (r *dashboardAggregationRepository) GetUsageHistoryMetadata(ctx context.Context, start, end time.Time, userID int64) (usagestats.HistoryMetadata, error) {
	return usageHistoryMetadata(ctx, r.sql, "usage_statistics_range($1, $2)", []any{start, end})
}

func (r *usageLogRepository) GetAccountUsageHistoryMetadata(ctx context.Context, start, end time.Time, accountID int64) (usagestats.HistoryMetadata, error) {
	return usageHistoryMetadata(ctx, r.sql, "usage_statistics_range($1, $2) WHERE account_id = $3", []any{start, end, accountID})
}

func (r *usageLogRepository) GetAPIKeyUsageHistoryMetadata(ctx context.Context, start, end time.Time, userID, apiKeyID int64) (usagestats.HistoryMetadata, error) {
	return usageHistoryMetadata(ctx, r.sql, "usage_statistics_range($1, $2) WHERE user_id = $3 AND api_key_id = $4", []any{start, end, userID, apiKeyID})
}

func usageStatsDateExpression(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf("(%screated_at AT TIME ZONE COALESCE(%sbucket_timezone, %s))", prefix, prefix, pq.QuoteLiteral(resolveUsageStatsTimezone()))
}

// Apply the range-function policy to separate windows within one scoped query.
func usageStatsRangeCondition(alias, start, end string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf("(%s < %s AND %screated_at < %s AND CASE WHEN %sis_archived THEN %sbucket_end > %s ELSE %screated_at >= %s END)",
		start, end, prefix, end, prefix, prefix, start, prefix, start)
}

func (r *usageLogRepository) withUsageHistory(ctx context.Context, stats *usagestats.UsageStats, source string, args []any) (*usagestats.UsageStats, error) {
	metadata, err := usageHistoryMetadata(ctx, r.sql, source, args)
	if err != nil {
		return nil, err
	}
	stats.HistoryMetadata = metadata
	return stats, nil
}
