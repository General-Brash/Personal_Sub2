package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AdminMutationTx is the small raw-SQL surface required by the atomic admin
// authorizer. Both *sql.Tx and an ent transaction client satisfy it, so an
// entitlement or user mutation can reuse the exact same transaction guard.
type AdminMutationTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type AdminMutationPermission struct {
	Permission string
	Scope      map[string]any
}

// AdminMutationAuthorizationOptions describes the mutation being authorized.
// TargetUserID == 0 means a create operation with no existing target row.
type AdminMutationAuthorizationOptions struct {
	TargetUserID      int64
	TargetMustBeAdmin bool
	TargetNotFound    error
	// ExpectedTargetRole/Status are optional service-preflight snapshots. When
	// supplied, the locked row must still match or the call returns a 409.
	ExpectedTargetRole             string
	ExpectedTargetStatus           string
	Permissions                    []AdminMutationPermission
	Privilege                      *AdminMutationPermission
	RequireHumanSuperAdmin         bool
	RequireHumanSuperAdminForAdmin bool
	DisallowSelfTarget             bool
}

// AdminMutationAuthorization is the state read and locked by
// AuthorizeAdminMutationTx. Callers may use it for audit data, but must not
// replace the locked values with an earlier service-layer snapshot.
type AdminMutationAuthorization struct {
	ActorUserID   int64
	ActorKind     string
	ActorRole     string
	ActorStatus   string
	ActorVersion  int64
	TargetUserID  int64
	TargetRole    string
	TargetStatus  string
	TargetVersion int64
	TargetExists  bool
}

func userMutationScope(userID int64) map[string]any {
	return map[string]any{
		"id":        userID,
		"user_id":   userID,
		"users_ids": userID,
	}
}

func authorizeUserCreateMutationTx(ctx context.Context, tx AdminMutationTx, actorUserID int64, role string) error {
	opts := AdminMutationAuthorizationOptions{
		Permissions: []AdminMutationPermission{{Permission: "users.update"}},
	}
	switch role {
	case service.RoleAdmin:
		opts.Permissions = append(opts.Permissions, AdminMutationPermission{Permission: "users.role.assign"})
	case service.RoleSuperAdmin:
		opts.Permissions = append(opts.Permissions, AdminMutationPermission{Permission: "security.superadmin.assign"})
		opts.RequireHumanSuperAdmin = true
	}
	_, err := AuthorizeAdminMutationTx(ctx, tx, actorUserID, opts)
	return err
}

func authorizeUserUpdateMutationTx(ctx context.Context, tx AdminMutationTx, actorUserID, targetUserID int64, userIn *service.User, fields service.UserUpdateFields) error {
	opts := AdminMutationAuthorizationOptions{
		TargetUserID:   targetUserID,
		TargetNotFound: service.ErrUserNotFound,
		Permissions:    []AdminMutationPermission{{Permission: "users.update", Scope: userMutationScope(targetUserID)}},
	}
	if fields.Role {
		permission := "users.role.assign"
		if userIn != nil && userIn.Role == service.RoleSuperAdmin {
			permission = "security.superadmin.assign"
			opts.RequireHumanSuperAdmin = true
		}
		opts.Permissions = append(opts.Permissions, AdminMutationPermission{Permission: permission, Scope: userMutationScope(targetUserID)})
	}
	if fields.Status {
		opts.Permissions = append(opts.Permissions, AdminMutationPermission{Permission: "users.status", Scope: userMutationScope(targetUserID)})
	}
	_, err := AuthorizeAdminMutationTx(ctx, tx, actorUserID, opts)
	return err
}

func authorizeUserDeleteMutationTx(ctx context.Context, tx AdminMutationTx, actorUserID, targetUserID int64) error {
	_, err := AuthorizeAdminMutationTx(ctx, tx, actorUserID, AdminMutationAuthorizationOptions{
		TargetUserID:                   targetUserID,
		TargetNotFound:                 service.ErrUserNotFound,
		Permissions:                    []AdminMutationPermission{{Permission: "users.delete", Scope: userMutationScope(targetUserID)}},
		RequireHumanSuperAdminForAdmin: true,
	})
	return err
}

