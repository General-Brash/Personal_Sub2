package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type oidcRepository struct{ db *sql.DB }

var _ service.OIDCProviderRepository = (*oidcRepository)(nil)

func NewOIDCProviderRepository(db *sql.DB) service.OIDCProviderRepository {
	return &oidcRepository{db: db}
}

func (r *oidcRepository) ensureDB() error {
	if r == nil || r.db == nil {
		return errors.New("oidc repository database is unavailable")
	}
	return nil
}

type oidcQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const oidcClientColumns = `id,client_id,name,owner,client_type,enabled,trusted_skip_consent,policy_version,version,created_at,updated_at,disabled_at,disabled_reason`

// oidcClientColumnsQualified is used by queries that join oidc_clients with
// other OIDC tables. Both sides contain id/created_at, so leaving these
// columns unqualified makes PostgreSQL reject AuthenticateClient with an
// ambiguous-column error, which the token handler exposes as invalid_client.
const oidcClientColumnsQualified = `c.id,c.client_id,c.name,c.owner,c.client_type,c.enabled,c.trusted_skip_consent,c.policy_version,c.version,c.created_at,c.updated_at,c.disabled_at,c.disabled_reason`

func scanOIDCClient(row *sql.Row) (*service.OIDCClientRecord, error) {
	var out service.OIDCClientRecord
	var disabledAt sql.NullTime
	var disabledReason sql.NullString
	if err := row.Scan(&out.ID, &out.ClientID, &out.Name, &out.Owner, &out.ClientType, &out.Enabled, &out.TrustedSkipConsent, &out.PolicyVersion, &out.Version, &out.CreatedAt, &out.UpdatedAt, &disabledAt, &disabledReason); err != nil {
		return nil, err
	}
	if disabledAt.Valid {
		out.DisabledAt = &disabledAt.Time
	}
	if disabledReason.Valid {
		out.DisabledReason = disabledReason.String
	}
	return &out, nil
}

func scanOIDCClientFrom(scan func(...any) error) (*service.OIDCClientRecord, error) {
	var out service.OIDCClientRecord
	var disabledAt sql.NullTime
	var disabledReason sql.NullString
	if err := scan(&out.ID, &out.ClientID, &out.Name, &out.Owner, &out.ClientType, &out.Enabled, &out.TrustedSkipConsent, &out.PolicyVersion, &out.Version, &out.CreatedAt, &out.UpdatedAt, &disabledAt, &disabledReason); err != nil {
		return nil, err
	}
	if disabledAt.Valid {
		out.DisabledAt = &disabledAt.Time
	}
	if disabledReason.Valid {
		out.DisabledReason = disabledReason.String
	}
	return &out, nil
}

func (r *oidcRepository) GetClient(ctx context.Context, clientID string) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	client, err := scanOIDCClient(r.db.QueryRowContext(ctx, `SELECT `+oidcClientColumns+` FROM oidc_clients WHERE client_id=$1`, clientID))
	if err != nil {
		return nil, err
	}
	return r.loadOIDCClientDetails(ctx, r.db, client)
}

func (r *oidcRepository) GetClientByID(ctx context.Context, clientPK int64) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	client, err := scanOIDCClient(r.db.QueryRowContext(ctx, `SELECT `+oidcClientColumns+` FROM oidc_clients WHERE id=$1`, clientPK))
	if err != nil {
		return nil, err
	}
	return r.loadOIDCClientDetails(ctx, r.db, client)
}

func (r *oidcRepository) loadOIDCClientDetails(ctx context.Context, q oidcQueryer, client *service.OIDCClientRecord) (*service.OIDCClientRecord, error) {
	client.RedirectURIs = nil
	client.AllowedScopes = nil
	client.Secrets = nil
	redirectRows, err := q.QueryContext(ctx, `SELECT redirect_uri FROM oidc_client_redirect_uris WHERE client_pk=$1 AND enabled=TRUE ORDER BY id`, client.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = redirectRows.Close() }()
	for redirectRows.Next() {
		var uri string
		if err := redirectRows.Scan(&uri); err != nil {
			return nil, err
		}
		client.RedirectURIs = append(client.RedirectURIs, uri)
	}
	if err := redirectRows.Err(); err != nil {
		return nil, err
	}

	scopeRows, err := q.QueryContext(ctx, `SELECT scope FROM oidc_client_scopes WHERE client_pk=$1 AND enabled=TRUE ORDER BY id`, client.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = scopeRows.Close() }()
	for scopeRows.Next() {
		var scope string
		if err := scopeRows.Scan(&scope); err != nil {
			return nil, err
		}
		client.AllowedScopes = append(client.AllowedScopes, scope)
	}
	if err := scopeRows.Err(); err != nil {
		return nil, err
	}

	secretRows, err := q.QueryContext(ctx, `SELECT id,client_pk,fingerprint,status,not_before,expires_at,created_at,revoked_at FROM oidc_client_secrets WHERE client_pk=$1 ORDER BY id DESC`, client.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = secretRows.Close() }()
	for secretRows.Next() {
		var secret service.OIDCClientSecretRecord
		var revokedAt sql.NullTime
		if err := secretRows.Scan(&secret.ID, &secret.ClientPK, &secret.Fingerprint, &secret.Status, &secret.NotBefore, &secret.ExpiresAt, &secret.CreatedAt, &revokedAt); err != nil {
			return nil, err
		}
		if revokedAt.Valid {
			secret.RevokedAt = &revokedAt.Time
		}
		client.Secrets = append(client.Secrets, secret)
	}
	return client, secretRows.Err()
}

