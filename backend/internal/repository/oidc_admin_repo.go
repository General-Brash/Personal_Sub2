package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func scanOIDCConsent(scan func(...any) error) (*service.OIDCConsentRecord, error) {
	var out service.OIDCConsentRecord
	var approvedAt, revokedAt sql.NullTime
	var revokeReason sql.NullString
	if err := scan(&out.ID, &out.UserID, &out.ClientPK, &out.ClientID, &out.ClientName, &out.ScopeSnapshot, &out.ScopeSetHash, &out.PolicyVersion, &out.Source, &out.Status, &approvedAt, &revokedAt, &revokeReason); err != nil {
		return nil, err
	}
	out.Scopes = strings.Fields(out.ScopeSnapshot)
	if approvedAt.Valid {
		out.ApprovedAt = &approvedAt.Time
	}
	if revokedAt.Valid {
		out.RevokedAt = &revokedAt.Time
	}
	if revokeReason.Valid {
		out.RevokedReason = revokeReason.String
	}
	return &out, nil
}

const oidcConsentSelect = `
	SELECT c.id,c.user_id,c.client_pk,cl.client_id,cl.name,c.scope_snapshot,c.scope_set_hash,c.policy_version,c.source,c.status,c.approved_at,c.revoked_at,c.revoke_reason
	FROM oidc_consents c JOIN oidc_clients cl ON cl.id=c.client_pk`

func (r *oidcRepository) ListConsents(ctx context.Context, input service.OIDCConsentListInput) ([]service.OIDCConsentRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	page := input.Page
	if page <= 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	clauses := []string{"1=1"}
	args := make([]any, 0, 6)
	if v := strings.TrimSpace(input.ClientID); v != "" {
		args = append(args, v)
		clauses = append(clauses, "cl.client_id=$"+strconv.Itoa(len(args)))
	}
	if v := strings.TrimSpace(input.Status); v != "" {
		args = append(args, v)
		clauses = append(clauses, "c.status=$"+strconv.Itoa(len(args)))
	}
	if v := strings.TrimSpace(input.Query); v != "" {
		args = append(args, "%"+v+"%")
		idx := strconv.Itoa(len(args))
		clauses = append(clauses, "(cl.client_id ILIKE $"+idx+" OR cl.name ILIKE $"+idx+" OR CAST(c.user_id AS TEXT) ILIKE $"+idx+")")
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := oidcConsentSelect + " WHERE " + strings.Join(clauses, " AND ") + " ORDER BY c.id DESC LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]service.OIDCConsentRecord, 0, pageSize)
	for rows.Next() {
		item, err := scanOIDCConsent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *oidcRepository) GetConsentByID(ctx context.Context, consentID int64) (*service.OIDCConsentRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	return scanOIDCConsent(r.db.QueryRowContext(ctx, oidcConsentSelect+` WHERE c.id=$1`, consentID).Scan)
}

func (r *oidcRepository) RevokeConsent(ctx context.Context, consentID, actorID int64, reason string) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM oidc_consents WHERE id=$1 FOR UPDATE`, consentID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	if status != "active" {
		if status == "revoked" {
			return tx.Commit()
		}
		return sql.ErrNoRows
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE oidc_consents SET status='revoked',revoked_by=$1,revoked_at=$2,revoke_reason=$3 WHERE id=$4 AND status='active'`, actorID, now, reason, consentID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	// Revoke every credential derived from this consent in the same transaction.
	// The consent row is locked first, matching code exchange and refresh
	// rotation, so a concurrent issuer either commits before this cascade or
	// observes the revoked consent and fails closed.
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_authorization_codes SET status='revoked',consumed_at=$1 WHERE consent_id=$2 AND status='active'`, now, consentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_authorization_transactions SET status='cancelled',consumed_at=$1,version=version+1 WHERE consent_id=$2 AND status IN ('created','authenticated','consented')`, now, consentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_access_tokens SET revoked_at=$1,revoked_by=$2,revoke_reason=$3 WHERE consent_id=$4 AND revoked_at IS NULL`, now, actorID, reason, consentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_token_families SET status='revoked',revoked_at=$1,revoked_by=$2,revoke_reason=$3,version=version+1 WHERE consent_id=$4 AND status<>'revoked'`, now, actorID, reason, consentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET status='revoked',revoked_at=$1 WHERE family_id IN (SELECT family_id FROM oidc_refresh_token_families WHERE consent_id=$2) AND status<>'revoked'`, now, consentID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *oidcRepository) ListAuditEvents(ctx context.Context, input service.OIDCAuditListInput) ([]service.OIDCAuditEventRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	page := input.Page
	if page <= 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	args := []any{}
	where := `(action LIKE 'oidc.%' OR path LIKE '/api/v1/admin/oidc-provider/%')`
	if query := strings.TrimSpace(input.Query); query != "" {
		args = append(args, "%"+query+"%")
		idx := strconv.Itoa(len(args))
		where += " AND (action ILIKE $" + idx + " OR path ILIKE $" + idx + ")"
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := `SELECT id,created_at,action,status_code,actor_user_id,request_id,extra FROM audit_logs WHERE ` + where + ` ORDER BY created_at DESC,id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]service.OIDCAuditEventRecord, 0, pageSize)
	for rows.Next() {
		var item service.OIDCAuditEventRecord
		var actorID sql.NullInt64
		var requestID sql.NullString
		var extraRaw []byte
		var statusCode int
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Action, &statusCode, &actorID, &requestID, &extraRaw); err != nil {
			return nil, err
		}
		if actorID.Valid {
			item.ActorUserID = &actorID.Int64
		}
		if requestID.Valid && requestID.String != "" {
			item.RequestID = stringPtr(requestID.String)
		}
		item.Result = "success"
		if statusCode >= 400 {
			item.Result = "failure"
		}
		var extra map[string]any
		if len(extraRaw) > 0 && json.Unmarshal(extraRaw, &extra) == nil {
			applyOIDCAuditExtra(&item, extra)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func applyOIDCAuditExtra(item *service.OIDCAuditEventRecord, extra map[string]any) {
	if result, ok := extra["result"].(string); ok && result != "" {
		item.Result = result
	}
	if reason, ok := extra["reason"].(string); ok && reason != "" {
		item.Reason = stringPtr(reason)
	}
	if requestID, ok := extra["request_id"].(string); ok && requestID != "" {
		item.RequestID = stringPtr(requestID)
	}
	if value, ok := extra["client_id"].(string); ok && value != "" {
		item.ClientID = stringPtr(value)
	}
	if value, ok := extra["secret_id"].(string); ok && value != "" {
		item.SecretID = stringPtr(value)
	}
	if value, ok := extra["secret_fingerprint"].(string); ok && value != "" {
		item.SecretFingerprint = stringPtr(value)
	}
	if value, ok := extra["kid"].(string); ok && value != "" {
		item.KID = stringPtr(value)
	}
	if value, ok := extra["family_id"].(string); ok && value != "" {
		item.FamilyID = stringPtr(value)
	}
	if value, ok := extra["old_status"].(string); ok && value != "" {
		item.OldStatus = stringPtr(value)
	}
	if value, ok := extra["new_status"].(string); ok && value != "" {
		item.NewStatus = stringPtr(value)
	}
}

func stringPtr(value string) *string { return &value }
