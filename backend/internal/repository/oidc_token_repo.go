package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func oidcPKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (r *oidcRepository) ExchangeAuthorizationCode(ctx context.Context, input service.OIDCAuthorizationCodeExchangeInput) (*service.OIDCUserRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if input.ConsentID <= 0 {
		return nil, service.ErrOIDCInvalidGrant
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var storedConsentID int64
	if err := tx.QueryRowContext(ctx, `SELECT c.consent_id FROM oidc_authorization_codes c WHERE c.code_digest=$1`, input.CodeDigest).Scan(&storedConsentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOIDCInvalidGrant
		}
		return nil, err
	}
	if storedConsentID != input.ConsentID {
		return nil, service.ErrOIDCInvalidGrant
	}
	var consentStatus, clientType, consentScope string
	var consentUserID, consentClientPK, consentPolicyVersion, clientPolicyVersion int64
	var clientEnabled bool
	if err := tx.QueryRowContext(ctx, `
		SELECT c.status,c.user_id,c.client_pk,c.scope_snapshot,c.policy_version,cl.enabled,cl.policy_version,cl.client_type
		FROM oidc_consents c JOIN oidc_clients cl ON cl.id=c.client_pk
		WHERE c.id=$1 FOR UPDATE`, input.ConsentID).Scan(&consentStatus, &consentUserID, &consentClientPK, &consentScope, &consentPolicyVersion, &clientEnabled, &clientPolicyVersion, &clientType); err != nil {
		return nil, service.ErrOIDCInvalidGrant
	}
	if consentStatus != "active" || !clientEnabled || clientType != service.OIDCClientTypeConfidential || consentClientPK != input.ClientPK || consentPolicyVersion != clientPolicyVersion {
		return nil, service.ErrOIDCInvalidGrant
	}
	var code service.OIDCAuthorizationCodeRecord
	var browserID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT id,transaction_id,code_digest,client_pk,user_id,consent_id,browser_session_id,redirect_uri,scope_snapshot,nonce_ciphertext,nonce_fingerprint,code_challenge,code_challenge_method,auth_time,status,issued_at,expires_at
		FROM oidc_authorization_codes WHERE code_digest=$1 AND consent_id=$2 FOR UPDATE`, input.CodeDigest, input.ConsentID).Scan(&code.ID, &code.TransactionID, &code.CodeDigest, &code.ClientPK, &code.UserID, &code.ConsentID, &browserID, &code.RedirectURI, &code.ScopeSnapshot, &code.NonceCiphertext, &code.NonceFingerprint, &code.CodeChallenge, &code.CodeChallengeMethod, &code.AuthTime, &code.Status, &code.IssuedAt, &code.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOIDCInvalidGrant
		}
		return nil, err
	}
	if browserID.Valid {
		code.BrowserSessionID = &browserID.Int64
	}
	if code.Status != "active" || !code.ExpiresAt.After(input.AccessIssuedAt) || code.ClientPK != input.ClientPK || code.UserID != consentUserID || code.RedirectURI != input.RedirectURI || code.CodeChallengeMethod != service.OIDCCodeChallengeS256 || oidcPKCEChallenge(input.CodeVerifier) != code.CodeChallenge || code.ScopeSnapshot != consentScope {
		return nil, service.ErrOIDCInvalidGrant
	}
	if code.ScopeSnapshot == "" || input.RawAccessToken == "" {
		return nil, service.ErrOIDCInvalidGrant
	}
	var user service.OIDCUserRecord
	var deleted sql.NullTime
	if _, err := tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, code.UserID); err != nil {
		return nil, service.ErrOIDCInvalidGrant
	}
	if _, err := ensureOIDCSubjectTx(ctx, tx, code.UserID); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.username,u.role,u.status,u.deleted_at,s.subject FROM users u JOIN oidc_subjects s ON s.user_id=u.id WHERE u.id=$1`, code.UserID).Scan(&user.ID, &user.Email, &user.Username, &user.Role, &user.Status, &deleted, &user.Subject); err != nil {
		return nil, err
	}
	if deleted.Valid {
		user.DeletedAt = &deleted.Time
	}
	if user.Status != service.StatusActive || user.DeletedAt != nil {
		return nil, service.ErrOIDCInvalidGrant
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_authorization_codes SET status='consumed',consumed_at=$1 WHERE id=$2 AND status='active'`, input.AccessIssuedAt, code.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_access_tokens(token_digest,client_pk,user_id,consent_id,purpose,scope_snapshot,issued_at,expires_at) VALUES($1,$2,$3,$4,'userinfo',$5,$6,$7)`, sha256Digest(input.RawAccessToken), code.ClientPK, code.UserID, code.ConsentID, code.ScopeSnapshot, input.AccessIssuedAt, input.AccessExpiresAt); err != nil {
		return nil, err
	}
	if input.RawRefreshToken != "" {
		if input.FamilyID == "" {
			return nil, service.ErrOIDCInvalidGrant
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_refresh_token_families(family_id,client_pk,user_id,consent_id,scope_snapshot,status,last_used_at,idle_expires_at,absolute_expires_at) VALUES($1,$2,$3,$4,$5,'active',$6,$7,$8)`, input.FamilyID, code.ClientPK, code.UserID, code.ConsentID, code.ScopeSnapshot, input.RefreshIssuedAt, input.RefreshIdleUntil, input.RefreshAbsoluteUntil); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_refresh_tokens(token_digest,family_id,status,issued_at,expires_at) VALUES($1,$2,'active',$3,$4)`, sha256Digest(input.RawRefreshToken), input.FamilyID, input.RefreshIssuedAt, input.RefreshAbsoluteUntil); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

func sha256Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (r *oidcRepository) GetAccessToken(ctx context.Context, tokenDigest string, now time.Time) (*service.OIDCUserRecord, string, int64, error) {
	if err := r.ensureDB(); err != nil {
		return nil, "", 0, err
	}
	var user service.OIDCUserRecord
	var deleted sql.NullTime
	var scope, consentScope string
	var clientPK int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT u.id,u.email,u.username,u.role,u.status,u.deleted_at,s.subject,a.scope_snapshot,a.client_pk,c.scope_snapshot
		FROM oidc_access_tokens a
		JOIN users u ON u.id=a.user_id
		JOIN oidc_subjects s ON s.user_id=u.id
		JOIN oidc_consents c ON c.id=a.consent_id AND c.status='active' AND c.user_id=a.user_id AND c.client_pk=a.client_pk
		JOIN oidc_clients cl ON cl.id=a.client_pk AND cl.enabled AND cl.client_type='confidential' AND cl.policy_version=c.policy_version
		WHERE a.token_digest=$1 AND a.purpose='userinfo' AND a.revoked_at IS NULL AND a.expires_at>$2`, tokenDigest, now).Scan(&user.ID, &user.Email, &user.Username, &user.Role, &user.Status, &deleted, &user.Subject, &scope, &clientPK, &consentScope); err != nil {
		return nil, "", 0, err
	}
	if deleted.Valid {
		user.DeletedAt = &deleted.Time
	}
	if user.Status != service.StatusActive || user.DeletedAt != nil {
		return nil, "", 0, service.ErrOIDCUserInactive
	}
	if !scopeSubset(scope, consentScope) {
		return nil, "", 0, service.ErrOIDCInvalidGrant
	}
	return &user, scope, clientPK, nil
}

func scopeSubset(requested, original string) bool {
	allowed := map[string]struct{}{}
	for _, s := range strings.Fields(original) {
		allowed[s] = struct{}{}
	}
	for _, s := range strings.Fields(requested) {
		if _, ok := allowed[s]; !ok {
			return false
		}
	}
	return true
}

func (r *oidcRepository) RotateRefreshToken(ctx context.Context, input service.OIDCRefreshRotationInput) (*service.OIDCRefreshRotationResult, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var consentID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT f.consent_id
		FROM oidc_refresh_tokens rt JOIN oidc_refresh_token_families f ON f.family_id=rt.family_id
		WHERE rt.token_digest=$1`, input.TokenDigest).Scan(&consentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOIDCInvalidGrant
		}
		return nil, err
	}
	if !consentID.Valid || consentID.Int64 <= 0 {
		return nil, service.ErrOIDCInvalidGrant
	}
	var consentStatus, clientType, consentScope string
	var consentUserID, consentClientPK, consentPolicyVersion, clientPolicyVersion int64
	var clientEnabled bool
	if err := tx.QueryRowContext(ctx, `
		SELECT c.status,c.user_id,c.client_pk,c.scope_snapshot,c.policy_version,cl.enabled,cl.policy_version,cl.client_type
		FROM oidc_consents c JOIN oidc_clients cl ON cl.id=c.client_pk
		WHERE c.id=$1 FOR UPDATE`, consentID.Int64).Scan(&consentStatus, &consentUserID, &consentClientPK, &consentScope, &consentPolicyVersion, &clientEnabled, &clientPolicyVersion, &clientType); err != nil {
		return nil, service.ErrOIDCInvalidGrant
	}
	if consentStatus != "active" || !clientEnabled || clientType != service.OIDCClientTypeConfidential || consentClientPK != input.ClientPK || consentPolicyVersion != clientPolicyVersion {
		return nil, service.ErrOIDCInvalidGrant
	}
	var tokenID, clientPK, userID int64
	var familyID, tokenStatus, familyStatus, scope string
	var idleExpires, absoluteExpires, tokenExpires time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT rt.id,rt.family_id,rt.status,rt.expires_at,f.client_pk,f.user_id,f.status,f.scope_snapshot,f.idle_expires_at,f.absolute_expires_at
		FROM oidc_refresh_tokens rt JOIN oidc_refresh_token_families f ON f.family_id=rt.family_id
		WHERE rt.token_digest=$1 AND f.consent_id=$2 FOR UPDATE`, input.TokenDigest, consentID.Int64).Scan(&tokenID, &familyID, &tokenStatus, &tokenExpires, &clientPK, &userID, &familyStatus, &scope, &idleExpires, &absoluteExpires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrOIDCInvalidGrant
		}
		return nil, err
	}
	if clientPK != input.ClientPK || clientPK != consentClientPK || userID != consentUserID || !scopeSubset(scope, consentScope) {
		return nil, service.ErrOIDCInvalidGrant
	}
	if tokenStatus != "active" || familyStatus != "active" || !tokenExpires.After(input.Now) || !idleExpires.After(input.Now) || !absoluteExpires.After(input.Now) {
		if tokenStatus == "rotated" || tokenStatus == "replayed" || familyStatus == "compromised" {
			_, _ = tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET status='replayed',replayed_at=$1 WHERE token_digest=$2 AND status<>'replayed'`, input.Now, input.TokenDigest)
			_, _ = tx.ExecContext(ctx, `UPDATE oidc_refresh_token_families SET status='compromised',replay_detected_at=$1,replay_fingerprint=$2,revoked_at=$1,revoke_reason='refresh_replay',version=version+1 WHERE family_id=$3 AND consent_id=$4 AND status='active'`, input.Now, input.TokenDigest, familyID, consentID.Int64)
			_, _ = tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET status='revoked',revoked_at=$1 WHERE family_id=$2 AND status='active'`, input.Now, familyID)
			_, _ = tx.ExecContext(ctx, `UPDATE oidc_access_tokens SET revoked_at=$1,revoke_reason='refresh_replay' WHERE consent_id=$2 AND revoked_at IS NULL`, input.Now, consentID.Int64)
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &service.OIDCRefreshRotationResult{FamilyID: familyID, Replayed: true}, nil
		}
		return nil, service.ErrOIDCInvalidGrant
	}
	requested := scope
	if input.RequestedScope != "" {
		requested = input.RequestedScope
	}
	if !scopeSubset(requested, scope) {
		return nil, service.ErrOIDCInvalidGrant
	}
	var user service.OIDCUserRecord
	var deleted sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.username,u.role,u.status,u.deleted_at,s.subject FROM users u JOIN oidc_subjects s ON s.user_id=u.id WHERE u.id=$1 FOR UPDATE`, userID).Scan(&user.ID, &user.Email, &user.Username, &user.Role, &user.Status, &deleted, &user.Subject); err != nil {
		return nil, err
	}
	if deleted.Valid {
		user.DeletedAt = &deleted.Time
	}
	if user.Status != service.StatusActive || user.DeletedAt != nil {
		return nil, service.ErrOIDCUserInactive
	}
	newIdle := input.NewRefreshExpiresAt
	if newIdle.After(absoluteExpires) {
		newIdle = absoluteExpires
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET status='rotated',rotated_at=$1,used_at=$1 WHERE id=$2 AND status='active'`, input.Now, tokenID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_access_tokens(token_digest,client_pk,user_id,consent_id,purpose,scope_snapshot,issued_at,expires_at) VALUES($1,$2,$3,$4,'userinfo',$5,$6,$7)`, sha256Digest(input.NewAccessTokenDigest), clientPK, userID, consentID.Int64, requested, input.NewAccessIssuedAt, input.NewAccessExpiresAt); err != nil {
		return nil, err
	}
	var childID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO oidc_refresh_tokens(token_digest,family_id,parent_token_id,status,issued_at,expires_at) VALUES($1,$2,$3,'active',$4,$5) RETURNING id`, sha256Digest(input.NewTokenDigest), familyID, tokenID, input.NewRefreshIssuedAt, newIdle).Scan(&childID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET child_token_id=$1 WHERE id=$2`, childID, tokenID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_token_families SET scope_snapshot=$1,last_used_at=$2,idle_expires_at=$3,version=version+1 WHERE family_id=$4 AND consent_id=$5 AND status='active'`, requested, input.Now, newIdle, familyID, consentID.Int64); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.OIDCRefreshRotationResult{User: &user, Scope: requested, FamilyID: familyID}, nil
}

func (r *oidcRepository) RevokeToken(ctx context.Context, clientPK int64, tokenDigest, reason string) error {
	if strings.TrimSpace(tokenDigest) == "" {
		return service.ErrOIDCInvalidRequest
	}
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_access_tokens SET revoked_at=NOW(),revoked_by=NULL,revoke_reason=$1 WHERE client_pk=$2 AND token_digest=$3 AND revoked_at IS NULL`, reason, clientPK, tokenDigest); err != nil {
		return err
	}
	var familyID string
	err = tx.QueryRowContext(ctx, `SELECT rt.family_id FROM oidc_refresh_tokens rt JOIN oidc_refresh_token_families f ON f.family_id=rt.family_id WHERE rt.token_digest=$1 AND f.client_pk=$2`, tokenDigest, clientPK).Scan(&familyID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_token_families SET status='revoked',revoked_at=NOW(),revoke_reason=$1,version=version+1 WHERE family_id=$2 AND status<>'revoked'`, reason, familyID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_refresh_tokens SET status='revoked',revoked_at=NOW() WHERE family_id=$1 AND status='active'`, familyID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_access_tokens SET revoked_at=NOW(),revoke_reason=$1 WHERE consent_id=(SELECT consent_id FROM oidc_refresh_token_families WHERE family_id=$2) AND revoked_at IS NULL`, reason, familyID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
