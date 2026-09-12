package service

import (
	"context"
	"errors"
)

var (
	ErrAdminCannotModifySuperAdmin = errors.New("ordinary administrators cannot modify super administrators")
	ErrAdminCannotSelfGrant        = errors.New("administrators cannot grant themselves management privileges")
	ErrSuperAdminAssignmentDenied  = errors.New("only a super administrator can assign the super_admin role")
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

	if requestedRole == RoleSuperAdmin && actor.Role != RoleSuperAdmin {
		return ErrSuperAdminAssignmentDenied
	}
	if actor.Role != RoleSuperAdmin {
		if targetID == actorID {
			if requestedRole != "" || requestedStatus != "" || (targetID == actorID && targetID > 0) {
				// A normal admin may locally edit profile fields, but role/status
				// changes are privilege-sensitive and must be denied.
				if requestedRole != "" || requestedStatus != "" {
					return ErrAdminCannotSelfGrant
				}
			}
		}
		target, targetErr := loadAdminPermissionState(ctx, s, targetID)
		if targetErr != nil && mode == AdminPermissionModeEnforce {
			return ErrAdminPermissionDenied
		}
		if targetErr == nil && target != nil && target.Role == RoleSuperAdmin {
			return ErrAdminCannotModifySuperAdmin
		}
	}
	if mode == AdminPermissionModeEnforce {
		permission := ""
		switch {
		case requestedRole != "":
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
	if AdminPermissionModeFromEnv() != AdminPermissionModeEnforce {
		return nil
	}
	principal, ok := AdminPrincipalFromContext(ctx)
	if !ok || principal.UserID != actorID {
		return ErrAdminPermissionDenied
	}
	return AuthorizeAdminRequest(ctx, permission, nil)
}
