package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *oidcRepository) CreateBrowserSession(ctx context.Context, handleDigest string, userID int64, authTime time.Time, amr string, idleExpiresAt, absoluteExpiresAt time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO oidc_browser_sessions(handle_digest,user_id,auth_time,amr,idle_expires_at,absolute_expires_at,session_version) VALUES($1,$2,$3,$4,$5,$6,1)`, handleDigest, userID, authTime, amr, idleExpiresAt, absoluteExpiresAt)
	return err
}

func (r *oidcRepository) GetBrowserSession(ctx context.Context, handleDigest string, now time.Time) (*service.OIDCBrowserSessionRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	var out service.OIDCBrowserSessionRecord
	if err := r.db.QueryRowContext(ctx, `
		SELECT id,user_id,auth_time,amr,session_version,idle_expires_at,absolute_expires_at,last_seen_at
		FROM oidc_browser_sessions
		WHERE handle_digest=$1 AND revoked_at IS NULL AND session_version>0 AND idle_expires_at>$2 AND absolute_expires_at>$2`, handleDigest, now).Scan(&out.ID, &out.UserID, &out.AuthTime, &out.AMR, &out.SessionVersion, &out.IdleExpiresAt, &out.AbsoluteExpiresAt, &out.LastSeenAt); err != nil {
		return nil, err
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE oidc_browser_sessions SET last_seen_at=$1 WHERE handle_digest=$2 AND revoked_at IS NULL`, now, handleDigest)
	return &out, nil
}

func (r *oidcRepository) RevokeBrowserSession(ctx context.Context, handleDigest, reason string) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `UPDATE oidc_browser_sessions SET revoked_at=NOW(), revoke_reason=$1 WHERE handle_digest=$2 AND revoked_at IS NULL`, reason, handleDigest)
	return err
}

func (r *oidcRepository) CreateAuthorizationTransaction(ctx context.Context, input service.OIDCTransactionCreateInput) (int64, error) {
	if err := r.ensureDB(); err != nil {
		return 0, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO oidc_authorization_transactions(handle_digest,client_pk,consent_id,redirect_uri,scope_snapshot,state_ciphertext,state_fingerprint,nonce_ciphertext,nonce_fingerprint,code_challenge,code_challenge_method,prompt,max_age_seconds,display,status,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'created',$15) RETURNING id`,
		input.HandleDigest, input.ClientPK, input.ConsentID, input.RedirectURI, input.ScopeSnapshot, input.StateCiphertext, input.StateFingerprint, input.NonceCiphertext, input.NonceFingerprint, input.CodeChallenge, input.CodeChallengeMethod, input.Prompt, input.MaxAgeSeconds, input.Display, input.ExpiresAt).Scan(&id)
	return id, err
}

func (r *oidcRepository) GetAuthorizationTransaction(ctx context.Context, handleDigest string, now time.Time) (*service.OIDCTransactionRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	var out service.OIDCTransactionRecord
	var browserID, userID, maxAge, consentID sql.NullInt64
	var display sql.NullString
	var authTime sql.NullTime
	if err := r.db.QueryRowContext(ctx, `
		SELECT id,handle_digest,client_pk,browser_session_id,user_id,redirect_uri,scope_snapshot,state_ciphertext,state_fingerprint,nonce_ciphertext,nonce_fingerprint,code_challenge,code_challenge_method,prompt,max_age_seconds,display,auth_time,consent_id,status,expires_at
		FROM oidc_authorization_transactions WHERE handle_digest=$1 AND expires_at>$2 AND status NOT IN ('denied','expired','cancelled','code_issued')`, handleDigest, now).Scan(
		&out.ID, &out.HandleDigest, &out.ClientPK, &browserID, &userID, &out.RedirectURI, &out.ScopeSnapshot, &out.StateCiphertext, &out.StateFingerprint, &out.NonceCiphertext, &out.NonceFingerprint, &out.CodeChallenge, &out.CodeChallengeMethod, &out.Prompt, &maxAge, &display, &authTime, &consentID, &out.Status, &out.ExpiresAt); err != nil {
		return nil, err
	}
	if browserID.Valid {
		out.BrowserSessionID = &browserID.Int64
	}
	if userID.Valid {
		out.UserID = &userID.Int64
	}
	if maxAge.Valid {
		out.MaxAgeSeconds = &maxAge.Int64
	}
	if display.Valid {
		out.Display = &display.String
	}
	if authTime.Valid {
		out.AuthTime = &authTime.Time
	}
	if consentID.Valid {
		out.ConsentID = &consentID.Int64
	}
	return &out, nil
}

func (r *oidcRepository) SetTransactionAuthenticated(ctx context.Context, transactionID, sessionID, userID int64, authTime time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE oidc_authorization_transactions
		SET browser_session_id=$1,user_id=$2,auth_time=COALESCE(auth_time,$3),status='authenticated',version=CASE WHEN status='created' THEN version+1 ELSE version END
		WHERE id=$4 AND expires_at>NOW()
		  AND (status='created' OR (status='authenticated' AND browser_session_id=$1 AND user_id=$2))`, sessionID, userID, authTime, transactionID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return service.ErrOIDCInvalidRequest
	}
	return nil
}

