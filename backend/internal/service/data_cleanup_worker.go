package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var errCleanupPromptPayload = errors.New("prompt payload cleanup unavailable")

func (s *DataCleanupService) Start() {
	if s == nil || s.db == nil || s.workerCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.workerCancel = cancel
	s.workerWG.Add(1)
	go func() {
		defer s.workerWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runCleanupTask(ctx)
			}
		}
	}()
}

func (s *DataCleanupService) Stop() {
	if s == nil {
		return
	}
	if s.workerCancel != nil {
		s.workerCancel()
	}
	s.workerWG.Wait()
}

func (s *DataCleanupService) queueCleanup(ctx context.Context, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator) (*DataCleanupExecuteResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if filter.Category == "usage_logs" {
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(25020261110::bigint)"); err != nil {
			return nil, err
		}
		var active bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM data_cleanup_audits WHERE category='usage_logs' AND status IN ('pending','running'))").Scan(&active); err != nil {
			return nil, err
		}
		if active {
			return nil, infraerrors.Conflict("DATA_CLEANUP_USAGE_ACTIVE", "usage compaction is already pending or running")
		}
	}
	result, err := queueDataCleanup(ctx, tx, filter, preview, operator)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func queueDataCleanup(ctx context.Context, tx *sql.Tx, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator) (*DataCleanupExecuteResult, error) {
	id, err := insertDataCleanupAudit(ctx, tx, filter, preview, operator, "pending")
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "UPDATE data_cleanup_audits SET filters=filters||jsonb_build_object('preview_token',$2::text,'next_chunk',0,'processed_rows',0) WHERE id=$1 RETURNING id", id, preview.PreviewToken).Scan(&id); err != nil {
		return nil, err
	}
	return &DataCleanupExecuteResult{AuditID: id, Status: "pending", TaskID: &id}, nil
}

