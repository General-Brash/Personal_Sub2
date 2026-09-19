package service

import (
	"context"
	"errors"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAdminCannotModifySuperAdmin = infraerrors.Forbidden("ADMIN_TARGET_PROTECTED", "ordinary administrators cannot modify super administrators")
	ErrAdminCannotSelfGrant        = infraerrors.Forbidden("ADMIN_SELF_GRANT_DENIED", "administrators cannot grant themselves management privileges")
	ErrSuperAdminAssignmentDenied  = infraerrors.Forbidden("SUPER_ADMIN_ASSIGNMENT_DENIED", "only a human super administrator can assign the super_admin role")
	ErrAdminMutationConflict       = infraerrors.Conflict("ADMIN_MUTATION_CONFLICT", "the administrator mutation target changed; reread and retry")
)

type AdminPermissionState struct {
	UserID  int64
	Role    string
	Status  string
	Version int64
	Grants  []AdminGrant
}

type AdminPermissionStateReader interface {
	ReadAdminPermissionState(ctx context.Context, userID int64) (*AdminPermissionState, error)
}

func loadAdminPermissionState(ctx context.Context, s *adminServiceImpl, userID int64) (*AdminPermissionState, error) {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return nil, ErrAdminPrincipalNotFound
	}
	reader, ok := s.userRepo.(AdminPermissionStateReader)
	if !ok {
		return nil, nil
	}
	return reader.ReadAdminPermissionState(ctx, userID)
}

func authorizeAdminPrincipalMutation(ctx context.Context, s *adminServiceImpl, actorID, targetID int64, requestedRole, requestedStatus string) error {
	mode := AdminPermissionModeFromEnv()
	if permissionService := AdminAuthorizationService(ctx); permissionService != nil {
		mode = permissionService.Mode()
	}
	actor, err := loadAdminPermissionState(ctx, s, actorID)
	if err != nil || actor == nil {
		if mode == AdminPermissionModeEnforce {
			return ErrAdminPermissionDenied
		}
		// Legacy/disabled mode preserves old behavior, except a non-super admin
		// can never mint a new super-admin identity.
		if requestedRole == RoleSuperAdmin {
			return ErrSuperAdminAssignmentDenied
		}
		return nil
	}

	// The first GetByID is the service preflight. If the target row has already
	// changed or disappeared by this second read, report a conflict instead of
	// reclassifying the stale request as a fresh permission denial.
	var target *AdminPermissionState
	var targetErr error
	if targetID > 0 {
		target, targetErr = loadAdminPermissionState(ctx, s, targetID)
		if targetErr != nil {
			if mode == AdminPermissionModeEnforce {
				if _, hasSnapshot := AdminMutationTargetSnapshotFromContext(ctx); hasSnapshot {
					return ErrAdminMutationConflict
				}
				return ErrAdminPermissionDenied
			}
		} else if target != nil {
			if expected, ok := AdminMutationTargetSnapshotFromContext(ctx); ok &&
				((expected.Role != "" && target.Role != expected.Role) ||
					(expected.Status != "" && target.Status != expected.Status)) {
				return ErrAdminMutationConflict
			}
		}
	}

	if requestedRole == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
		return ErrSuperAdminAssignmentDenied
	}
	if actor.Role != RoleSuperAdmin {
		if targetID == actorID && targetID > 0 && (requestedRole != "" || requestedStatus != "") {
			// A normal admin may locally edit profile fields, but role/status
			// changes are privilege-sensitive and must be denied.
			return ErrAdminCannotSelfGrant
		}
		if targetErr == nil && target != nil && target.Role == RoleSuperAdmin {
			return ErrAdminCannotModifySuperAdmin
		}
	}
	if mode == AdminPermissionModeEnforce {
		permission := ""
		switch {
		case requestedRole == RoleSuperAdmin:
			permission = "security.superadmin.assign"
		case requestedRole != "" && targetID > 0:
			permission = "users.role.assign"
		case requestedStatus != "":
			permission = "users.status"
		}
		if permission != "" {
			if err := authorizeAdminPermission(ctx, s, actorID, permission); err != nil {
				return err
			}
		}
	}
	return nil
}

func authorizeAdminPermission(ctx context.Context, s *adminServiceImpl, actorID int64, permission string) error {
	mode := AdminPermissionModeFromEnv()
	if permissionService := AdminAuthorizationService(ctx); permissionService != nil {
		mode = permissionService.Mode()
	}
	principal, ok := AdminPrincipalFromContext(ctx)
	apiKey := ok && principal != nil && principal.Kind == AdminPrincipalKindAPIKey
	if !ok || principal == nil || principal.UserID != actorID {
		return ErrAdminPermissionDenied
	}
	if mode != AdminPermissionModeEnforce && !apiKey {
		return nil
	}
	return AuthorizeAdminRequest(ctx, permission, nil)
}

// Keep concurrent transaction aborts in the mutation-conflict contract without
// automatically retrying a write or changing unrelated global error handling.
func normalizeAdminMutationError(err error) error {
	if err == nil {
		return nil
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01") {
		return fmt.Errorf("%w: %w", ErrAdminMutationConflict, err)
	}
	return err
}
