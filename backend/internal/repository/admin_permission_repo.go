package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type adminPermissionRepository struct {
	sqlDB *sql.DB
}

var _ service.AdminPermissionRepository = (*adminPermissionRepository)(nil)

func NewAdminPermissionRepository(sqlDB *sql.DB) service.AdminPermissionRepository {
	return &adminPermissionRepository{sqlDB: sqlDB}
}

func (r *adminPermissionRepository) requireDB() (*sql.DB, error) {
	if r == nil || r.sqlDB == nil {
		return nil, errors.New("admin permission repository database is nil")
	}
	return r.sqlDB, nil
}

func (r *adminPermissionRepository) GetAdminPrincipal(ctx context.Context, userID int64) (record *service.AdminPrincipalRecord, err error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	row := db.QueryRowContext(ctx, `
SELECT u.id, u.role, u.status, COALESCE(v.version, 0)
FROM users u
LEFT JOIN admin_permission_versions v ON v.user_id = u.id
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID)
	var result service.AdminPrincipalRecord
	if err := row.Scan(&result.UserID, &result.Role, &result.Status, &result.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAdminPrincipalNotFound
		}
		return nil, fmt.Errorf("load admin principal: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
SELECT permission, effect, scope
FROM admin_principal_grants
WHERE user_id = $1
ORDER BY effect DESC, permission ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("load admin grants: %w", err)
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	for rows.Next() {
		var grant service.AdminGrant
		var raw []byte
		if err := rows.Scan(&grant.Permission, &grant.Effect, &raw); err != nil {
			return nil, fmt.Errorf("scan admin grant: %w", err)
		}
		grant.Scope = decodePermissionScope(raw)
		result.Grants = append(result.Grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin grants: %w", err)
	}
	return &result, nil
}

func (r *adminPermissionRepository) ResolveAdminAPIKeyBinding(ctx context.Context, bindingKey string) (*service.AdminPrincipalRecord, error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	row := db.QueryRowContext(ctx, `
SELECT b.principal_user_id, b.scopes, COALESCE(v.version, 0), u.status
FROM admin_api_key_bindings b
JOIN users u ON u.id = b.principal_user_id AND u.deleted_at IS NULL
LEFT JOIN admin_permission_versions v ON v.user_id = b.principal_user_id
WHERE b.binding_key = $1 AND b.enabled = TRUE`, bindingKey)
	var record service.AdminPrincipalRecord
	var raw []byte
	if err := row.Scan(&record.UserID, &raw, &record.Version, &record.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAdminPermissionEnforceNoBind
		}
		return nil, fmt.Errorf("load admin api-key binding: %w", err)
	}
	record.Role = service.RoleAdmin
	grants, err := decodeAPIKeyScopes(raw)
	if err != nil {
		return nil, err
	}
	record.Grants = grants
	return &record, nil
}

func (r *adminPermissionRepository) ListAdminPermissionDefinitions(ctx context.Context) (out []service.AdminPermissionDefinition, err error) {
	db, err := r.requireDB()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT permission, resource, action, sensitive, description FROM admin_permissions ORDER BY resource, action`)
	if err != nil {
		return nil, fmt.Errorf("list admin permissions: %w", err)
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	for rows.Next() {
		var item service.AdminPermissionDefinition
		if err := rows.Scan(&item.Permission, &item.Resource, &item.Action, &item.Sensitive, &item.Description); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *adminPermissionRepository) UpsertAdminGrant(ctx context.Context, targetUserID int64, permission, effect string, scope map[string]any, actorUserID int64, reason string) error {
	db, err := r.requireDB()
	if err != nil {
		return err
	}
	rawScope, err := json.Marshal(scope)
	if err != nil {
		return fmt.Errorf("marshal permission scope: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO admin_principal_grants (user_id, permission, effect, scope, granted_by, reason, updated_at)
VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, 0), $6, NOW())
ON CONFLICT (user_id, permission) DO UPDATE
SET effect = EXCLUDED.effect, scope = EXCLUDED.scope, granted_by = EXCLUDED.granted_by, reason = EXCLUDED.reason, updated_at = NOW()`,
		targetUserID, permission, effect, string(rawScope), actorUserID, reason)
	if err != nil {
		return fmt.Errorf("upsert admin grant: %w", err)
	}
	return nil
}

func (r *adminPermissionRepository) DeleteAdminGrant(ctx context.Context, targetUserID int64, permission string, actorUserID int64, reason string) error {
	db, err := r.requireDB()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM admin_principal_grants WHERE user_id = $1 AND permission = $2`, targetUserID, permission)
	if err != nil {
		return fmt.Errorf("delete admin grant: %w", err)
	}
	return nil
}

func (r *adminPermissionRepository) BumpAdminPermissionVersion(ctx context.Context, userID int64) (int64, error) {
	db, err := r.requireDB()
	if err != nil {
		return 0, err
	}
	var version int64
	err = db.QueryRowContext(ctx, `
INSERT INTO admin_permission_versions (user_id, version, updated_at)
VALUES ($1, 1, NOW())
ON CONFLICT (user_id) DO UPDATE SET version = admin_permission_versions.version + 1, updated_at = NOW()
RETURNING version`, userID).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("bump admin permission version: %w", err)
	}
	return version, nil
}

