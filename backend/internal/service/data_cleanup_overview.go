package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type DataCleanupStorage struct {
	Table         string `json:"table"`
	EstimatedRows int64  `json:"estimated_rows"`
	TableBytes    int64  `json:"table_bytes"`
	IndexBytes    int64  `json:"index_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Protected     bool   `json:"protected"`
}

type DataCleanupMonth struct {
	Month      string `json:"month"`
	Users      int64  `json:"users"`
	Requests   int64  `json:"requests"`
	Tokens     int64  `json:"tokens"`
	ActualCost string `json:"actual_cost"`
}

type DataCleanupOverview struct {
	MeasuredAt        time.Time            `json:"measured_at"`
	Timezone          string               `json:"timezone"`
	DatabaseBytes     int64                `json:"database_bytes"`
	Tables            []DataCleanupStorage `json:"tables"`
	Months            []DataCleanupMonth   `json:"months"`
	Requests          int64                `json:"requests"`
	Tokens            int64                `json:"tokens"`
	ActualCost        string               `json:"actual_cost"`
	Years             []DataCleanupMonth   `json:"years"`
	LastCleanupAt     *time.Time           `json:"last_cleanup_at,omitempty"`
	LastCleanupResult string               `json:"last_cleanup_result"`
}

func (s *DataCleanupService) Overview(ctx context.Context) (*DataCleanupOverview, error) {
	out := &DataCleanupOverview{MeasuredAt: time.Now().UTC(), Timezone: cleanupTimezone(), Tables: []DataCleanupStorage{}, Months: []DataCleanupMonth{}, Years: []DataCleanupMonth{}}
	_ = s.db.QueryRowContext(ctx, "SELECT last_success_at,COALESCE(last_result,'') FROM ops_job_heartbeats WHERE job_name='ops_cleanup'").Scan(&out.LastCleanupAt, &out.LastCleanupResult)
	if err := s.db.QueryRowContext(ctx, "SELECT pg_database_size(current_database())").Scan(&out.DatabaseBytes); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT relname,n_live_tup,pg_table_size(relid),pg_indexes_size(relid),pg_total_relation_size(relid)
	 FROM pg_stat_user_tables WHERE relname IN ('usage_logs','usage_history_daily','usage_request_archive','usage_billing_dedup',
	 'usage_billing_dedup_archive','temporary_credit_consumptions','bank_ledger','ops_system_logs','ops_error_logs','ops_alert_events',
	 'ops_system_metrics','ops_metrics_hourly','ops_metrics_daily','audit_logs','data_cleanup_audits','ops_system_log_cleanup_audits')
	 ORDER BY pg_total_relation_size(relid) DESC`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item DataCleanupStorage
		if err = rows.Scan(&item.Table, &item.EstimatedRows, &item.TableBytes, &item.IndexBytes, &item.TotalBytes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.Protected = item.Table == "usage_history_daily" || item.Table == "usage_request_archive" || item.Table == "usage_billing_dedup" || item.Table == "usage_billing_dedup_archive" || item.Table == "temporary_credit_consumptions" || item.Table == "bank_ledger" || item.Table == "data_cleanup_audits"
		out.Tables = append(out.Tables, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(request_count),0),COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0),COALESCE(SUM(actual_cost),0)::text FROM usage_statistics`).Scan(&out.Requests, &out.Tokens, &out.ActualCost); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT month_start::text,COUNT(DISTINCT user_id),SUM(request_count),
	 SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),SUM(actual_cost)::text FROM usage_user_monthly GROUP BY month_start ORDER BY month_start DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var month DataCleanupMonth
		if err = rows.Scan(&month.Month, &month.Users, &month.Requests, &month.Tokens, &month.ActualCost); err != nil {
			return nil, err
		}
		out.Months = append(out.Months, month)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT year::text,request_count,input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens,actual_cost::text FROM usage_site_yearly ORDER BY year DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var year DataCleanupMonth
		if err = rows.Scan(&year.Month, &year.Requests, &year.Tokens, &year.ActualCost); err != nil {
			return nil, err
		}
		out.Years = append(out.Years, year)
	}
	return out, rows.Err()
}

func cleanupCategoryRetention(ctx context.Context, db *sql.DB, category string, cutoff time.Time) (int64, error) {
	target, ok := dataCleanupTargets[category]
	if !ok {
		return 0, fmt.Errorf("unsupported cleanup category %q", category)
	}
	filter := DataCleanupFilter{Category: category, Mode: DataCleanupModeRange, StartTime: ptrCleanupTime(time.Unix(0, 0).UTC()), EndTime: &cutoff}
	var total int64
	for {
		where, args := dataCleanupWhere(filter, target, nil)
		args = append(args, dataCleanupBatchSize)
		result, err := db.ExecContext(ctx, fmt.Sprintf(`WITH batch AS (SELECT target.id FROM %s target WHERE %s ORDER BY target.id LIMIT $%d)
	 DELETE FROM %s WHERE id IN (SELECT id FROM batch)`, target.table, where, len(args), target.table), args...)
		if err != nil {
			return total, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < dataCleanupBatchSize {
			return total, nil
		}
	}
}

func cleanupRuntimeRetention(ctx context.Context, db *sql.DB, policies map[string]OpsRetentionPolicy, now time.Time) (int64, error) {
	base := policies["ops_system_logs"]
	if !base.Enabled || base.RetentionDays <= 0 {
		return 0, nil
	}
	var total int64
	for {
		args := []any{now.AddDate(0, 0, -base.RetentionDays)}
		clauses := []string{"target.created_at < $1"}
		for _, category := range []string{"server_auto_retry", "http_access"} {
			policy := policies[category]
			predicate := dataCleanupTargets[category].extraWhere
			if category == "server_auto_retry" {
				predicate = "target.message LIKE 'server_auto_retry.%'"
			}
			if policy.Enabled && policy.RetentionDays > base.RetentionDays {
				args = append(args, now.AddDate(0, 0, -policy.RetentionDays))
				clauses = append(clauses, fmt.Sprintf("(NOT (%s) OR target.created_at < $%d)", predicate, len(args)))
			}
		}
		args = append(args, dataCleanupBatchSize)
		result, err := db.ExecContext(ctx, fmt.Sprintf(`WITH batch AS(SELECT target.id FROM ops_system_logs target WHERE %s ORDER BY target.id LIMIT $%d)
		 DELETE FROM ops_system_logs WHERE id IN(SELECT id FROM batch)`, strings.Join(clauses, " AND "), len(args)), args...)
		if err != nil {
			return total, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < dataCleanupBatchSize {
			return total, nil
		}
	}
}

func ptrCleanupTime(value time.Time) *time.Time { return &value }