func (s *DataCleanupService) queueUsageRetention(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("data cleanup service not initialized")
	}
	end := UsageCompactionEnd(cutoff)
	if latest := UsageCompactionEnd(time.Now()); end.After(latest) {
		end = latest
	}
	start := time.Unix(0, 0).UTC()
	if !end.After(start) {
		return 0, nil
	}
	key := end.Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize schedulers before reading the durable key, including terminal
	// jobs: cancellation must not cause an identical job on the next cron tick.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(25020261110::bigint)"); err != nil {
		return 0, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM data_cleanup_audits WHERE category='usage_logs'
	 AND (filters->>'retention_key'=$1 OR status IN ('pending','running')))`, key).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, nil
	}
	filter := DataCleanupFilter{Category: "usage_logs", Mode: DataCleanupModeRange, StartTime: &start, EndTime: &end}
	target := dataCleanupTargets[filter.Category]
	matched, blocked, maxID, snapshotStart, snapshotEnd, err := s.countRows(ctx, tx, filter, target, nil)
	if err != nil || matched == 0 {
		return 0, err
	}
	batchSize := s.usageCleanup.batchSize()
	if batchSize > 10000 {
		batchSize = 10000
	}
	digests, count, err := dataCleanupSnapshotDigests(ctx, tx, filter, target, maxID, batchSize, "target.created_at,target.id")
	if err != nil {
		return 0, err
	}
	if count != matched {
		return 0, ErrUsageCleanupSnapshotMismatch
	}
	preview := &DataCleanupPreview{MatchedRows: matched, BlockedRows: blocked, snapshotMaxID: maxID,
		snapshotStart: snapshotStart, snapshotEnd: snapshotEnd, snapshotBatchSize: batchSize, snapshotDigests: digests}
	preview.PreviewToken, err = s.signPreviewToken(filter, preview)
	if err != nil {
		return 0, err
	}
	result, err := queueDataCleanup(ctx, tx, filter, preview, DataCleanupOperator{AuthMethod: "scheduler"})
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE data_cleanup_audits SET filters=filters||jsonb_build_object('retention_key',$2::text) WHERE id=$1", result.AuditID, key); err != nil {
		return 0, err
	}
	return 0, tx.Commit()
}

func (s *DataCleanupService) runCleanupTask(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	// Committed batches must lose their payloads even if their task was canceled
	// or the process stopped between PostgreSQL commit and Redis deletion.
	_ = s.flushPromptCleanupPayloads(ctx, 0)
	var id int64
	var raw []byte
	owner := uuid.NewString()
	err := s.db.QueryRowContext(ctx, `WITH next AS (
	 SELECT id FROM data_cleanup_audits WHERE filters ? 'preview_token' AND
	 (status='pending' OR (status='running' AND COALESCE((filters->>'heartbeat')::timestamptz,started_at)<NOW()-interval '2 minutes'))
	 ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
	 UPDATE data_cleanup_audits a SET status='running',filters=a.filters||jsonb_build_object('heartbeat',NOW()::text,'worker',$1::text)
	 FROM next WHERE a.id=next.id RETURNING a.id,a.filters`, owner).Scan(&id, &raw)
	if err != nil {
		return
	}
	var state struct {
		PreviewToken string `json:"preview_token"`
		NextChunk    int    `json:"next_chunk"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		s.failQueuedTask(id, owner, err)
		return
	}
	token, err := s.decodeQueuedPreview(state.PreviewToken)
	if err != nil {
		s.failQueuedTask(id, owner, err)
		return
	}
	if state.NextChunk < 0 || state.NextChunk > len(token.SnapshotDigests) {
		s.failQueuedTask(id, owner, errors.New("invalid cleanup resume position"))
		return
	}
	target, ok := dataCleanupTargets[token.Filter.Category]
	if !ok {
		s.failQueuedTask(id, owner, errors.New("unsupported queued cleanup category"))
		return
	}
	if token.Filter.Category == "prompt_audit" {
		if err = s.flushPromptCleanupPayloads(ctx, id); err != nil {
			s.failQueuedTask(id, owner, err)
			return
		}
	}
	for chunk := state.NextChunk; chunk < len(token.SnapshotDigests); chunk++ {
		if ctx.Err() != nil {
			return
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			if ctx.Err() == nil {
				s.failQueuedTask(id, owner, err)
			}
			return
		}
		if token.Filter.Category == "usage_logs" {
			_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(25020261109::bigint)")
			if err == nil {
				_, err = tx.ExecContext(ctx, "SELECT id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE NOWAIT")
			}
		}
		var status string
		var currentChunk int
		var currentOwner string
		if err == nil {
			err = tx.QueryRowContext(ctx, "SELECT status,COALESCE((filters->>'next_chunk')::integer,0),COALESCE(filters->>'worker','') FROM data_cleanup_audits WHERE id=$1 FOR UPDATE", id).Scan(&status, &currentChunk, &currentOwner)
		}
		if err != nil {
			_ = tx.Rollback()
			if ctx.Err() == nil {
				s.failQueuedTask(id, owner, err)
			}
			return
		}
		if status != "running" || currentChunk != chunk || currentOwner != owner {
			_ = tx.Rollback()
			return
		}
		remaining := token.PreviewRows - int64(chunk*token.SnapshotBatchSize)
		limit := int64(token.SnapshotBatchSize)
		if remaining < limit {
			limit = remaining
		}
		where, args := dataCleanupWhere(token.Filter, target, &token.SnapshotMaxID)
		args = append(args, limit)
		order := "target.id"
		if token.Filter.Category == "usage_logs" {
			order = "target.created_at,target.id"
		}
		var promptJobIDs []int64
		if token.Filter.Category == "prompt_audit" {
			// Match prompt completion's parent-before-event lock order. Delete
			// only these parents, and never cascade into an unmatched sibling.
			var parents *sql.Rows
			parents, err = tx.QueryContext(ctx, fmt.Sprintf(`SELECT j.id FROM prompt_audit_jobs j WHERE j.id IN (
			 SELECT target.job_id FROM prompt_audit_events target WHERE %s ORDER BY target.id LIMIT $%d)
			 ORDER BY j.id FOR UPDATE NOWAIT`, where, len(args)), args...)
			if err == nil {
				for parents.Next() {
					var jobID int64
					if err = parents.Scan(&jobID); err != nil {
						break
					}
					promptJobIDs = append(promptJobIDs, jobID)
				}
				if err == nil {
					err = parents.Err()
				}
				closeErr := parents.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				_ = tx.Rollback()
				if ctx.Err() == nil {
					s.failQueuedTask(id, owner, err)
				}
				return
			}
		}
		rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT target.id FROM %s target WHERE %s ORDER BY %s LIMIT $%d FOR UPDATE NOWAIT", target.table, where, order, len(args)), args...)
		ids := make([]int64, 0, limit)
		if err == nil {
			for rows.Next() {
				var rowID int64
				if err = rows.Scan(&rowID); err != nil {
					break
				}
				ids = append(ids, rowID)
			}
			if err == nil {
				err = rows.Err()
			}
			closeErr := rows.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err == nil && (int64(len(ids)) != limit || UsageCleanupRowIdentityDigest(ids) != token.SnapshotDigests[chunk]) {
			err = ErrUsageCleanupSnapshotMismatch
		}
		processed, deleted, protected := int64(len(ids)), int64(0), int64(0)
		deletedJobIDs := make([]int64, 0)
		if err == nil && token.Filter.Category == "usage_logs" {
			encoded, _ := json.Marshal(ids)
			err = tx.QueryRowContext(ctx, `SELECT processed_rows,deleted_rows,protected_rows FROM compact_usage_batch(
			ARRAY(SELECT value::bigint FROM jsonb_array_elements_text($1::jsonb)),$2)`, string(encoded), cleanupTimezone()).Scan(&processed, &deleted, &protected)
		} else if err == nil {
			var result sql.Result
			result, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id=ANY($1)", target.table), pq.Array(ids))
			if err == nil {
				deleted, err = result.RowsAffected()
			}
		}
		if err == nil && token.Filter.Category == "prompt_audit" && len(promptJobIDs) > 0 {
			var jobs *sql.Rows
			jobs, err = tx.QueryContext(ctx, `DELETE FROM prompt_audit_jobs j WHERE j.id=ANY($1) AND j.status<>'processing'
			 AND NOT EXISTS(SELECT 1 FROM prompt_audit_events e WHERE e.job_id=j.id) RETURNING j.id`, pq.Array(promptJobIDs))
			if err == nil {
				for jobs.Next() {
					var jobID int64
					if err = jobs.Scan(&jobID); err != nil {
						break
					}
					deletedJobIDs = append(deletedJobIDs, jobID)
				}
				if err == nil {
					err = jobs.Err()
				}
				closeErr := jobs.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err == nil && processed != limit {
			err = ErrUsageCleanupSnapshotMismatch
		}
		if err == nil && token.Filter.Category != "usage_logs" && deleted != limit {
			err = ErrUsageCleanupSnapshotMismatch
		}
		if err == nil {
			payloadIDs, _ := json.Marshal(deletedJobIDs)
			_, err = tx.ExecContext(ctx, `UPDATE data_cleanup_audits SET deleted_rows=deleted_rows+$2,
		 filters=filters||jsonb_build_object('processed_rows',COALESCE((filters->>'processed_rows')::bigint,0)+$3::bigint,
		 'protected_rows',COALESCE((filters->>'protected_rows')::bigint,0)+$4::bigint,'next_chunk',$5::integer,'heartbeat',NOW()::text,
		 'deleted_jobs',COALESCE((filters->>'deleted_jobs')::bigint,0)+$6::bigint)
		 || CASE WHEN jsonb_array_length($7::jsonb)>0 THEN jsonb_build_object('pending_prompt_payloads',$7::jsonb) ELSE '{}'::jsonb END
		 WHERE id=$1`, id, deleted, processed, protected, chunk+1, len(deletedJobIDs), string(payloadIDs))
		}
		if err != nil {
			_ = tx.Rollback()
			if ctx.Err() == nil {
				s.failQueuedTask(id, owner, err)
			}
			return
		}
		if err = tx.Commit(); err != nil {
			if ctx.Err() == nil {
				s.failQueuedTask(id, owner, err)
			}
			return
		}
		if token.Filter.Category == "prompt_audit" {
			if err = s.flushPromptCleanupPayloads(ctx, id); err != nil {
				s.failQueuedTask(id, owner, err)
				return
			}
		}
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE data_cleanup_audits SET status='succeeded',finished_at=NOW() WHERE id=$1 AND status='running' AND filters->>'worker'=$2 AND NOT (filters ? 'pending_prompt_payloads')", id, owner)
}

func (s *DataCleanupService) flushPromptCleanupPayloads(parent context.Context, taskID int64) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `SELECT id,filters->'pending_prompt_payloads' FROM data_cleanup_audits
	 WHERE category='prompt_audit' AND filters ? 'pending_prompt_payloads' AND ($1::bigint=0 OR id=$1)
	 ORDER BY id LIMIT 1 FOR UPDATE`
	if taskID == 0 {
		query += " SKIP LOCKED"
	}
	var id int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, query, taskID).Scan(&id, &raw); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	var jobIDs []int64
	if err = json.Unmarshal(raw, &jobIDs); err != nil {
		return err
	}
	if len(jobIDs) > 0 {
		if s.promptPayloads == nil {
			return errCleanupPromptPayload
		}
		keys := make([]string, 0, len(jobIDs))
		for _, jobID := range jobIDs {
			keys = append(keys, fmt.Sprintf("sub2api:prompt_audit:payload:%d", jobID))
		}
		if err = s.promptPayloads.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("%w: %v", errCleanupPromptPayload, err)
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE data_cleanup_audits SET filters=filters-'pending_prompt_payloads' WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DataCleanupService) decodeQueuedPreview(raw string) (*dataCleanupPreviewToken, error) {
	// The signed preview was accepted before queuing; expiry governs submission,
	// not restart/resume of an already-authorized job.
	return s.decodePreviewToken(raw, false)
}

func cleanupTimezone() string {
	if name := timezone.Name(); name != "" && name != "Local" {
		return name
	}
	return "Asia/Shanghai"
}

func (s *DataCleanupService) failQueuedTask(id int64, owner string, taskErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var pgErr *pq.Error
	if errors.Is(taskErr, errCleanupPromptPayload) || errors.Is(taskErr, context.Canceled) || errors.Is(taskErr, context.DeadlineExceeded) ||
		(errors.As(taskErr, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40P01" || pgErr.Code == "40001" || pgErr.Code == "57014")) {
		_, _ = s.db.ExecContext(ctx, "UPDATE data_cleanup_audits SET status='pending',filters=filters-'worker' WHERE id=$1 AND status='running' AND filters->>'worker'=$2", id, owner)
		return
	}
	_, _ = s.db.ExecContext(ctx, "UPDATE data_cleanup_audits SET status='failed',error_message=$2,finished_at=NOW() WHERE id=$1 AND status='running' AND filters->>'worker'=$3", id, errorString(taskErr), owner)
}

func (s *DataCleanupService) CancelTask(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "UPDATE data_cleanup_audits SET status='canceled',finished_at=NOW() WHERE id=$1 AND status IN ('pending','running') AND filters ? 'preview_token'", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *DataCleanupService) TaskCategory(ctx context.Context, id int64) (string, error) {
	var category string
	err := s.db.QueryRowContext(ctx, "SELECT category FROM data_cleanup_audits WHERE id=$1", id).Scan(&category)
	return category, err
}
