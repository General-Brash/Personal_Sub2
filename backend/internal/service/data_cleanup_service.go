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
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/redis/go-redis/v9"
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
	Category     string                   `json:"category"`
	Mode         string                   `json:"mode"`
	StartTime    *time.Time               `json:"start_time,omitempty"`
	EndTime      *time.Time               `json:"end_time,omitempty"`
	Month        string                   `json:"month,omitempty"`
	StartDate    string                   `json:"start_date,omitempty"`
	EndDate      string                   `json:"end_date,omitempty"`
	PromptFilter *DataCleanupPromptFilter `json:"prompt_filter,omitempty"`
}

type DataCleanupPromptFilter struct {
	Decision   string `json:"decision,omitempty"`
	RiskLevel  string `json:"risk_level,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	GroupID    *int64 `json:"group_id,omitempty"`
	UserID     *int64 `json:"user_id,omitempty"`
	APIKeyID   *int64 `json:"api_key_id,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	PromptHash string `json:"prompt_hash,omitempty"`
	Keyword    string `json:"keyword,omitempty"`
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
	EstimatedBytes  int64  `json:"estimated_bytes"`

	snapshotMaxID     int64
	snapshotStart     *time.Time
	snapshotEnd       *time.Time
	snapshotBatchSize int
	snapshotDigests   []string
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
	Filter            DataCleanupFilter `json:"filter"`
	PreviewRows       int64             `json:"preview_rows"`
	BlockedRows       int64             `json:"blocked_rows"`
	SnapshotMaxID     int64             `json:"snapshot_max_id"`
	SnapshotStart     *time.Time        `json:"snapshot_start,omitempty"`
	SnapshotEnd       *time.Time        `json:"snapshot_end,omitempty"`
	SnapshotBatchSize int               `json:"snapshot_batch_size"`
	SnapshotDigests   []string          `json:"snapshot_digests"`
	ExpiresAtUnix     int64             `json:"expires_at"`
}

var dataCleanupTargets = map[string]dataCleanupTarget{
	"ops_error_logs":                {table: "ops_error_logs", timeColumn: "created_at"},
	"ops_alert_events":              {table: "ops_alert_events", timeColumn: "created_at"},
	"ops_system_logs":               {table: "ops_system_logs", timeColumn: "created_at"},
	"server_auto_retry":             {table: "ops_system_logs", timeColumn: "created_at", extraWhere: "target.message='server_auto_retry.retry_attempt' AND EXISTS (SELECT 1 FROM ops_system_logs f WHERE f.request_id=target.request_id AND f.message='server_auto_retry.final')"},
	"http_access":                   {table: "ops_system_logs", timeColumn: "created_at", extraWhere: "target.component = 'http.access' AND target.extra->>'status_code' IN ('200','201','204','304') AND CASE WHEN jsonb_typeof(target.extra->'latency_ms')='number' THEN (target.extra->>'latency_ms')::numeric < 60000 ELSE false END AND NOT EXISTS (SELECT 1 FROM ops_error_logs e WHERE e.request_id=target.request_id) AND NOT EXISTS (SELECT 1 FROM ops_system_logs f WHERE f.request_id=target.request_id AND f.message='server_auto_retry.final' AND f.extra->>'outcome'='failed')"},
	"audit_statistics":              {table: "audit_logs", timeColumn: "created_at", sensitive: true, extraWhere: "target.action IN ('admin.accounts.today_stats.batch.create','admin.dashboard.users_usage.create') AND target.status_code BETWEEN 200 AND 299"},
	"ops_system_log_cleanup_audits": {table: "ops_system_log_cleanup_audits", timeColumn: "created_at"},
	"ops_system_metrics":            {table: "ops_system_metrics", timeColumn: "created_at"},
	"ops_metrics_hourly":            {table: "ops_metrics_hourly", timeColumn: "bucket_start"},
	"ops_metrics_daily":             {table: "ops_metrics_daily", timeColumn: "bucket_date", castDate: true},
	"usage_logs":                    {table: "usage_logs", timeColumn: "created_at"},
	"audit_logs":                    {table: "audit_logs", timeColumn: "created_at", sensitive: true},
	"prompt_audit":                  {table: "prompt_audit_events", timeColumn: "created_at", sensitive: true},
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
	db             *sql.DB
	settings       *SettingService
	usageCleanup   *UsageCleanupService
	previewKey     []byte
	workerCancel   context.CancelFunc
	workerWG       sync.WaitGroup
	promptPayloads redis.Cmdable
}