// AuthorizeAdminEntitlementMutationTx is the transaction-bound contract for
// single-target entitlement writers. The caller must invoke it after starting
// the write transaction and before changing any entitlement row, once for each
// sorted target ID in a batch, with the preview's expected target role/status
// when available. The batch writer uses AuthorizeAdminMutationTx once after
// locking the complete sorted user set, then validates all preview targets.
// This helper rechecks the actor principal/version and locks actor and
// target user rows under the shared advisory lock. The caller may then lock its
// policy and entitlement rows and write them in the same transaction; any
// non-nil error must roll the whole transaction back.
//
// The helper intentionally does not grant entitlement permission to a caller
// that only has a role, and it never lets an API-key principal inherit a human
// super-admin role. The context must carry the HTTP AdminPrincipal plus the
// AdminPermissionService in enforce mode; this wrapper marks actorUserID for
// legacy/shadow compatibility and still fails closed when enforce lacks one.
func AuthorizeAdminEntitlementMutationTx(
	ctx context.Context,
	tx AdminMutationTx,
	actorUserID, targetUserID int64,
	expectedTargetRole, expectedTargetStatus string,
) (*AdminMutationAuthorization, error) {
	ctx = service.ContextWithAdminMutationActorID(ctx, actorUserID)
	return AuthorizeAdminMutationTx(ctx, tx, actorUserID, AdminMutationAuthorizationOptions{
		TargetUserID:         targetUserID,
		TargetNotFound:       service.ErrUserNotFound,
		ExpectedTargetRole:   expectedTargetRole,
		ExpectedTargetStatus: expectedTargetStatus,
		Permissions: []AdminMutationPermission{{
			Permission: "users.entitlement.manage",
			Scope:      userMutationScope(targetUserID),
		}},
	})
}

