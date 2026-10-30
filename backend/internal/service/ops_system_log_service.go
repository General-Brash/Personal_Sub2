package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *OpsService) ListSystemLogs(ctx context.Context, filter *OpsSystemLogFilter) (*OpsSystemLogList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return &OpsSystemLogList{
			Logs:     []*OpsSystemLog{},
			Total:    0,
			Page:     1,
			PageSize: 50,
		}, nil
	}
	if filter == nil {
		filter = &OpsSystemLogFilter{}
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 50
	}
	if filter.PageSize > 200 {
		filter.PageSize = 200
	}

	result, err := s.opsRepo.ListSystemLogs(ctx, filter)
	if err != nil {
		return nil, infraerrors.InternalServer("OPS_SYSTEM_LOG_LIST_FAILED", "Failed to list system logs").WithCause(err)
	}
	return result, nil
}

// Pointer fields distinguish missing evidence from an observed zero or false.
type OpsServerAutoRetryMetadata struct {
	Reason            string  `json:"reason"`
	RawHTTPStatus     *int    `json:"raw_http_status"`
	SemanticStatus    *int    `json:"semantic_status"`
	AccountID         *int64  `json:"account_id"`
	DispatchCount     *int    `json:"dispatch_count"`
	RetryCount        *int    `json:"retry_count"`
	SwitchCount       *int    `json:"switch_count"`
	WaitMS            *int64  `json:"wait_ms"`
	ResponseCommitted *bool   `json:"response_committed"`
	HasSemanticOutput *bool   `json:"has_semantic_output"`
	UsageEvidence     string  `json:"usage_evidence"`
	StopReason        string  `json:"stop_reason"`
	Outcome           string  `json:"outcome"`
	UpstreamRequestID *string `json:"upstream_request_id"`
}

type OpsServerAutoRetryEvent struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id"`
	EventType string    `json:"event_type"`
	OpsServerAutoRetryMetadata
}