func (r *oidcRepository) ListClients(ctx context.Context) ([]service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+oidcClientColumns+` FROM oidc_clients ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.OIDCClientRecord, 0)
	for rows.Next() {
		client, err := scanOIDCClientFrom(rows.Scan)
		if err != nil {
			return nil, err
		}
		loaded, err := r.loadOIDCClientDetails(ctx, r.db, client)
		if err != nil {
			return nil, err
		}
		result = append(result, *loaded)
	}
	return result, rows.Err()
}

func (r *oidcRepository) CreateClient(ctx context.Context, input service.OIDCClientCreateInput) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ClientID) == "" || len(input.RedirectURIs) == 0 || len(input.AllowedScopes) == 0 {
		return nil, fmt.Errorf("oidc client fields are incomplete")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	client, err := scanOIDCClient(tx.QueryRowContext(ctx, `
		INSERT INTO oidc_clients(client_id,name,owner,client_type,enabled,trusted_skip_consent,policy_version,created_by,created_at,updated_by,updated_at,last_change_reason)
		VALUES($1,$2,$3,'confidential',TRUE,$4,1,$5,$6,$5,$6,$7)
		RETURNING `+oidcClientColumns, input.ClientID, input.Name, input.Owner, input.TrustedSkipConsent, input.ActorID, now, input.Reason))
	if err != nil {
		return nil, err
	}
	for _, redirect := range uniqueStrings(input.RedirectURIs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_client_redirect_uris(client_pk,redirect_uri,created_by) VALUES($1,$2,$3)`, client.ID, redirect, input.ActorID); err != nil {
			return nil, err
		}
	}
	for _, scope := range uniqueStrings(input.AllowedScopes) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_client_scopes(client_pk,scope) VALUES($1,$2)`, client.ID, scope); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_client_secrets(client_pk,secret_digest,fingerprint,status,not_before,expires_at,created_by,created_reason) VALUES($1,$2,$3,'active',$4,$5,$6,$7)`, client.ID, input.SecretDigest, input.SecretFingerprint, input.SecretNotBefore, input.SecretExpiresAt, input.ActorID, input.Reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	client.RedirectURIs = append([]string(nil), uniqueStrings(input.RedirectURIs)...)
	client.AllowedScopes = append([]string(nil), uniqueStrings(input.AllowedScopes)...)
	return r.GetClientByID(ctx, client.ID)
}

func (r *oidcRepository) UpdateClient(ctx context.Context, input service.OIDCClientUpdateInput) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM oidc_clients WHERE id=$1 FOR UPDATE`, input.ClientPK).Scan(&currentVersion); err != nil {
		return nil, err
	}
	if currentVersion != input.ExpectedVersion {
		return nil, service.ErrOIDCVersionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_clients SET name=$1,owner=$2,trusted_skip_consent=$3,policy_version=policy_version+1,updated_by=$4,updated_at=NOW(),last_change_reason=$5,version=version+1 WHERE id=$6 AND version=$7`, input.Name, input.Owner, input.TrustedSkipConsent, input.ActorID, input.Reason, input.ClientPK, input.ExpectedVersion); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_client_redirect_uris SET enabled=FALSE,disabled_at=COALESCE(disabled_at,NOW()) WHERE client_pk=$1`, input.ClientPK); err != nil {
		return nil, err
	}
	for _, redirect := range uniqueStrings(input.RedirectURIs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_client_redirect_uris(client_pk,redirect_uri,enabled,created_by) VALUES($1,$2,TRUE,$3) ON CONFLICT(client_pk,redirect_uri) DO UPDATE SET enabled=TRUE,disabled_at=NULL`, input.ClientPK, redirect, input.ActorID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_client_scopes SET enabled=FALSE WHERE client_pk=$1`, input.ClientPK); err != nil {
		return nil, err
	}
	for _, scope := range uniqueStrings(input.AllowedScopes) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oidc_client_scopes(client_pk,scope,enabled) VALUES($1,$2,TRUE) ON CONFLICT(client_pk,scope) DO UPDATE SET enabled=TRUE`, input.ClientPK, scope); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetClientByID(ctx, input.ClientPK)
}

