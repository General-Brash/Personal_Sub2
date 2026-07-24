package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	DataCleanupModeRange = "range"
	DataCleanupModeAll   = "all"

	dataCleanupBatchSize    = 5000
	dataCleanupMaxRangeDays = 31
	dataCleanupPreviewTTL   = 10 * time.Minute
	dataCleanupAuditTimeout = 5 * time.Second
)

type DataCleanupFilter struct {
	Category  string     `json:"category"`
	Mode      string     `json:"mode"`
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
}

type DataCleanupPreview struct {
	Category        string `json:"category"`
	Mode            string `json:"mode"`
	MatchedRows     int64  `json:"matched_rows"`
	BlockedRows     int64  `json:"blocked_rows"`
	PreviewToken    string `json:"preview_token"`
	Confirmation    string `json:"confirmation"`
	RequiresTOTP    bool   `json:"requires_totp"`
	MaxRangeDays    int    `json:"max_range_days"`
	DeletionWarning string `json:"deletion_warning,omitempty"`

	snapshotMaxID int64
	snapshotStart *time.Time
	snapshotEnd   *time.Time
}

type DataCleanupOperator struct {
	UserID     int64
	Email      string
	AuthMethod string
}

type DataCleanupExecuteResult struct {
	AuditID     int64  `json:"audit_id"`
	Status      string `json:"status"`
	DeletedRows int64  `json:"deleted_rows"`
	TaskID      *int64 `json:"task_id,omitempty"`
}