func NewDataCleanupService(db *sql.DB, settings *SettingService, usageCleanup *UsageCleanupService, promptPayloads ...redis.Cmdable) *DataCleanupService {
	key := make([]byte, sha256.Size)
	if settings != nil && settings.cfg != nil && strings.TrimSpace(settings.cfg.JWT.Secret) != "" {
		digest := sha256.Sum256([]byte("data-cleanup-preview:" + strings.TrimSpace(settings.cfg.JWT.Secret)))
		copy(key, digest[:])
	} else if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("generate data cleanup preview key: %v", err))
	}
	svc := &DataCleanupService{db: db, settings: settings, usageCleanup: usageCleanup, previewKey: key}
	if len(promptPayloads) > 0 {
		svc.promptPayloads = promptPayloads[0]
	}
	if usageCleanup != nil {
		usageCleanup.SetDataCleanupCompletionHook(svc.completeUsageCleanupAudit)
		usageCleanup.setRetentionScheduler(svc.queueUsageRetention)
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
	if s == nil || s.db == nil {
		return nil, errors.New("data cleanup service not initialized")
	}
	target, normalized, err := validateDataCleanupFilter(filter)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	matched, blocked, snapshotMaxID, snapshotStart, snapshotEnd, err := s.countRows(ctx, tx, normalized, target, nil)
	if err != nil {
		return nil, err
	}
	batchSize := dataCleanupBatchSize
	orderBy := "target.id"
	if normalized.Category == "usage_logs" {
		orderBy = "target.created_at, target.id"
		if s.usageCleanup != nil {
			batchSize = s.usageCleanup.batchSize()
		}
		if batchSize > 10000 {
			batchSize = 10000
		}
	}
	digests, snapshotRows, err := dataCleanupSnapshotDigests(ctx, tx, normalized, target, snapshotMaxID, batchSize, orderBy)
	if err != nil {
		return nil, err
	}
	if snapshotRows != matched {
		return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup rows changed while previewing; preview again")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	preview := &DataCleanupPreview{
		Category:          normalized.Category,
		Mode:              normalized.Mode,
		MatchedRows:       matched,
		BlockedRows:       blocked,
		Confirmation:      dataCleanupConfirmation(normalized, matched),
		RequiresTOTP:      target.sensitive || normalized.Mode == DataCleanupModeAll || normalized.Category == "usage_logs",
		MaxRangeDays:      dataCleanupMaxRangeDays,
		DeletionWarning:   target.warning,
		snapshotMaxID:     snapshotMaxID,
		snapshotStart:     snapshotStart,
		snapshotEnd:       snapshotEnd,
		snapshotBatchSize: batchSize,
		snapshotDigests:   digests,
	}
	preview.PreviewToken, err = s.signPreviewToken(normalized, preview)
	if err != nil {
		return nil, err
	}
	_ = s.db.QueryRowContext(ctx, `SELECT CASE WHEN COALESCE(reltuples,0)>0
	 THEN (pg_total_relation_size(oid)::numeric * $2 / reltuples)::bigint ELSE 0 END FROM pg_class WHERE oid=to_regclass($1)`, target.table, matched).Scan(&preview.EstimatedBytes)
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
		Category:          normalized.Category,
		Mode:              normalized.Mode,
		MatchedRows:       token.PreviewRows,
		PreviewToken:      strings.TrimSpace(previewToken),
		BlockedRows:       token.BlockedRows,
		snapshotMaxID:     token.SnapshotMaxID,
		snapshotStart:     token.SnapshotStart,
		snapshotEnd:       token.SnapshotEnd,
		snapshotBatchSize: token.SnapshotBatchSize,
		snapshotDigests:   append([]string(nil), token.SnapshotDigests...),
	}
	if strings.TrimSpace(confirmation) != dataCleanupConfirmation(normalized, token.PreviewRows) {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_CONFIRMATION_INVALID", "confirmation text does not match")
	}

	if normalized.Category == "usage_logs" || normalized.Category == "prompt_audit" {
		matched, blocked, _, _, _, err := s.countRows(ctx, nil, normalized, target, &token.SnapshotMaxID)
		if err != nil {
			return nil, err
		}
		orderBy := "target.id"
		if normalized.Category == "usage_logs" {
			orderBy = "target.created_at, target.id"
		}
		digests, snapshotRows, err := dataCleanupSnapshotDigests(ctx, s.db, normalized, target, token.SnapshotMaxID, token.SnapshotBatchSize, orderBy)
		if err != nil {
			return nil, err
		}
		if matched != token.PreviewRows || blocked != token.BlockedRows || !sameDataCleanupSnapshot(digests, snapshotRows, token.SnapshotDigests, token.PreviewRows) {
			return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup preview row identity changed; preview again before executing")
		}
		return s.queueCleanup(ctx, normalized, preview, operator)
	}
	if token.PreviewRows > dataCleanupBatchSize {
		return s.queueCleanup(ctx, normalized, preview, operator)
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
       (filters-'preview_token'-'worker')::text, preview_rows, blocked_rows, deleted_rows, status,
       error_message, started_at, finished_at, created_at
FROM data_cleanup_audits
WHERE status IN ('pending','running') OR id IN (
    SELECT id FROM data_cleanup_audits WHERE status NOT IN ('pending','running') ORDER BY id DESC LIMIT $1
)
ORDER BY (status IN ('pending','running')) DESC, id DESC`, limit)
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
	if filter.Month != "" {
		month, err := time.ParseInLocation("2006-01", filter.Month, timezone.Location())
		if err != nil || filter.Category != "usage_logs" {
			return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_USAGE_MONTH_REQUIRED", "invalid usage month")
		}
		end := month.AddDate(0, 1, 0)
		filter.StartTime, filter.EndTime = &month, &end
	}
	for _, date := range []struct {
		value  string
		target **time.Time
	}{{filter.StartDate, &filter.StartTime}, {filter.EndDate, &filter.EndTime}} {
		if date.value == "" {
			continue
		}
		parsed, err := time.ParseInLocation("2006-01-02", date.value, timezone.Location())
		if err != nil {
			return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_INVALID", "invalid cleanup date")
		}
		*date.target = &parsed
	}
	filter.Month, filter.StartDate, filter.EndDate = "", "", ""
	target, ok := dataCleanupTargets[filter.Category]
	if !ok {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_CATEGORY_INVALID", "unsupported cleanup category")
	}
	if filter.Category != "prompt_audit" && filter.PromptFilter != nil {
		return target, filter, infraerrors.BadRequest("DATA_CLEANUP_FILTER_INVALID", "prompt filters require the prompt_audit category")
	}
	if filter.Category == "prompt_audit" {
		prompt := DataCleanupPromptFilter{}
		if filter.PromptFilter != nil {
			prompt = *filter.PromptFilter
		}
		prompt.Decision = strings.ToLower(strings.TrimSpace(prompt.Decision))
		prompt.RiskLevel = strings.ToLower(strings.TrimSpace(prompt.RiskLevel))
		prompt.Endpoint = strings.TrimSpace(prompt.Endpoint)
		prompt.RequestID = strings.TrimSpace(prompt.RequestID)
		prompt.PromptHash = strings.ToLower(strings.TrimSpace(prompt.PromptHash))
		prompt.Keyword = strings.TrimSpace(prompt.Keyword)
		if keyword := []rune(prompt.Keyword); len(keyword) > 128 {
			prompt.Keyword = string(keyword[:128]) + "\u2026"
		}
		for _, id := range []*int64{prompt.GroupID, prompt.UserID, prompt.APIKeyID} {
			if id != nil && *id <= 0 {
				return target, filter, infraerrors.BadRequest("DATA_CLEANUP_FILTER_INVALID", "prompt filter IDs must be positive")
			}
		}
		filter.PromptFilter = &prompt
	}
	if target.sensitive && (filter.Category == "bank_ledger" || filter.Category == "payment_orders" || filter.Category == "mall_purchases") {
		return target, filter, infraerrors.BadRequest("DATA_CLEANUP_FINANCIAL_PROTECTED", "financial records cannot be deleted by data cleanup")
	}
	if filter.Mode != DataCleanupModeRange && filter.Mode != DataCleanupModeAll {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_MODE_INVALID", "cleanup mode must be range or all")
	}
	if filter.Mode == DataCleanupModeAll {
		filter.StartTime = nil
		filter.EndTime = nil
		if filter.Category == "usage_logs" {
			return target, filter, infraerrors.BadRequest("DATA_CLEANUP_USAGE_MONTH_REQUIRED", "usage compaction requires an explicit closed month")
		}
		return target, filter, nil
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_REQUIRED", "start_time and end_time are required")
	}
	if !filter.EndTime.After(*filter.StartTime) {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_INVALID", "end_time must be after start_time")
	}
	if filter.Category == "usage_logs" {
		start := timezone.StartOfMonth(*filter.StartTime)
		end := start.AddDate(0, 1, 0)
		if !filter.StartTime.Equal(start) || !filter.EndTime.Equal(end) || end.After(UsageCompactionEnd(time.Now())) {
			return target, filter, infraerrors.BadRequest("DATA_CLEANUP_USAGE_MONTH_REQUIRED", "select a complete month before the current month")
		}
	}
	if filter.Category != "usage_logs" && filter.Category != "prompt_audit" && filter.EndTime.Sub(*filter.StartTime) > dataCleanupMaxRangeDays*24*time.Hour {
		return dataCleanupTarget{}, filter, infraerrors.BadRequest("DATA_CLEANUP_RANGE_TOO_LARGE", fmt.Sprintf("date range exceeds %d days", dataCleanupMaxRangeDays))
	}
	startUTC := filter.StartTime.UTC()
	endUTC := filter.EndTime.UTC()
	filter.StartTime = &startUTC
	filter.EndTime = &endUTC
	return target, filter, nil
}

func dataCleanupConfirmation(filter DataCleanupFilter, matched int64) string {
	if filter.Category == "usage_logs" {
		return fmt.Sprintf("COMPACT usage_logs %d", matched)
	}
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
		Filter:            filter,
		PreviewRows:       preview.MatchedRows,
		BlockedRows:       preview.BlockedRows,
		SnapshotMaxID:     preview.snapshotMaxID,
		SnapshotStart:     preview.snapshotStart,
		SnapshotEnd:       preview.snapshotEnd,
		SnapshotBatchSize: preview.snapshotBatchSize,
		SnapshotDigests:   append([]string(nil), preview.snapshotDigests...),
		ExpiresAtUnix:     time.Now().UTC().Add(dataCleanupPreviewTTL).Unix(),
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
	return s.decodePreviewToken(token, true)
}

func (s *DataCleanupService) decodePreviewToken(token string, checkExpiry bool) (*dataCleanupPreviewToken, error) {
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
	if checkExpiry && payload.ExpiresAtUnix <= time.Now().UTC().Unix() {
		return nil, infraerrors.Conflict("DATA_CLEANUP_PREVIEW_EXPIRED", "cleanup preview expired; preview again before executing")
	}
	expectedChunks := 0
	if payload.PreviewRows > 0 && payload.SnapshotBatchSize > 0 {
		expectedChunks = int((payload.PreviewRows + int64(payload.SnapshotBatchSize) - 1) / int64(payload.SnapshotBatchSize))
	}
	if payload.SnapshotBatchSize <= 0 || len(payload.SnapshotDigests) != expectedChunks {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "cleanup preview token is missing row identity data")
	}
	if payload.PreviewRows < 0 || payload.BlockedRows < 0 || payload.SnapshotMaxID < 0 || payload.SnapshotBatchSize > 10000 {
		return nil, infraerrors.BadRequest("DATA_CLEANUP_PREVIEW_TOKEN_INVALID", "invalid cleanup snapshot")
	}
	return &payload, nil
}

func sameDataCleanupFilter(left, right DataCleanupFilter) bool {
	if left.Category != right.Category || left.Mode != right.Mode {
		return false
	}
	leftPrompt, _ := json.Marshal(left.PromptFilter)
	rightPrompt, _ := json.Marshal(right.PromptFilter)
	return sameOptionalTime(left.StartTime, right.StartTime) && sameOptionalTime(left.EndTime, right.EndTime) && hmac.Equal(leftPrompt, rightPrompt)
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
			endOperator := " < "
			if filter.Category == "prompt_audit" {
				endOperator = " <= "
			}
			conditions = append(conditions, "target."+target.timeColumn+" >= $1", "target."+target.timeColumn+endOperator+"$2")
			args = append(args, filter.StartTime.UTC(), filter.EndTime.UTC())
		}
	}
	if filter.Category == "prompt_audit" && filter.PromptFilter != nil {
		prompt := filter.PromptFilter
		for _, field := range []struct{ column, value string }{
			{"decision", prompt.Decision}, {"risk_level", prompt.RiskLevel}, {"endpoint", prompt.Endpoint},
			{"request_id", prompt.RequestID}, {"prompt_hash", prompt.PromptHash},
		} {
			if field.value != "" {
				args = append(args, field.value)
				conditions = append(conditions, fmt.Sprintf("target.%s = $%d", field.column, len(args)))
			}
		}
		for _, field := range []struct {
			column string
			value  *int64
		}{
			{"group_id", prompt.GroupID}, {"user_id", prompt.UserID}, {"api_key_id", prompt.APIKeyID},
		} {
			if field.value != nil {
				args = append(args, *field.value)
				conditions = append(conditions, fmt.Sprintf("target.%s = $%d", field.column, len(args)))
			}
		}
		if prompt.Keyword != "" {
			args = append(args, "%"+prompt.Keyword+"%")
			conditions = append(conditions, fmt.Sprintf(`(target.request_id ILIKE $%[1]d OR target.prompt_hash ILIKE $%[1]d OR target.redacted_preview ILIKE $%[1]d
			 OR target.username_snapshot ILIKE $%[1]d OR target.user_email_snapshot ILIKE $%[1]d OR target.api_key_name_snapshot ILIKE $%[1]d)`, len(args)))
		}
	}
	if snapshotMaxID != nil {
		conditions = append(conditions, fmt.Sprintf("target.id <= $%d", len(args)+1))
		args = append(args, *snapshotMaxID)
	}
	if target.extraWhere != "" {
		conditions = append(conditions, target.extraWhere)
	}
	if filter.Category == "usage_logs" {
		conditions = append(conditions, "target.compacted_at IS NULL", fmt.Sprintf("target.created_at < $%d", len(args)+1))
		args = append(args, UsageCompactionEnd(time.Now()))
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
	if filter.Category == "usage_logs" {
		protectedQuery := fmt.Sprintf(`SELECT COUNT(*) FROM usage_logs target WHERE %s AND
			(EXISTS(SELECT 1 FROM temporary_credit_consumptions t WHERE t.usage_log_id=target.id)
			 OR EXISTS(SELECT 1 FROM billing_usage_entries b WHERE b.usage_log_id=target.id))`, where)
		if err := queryer.QueryRowContext(ctx, protectedQuery, args...).Scan(&blocked); err != nil {
			return 0, 0, 0, nil, nil, err
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

type dataCleanupRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func dataCleanupSnapshotDigests(
	ctx context.Context,
	queryer dataCleanupRowsQueryer,
	filter DataCleanupFilter,
	target dataCleanupTarget,
	snapshotMaxID int64,
	batchSize int,
	orderBy string,
) ([]string, int64, error) {
	if batchSize <= 0 {
		batchSize = dataCleanupBatchSize
	}
	where, args := dataCleanupWhere(filter, target, &snapshotMaxID)
	query := fmt.Sprintf("SELECT target.id FROM %s AS target WHERE %s ORDER BY %s", target.table, where, orderBy)
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	digests := make([]string, 0)
	ids := make([]int64, 0, batchSize)
	var total int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
		total++
		if len(ids) == batchSize {
			digests = append(digests, UsageCleanupRowIdentityDigest(ids))
			ids = ids[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(ids) > 0 {
		digests = append(digests, UsageCleanupRowIdentityDigest(ids))
	}
	return digests, total, nil
}

func sameDataCleanupSnapshot(digests []string, rows int64, expectedDigests []string, expectedRows int64) bool {
	if rows != expectedRows || len(digests) != len(expectedDigests) {
		return false
	}
	for i := range digests {
		if !hmac.Equal([]byte(digests[i]), []byte(expectedDigests[i])) {
			return false
		}
	}
	return true
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
	digests, snapshotRows, err := dataCleanupSnapshotDigests(ctx, tx, filter, target, preview.snapshotMaxID, preview.snapshotBatchSize, "target.id")
	if err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if !sameDataCleanupSnapshot(digests, snapshotRows, preview.snapshotDigests, preview.MatchedRows) {
		err = infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup preview row identity changed; preview again before executing")
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	deleted, err := deleteDataCleanupRows(ctx, tx, filter, target, preview.snapshotMaxID, preview.MatchedRows)
	if err != nil {
		_ = tx.Rollback()
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if deleted != preview.MatchedRows {
		err = infraerrors.Conflict("DATA_CLEANUP_PREVIEW_STALE", "cleanup row identity changed during execution; transaction rolled back")
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE data_cleanup_audits
		SET status='succeeded',deleted_rows=$2,finished_at=NOW() WHERE id=$1`, auditID, deleted); err != nil {
		_ = tx.Rollback()
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		s.finishAuditFailureDetached(auditID, filter, preview, operator, err)
		return nil, err
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

func buildDataCleanupUsageFilters(filter DataCleanupFilter, preview *DataCleanupPreview, auditID int64) UsageCleanupFilters {
	usageFilter := UsageCleanupFilters{
		All:                          filter.Mode == DataCleanupModeAll,
		DataCleanupAuditID:           auditID,
		DataCleanupSnapshotMaxID:     preview.snapshotMaxID,
		DataCleanupSnapshotRows:      preview.MatchedRows,
		DataCleanupSnapshotBatchSize: preview.snapshotBatchSize,
		DataCleanupSnapshotDigests:   append([]string(nil), preview.snapshotDigests...),
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
	filtersJSON, _ := json.Marshal(struct {
		DataCleanupFilter
		SnapshotMaxID       int64 `json:"snapshot_max_id"`
		SnapshotBatchSize   int   `json:"snapshot_batch_size"`
		SnapshotDigestCount int   `json:"snapshot_digest_count"`
	}{filter, preview.snapshotMaxID, preview.snapshotBatchSize, len(preview.snapshotDigests)})
	var id int64
	err := executor.QueryRowContext(ctx, `
INSERT INTO data_cleanup_audits (
    operator_id, operator_email, auth_method, category, cleanup_mode,
    filters, preview_rows, blocked_rows, status
) VALUES (NULLIF($1::bigint,0), $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
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
WHERE id = $1 AND status NOT IN ('succeeded','canceled')`, auditID, status, deleted, errText)
	return err
}

func (s *DataCleanupService) completeUsageCleanupAudit(ctx context.Context, task *UsageCleanupTask, status string, completionErr error) {
	if task == nil || task.Filters.DataCleanupAuditID <= 0 {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), dataCleanupAuditTimeout)
	defer cancel()
	_, _ = s.db.ExecContext(auditCtx, `UPDATE data_cleanup_audits SET status=$2,error_message=$3,finished_at=NOW()
		WHERE id=$1 AND status<>'canceled'`, task.Filters.DataCleanupAuditID, status, errorString(completionErr))
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return truncateString(err.Error(), 500)
}