func (r *adminPermissionRepository) CreateAdminPermissionAudit(ctx context.Context, event service.AdminPermissionAudit) error {
	db, err := r.requireDB()
	if err != nil {
		return err
	}
	oldValue, _ := json.Marshal(event.OldValue)
	newValue, _ := json.Marshal(event.NewValue)
	_, err = db.ExecContext(ctx, `
INSERT INTO admin_permission_audit_logs (actor_user_id, target_user_id, action, permission, old_value, new_value, request_id)
VALUES (NULLIF($1, 0), NULLIF($2, 0), $3, NULLIF($4, ''), $5::jsonb, $6::jsonb, $7)`,
		event.ActorUserID, event.TargetUserID, event.Action, event.Permission, string(oldValue), string(newValue), event.RequestID)
	if err != nil {
		return fmt.Errorf("create admin permission audit: %w", err)
	}
	return nil
}

func (r *adminPermissionRepository) ApplyAdminPermissionChange(ctx context.Context, change service.AdminPermissionChange) (int64, error) {
	db, err := r.requireDB()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin admin permission change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var targetRole string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, change.TargetUserID).Scan(&targetRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, service.ErrAdminPrincipalNotFound
		}
		return 0, fmt.Errorf("lock admin permission target: %w", err)
	}
	if targetRole != service.RoleAdmin && targetRole != service.RoleSuperAdmin {
		return 0, service.ErrAdminPermissionTargetNotAdmin
	}
	if targetRole == service.RoleSuperAdmin && !change.ActorIsSuper {
		return 0, service.ErrAdminCannotModifySuperAdmin
	}

	switch change.Action {
	case "grant":
		rawScope, err := json.Marshal(change.Scope)
		if err != nil {
			return 0, fmt.Errorf("marshal permission scope: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO admin_principal_grants (user_id, permission, effect, scope, granted_by, reason, updated_at)
VALUES ($1, $2, $3, $4::jsonb, NULLIF($5, 0), $6, NOW())
ON CONFLICT (user_id, permission) DO UPDATE
SET effect = EXCLUDED.effect, scope = EXCLUDED.scope, granted_by = EXCLUDED.granted_by, reason = EXCLUDED.reason, updated_at = NOW()`,
			change.TargetUserID, change.Permission, change.Effect, string(rawScope), change.ActorUserID, change.Reason); err != nil {
			return 0, fmt.Errorf("upsert admin grant: %w", err)
		}
	case "revoke":
		result, err := tx.ExecContext(ctx, `DELETE FROM admin_principal_grants WHERE user_id = $1 AND permission = $2`, change.TargetUserID, change.Permission)
		if err != nil {
			return 0, fmt.Errorf("delete admin grant: %w", err)
		}
		if affected, err := result.RowsAffected(); err == nil && affected == 0 {
			var current int64
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM admin_permission_versions WHERE user_id = $1`, change.TargetUserID).Scan(&current); err != nil {
				return 0, fmt.Errorf("read unchanged admin permission version: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return 0, fmt.Errorf("commit unchanged admin permission change: %w", err)
			}
			return current, nil
		}
	default:
		return 0, fmt.Errorf("invalid admin permission action %q", change.Action)
	}

	var version int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO admin_permission_versions (user_id, version, updated_at)
VALUES ($1, 1, NOW())
ON CONFLICT (user_id) DO UPDATE SET version = admin_permission_versions.version + 1, updated_at = NOW()
RETURNING version`, change.TargetUserID).Scan(&version); err != nil {
		return 0, fmt.Errorf("bump admin permission version: %w", err)
	}

	oldValue, _ := json.Marshal(change.OldValue)
	newValue, _ := json.Marshal(change.NewValue)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO admin_permission_audit_logs (actor_user_id, target_user_id, action, permission, old_value, new_value, request_id)
VALUES (NULLIF($1, 0), NULLIF($2, 0), $3, NULLIF($4, ''), $5::jsonb, $6::jsonb, $7)`,
		change.ActorUserID, change.TargetUserID, change.Action, change.Permission, string(oldValue), string(newValue), change.RequestID); err != nil {
		return 0, fmt.Errorf("create admin permission audit: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit admin permission change: %w", err)
	}
	return version, nil
}

func decodePermissionScope(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func decodeAPIKeyScopes(raw []byte) ([]service.AdminGrant, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var permissions []string
	if err := json.Unmarshal(raw, &permissions); err == nil {
		out := make([]service.AdminGrant, 0, len(permissions))
		for _, permission := range permissions {
			out = append(out, service.AdminGrant{Permission: permission, Effect: service.AdminGrantAllow, Scope: map[string]any{"*": "*"}})
		}
		return out, nil
	}
	var grants []service.AdminGrant
	if err := json.Unmarshal(raw, &grants); err != nil {
		return nil, fmt.Errorf("decode admin api-key scopes: %w", err)
	}
	return grants, nil
}

func joinRowsCloseError(rows *sql.Rows, current error) error {
	closeErr := rows.Close()
	if closeErr == nil {
		return current
	}
	closeErr = fmt.Errorf("close query rows: %w", closeErr)
	if current == nil {
		return closeErr
	}
	return errors.Join(current, closeErr)
}
