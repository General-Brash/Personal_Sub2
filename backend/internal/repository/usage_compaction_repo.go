package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const usageCompactionLockID int64 = 25020261109

func (r *usageCleanupRepository) RecordUsageCompactionRetention(ctx context.Context, processed, deleted, protected int64) error {
	_, err := r.sql.ExecContext(ctx, `INSERT INTO data_cleanup_audits(auth_method,category,cleanup_mode,filters,preview_rows,blocked_rows,deleted_rows,status,finished_at)
	 VALUES('scheduler','usage_logs','range',jsonb_build_object('processed_rows',$1::bigint,'protected_rows',$3::bigint),$1,$3,$2,'succeeded',NOW())`, processed, deleted, protected)
	return err
}

func (r *usageCleanupRepository) CompactUsageLogsBatch(ctx context.Context, filters service.UsageCleanupFilters, limit int) (service.UsageCompactionResult, error) {
	var out service.UsageCompactionResult
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return out, errors.New("usage compaction requires a transactional PostgreSQL connection")
	}
	if limit <= 0 {
		limit = 5000
	}
	end := service.UsageCompactionEnd(time.Now())
	if filters.EndTime.IsZero() || filters.EndTime.After(end) {
		filters.EndTime = end
	}
	where, args := buildUsageCleanupWhere(filters)
	where += " AND compacted_at IS NULL AND created_at < $" + fmt.Sprint(len(args)+1)
	args = append(args, end)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	// Match the INSERT trigger's lock order before locking rows or rollup state.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", usageCompactionLockID); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, "SELECT id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE NOWAIT"); err != nil {
		return out, err
	}
	if filters.DataCleanupAuditID > 0 {
		var status string
		var progressRaw []byte
		if err := tx.QueryRowContext(ctx, "SELECT status, filters FROM data_cleanup_audits WHERE id=$1 FOR UPDATE", filters.DataCleanupAuditID).Scan(&status, &progressRaw); err != nil {
			return out, err
		}
		if status == "canceled" {
			return out, context.Canceled
		}
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT id FROM usage_logs WHERE %s ORDER BY created_at,id LIMIT $%d FOR UPDATE NOWAIT", where, len(args)), args...)
	if err != nil {
		return out, err
	}
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil {
		return out, rowErr
	}
	if closeErr != nil {
		return out, closeErr
	}
	if len(ids) == 0 {
		return out, tx.Commit()
	}
	if len(filters.DataCleanupSnapshotDigests) > 0 {
		chunk := filters.DataCleanupSnapshotChunk
		if chunk < 0 || chunk >= len(filters.DataCleanupSnapshotDigests) || service.UsageCleanupRowIdentityDigest(ids) != filters.DataCleanupSnapshotDigests[chunk] {
			return out, service.ErrUsageCleanupSnapshotMismatch
		}
	}
	if err := tx.QueryRowContext(ctx, "SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch($1,$2)", pq.Array(ids), resolveUsageStatsTimezone()).Scan(&out.ProcessedRows, &out.DeletedRows, &out.ProtectedRows); err != nil {
		return out, err
	}
	if out.ProcessedRows != int64(len(ids)) {
		return out, service.ErrUsageCleanupSnapshotMismatch
	}
	if filters.DataCleanupAuditID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE data_cleanup_audits SET deleted_rows=deleted_rows+$2,
			filters=jsonb_set(jsonb_set(filters,'{processed_rows}',to_jsonb(COALESCE((filters->>'processed_rows')::bigint,0)+$3::bigint)),
			'{protected_rows}',to_jsonb(COALESCE((filters->>'protected_rows')::bigint,0)+$4::bigint)) WHERE id=$1`,
			filters.DataCleanupAuditID, out.DeletedRows, out.ProcessedRows, out.ProtectedRows); err != nil {
			return out, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE usage_cleanup_tasks SET deleted_rows=deleted_rows+$2,updated_at=NOW()
			WHERE (filters->>'data_cleanup_audit_id')::bigint=$1 AND status='running'`, filters.DataCleanupAuditID, out.ProcessedRows); err != nil {
			return out, err
		}
	}
	if err := tx.Commit(); err != nil {
		return service.UsageCompactionResult{}, err
	}
	return out, nil
}