type OpsServerAutoRetryEventList struct {
	Items    []*OpsServerAutoRetryEvent `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

func DecodeOpsServerAutoRetryMetadata(fields map[string]any) OpsServerAutoRetryMetadata {
	metadata := OpsServerAutoRetryMetadata{}
	for name, target := range map[string]any{
		"reason": &metadata.Reason, "raw_http_status": &metadata.RawHTTPStatus,
		"semantic_status": &metadata.SemanticStatus, "account_id": &metadata.AccountID,
		"dispatch_count": &metadata.DispatchCount, "retry_count": &metadata.RetryCount,
		"switch_count": &metadata.SwitchCount, "wait_ms": &metadata.WaitMS,
		"response_committed": &metadata.ResponseCommitted, "has_semantic_output": &metadata.HasSemanticOutput,
		"usage_evidence": &metadata.UsageEvidence, "stop_reason": &metadata.StopReason,
		"outcome": &metadata.Outcome, "upstream_request_id": &metadata.UpstreamRequestID,
	} {
		if value, ok := fields[name]; ok {
			if raw, err := json.Marshal(value); err == nil {
				if err := json.Unmarshal(raw, target); err != nil {
					// Unmarshal can allocate a pointer before rejecting its value.
					// Reset it rather than expose a fabricated zero/false.
					switch pointer := target.(type) {
					case **int:
						*pointer = nil
					case **int64:
						*pointer = nil
					case **bool:
						*pointer = nil
					case **string:
						*pointer = nil
					case *string:
						*pointer = ""
					}
				}
			}
		}
	}
	for _, value := range []*string{&metadata.Reason, &metadata.UsageEvidence, &metadata.StopReason, &metadata.Outcome} {
		*value = truncateString(strings.TrimSpace(*value), 128)
		if *value == "" {
			*value = "unknown"
		}
	}
	for _, status := range []**int{&metadata.RawHTTPStatus, &metadata.SemanticStatus} {
		if *status != nil && (**status < 100 || **status > 599) {
			*status = nil
		}
	}
	for _, count := range []**int{&metadata.DispatchCount, &metadata.RetryCount, &metadata.SwitchCount} {
		if *count != nil && **count < 0 {
			*count = nil
		}
	}
	if metadata.WaitMS != nil && *metadata.WaitMS < 0 {
		metadata.WaitMS = nil
	}
	if metadata.AccountID != nil && *metadata.AccountID <= 0 {
		metadata.AccountID = nil
	}
	if metadata.UpstreamRequestID != nil {
		value := truncateString(strings.TrimSpace(*metadata.UpstreamRequestID), 128)
		metadata.UpstreamRequestID = &value
		if value == "" {
			metadata.UpstreamRequestID = nil
		}
	}
	return metadata
}

// Historical events remain readable even when monitoring or retries are disabled.
func (s *OpsService) ListServerAutoRetryEvents(ctx context.Context, requestID string, page, pageSize int) (*OpsServerAutoRetryEventList, error) {
	if s == nil || s.opsRepo == nil {
		return nil, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if page > int(^uint(0)>>1)/pageSize {
		return nil, infraerrors.BadRequest("INVALID_SERVER_AUTO_RETRY_PAGE", "page is too large")
	}
	logs, err := s.opsRepo.ListSystemLogs(ctx, &OpsSystemLogFilter{
		RequestID: strings.TrimSpace(requestID), Page: page, PageSize: pageSize,
		Messages: []string{"server_auto_retry.retry_attempt", "server_auto_retry.final"},
	})
	if err != nil {
		return nil, infraerrors.InternalServer("SERVER_AUTO_RETRY_EVENTS_FAILED", "Failed to list retry events").WithCause(err)
	}
	out := &OpsServerAutoRetryEventList{Items: []*OpsServerAutoRetryEvent{}, Page: page, PageSize: pageSize}
	if logs == nil {
		return out, nil
	}
	out.Total, out.Page, out.PageSize = logs.Total, logs.Page, logs.PageSize
	for _, logEntry := range logs.Logs {
		if logEntry == nil {
			continue
		}
		metadata := DecodeOpsServerAutoRetryMetadata(logEntry.Extra)
		if metadata.AccountID == nil && logEntry.AccountID != nil {
			value := *logEntry.AccountID
			metadata.AccountID = &value
		}
		out.Items = append(out.Items, &OpsServerAutoRetryEvent{
			ID: logEntry.ID, Timestamp: logEntry.CreatedAt, RequestID: logEntry.RequestID,
			EventType: logEntry.Message, OpsServerAutoRetryMetadata: metadata,
		})
	}
	return out, nil
}

func (s *OpsService) CleanupSystemLogs(ctx context.Context, filter *OpsSystemLogCleanupFilter, operatorID int64) (int64, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return 0, err
	}
	if s.opsRepo == nil {
		return 0, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if operatorID <= 0 {
		return 0, infraerrors.BadRequest("OPS_SYSTEM_LOG_CLEANUP_INVALID_OPERATOR", "invalid operator")
	}
	if filter == nil {
		filter = &OpsSystemLogCleanupFilter{}
	}
	if filter.EndTime != nil && filter.StartTime != nil && filter.StartTime.After(*filter.EndTime) {
		return 0, infraerrors.BadRequest("OPS_SYSTEM_LOG_CLEANUP_INVALID_RANGE", "invalid time range")
	}

	deletedRows, err := s.opsRepo.DeleteSystemLogs(ctx, filter)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "requires at least one filter") {
			return 0, infraerrors.BadRequest("OPS_SYSTEM_LOG_CLEANUP_FILTER_REQUIRED", "cleanup requires at least one filter condition")
		}
		return 0, infraerrors.InternalServer("OPS_SYSTEM_LOG_CLEANUP_FAILED", "Failed to cleanup system logs").WithCause(err)
	}

	if auditErr := s.opsRepo.InsertSystemLogCleanupAudit(ctx, &OpsSystemLogCleanupAudit{
		CreatedAt:   time.Now().UTC(),
		OperatorID:  operatorID,
		Conditions:  marshalSystemLogCleanupConditions(filter),
		DeletedRows: deletedRows,
	}); auditErr != nil {
		// 审计失败不影响主流程，避免运维清理被阻塞。
		log.Printf("[OpsSystemLog] cleanup audit failed: %v", auditErr)
	}
	return deletedRows, nil
}

func marshalSystemLogCleanupConditions(filter *OpsSystemLogCleanupFilter) string {
	if filter == nil {
		return "{}"
	}
	payload := map[string]any{
		"host":              strings.TrimSpace(filter.Host),
		"level":             strings.TrimSpace(filter.Level),
		"component":         strings.TrimSpace(filter.Component),
		"request_id":        strings.TrimSpace(filter.RequestID),
		"client_request_id": strings.TrimSpace(filter.ClientRequestID),
		"platform":          strings.TrimSpace(filter.Platform),
		"model":             strings.TrimSpace(filter.Model),
		"query":             strings.TrimSpace(filter.Query),
	}
	if filter.UserID != nil {
		payload["user_id"] = *filter.UserID
	}
	if filter.APIKeyID != nil {
		payload["api_key_id"] = *filter.APIKeyID
	}
	if filter.AccountID != nil {
		payload["account_id"] = *filter.AccountID
	}
	if filter.StartTime != nil && !filter.StartTime.IsZero() {
		payload["start_time"] = filter.StartTime.UTC().Format(time.RFC3339Nano)
	}
	if filter.EndTime != nil && !filter.EndTime.IsZero() {
		payload["end_time"] = filter.EndTime.UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func (s *OpsService) GetSystemLogSinkHealth() OpsSystemLogSinkHealth {
	if s == nil || s.systemLogSink == nil {
		return OpsSystemLogSinkHealth{}
	}
	return s.systemLogSink.Health()
}