func (r *oidcRepository) CreateClientSecret(ctx context.Context, clientPK, actorID int64, digest, fingerprint string, notBefore, expiresAt time.Time, reason string) (*service.OIDCClientSecretRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var clientID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM oidc_clients WHERE id=$1 FOR UPDATE`, clientPK).Scan(&clientID); err != nil {
		return nil, err
	}
	now := notBefore
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_client_secrets SET status='expired' WHERE client_pk=$1 AND status IN ('active','retiring') AND expires_at <= $2`, clientPK, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oidc_client_secrets SET status='retiring' WHERE client_pk=$1 AND status='active' AND expires_at > $2`, clientPK, now); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oidc_client_secrets WHERE client_pk=$1 AND status IN ('active','retiring') AND expires_at > $2`, clientPK, now).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 2 {
		return nil, fmt.Errorf("oidc client has too many active secrets")
	}
	var out service.OIDCClientSecretRecord
	var revokedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `INSERT INTO oidc_client_secrets(client_pk,secret_digest,fingerprint,status,not_before,expires_at,created_by,created_reason) VALUES($1,$2,$3,'active',$4,$5,$6,$7) RETURNING id,client_pk,fingerprint,status,not_before,expires_at,created_at,revoked_at`, clientPK, digest, fingerprint, notBefore, expiresAt, actorID, reason).Scan(&out.ID, &out.ClientPK, &out.Fingerprint, &out.Status, &out.NotBefore, &out.ExpiresAt, &out.CreatedAt, &revokedAt); err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		out.RevokedAt = &revokedAt.Time
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *oidcRepository) AuthenticateClient(ctx context.Context, clientID, digest string, now time.Time) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	client, err := scanOIDCClient(r.db.QueryRowContext(ctx, `
		SELECT `+oidcClientColumnsQualified+`
		FROM oidc_clients c JOIN oidc_client_secrets s ON s.client_pk=c.id
		WHERE c.client_id=$1 AND c.enabled=TRUE AND c.client_type='confidential'
		  AND s.secret_digest=$2 AND s.status IN ('active','retiring') AND s.not_before <= $3 AND s.expires_at > $3`, clientID, digest, now))
	if err != nil {
		return nil, err
	}
	return r.loadOIDCClientDetails(ctx, r.db, client)
}

func (r *oidcRepository) SetClientEnabled(ctx context.Context, clientPK, actorID int64, enabled bool, reason string) (*service.OIDCClientRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	var clientID int64
	var client service.OIDCClientRecord
	var disabledAt sql.NullTime
	var disabledReason sql.NullString
	err := r.db.QueryRowContext(ctx, `UPDATE oidc_clients SET enabled=$1,updated_by=$2,updated_at=NOW(),disabled_by=CASE WHEN $1=FALSE THEN $2 ELSE NULL END,disabled_at=CASE WHEN $1=FALSE THEN NOW() ELSE NULL END,disabled_reason=CASE WHEN $1=FALSE THEN $3 ELSE NULL END,last_change_reason=$3,version=version+1 WHERE id=$4 RETURNING id,client_id,name,owner,client_type,enabled,trusted_skip_consent,policy_version,version,created_at,updated_at,disabled_at,disabled_reason`, enabled, actorID, reason, clientPK).Scan(&client.ID, &client.ClientID, &client.Name, &client.Owner, &client.ClientType, &client.Enabled, &client.TrustedSkipConsent, &client.PolicyVersion, &client.Version, &client.CreatedAt, &client.UpdatedAt, &disabledAt, &disabledReason)
	if err != nil {
		return nil, err
	}
	_ = clientID
	if disabledAt.Valid {
		client.DisabledAt = &disabledAt.Time
	}
	if disabledReason.Valid {
		client.DisabledReason = disabledReason.String
	}
	return r.loadOIDCClientDetails(ctx, r.db, &client)
}

func (r *oidcRepository) RevokeClientSecret(ctx context.Context, clientPK, secretID, actorID int64, reason string) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM oidc_client_secrets WHERE id=$1 AND client_pk=$2 FOR UPDATE`, secretID, clientPK).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	if status == "revoked" {
		return tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE oidc_client_secrets SET status='revoked',revoked_by=$1,revoked_at=NOW(),revoke_reason=$2 WHERE id=$3 AND client_pk=$4 AND status<>'revoked'`, actorID, reason, secretID, clientPK)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
