package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const oidcSigningKeyColumns = `id,kid,alg,public_jwk,private_key_ciphertext,fingerprint,status,not_before,not_after,created_at,activated_at,retired_at,revoked_at`

func scanOIDCSigningKey(scan func(...any) error) (*service.OIDCSigningKeyRecord, error) {
	var out service.OIDCSigningKeyRecord
	var activatedAt, retiredAt, revokedAt sql.NullTime
	if err := scan(&out.ID, &out.KID, &out.Alg, &out.PublicJWK, &out.PrivateKeyCiphertext, &out.Fingerprint, &out.Status, &out.NotBefore, &out.NotAfter, &out.CreatedAt, &activatedAt, &retiredAt, &revokedAt); err != nil {
		return nil, err
	}
	if activatedAt.Valid {
		out.ActivatedAt = &activatedAt.Time
	}
	if retiredAt.Valid {
		out.RetiredAt = &retiredAt.Time
	}
	if revokedAt.Valid {
		out.RevokedAt = &revokedAt.Time
	}
	return &out, nil
}

func (r *oidcRepository) ListSigningKeys(ctx context.Context, now time.Time) ([]service.OIDCSigningKeyRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+oidcSigningKeyColumns+` FROM oidc_signing_keys WHERE status IN ('active','retiring') AND not_before <= $1 AND not_after > $1 ORDER BY created_at DESC`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.OIDCSigningKeyRecord, 0)
	for rows.Next() {
		key, err := scanOIDCSigningKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, *key)
	}
	return result, rows.Err()
}

func (r *oidcRepository) ListAllSigningKeys(ctx context.Context) ([]service.OIDCSigningKeyRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+oidcSigningKeyColumns+` FROM oidc_signing_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.OIDCSigningKeyRecord, 0)
	for rows.Next() {
		key, err := scanOIDCSigningKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, *key)
	}
	return result, rows.Err()
}

func (r *oidcRepository) GetActiveSigningKey(ctx context.Context, now time.Time) (*service.OIDCSigningKeyRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	return scanOIDCSigningKey(r.db.QueryRowContext(ctx, `SELECT `+oidcSigningKeyColumns+` FROM oidc_signing_keys WHERE status='active' AND not_before <= $1 AND not_after > $1 ORDER BY created_at DESC LIMIT 1`, now).Scan)
}

func (r *oidcRepository) CreateSigningKey(ctx context.Context, input service.OIDCSigningKeyCreateInput) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO oidc_signing_keys(kid,alg,public_jwk,private_key_ciphertext,fingerprint,status,not_before,not_after,created_by,last_changed_by,last_change_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10)`, input.KID, input.Alg, input.PublicJWK, input.PrivateKeyCiphertext, input.Fingerprint, input.Status, input.NotBefore, input.NotAfter, input.CreatedBy, input.ChangeReason)
	return err
}

func (r *oidcRepository) SetSigningKeyStatus(ctx context.Context, kid, status string, actorID int64, reason string) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM oidc_signing_keys WHERE kid=$1 FOR UPDATE`, kid).Scan(&current); err != nil {
		return err
	}
	nowSQL := `last_changed_by=$2,last_change_reason=$3`
	switch status {
	case "active":
		if current != "pending" && current != "active" {
			return service.ErrOIDCKeyStateConflict
		}
		if current == "active" {
			return tx.Commit()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_signing_keys SET status='retiring',`+nowSQL+` WHERE status='active' AND kid<>$1`, kid, actorID, reason); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE oidc_signing_keys SET status='active',activated_at=COALESCE(activated_at,NOW()),`+nowSQL+` WHERE kid=$1 AND status='pending'`, kid, actorID, reason)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return service.ErrOIDCKeyStateConflict
		}
	case "retiring":
		if current == "retiring" || current == "retired" || current == "revoked" {
			return tx.Commit()
		}
		if current != "active" {
			return service.ErrOIDCKeyStateConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_signing_keys SET status='retiring',retired_at=COALESCE(retired_at,NOW()),`+nowSQL+` WHERE kid=$1 AND status='active'`, kid, actorID, reason); err != nil {
			return err
		}
	case "retired":
		if current == "retired" || current == "revoked" {
			return tx.Commit()
		}
		if current != "retiring" {
			return service.ErrOIDCKeyStateConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_signing_keys SET status='retired',retired_at=COALESCE(retired_at,NOW()),`+nowSQL+` WHERE kid=$1 AND status='retiring'`, kid, actorID, reason); err != nil {
			return err
		}
	case "revoked":
		if current == "revoked" {
			return tx.Commit()
		}
		if current == "active" {
			var active int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oidc_signing_keys WHERE status='active' AND not_before <= NOW() AND not_after > NOW()`).Scan(&active); err != nil {
				return err
			}
			if active <= 1 {
				return service.ErrOIDCKeyStateConflict
			}
		}
		if current != "pending" && current != "retiring" && current != "active" {
			return service.ErrOIDCKeyStateConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oidc_signing_keys SET status='revoked',revoked_at=COALESCE(revoked_at,NOW()),`+nowSQL+` WHERE kid=$1 AND status IN ('pending','retiring','active')`, kid, actorID, reason); err != nil {
			return err
		}
	default:
		return service.ErrOIDCInvalidRequest
	}
	return tx.Commit()
}