type DataCleanupAudit struct {
	ID            int64      `json:"id"`
	OperatorID    *int64     `json:"operator_id,omitempty"`
	OperatorEmail string     `json:"operator_email"`
	AuthMethod    string     `json:"auth_method"`
	Category      string     `json:"category"`
	Mode          string     `json:"mode"`
	Filters       string     `json:"filters"`
	PreviewRows   int64      `json:"preview_rows"`
	BlockedRows   int64      `json:"blocked_rows"`
	DeletedRows   int64      `json:"deleted_rows"`
	Status        string     `json:"status"`
	ErrorMessage  string     `json:"error_message"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type dataCleanupTarget struct {
	table      string
	timeColumn string
	castDate   bool
	sensitive  bool
	extraWhere string
	blockedBy  string
	warning    string
}

type dataCleanupPreviewToken struct {
	Filter        DataCleanupFilter `json:"filter"`
	PreviewRows   int64             `json:"preview_rows"`
	BlockedRows   int64             `json:"blocked_rows"`
	SnapshotMaxID int64             `json:"snapshot_max_id"`
	SnapshotStart *time.Time        `json:"snapshot_start,omitempty"`
	SnapshotEnd   *time.Time        `json:"snapshot_end,omitempty"`
	ExpiresAtUnix int64             `json:"expires_at"`
}

var dataCleanupTargets = map[string]dataCleanupTarget{
	"ops_error_logs":                {table: "ops_error_logs", timeColumn: "created_at"},
	"ops_alert_events":              {table: "ops_alert_events", timeColumn: "created_at"},
	"ops_system_logs":               {table: "ops_system_logs", timeColumn: "created_at"},
	"ops_system_log_cleanup_audits": {table: "ops_system_log_cleanup_audits", timeColumn: "created_at"},
	"ops_system_metrics":            {table: "ops_system_metrics", timeColumn: "created_at"},
	"ops_metrics_hourly":            {table: "ops_metrics_hourly", timeColumn: "bucket_start"},
	"ops_metrics_daily":             {table: "ops_metrics_daily", timeColumn: "bucket_date", castDate: true},
	"usage_logs":                    {table: "usage_logs", timeColumn: "created_at"},
	"audit_logs":                    {table: "audit_logs", timeColumn: "created_at", sensitive: true},
	"mall_purchases": {
		table:      "mall_purchases",
		timeColumn: "created_at",
		sensitive:  true,
		extraWhere: "NOT EXISTS (SELECT 1 FROM temporary_credit_grants AS tcg WHERE tcg.mall_purchase_id = target.id)",
		blockedBy:  "EXISTS (SELECT 1 FROM temporary_credit_grants AS tcg WHERE tcg.mall_purchase_id = target.id)",
		warning:    "purchases referenced by temporary credit grants are protected",
	},
	"payment_orders": {table: "payment_orders", timeColumn: "created_at", sensitive: true},
	"bank_ledger": {
		table:      "bank_ledger",
		timeColumn: "created_at",
		sensitive:  true,
		warning:    "deleting bank ledger rows permanently removes financial history",
	},
}

// DataCleanupService owns the destructive, allowlisted cleanup workflow and its
// independent audit trail. Automatic scheduling remains in the existing ops,
// usage and audit services.
type DataCleanupService struct {
	db           *sql.DB
	settings     *SettingService
	usageCleanup *UsageCleanupService
	previewKey   []byte
}

func NewDataCleanupService(db *sql.DB, settings *SettingService, usageCleanup *UsageCleanupService) *DataCleanupService {
	key := make([]byte, sha256.Size)
	if settings != nil && settings.cfg != nil && strings.TrimSpace(settings.cfg.JWT.Secret) != "" {
		digest := sha256.Sum256([]byte("data-cleanup-preview:" + strings.TrimSpace(settings.cfg.JWT.Secret)))
		copy(key, digest[:])
	} else if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("generate data cleanup preview key: %v", err))
	}
	svc := &DataCleanupService{db: db, settings: settings, usageCleanup: usageCleanup, previewKey: key}
	if usageCleanup != nil {
		usageCleanup.SetDataCleanupCompletionHook(svc.completeUsageCleanupAudit)
	}
	return svc
}

func (s *DataCleanupService) GetAuditLogRetentionDays(ctx context.Context) int {
	if s == nil || s.settings == nil {
		return 0
	}
	return s.settings.GetAuditLogRetentionDays(ctx)
}

func (s *DataCleanupService) SetAuditLogRetentionDays(ctx context.Context, days int) error {
	if s == nil || s.settings == nil {
		return errors.New("setting service not initialized")
	}
	if days < 0 || days > 3650 {
		return infraerrors.BadRequest("DATA_CLEANUP_AUDIT_RETENTION_INVALID", "audit log retention days must be between 0 and 3650")
	}
	return s.settings.SetAuditLogRetentionDays(ctx, days)
}

func (s *DataCleanupService) Preview(ctx context.Context, filter DataCleanupFilter) (*DataCleanupPreview, error) {
	target, normalized, err := validateDataCleanupFilter(filter)
	if err != nil {
		return nil, err
	}
	matched, blocked, snapshotMaxID, snapshotStart, snapshotEnd, err := s.countRows(ctx, nil, normalized, target, nil)
	if err != nil {
		return nil, err
	}
	preview := &DataCleanupPreview{
		Category:        normalized.Category,
		Mode:            normalized.Mode,
		MatchedRows:     matched,
		BlockedRows:     blocked,
		Confirmation:    dataCleanupConfirmation(normalized, matched),
		RequiresTOTP:    target.sensitive || normalized.Mode == DataCleanupModeAll,
		MaxRangeDays:    dataCleanupMaxRangeDays,
		DeletionWarning: target.warning,
		snapshotMaxID:   snapshotMaxID,
		snapshotStart:   snapshotStart,
		snapshotEnd:     snapshotEnd,
	}
	preview.PreviewToken, err = s.signPreviewToken(normalized, preview)
	if err != nil {
		return nil, err
	}
	return preview, nil
}

func (s *DataCleanupService) Execute(
	ctx context.Context,
	filter DataCleanupFilter,
	previewRows int64,
	confirmation string,
	previewToken string,
	operator DataCleanupOperator,
) (*DataCleanupExecuteResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("data cleanup service not initialized")
	}
	if operator.UserID <= 0 {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_OPERATOR_INVALID", "invalid cleanup operator")
	}
	target, normalized, err := validateDataCleanupFilter(filter)
	if err != nil {
		return nil, err
	}
	token, err := s.verifyPreviewToken(strings.TrimSpace(previewToken))
	if err != nil {
		return nil, err
	}
	if !sameDataCleanupFilter(token.Filter, normalized) || token.PreviewRows != previewRows {
		return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_MISMATCH", "cleanup preview does not match the requested filter")
	}
	preview := &DataCleanupPreview{
		Category:      normalized.Category,
		Mode:          normalized.Mode,
		MatchedRows:   token.PreviewRows,
		BlockedRows:   token.BlockedRows,
		snapshotMaxID: token.SnapshotMaxID,
		snapshotStart: token.SnapshotStart,
		snapshotEnd:   token.SnapshotEnd,
	}
	if strings.TrimSpace(confirmation) != dataCleanupConfirmation(normalized, token.PreviewRows) {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_CONFIRMATION_INVALID", "confirmation text does not match")
	}

	if normalized.Category == "usage_logs" {
		matched, blocked, _, _, _, err := s.countRows(ctx, nil, normalized, target, &token.SnapshotMaxID)
		if err != nil {
			return nil, err
		}
		if matched != token.PreviewRows || blocked != token.BlockedRows {
			return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup preview changed; preview again before executing")
		}
		return s.queueUsageCleanup(ctx, normalized, preview, operator)
	}
	return s.executeTransactional(ctx, normalized, target, preview, operator)
}

func (s *DataCleanupService) ListAudits(ctx context.Context, limit int) ([]DataCleanupAudit, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("data cleanup service not initialized")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, operator_id, operator_email, auth_method, category, cleanup_mode,
       filters::text, preview_rows, blocked_rows, deleted_rows, status,
       error_message, started_at, finished_at, created_at
FROM data_cleanup_audits
ORDER BY id DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]DataCleanupAudit, 0, limit)
	for rows.Next() {
		var item DataCleanupAudit
		if err := rows.Scan(
			&item.ID, &item.OperatorID, &item.OperatorEmail, &item.AuthMethod,
			&item.Category, &item.Mode, &item.Filters, &item.PreviewRows,
			&item.BlockedRows, &item.DeletedRows, &item.Status,
			&item.ErrorMessage, &item.StartedAt, &item.FinishedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateDataCleanupFilter(filter DataCleanupFilter) (dataCleanupTarget, DataCleanupFilter, error) {
	filter.Category = strings.ToLower(strings.TrimSpace(filter.Category))
	filter.Mode = strings.ToLower(strings.TrimSpace(filter.Mode))
	if filter.Mode == "" {
		filter.Mode = DataCleanupModeRange
	}
	target, ok := dataCleanupTargets[filter.Category]
	if !ok {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_CATEGORY_INVALID", "unsupported cleanup category")
	}
	if filter.Mode != DataCleanupModeRange && filter.Mode != DataCleanupModeAll {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_MODE_INVALID", "cleanup mode must be range or all")
	}
	if filter.Mode == DataCleanupModeAll {
		filter.StartTime = nil
		filter.EndTime = nil
		return target, filter, nil
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_REQUIRED", "start_time and end_time are required")
	}
	if !filter.EndTime.After(*filter.StartTime) {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_INVALID", "end_time must be after start_time")
	}
	if filter.EndTime.Sub(*filter.StartTime) > dataCleanupMaxRangeDays*24*time.Hour {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_TOO_LARGE", fmt.Sprintf("date range exceeds %d days", dataCleanupMaxRangeDays))
	}
	startUTC := filter.StartTime.UTC()
	endUTC := filter.EndTime.UTC()
	filter.StartTime = &startUTC
	filter.EndTime = &endUTC
	return target, filter, nil
}

func dataCleanupConfirmation(filter DataCleanupFilter, matched int64) string {
	if filter.Mode == DataCleanupModeAll {
		return "DELETE ALL " + filter.Category
	}
	return fmt.Sprintf("DELETE %s %d", filter.Category, matched)
}

func (s *DataCleanupService) signPreviewToken(filter DataCleanupFilter, preview *DataCleanupPreview) (string, error) {
	if s == nil || len(s.previewKey) == 0 || preview == nil {
		return "", errors.New("data cleanup preview token service not initialized")
	}
	payload := dataCleanupPreviewToken{
		Filter:        filter,
		PreviewRows:   preview.MatchedRows,
		BlockedRows:   preview.BlockedRows,
		SnapshotMaxID: preview.snapshotMaxID,
		SnapshotStart: preview.snapshotStart,
		SnapshotEnd:   preview.snapshotEnd,
		ExpiresAtUnix: time.Now().UTC().Add(dataCleanupPreviewTTL).Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.previewKey)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *DataCleanupService) verifyPreviewToken(token string) (*dataCleanupPreviewToken, error) {
	if s == nil || len(s.previewKey) == 0 || token == "" {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "a valid cleanup preview token is required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is invalid")
	}
	mac := hmac.New(sha256.New, s.previewKey)
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is invalid")
	}
	var payload dataCleanupPreviewToken
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is invalid")
	}
	if payload.ExpiresAtUnix <= time.Now().UTC().Unix() {
		return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_EXPIRED", "cleanup preview expired; preview again before executing")
	}
	return &payload, nil
}

func sameDataCleanupFilter(left, right DataCleanupFilter) bool {
	if left.Category != right.Category || left.Mode != right.Mode {
		return false
	}
	return sameOptionalTime(left.StartTime, right.StartTime) && sameOptionalTime(left.EndTime, right.EndTime)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func dataCleanupWhere(filter DataCleanupFilter, target dataCleanupTarget, snapshotMaxID *int64) (string, []any) {
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if filter.Mode == DataCleanupModeRange {
		if target.castDate {
			conditions = append(conditions, "target."+target.timeColumn+" >= $1::date", "target."+target.timeColumn+" < $2::date")
			args = append(args,
				dataCleanupDateBoundary(*filter.StartTime, timezone.Location()),
				dataCleanupDateBoundary(*filter.EndTime, timezone.Location()),
			)
		} else {
			conditions = append(conditions, "target."+target.timeColumn+" >= $1", "target."+target.timeColumn+" < $2")
			args = append(args, filter.StartTime.UTC(), filter.EndTime.UTC())
		}
	}
	if snapshotMaxID != nil {
		conditions = append(conditions, fmt.Sprintf("target.id <= $%d", len(args)+1))
		args = append(args, *snapshotMaxID)
	}
	if target.extraWhere != "" {
		conditions = append(conditions, target.extraWhere)
	}
	if len(conditions) == 0 {
		return "TRUE", args
	}
	return strings.Join(conditions, " AND "), args
}

func dataCleanupDateBoundary(value time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return value.In(loc).Format("2006-01-02")
}

func (s *DataCleanupService) countRows(
	ctx context.Context,
	tx *sql.Tx,
	filter DataCleanupFilter,
	target dataCleanupTarget,
	snapshotMaxID *int64,
) (int64, int64, int64, *time.Time, *time.Time, error) {
	if s == nil || s.db == nil {
		return 0, 0, 0, nil, nil, errors.New("data cleanup service not initialized")
	}
	where, args := dataCleanupWhere(filter, target, snapshotMaxID)
	queryer := interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}(s.db)
	if tx != nil {
		queryer = tx
	}
	var matched, maxID int64
	var minTime, maxTime sql.NullTime
	query := fmt.Sprintf(
		"SELECT COUNT(*), COALESCE(MAX(target.id), 0), MIN(target.%s), MAX(target.%s) FROM %s AS target WHERE %s",
		target.timeColumn, target.timeColumn, target.table, where,
	)
	if err := queryer.QueryRowContext(ctx, query, args...).Scan(&matched, &maxID, &minTime, &maxTime); err != nil {
		return 0, 0, 0, nil, nil, err
	}
	var blocked int64
	if target.blockedBy != "" {
		blockedWhere, blockedArgs := dataCleanupWhere(filter, dataCleanupTarget{timeColumn: target.timeColumn, castDate: target.castDate}, snapshotMaxID)
		blockedWhere += " AND " + target.blockedBy
		var blockedMaxID int64
		if err := queryer.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*), COALESCE(MAX(target.id), 0) FROM %s AS target WHERE %s", target.table, blockedWhere), blockedArgs...).Scan(&blocked, &blockedMaxID); err != nil {
			return 0, 0, 0, nil, nil, err
		}
		if blockedMaxID > maxID {
			maxID = blockedMaxID
		}
	}
	var snapshotStart, snapshotEnd *time.Time
	if minTime.Valid {
		value := minTime.Time.UTC()
		snapshotStart = &value
	}
	if maxTime.Valid {
		value := maxTime.Time.UTC()
		snapshotEnd = &value
	}
	return matched, blocked, maxID, snapshotStart, snapshotEnd, nil
}

func (s *DataCleanupService) executeTransactional(
	ctx context.Context,
	filter DataCleanupFilter,
	target dataCleanupTarget,
	preview *DataCleanupPreview,
	operator DataCleanupOperator,
) (*DataCleanupExecuteResult, error) {
	auditCtx, auditCancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	auditID, err := insertDataCleanupAudit(auditCtx, s.db, filter, preview, operator, "running")
	auditCancel()
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	matched, blocked, _, _, _, err := s.countRows(ctx, tx, filter, target, &preview.snapshotMaxID)
	if err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if matched != preview.MatchedRows || blocked != preview.BlockedRows {
		err = infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup preview changed; preview again before executing")
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	deleted, err := deleteDataCleanupRows(ctx, tx, filter, target, preview.snapshotMaxID, preview.MatchedRows)
	if err != nil {
		_ = tx.Rollback()
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	finishErr := s.finishAudit(finishCtx, auditID, "succeeded", deleted, nil)
	finishCancel()
	if finishErr != nil {
		s.insertFailureAuditDetached(filter, preview, operator, fmt.Errorf("cleanup committed but audit finalization failed: %w", finishErr))
		return nil, fmt.Errorf("cleanup committed but audit finalization failed: %w", finishErr)
	}
	return &DataCleanupExecuteResult{AuditID: auditID, Status: "succeeded", DeletedRows: deleted}, nil
}

func deleteDataCleanupRows(ctx context.Context, tx *sql.Tx, filter DataCleanupFilter, target dataCleanupTarget, snapshotMaxID, maxRows int64) (int64, error) {
	if maxRows <= 0 {
		return 0, nil
	}
	where, baseArgs := dataCleanupWhere(filter, target, &snapshotMaxID)
	limitPos := len(baseArgs) + 1
	query := fmt.Sprintf(`
WITH batch AS (
    SELECT target.id FROM %s AS target
    WHERE %s
    ORDER BY target.id
    LIMIT $%d
    FOR UPDATE
)
DELETE FROM %s AS target
WHERE target.id IN (SELECT id FROM batch)`, target.table, where, limitPos, target.table)
	var total int64
	for total < maxRows {
		limit := int64(dataCleanupBatchSize)
		if remaining := maxRows - total; remaining < limit {
			limit = remaining
		}
		args := append(append([]any{}, baseArgs...), limit)
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < limit {
			return total, nil
		}
	}
	return total, nil
}

func (s *DataCleanupService) queueUsageCleanup(ctx context.Context, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator) (*DataCleanupExecuteResult, error) {
	if s.usageCleanup == nil {
		return nil, errors.New("usage cleanup service not initialized")
	}
	auditCtx, auditCancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	auditID, err := insertDataCleanupAudit(auditCtx, s.db, filter, preview, operator, "pending")
	auditCancel()
	if err != nil {
		return nil, err
	}
	usageFilter := buildDataCleanupUsageFilters(filter, preview, auditID)
	task, err := s.usageCleanup.CreateTask(ctx, usageFilter, operator.UserID)
	if err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	filtersJSON, _ := json.Marshal(map[string]any{
		"category":              filter.Category,
		"mode":                  filter.Mode,
		"start_time":            filter.StartTime,
		"end_time":              filter.EndTime,
		"snapshot_max_id":       preview.snapshotMaxID,
		"usage_cleanup_task_id": task.ID,
	})
	updateCtx, updateCancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	_, _ = s.db.ExecContext(updateCtx, "UPDATE data_cleanup_audits SET filters = $2::jsonb WHERE id = $1", auditID, string(filtersJSON))
	updateCancel()
	return &DataCleanupExecuteResult{AuditID: auditID, Status: "pending", TaskID: &task.ID}, nil
}

func buildDataCleanupUsageFilters(filter DataCleanupFilter, preview *DataCleanupPreview, auditID int64) UsageCleanupFilters {
	usageFilter := UsageCleanupFilters{
		All:                      filter.Mode == DataCleanupModeAll,
		DataCleanupAuditID:       auditID,
		DataCleanupSnapshotMaxID: preview.snapshotMaxID,
		DataCleanupSnapshotRows:  preview.MatchedRows,
	}
	if filter.StartTime != nil {
		usageFilter.StartTime = filter.StartTime.UTC()
	}
	if filter.EndTime != nil {
		usageFilter.EndTime = filter.EndTime.UTC()
	}
	if usageFilter.All {
		now := time.Now().UTC()
		usageFilter.StartTime = now
		usageFilter.EndTime = now.Add(time.Microsecond)
		if preview.snapshotStart != nil {
			usageFilter.StartTime = preview.snapshotStart.UTC()
		}
		if preview.snapshotEnd != nil {
			usageFilter.EndTime = preview.snapshotEnd.UTC().Add(time.Microsecond)
		}
	}
	return usageFilter
}

func insertDataCleanupAudit(ctx context.Context, executor interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator, status string) (int64, error) {
	filtersJSON, _ := json.Marshal(map[string]any{
		"category":        filter.Category,
		"mode":            filter.Mode,
		"start_time":      filter.StartTime,
		"end_time":        filter.EndTime,
		"snapshot_max_id": preview.snapshotMaxID,
	})
	var id int64
	err := executor.QueryRowContext(ctx, `
INSERT INTO data_cleanup_audits (
    operator_id, operator_email, auth_method, category, cleanup_mode,
    filters, preview_rows, blocked_rows, status
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
RETURNING id`, operator.UserID, truncateString(operator.Email, 255), truncateString(operator.AuthMethod, 32),
		filter.Category, filter.Mode, string(filtersJSON), preview.MatchedRows, preview.BlockedRows, status).Scan(&id)
	return id, err
}

func (s *DataCleanupService) insertFailureAudit(ctx context.Context, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator, cleanupErr error) error {
	auditID, err := insertDataCleanupAudit(ctx, s.db, filter, preview, operator, "failed")
	if err != nil {
		return err
	}
	return s.finishAudit(ctx, auditID, "failed", 0, cleanupErr)
}

func (s *DataCleanupService) insertFailureAuditDetached(filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator, cleanupErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	defer cancel()
	_ = s.insertFailureAudit(ctx, filter, preview, operator, cleanupErr)
}

func (s *DataCleanupService) finishAuditFailureDetached(auditID int64, filter DataCleanupFilter, preview *DataCleanupPreview, operator DataCleanupOperator, cleanupErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	err := s.finishAudit(ctx, auditID, "failed", 0, cleanupErr)
	cancel()
	if err != nil {
		s.insertFailureAuditDetached(filter, preview, operator, fmt.Errorf("cleanup failed and audit finalization failed: %v; audit error: %w", cleanupErr, err))
	}
}

func (s *DataCleanupService) finishAudit(ctx context.Context, auditID int64, status string, deleted int64, cleanupErr error) error {
	if s == nil || s.db == nil || auditID <= 0 {
		return nil
	}
	errText := ""
	if cleanupErr != nil {
		errText = truncateString(cleanupErr.Error(), 500)
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE data_cleanup_audits
SET status = $2, deleted_rows = $3, error_message = $4, finished_at = NOW()
WHERE id = $1`, auditID, status, deleted, errText)
	return err
}

func (s *DataCleanupService) completeUsageCleanupAudit(ctx context.Context, task *UsageCleanupTask, status string, completionErr error) {
	if task == nil || task.Filters.DataCleanupAuditID <= 0 {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	defer cancel()
	_ = s.finishAudit(auditCtx, task.Filters.DataCleanupAuditID, status, task.DeletedRows, completionErr)
}
