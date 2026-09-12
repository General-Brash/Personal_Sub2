package service

import (
	"context"
	"errors"
)

type adminAuthorizationContextKey struct{}

func ContextWithAdminAuthorization(ctx context.Context, svc *AdminPermissionService) context.Context {
	return context.WithValue(ctx, adminAuthorizationContextKey{}, svc)
}

// AuthorizeAdminRequest preserves scoped API-key identity all the way into a
// domain operation. Reconstructing authority from only a user ID is unsafe.
func AuthorizeAdminRequest(ctx context.Context, permission string, scope map[string]any) error {
	svc, _ := ctx.Value(adminAuthorizationContextKey{}).(*AdminPermissionService)
	principal, ok := AdminPrincipalFromContext(ctx)
	if svc == nil || !ok {
		return ErrAdminPermissionDenied
	}
	allowed, err := svc.CheckPermission(ctx, principal, permission, scope)
	if err != nil || !allowed {
		return ErrAdminPermissionDenied
	}
	return nil
}

func RecheckAdminStream(ctx context.Context) error {
	svc, _ := ctx.Value(adminAuthorizationContextKey{}).(*AdminPermissionService)
	if svc == nil || svc.Mode() != AdminPermissionModeEnforce {
		return nil
	}
	if err := AuthorizeAdminRequest(ctx, "ops.read", nil); err != nil {
		return errors.New("administrator stream permission revoked")
	}
	return nil
}

func AdminAuthorizationService(ctx context.Context) *AdminPermissionService {
	svc, _ := ctx.Value(adminAuthorizationContextKey{}).(*AdminPermissionService)
	return svc
}

// AssertMutableTarget applies to any endpoint that mutates a user (not just
// the role editor), including balances, bulk limits and entitlement changes.
func (s *AdminPermissionService) AssertMutableTarget(ctx context.Context, actor *AdminPrincipal, targetID int64) error {
	if s == nil || s.repo == nil || actor == nil || targetID <= 0 {
		return ErrAdminPermissionDenied
	}
	target, err := s.repo.GetAdminPrincipal(ctx, targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrAdminPrincipalNotFound
	}
	if target.Role == RoleSuperAdmin && (!actor.IsSuperAdmin() || actor.Kind == AdminPrincipalKindAPIKey) {
		return ErrAdminCannotModifySuperAdmin
	}
	return nil
}
