package service

import (
	"context"
	"errors"
)

type adminAuthorizationContextKey struct{}
type adminMutationActorContextKey struct{}
type adminMutationTargetSnapshotContextKey struct{}
type adminMutationPrincipalKindContextKey struct{}

func ContextWithAdminAuthorization(ctx context.Context, svc *AdminPermissionService) context.Context {
	return context.WithValue(ctx, adminAuthorizationContextKey{}, svc)
}

// ContextWithAdminMutationActorID carries the actor id into repository-owned
// transactions for legacy callers that do not attach an AdminPrincipal.  When
// a principal is present, repository authorization always prefers that
// principal and verifies the id matches it; this value is only a compatibility
// fallback for disabled/shadow mode.
func ContextWithAdminMutationActorID(ctx context.Context, actorID int64) context.Context {
	return context.WithValue(ctx, adminMutationActorContextKey{}, actorID)
}

func AdminMutationActorIDFromContext(ctx context.Context) (int64, bool) {
	actorID, ok := ctx.Value(adminMutationActorContextKey{}).(int64)
	return actorID, ok && actorID > 0
}

// AdminMutationContextPresent distinguishes an explicitly marked admin
// mutation from ordinary user creation/update paths that share UserRepository.
// The marker may intentionally carry actor id 0 in legacy unit/compatibility
// callers; that still differs from an unmarked consumer mutation.
func AdminMutationContextPresent(ctx context.Context) bool {
	_, ok := ctx.Value(adminMutationActorContextKey{}).(int64)
	return ok
}

// AdminMutationTargetSnapshot is the target role/status observed by the
// service-layer preflight. The repository guard compares it with the row it
// locks in the final write transaction; a mismatch is a 409 conflict rather
// than a second, stale authorization decision.
type AdminMutationTargetSnapshot struct {
	Role   string
	Status string
}

func ContextWithAdminMutationTargetSnapshot(ctx context.Context, role, status string) context.Context {
	return context.WithValue(ctx, adminMutationTargetSnapshotContextKey{}, AdminMutationTargetSnapshot{
		Role: role, Status: status,
	})
}

func AdminMutationTargetSnapshotFromContext(ctx context.Context) (AdminMutationTargetSnapshot, bool) {
	snapshot, ok := ctx.Value(adminMutationTargetSnapshotContextKey{}).(AdminMutationTargetSnapshot)
	return snapshot, ok
}

// ContextWithAdminMutationPrincipalKind preserves the authentication kind when
// a legacy/disabled deployment does not attach an explicit AdminPrincipal.
// This prevents an admin API key from falling back to the bound user's human
// super-admin role.
func ContextWithAdminMutationPrincipalKind(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, adminMutationPrincipalKindContextKey{}, kind)
}

func AdminMutationPrincipalKindFromContext(ctx context.Context) (string, bool) {
	kind, ok := ctx.Value(adminMutationPrincipalKindContextKey{}).(string)
	return kind, ok && kind != ""
}

// AuthorizeAdminRequest preserves scoped API-key identity all the way into a
// domain operation. Reconstructing authority from only a user ID is unsafe.
func AuthorizeAdminRequest(ctx context.Context, permission string, scope map[string]any) error {
	svc, _ := ctx.Value(adminAuthorizationContextKey{}).(*AdminPermissionService)
	principal, ok := AdminPrincipalFromContext(ctx)
	if svc == nil || !ok {
		return ErrAdminPermissionDenied
	}
	allowed, err := svc.AuthorizeRequest(ctx, principal, permission, scope)
	if err != nil || !allowed {
		return ErrAdminPermissionDenied
	}
	return nil
}

func RecheckAdminStream(ctx context.Context) error {
	svc, _ := ctx.Value(adminAuthorizationContextKey{}).(*AdminPermissionService)
	if svc == nil {
		return nil
	}
	if _, hasPrincipal := AdminPrincipalFromContext(ctx); !hasPrincipal {
		return ErrAdminPermissionDenied
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