func (r *oidcRepository) SetTransactionConsent(ctx context.Context, transactionID, consentID int64) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE oidc_authorization_transactions t
		SET consent_id=$1,status='consented',version=version+1
		WHERE t.id=$2 AND t.user_id IS NOT NULL AND t.status IN ('authenticated','consented') AND t.expires_at>NOW()
		  AND EXISTS (
			SELECT 1 FROM oidc_consents c
			JOIN oidc_clients cl ON cl.id=t.client_pk
			WHERE c.id=$1 AND c.user_id=t.user_id AND c.client_pk=t.client_pk
			  AND c.scope_snapshot=t.scope_snapshot AND c.status='active'
			  AND c.policy_version=cl.policy_version AND cl.enabled AND cl.client_type='confidential'
		  )`, consentID, transactionID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return service.ErrOIDCInvalidRequest
	}
	return nil
}

func (r *oidcRepository) DenyTransaction(ctx context.Context, transactionID int64) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `UPDATE oidc_authorization_transactions SET status='denied',consumed_at=NOW(),version=version+1 WHERE id=$1 AND status IN ('created','authenticated','consented')`, transactionID)
	return err
}
func (r *oidcRepository) CreateAuthorizationCode(ctx context.Context, input service.OIDCAuthorizationCodeCreateInput) (string, error) {
	if err := r.ensureDB(); err != nil {
		return "", err
	}
	if input.ConsentID <= 0 {
		return "", service.ErrOIDCConsentRequired
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var transactionConsentID int64
	if err := tx.QueryRowContext(ctx, `SELECT consent_id FROM oidc_authorization_transactions WHERE id=$1 AND consent_id=$2`, input.TransactionID, input.ConsentID).Scan(&transactionConsentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", service.ErrOIDCInvalidRequest
		}
		return "", err
	}
	var consentStatus, clientType string
	var consentUserID, consentClientPK, consentPolicyVersion, clientPolicyVersion int64
	var clientEnabled bool
	if err := tx.QueryRowContext(ctx, `
		SELECT c.status,c.user_id,c.client_pk,c.policy_version,cl.enabled,cl.policy_version,cl.client_type
		FROM oidc_consents c JOIN oidc_clients cl ON cl.id=c.client_pk
		WHERE c.id=$1 FOR UPDATE`, transactionConsentID).Scan(&consentStatus, &consentUserID, &consentClientPK, &consentPolicyVersion, &clientEnabled, &clientPolicyVersion, &clientType); err != nil {
		return "", service.ErrOIDCInvalidRequest
	}
	if consentStatus != "active" || !clientEnabled || clientType != service.OIDCClientTypeConfidential || consentClientPK != input.ClientPK || consentUserID != input.UserID || consentPolicyVersion != clientPolicyVersion {
		return "", service.ErrOIDCInvalidRequest
	}
	var consentID int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE oidc_authorization_transactions t
		SET status='code_issued',consumed_at=NOW(),version=version+1
		WHERE t.id=$1 AND t.status IN ('authenticated','consented') AND t.expires_at>$2 AND t.consent_id=$3
		RETURNING t.consent_id`, input.TransactionID, input.IssuedAt, input.ConsentID).Scan(&consentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", service.ErrOIDCInvalidRequest
		}
		return "", err
	}
	if consentID <= 0 {
		return "", service.ErrOIDCConsentRequired
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO oidc_authorization_codes(code_digest,transaction_id,client_pk,user_id,consent_id,browser_session_id,redirect_uri,scope_snapshot,nonce_ciphertext,nonce_fingerprint,code_challenge,code_challenge_method,auth_time,issued_at,expires_at,status)
		SELECT $1,t.id,t.client_pk,t.user_id,t.consent_id,t.browser_session_id,t.redirect_uri,t.scope_snapshot,t.nonce_ciphertext,t.nonce_fingerprint,t.code_challenge,t.code_challenge_method,t.auth_time,$2,$3,'active'
		FROM oidc_authorization_transactions t
		WHERE t.id=$4 AND t.status='code_issued' AND t.consent_id=$5 AND t.auth_time IS NOT NULL`, input.CodeDigest, input.IssuedAt, input.ExpiresAt, input.TransactionID, consentID)
	if err != nil {
		return "", err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return "", service.ErrOIDCInvalidRequest
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return input.CodeDigest, nil
}

func (r *oidcRepository) GetAuthorizationCode(ctx context.Context, codeDigest string, now time.Time) (*service.OIDCAuthorizationCodeRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	var out service.OIDCAuthorizationCodeRecord
	var browserID sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `
		SELECT c.id,c.transaction_id,t.handle_digest,c.code_digest,c.client_pk,c.user_id,c.consent_id,c.browser_session_id,c.redirect_uri,c.scope_snapshot,c.nonce_ciphertext,c.nonce_fingerprint,c.code_challenge,c.code_challenge_method,c.auth_time,c.status,c.issued_at,c.expires_at
		FROM oidc_authorization_codes c JOIN oidc_authorization_transactions t ON t.id=c.transaction_id WHERE c.code_digest=$1 AND c.expires_at>$2`, codeDigest, now).Scan(&out.ID, &out.TransactionID, &out.TransactionHandleDigest, &out.CodeDigest, &out.ClientPK, &out.UserID, &out.ConsentID, &browserID, &out.RedirectURI, &out.ScopeSnapshot, &out.NonceCiphertext, &out.NonceFingerprint, &out.CodeChallenge, &out.CodeChallengeMethod, &out.AuthTime, &out.Status, &out.IssuedAt, &out.ExpiresAt); err != nil {
		return nil, err
	}
	if browserID.Valid {
		out.BrowserSessionID = &browserID.Int64
	}
	return &out, nil
}

func (r *oidcRepository) GetConsent(ctx context.Context, userID, clientPK int64, scopeSetHash string, policyVersion int64) (*service.OIDCConsentRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	var out service.OIDCConsentRecord
	if err := r.db.QueryRowContext(ctx, `SELECT id,user_id,client_pk,scope_snapshot,scope_set_hash,policy_version,source,status FROM oidc_consents WHERE user_id=$1 AND client_pk=$2 AND scope_set_hash=$3 AND policy_version=$4 AND status='active'`, userID, clientPK, scopeSetHash, policyVersion).Scan(&out.ID, &out.UserID, &out.ClientPK, &out.ScopeSnapshot, &out.ScopeSetHash, &out.PolicyVersion, &out.Source, &out.Status); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *oidcRepository) CreateConsent(ctx context.Context, input service.OIDCConsentCreateInput) (int64, error) {
	if err := r.ensureDB(); err != nil {
		return 0, err
	}
	var id int64
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM oidc_consents WHERE user_id=$1 AND client_pk=$2 AND scope_set_hash=$3 AND policy_version=$4 AND status='active'`, input.UserID, input.ClientPK, input.ScopeSetHash, input.PolicyVersion).Scan(&id); err == nil {
		return id, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err := r.db.QueryRowContext(ctx, `INSERT INTO oidc_consents(user_id,client_pk,scope_snapshot,scope_set_hash,policy_version,source,status,approved_by,approved_at) VALUES($1,$2,$3,$4,$5,$6,'active',$7,NOW()) RETURNING id`, input.UserID, input.ClientPK, input.ScopeSnapshot, input.ScopeSetHash, input.PolicyVersion, input.Source, input.ActorID).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func ensureOIDCSubjectTx(ctx context.Context, tx *sql.Tx, userID int64) (string, error) {
	var subject string
	if err := tx.QueryRowContext(ctx, `SELECT subject FROM oidc_subjects WHERE user_id=$1`, userID).Scan(&subject); err == nil {
		return subject, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	subject = hex.EncodeToString(buf)
	if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_subjects(user_id,subject) VALUES($1,$2) ON CONFLICT(user_id) DO NOTHING`, userID, subject); err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT subject FROM oidc_subjects WHERE user_id=$1`, userID).Scan(&subject); err != nil {
		return "", err
	}
	return subject, nil
}

func scanOIDCUser(row *sql.Row) (*service.OIDCUserRecord, error) {
	var out service.OIDCUserRecord
	var deleted sql.NullTime
	if err := row.Scan(&out.ID, &out.Email, &out.Username, &out.Role, &out.Status, &deleted, &out.Subject); err != nil {
		return nil, err
	}
	if deleted.Valid {
		out.DeletedAt = &deleted.Time
	}
	return &out, nil
}

func (r *oidcRepository) GetUser(ctx context.Context, userID int64) (*service.OIDCUserRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}
	if _, err := ensureOIDCSubjectTx(ctx, tx, userID); err != nil {
		return nil, err
	}
	var out service.OIDCUserRecord
	var deleted sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.username,u.role,u.status,u.deleted_at,s.subject FROM users u JOIN oidc_subjects s ON s.user_id=u.id WHERE u.id=$1`, userID).Scan(&out.ID, &out.Email, &out.Username, &out.Role, &out.Status, &deleted, &out.Subject); err != nil {
		return nil, err
	}
	if deleted.Valid {
		out.DeletedAt = &deleted.Time
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &out, nil
}
