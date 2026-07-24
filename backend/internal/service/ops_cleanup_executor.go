package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	opsCleanupDefaultSchedule  = "0 2 * * *"
	opsCleanupBatchSize        = 5000
	opsCleanupCronStopTimeout  = 3 * time.Second
	opsCleanupRunTimeout       = 30 * time.Minute
	opsCleanupHeartbeatTimeout = 2 * time.Second
)

type opsCleanupTarget struct {
	retentionDays int
	table         string
	timeCol       string
	castDate      bool
	counter       *int64
}

type opsCleanupDeletedCounts struct {
	errorLogs     int64
	alertEvents   int64
	systemLogs    int64
	logAudits     int64
	systemMetrics int64
	hourlyPreagg  int64
	dailyPreagg   int64
	usageLogs     int64
}

func (c opsCleanupDeletedCounts) String() string {
	return fmt.Sprintf(
		"error_logs=%d alert_events=%d system_logs=%d log_audits=%d system_metrics=%d hourly_preagg=%d daily_preagg=%d usage_logs=%d",
		c.errorLogs,
		c.alertEvents,
		c.systemLogs,
		c.logAudits,
		c.systemMetrics,
		c.hourlyPreagg,
		c.dailyPreagg,
		c.usageLogs,
	)
}

// opsCleanupPlan 把"保留天数"翻译成具体的清理动作。
//   - days <= 0 → 跳过该项清理（ok=false）；0 明确表示禁用
//   - days > 0  → 批量 DELETE 早于 now-N天 的行，cutoff = now - N 天
func opsCleanupPlan(now time.Time, days int) (cutoff time.Time, truncate, ok bool) {
	if days <= 0 {
		return time.Time{}, false, false
	}
	return now.AddDate(0, 0, -days), false, true
}

func opsCleanupRunOne(
	ctx context.Context,
	db *sql.DB,
	_ bool,
	cutoff time.Time,
	table, timeCol string,
	castDate bool,
	batchSize int,
) (int64, error) {
	return deleteOldRowsByID(ctx, db, table, timeCol, cutoff, batchSize, castDate)
}

func deleteOldRowsByID(
	ctx context.Context,
	db *sql.DB,
	table string,
	timeColumn string,
	cutoff time.Time,
	batchSize int,
	castCutoffToDate bool,
) (int64, error) {
	if db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = opsCleanupBatchSize
	}

	where := fmt.Sprintf("%s < $1", timeColumn)
	cutoffArg := opsCleanupCutoffArgument(cutoff, castCutoffToDate, timezone.Location())
	if castCutoffToDate {
		where = fmt.Sprintf("%s < $1::date", timeColumn)
	}

	q := fmt.Sprintf(`
WITH batch AS (
  SELECT id FROM %s
  WHERE %s
  ORDER BY id
  LIMIT $2
)
DELETE FROM %s
WHERE id IN (SELECT id FROM batch)
`, table, where, table)

	var total int64
	for {
		res, err := db.ExecContext(ctx, q, cutoffArg, batchSize)
		if err != nil {
			if isMissingRelationError(err) {
				return total, nil
			}
			return total, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += affected
		if affected == 0 {
			break
		}
	}
	return total, nil
}

func opsCleanupCutoffArgument(cutoff time.Time, castToDate bool, loc *time.Location) any {
	if !castToDate {
		return cutoff.UTC()
	}
	if loc == nil {
		loc = time.UTC
	}
	return cutoff.In(loc).Format("2006-01-02")
}

func isMissingRelationError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "does not exist") && strings.Contains(s, "relation")
}