// AuthorizeAdminMutationTx atomically locks and rechecks the actor and target
// for a sensitive admin mutation. It deliberately reads the existing principal
// from ctx instead of rebuilding authority from actorUserID: JWT role/status,
// API-key binding/scopes, principal kind and permission version are all checked
// against the same transaction that will perform the write. The lock order is
// shared advisory mutation lock, then actor/target user rows in ascending ID
// order, then any caller-specific policy/entitlement rows and final writes.
// Callers must not write before this function returns nil.
//
// In enforce mode an explicit principal is mandatory. In disabled/shadow mode,
// callers that carry only actorUserID retain legacy behavior while still
// receiving row locking and role/active checks when an actor id is available.
func AuthorizeAdminMutationTx(
	ctx context.Context,
	tx AdminMutationTx,
	actorUserID int64,
	opts AdminMutationAuthorizationOptions,
) (*AdminMutationAuthorization, error) {
	if tx == nil {
		return nil, fmt.Errorf("admin mutation transaction is nil")
	}

	mode := service.AdminPermissionModeFromEnv()
	if permissionService := service.AdminAuthorizationService(ctx); permissionService != nil {
		mode = permissionService.Mode()
	}

	principal, hasPrincipal := service.AdminPrincipalFromContext(ctx)
	principalKind, hasPrincipalKind := service.AdminMutationPrincipalKindFromContext(ctx)
	if snapshot, ok := service.AdminMutationTargetSnapshotFromContext(ctx); ok {
		if opts.ExpectedTargetRole == "" {
			opts.ExpectedTargetRole = snapshot.Role
		}
		if opts.ExpectedTargetStatus == "" {
			opts.ExpectedTargetStatus = snapshot.Status
		}
	}
	if hasPrincipalKind && principalKind != service.AdminPrincipalKindJWT && principalKind != service.AdminPrincipalKindAPIKey {
		return nil, service.ErrAdminPermissionDenied
	}
	if hasPrincipal && hasPrincipalKind && (principal == nil || principal.Kind != principalKind) {
		return nil, service.ErrAdminPermissionDenied
	}
	markedMutation := service.AdminMutationContextPresent(ctx)
	if !hasPrincipal && !markedMutation {
		// UserRepository is also used by registration and OAuth account-linking
		// flows. Only an explicitly marked admin mutation (or an attached admin
		// principal) should enter this admin-only gate.
		return nil, nil
	}
	if actorUserID <= 0 {
		if id, ok := service.AdminMutationActorIDFromContext(ctx); ok {
			actorUserID = id
		} else if hasPrincipal && principal != nil {
			actorUserID = principal.UserID
		}
	}
	if hasPrincipal && (principal == nil || principal.UserID <= 0 || (actorUserID > 0 && principal.UserID != actorUserID)) {
		return nil, service.ErrAdminPermissionDenied
	}
	if mode == service.AdminPermissionModeEnforce && !hasPrincipal {
		return nil, service.ErrAdminPermissionDenied
	}
	// Once an operation is marked as administrative, an absent actor must not
	// bypass the final authorization, including in legacy/shadow deployments.
	if actorUserID <= 0 {
		return nil, service.ErrAdminPermissionDenied
	}

	if err := lockAdminMutationScope(ctx, tx); err != nil {
		return nil, fmt.Errorf("lock admin mutation scope: %w", err)
	}

	ids := []int64{actorUserID}
	if opts.TargetUserID > 0 && opts.TargetUserID != actorUserID {
		ids = append(ids, opts.TargetUserID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rowsByID, err := lockAdminMutationUsers(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	actor, ok := rowsByID[actorUserID]
	if !ok {
		return nil, service.ErrAdminPermissionDenied
	}
	if actor.Status != service.StatusActive || (actor.Role != service.RoleAdmin && actor.Role != service.RoleSuperAdmin) {
		return nil, service.ErrAdminPermissionDenied
	}

	freshPrincipal, err := freshAdminMutationPrincipal(ctx, tx, principal, hasPrincipal, principalKind, actorUserID, actor, mode)
	if err != nil {
		return nil, err
	}
	if freshPrincipal == nil {
		// Non-enforce legacy mode with an actor id still gets the locked role and
		// target protections below, but does not invent grant semantics.
		freshPrincipal = &service.AdminPrincipal{
			UserID:  actor.UserID,
			Kind:    service.AdminPrincipalKindJWT,
			Role:    actor.Role,
			Version: actor.Version,
		}
	}

	isHumanSuperAdmin := freshPrincipal.Kind == service.AdminPrincipalKindJWT && freshPrincipal.Role == service.RoleSuperAdmin
	if opts.RequireHumanSuperAdmin && !isHumanSuperAdmin {
		return nil, service.ErrSuperAdminAssignmentDenied
	}

	if mode == service.AdminPermissionModeEnforce {
		permissionService := service.NewAdminPermissionService(nil, service.AdminPermissionModeEnforce)
		for _, required := range opts.Permissions {
			allowed, checkErr := permissionService.Authorize(ctx, freshPrincipal, required.Permission, required.Scope)
			if checkErr != nil {
				return nil, checkErr
			}
			if !allowed {
				return nil, service.ErrAdminPermissionDenied
			}
		}
		if opts.Privilege != nil && !isHumanSuperAdmin {
			allowed, checkErr := permissionService.Authorize(ctx, freshPrincipal, opts.Privilege.Permission, opts.Privilege.Scope)
			if checkErr != nil {
				return nil, checkErr
			}
			if !allowed {
				return nil, service.ErrAdminPermissionPrivilege
			}
		}
	}
	// A currently denied operation is 403. If authority still permits this
	// operation but its preflight role/version changed, require a fresh request.
	if hasPrincipal && principal.Explicit && (principal.Version != actor.Version ||
		(principal.Kind == service.AdminPrincipalKindJWT && principal.Role != actor.Role)) {
		return nil, service.ErrAdminMutationConflict
	}

	authorized := &AdminMutationAuthorization{
		ActorUserID:  actor.UserID,
		ActorKind:    freshPrincipal.Kind,
		ActorRole:    freshPrincipal.Role,
		ActorStatus:  actor.Status,
		ActorVersion: actor.Version,
		TargetUserID: opts.TargetUserID,
	}
	if opts.TargetUserID > 0 {
		target, targetOK := rowsByID[opts.TargetUserID]
		if !targetOK {
			if opts.ExpectedTargetRole != "" || opts.ExpectedTargetStatus != "" {
				return nil, service.ErrAdminMutationConflict
			}
			if opts.TargetNotFound != nil {
				return nil, opts.TargetNotFound
			}
			return nil, service.ErrAdminPrincipalNotFound
		}
		authorized.TargetExists = true
		authorized.TargetRole = target.Role
		authorized.TargetStatus = target.Status
		authorized.TargetVersion = target.Version
		if (opts.ExpectedTargetRole != "" && target.Role != opts.ExpectedTargetRole) ||
			(opts.ExpectedTargetStatus != "" && target.Status != opts.ExpectedTargetStatus) {
			return nil, service.ErrAdminMutationConflict
		}
		if opts.DisallowSelfTarget && opts.TargetUserID == actorUserID {
			return nil, service.ErrAdminPermissionSelfGrant
		}
		if opts.TargetMustBeAdmin && target.Role != service.RoleAdmin && target.Role != service.RoleSuperAdmin {
			return nil, service.ErrAdminPermissionTargetNotAdmin
		}
		if target.Role == service.RoleSuperAdmin && !isHumanSuperAdmin {
			return nil, service.ErrAdminCannotModifySuperAdmin
		}
		if opts.RequireHumanSuperAdminForAdmin && target.Role == service.RoleAdmin && !isHumanSuperAdmin {
			return nil, service.ErrAdminCannotDeleteAdmin
		}
	}

	return authorized, nil
}

type adminMutationUserState struct {
	UserID  int64
	Role    string
	Status  string
	Version int64
}

func lockAdminMutationScope(ctx context.Context, tx AdminMutationTx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, superAdminGuardLockKey)
	return err
}

func lockAdminMutationUsers(ctx context.Context, tx AdminMutationTx, ids []int64) (map[int64]adminMutationUserState, error) {
	if len(ids) == 0 {
		return map[int64]adminMutationUserState{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := tx.QueryContext(ctx, `
SELECT u.id, u.role, u.status, COALESCE(v.version, 0)
FROM users u
LEFT JOIN admin_permission_versions v ON v.user_id = u.id
WHERE u.id IN (`+strings.Join(placeholders, ",")+`) AND u.deleted_at IS NULL
ORDER BY u.id
FOR UPDATE OF u`, args...)
	if err != nil {
		return nil, fmt.Errorf("lock admin mutation users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int64]adminMutationUserState, len(ids))
	for rows.Next() {
		var state adminMutationUserState
		if err := rows.Scan(&state.UserID, &state.Role, &state.Status, &state.Version); err != nil {
			return nil, fmt.Errorf("scan admin mutation user: %w", err)
		}
		result[state.UserID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin mutation users: %w", err)
	}
	return result, nil
}

func freshAdminMutationPrincipal(
	ctx context.Context,
	tx AdminMutationTx,
	principal *service.AdminPrincipal,
	hasPrincipal bool,
	principalKind string,
	actorUserID int64,
	actor adminMutationUserState,
	mode string,
) (*service.AdminPrincipal, error) {
	if !hasPrincipal {
		if mode == service.AdminPermissionModeEnforce {
			return nil, service.ErrAdminPermissionDenied
		}
		if actor.Role != service.RoleAdmin && actor.Role != service.RoleSuperAdmin {
			return nil, service.ErrAdminPermissionDenied
		}
		if principalKind == service.AdminPrincipalKindAPIKey {
			return &service.AdminPrincipal{
				ID: "admin-key:legacy", Kind: service.AdminPrincipalKindAPIKey,
				UserID: actor.UserID, Role: service.RoleAdmin, Version: actor.Version,
				Explicit: false, Source: "legacy_admin_api_key",
			}, nil
		}
		return nil, nil
	}
	if principal == nil || principal.UserID != actorUserID {
		return nil, service.ErrAdminPermissionDenied
	}
	if !principal.Explicit {
		if mode == service.AdminPermissionModeEnforce {
			return nil, service.ErrAdminPermissionDenied
		}
		if principalKind == service.AdminPrincipalKindAPIKey || principal.Kind == service.AdminPrincipalKindAPIKey {
			return &service.AdminPrincipal{
				ID: "admin-key:legacy", Kind: service.AdminPrincipalKindAPIKey,
				UserID: actor.UserID, Role: service.RoleAdmin, Version: actor.Version,
				Explicit: false, Source: "legacy_admin_api_key",
			}, nil
		}
		if actor.Role != service.RoleAdmin && actor.Role != service.RoleSuperAdmin {
			return nil, service.ErrAdminPermissionDenied
		}
		return nil, nil
	}

	switch principal.Kind {
	case service.AdminPrincipalKindJWT:
		if actor.Role != service.RoleAdmin && actor.Role != service.RoleSuperAdmin {
			return nil, service.ErrAdminPermissionDenied
		}
		grants, err := readAdminMutationGrants(ctx, tx, actorUserID)
		if err != nil {
			return nil, err
		}
		return &service.AdminPrincipal{
			ID:         principal.ID,
			Kind:       service.AdminPrincipalKindJWT,
			UserID:     actor.UserID,
			Role:       actor.Role,
			Version:    actor.Version,
			Grants:     grants,
			Explicit:   true,
			Source:     principal.Source,
			ResolvedAt: principal.ResolvedAt,
		}, nil

	case service.AdminPrincipalKindAPIKey:
		if principal.Role != service.RoleAdmin {
			return nil, service.ErrAdminPermissionDenied
		}
		bindingKey := strings.TrimSpace(strings.TrimPrefix(principal.ID, "admin-key:"))
		if bindingKey == "" {
			return nil, service.ErrAdminPermissionDenied
		}
		var boundUserID int64
		var enabled bool
		var rawScopes []byte
		bindingRows, queryErr := tx.QueryContext(ctx, `
SELECT principal_user_id, scopes, enabled
FROM admin_api_key_bindings
WHERE binding_key = $1
		FOR UPDATE`, bindingKey)
		if queryErr != nil {
			return nil, fmt.Errorf("lock admin api-key binding: %w", queryErr)
		}
		if !bindingRows.Next() {
			closeErr := bindingRows.Close()
			if closeErr != nil {
				return nil, fmt.Errorf("close admin api-key binding: %w", closeErr)
			}
			if err := bindingRows.Err(); err != nil {
				return nil, fmt.Errorf("iterate admin api-key binding: %w", err)
			}
			return nil, service.ErrAdminPermissionDenied
		}
		if err := bindingRows.Scan(&boundUserID, &rawScopes, &enabled); err != nil {
			_ = bindingRows.Close()
			return nil, fmt.Errorf("scan admin api-key binding: %w", err)
		}
		if err := bindingRows.Close(); err != nil {
			return nil, fmt.Errorf("close admin api-key binding: %w", err)
		}
		if !enabled || boundUserID != actorUserID {
			return nil, service.ErrAdminPermissionDenied
		}
		grants, err := decodeAPIKeyScopes(rawScopes)
		if err != nil {
			return nil, fmt.Errorf("decode admin api-key scopes: %w", err)
		}
		grants = filterAdminMutationAPIKeyGrants(grants)
		return &service.AdminPrincipal{
			ID:         "admin-key:" + bindingKey,
			Kind:       service.AdminPrincipalKindAPIKey,
			UserID:     actor.UserID,
			Role:       service.RoleAdmin,
			Version:    actor.Version,
			Grants:     grants,
			Explicit:   true,
			Source:     "admin_api_key_bindings",
			ResolvedAt: principal.ResolvedAt,
		}, nil
	default:
		return nil, service.ErrAdminPermissionDenied
	}
}

func filterAdminMutationAPIKeyGrants(grants []service.AdminGrant) []service.AdminGrant {
	filtered := make([]service.AdminGrant, 0, len(grants))
	for _, grant := range grants {
		if grant.Effect == "" {
			grant.Effect = service.AdminGrantAllow
		}
		if grant.Permission == "*" || !service.IsKnownAdminPermission(grant.Permission) ||
			(grant.Effect != service.AdminGrantAllow && grant.Effect != service.AdminGrantDeny) {
			continue
		}
		filtered = append(filtered, grant)
	}
	return filtered
}

func readAdminMutationGrants(ctx context.Context, tx AdminMutationTx, userID int64) ([]service.AdminGrant, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT permission, effect, scope
FROM admin_principal_grants
WHERE user_id = $1
ORDER BY effect DESC, permission ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("load admin mutation grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	grants := make([]service.AdminGrant, 0)
	for rows.Next() {
		var grant service.AdminGrant
		var raw []byte
		if err := rows.Scan(&grant.Permission, &grant.Effect, &raw); err != nil {
			return nil, fmt.Errorf("scan admin mutation grant: %w", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &grant.Scope); err != nil {
				return nil, fmt.Errorf("decode admin mutation grant scope: %w", err)
			}
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin mutation grants: %w", err)
	}
	return grants, nil
}
