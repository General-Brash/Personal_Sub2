package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AdminPermissionStateReader = (*userRepository)(nil)

func (r *userRepository) ReadAdminPermissionState(ctx context.Context, userID int64) (state *service.AdminPermissionState, err error) {
	if r == nil || r.sql == nil || userID <= 0 {
		return nil, nil
	}
	var result service.AdminPermissionState
	var version int64
	stateRows, err := r.sql.QueryContext(ctx, `
SELECT u.id, u.role, u.status, COALESCE(v.version, 0)
FROM users u
LEFT JOIN admin_permission_versions v ON v.user_id = u.id
WHERE u.id = $1 AND u.deleted_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("read admin permission state: %w", err)
	}
	if !stateRows.Next() {
		return nil, joinRowsCloseError(stateRows, service.ErrAdminPrincipalNotFound)
	}
	if err := stateRows.Scan(&result.UserID, &result.Role, &result.Status, &version); err != nil {
		return nil, joinRowsCloseError(stateRows, err)
	}
	if err := joinRowsCloseError(stateRows, nil); err != nil {
		return nil, err
	}
	result.Version = version
	rows, err := r.sql.QueryContext(ctx, `SELECT permission, effect, scope FROM admin_principal_grants WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("read admin grants: %w", err)
	}
	defer func() {
		err = joinRowsCloseError(rows, err)
	}()
	for rows.Next() {
		var grant service.AdminGrant
		var raw []byte
		if err := rows.Scan(&grant.Permission, &grant.Effect, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &grant.Scope)
		}
		result.Grants = append(result.Grants, grant)
	}
	return &result, rows.Err()
}
