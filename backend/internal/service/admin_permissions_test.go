package service

import (
	"context"
	"errors"
	"testing"
)

type adminPermissionRepoStub struct {
	record *AdminPrincipalRecord
}

func (r *adminPermissionRepoStub) GetAdminPrincipal(context.Context, int64) (*AdminPrincipalRecord, error) {
	return r.record, nil
}
func (*adminPermissionRepoStub) ResolveAdminAPIKeyBinding(context.Context, string) (*AdminPrincipalRecord, error) {
	return nil, ErrAdminPermissionEnforceNoBind
}
func (*adminPermissionRepoStub) ListAdminPermissionDefinitions(context.Context) ([]AdminPermissionDefinition, error) {
	return nil, nil
}
func (*adminPermissionRepoStub) UpsertAdminGrant(context.Context, int64, string, string, map[string]any, int64, string) error {
	return nil
}
func (*adminPermissionRepoStub) DeleteAdminGrant(context.Context, int64, string, int64, string) error {
	return nil
}
func (*adminPermissionRepoStub) BumpAdminPermissionVersion(context.Context, int64) (int64, error) {
	return 1, nil
}
func (*adminPermissionRepoStub) CreateAdminPermissionAudit(context.Context, AdminPermissionAudit) error {
	return nil
}
func (*adminPermissionRepoStub) ApplyAdminPermissionChange(context.Context, AdminPermissionChange) (int64, error) {
	return 1, nil
}

func TestAdminAuthorizeNilScopeIsNotUnlimited(t *testing.T) {
	svc := NewAdminPermissionService(nil, AdminPermissionModeEnforce)
	principal := &AdminPrincipal{
		UserID: 2,
		Role:   RoleAdmin,
		Kind:   AdminPrincipalKindJWT,
		Grants: []AdminGrant{{Permission: "users.read", Effect: AdminGrantAllow}},
	}
	allowed, err := svc.Authorize(context.Background(), principal, "users.read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("nil/empty grant scope must not become an unrestricted allow")
	}

	principal.Grants[0].Scope = map[string]any{"*": "*"}
	allowed, err = svc.Authorize(context.Background(), principal, "users.read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("explicit global scope should allow")
	}
}

func TestAdminAuthorizeDenyWinsAndScopedKeysStayScoped(t *testing.T) {
	svc := NewAdminPermissionService(nil, AdminPermissionModeEnforce)
	principal := &AdminPrincipal{
		UserID: 2,
		Role:   RoleAdmin,
		Kind:   AdminPrincipalKindAPIKey,
		Grants: []AdminGrant{
			{Permission: "groups.rates.manage", Effect: AdminGrantAllow, Scope: map[string]any{"group_id": float64(7)}},
			{Permission: "groups.rates.manage", Effect: AdminGrantDeny, Scope: map[string]any{"group_id": float64(7)}},
		},
	}
	allowed, err := svc.Authorize(context.Background(), principal, "groups.rates.manage", map[string]any{"group_id": int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("explicit deny must win")
	}

	principal.Grants = principal.Grants[:1]
	allowed, err = svc.Authorize(context.Background(), principal, "groups.rates.manage", map[string]any{"group_id": int64(8)})
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("API-key scope must not widen beyond its own group")
	}
}

func TestAdminAPIKeyNeverInheritsSuperAdmin(t *testing.T) {
	svc := NewAdminPermissionService(nil, AdminPermissionModeEnforce)
	principal := &AdminPrincipal{UserID: 9, Role: RoleSuperAdmin, Kind: AdminPrincipalKindAPIKey}
	allowed, err := svc.Authorize(context.Background(), principal, "users.read", nil)
	if err == nil && allowed {
		t.Fatal("API-key principal must never inherit super-admin")
	}
}

func TestGrantPermissionRejectsSelfSuperTargetAndMissingScope(t *testing.T) {
	target := &adminPermissionRepoStub{record: &AdminPrincipalRecord{UserID: 5, Role: RoleAdmin, Status: StatusActive}}
	svc := NewAdminPermissionService(target, AdminPermissionModeEnforce)
	actor := &AdminPrincipal{UserID: 1, Role: RoleSuperAdmin, Kind: AdminPrincipalKindJWT}
	if err := svc.GrantPermission(context.Background(), actor, actor.UserID, "users.read", AdminGrantAllow, map[string]any{"*": "*"}, "self"); !errors.Is(err, ErrAdminPermissionSelfGrant) {
		t.Fatalf("self grant error = %v", err)
	}
	if err := svc.GrantPermission(context.Background(), actor, 5, "users.read", AdminGrantAllow, nil, "missing scope"); !errors.Is(err, ErrAdminPermissionScopeInvalid) {
		t.Fatalf("missing scope error = %v", err)
	}
	target.record.Role = RoleSuperAdmin
	if err := svc.GrantPermission(context.Background(), &AdminPrincipal{UserID: 2, Role: RoleAdmin, Kind: AdminPrincipalKindJWT}, 5, "users.read", AdminGrantAllow, map[string]any{"*": "*"}, "target super"); !errors.Is(err, ErrAdminCannotModifySuperAdmin) {
		t.Fatalf("super target error = %v", err)
	}
}

func TestAdminCannotMutateSuperAdminThroughBalanceOrEntitlementEndpoints(t *testing.T) {
	repo := &adminPermissionRepoStub{record: &AdminPrincipalRecord{UserID: 5, Role: RoleSuperAdmin, Status: StatusActive}}
	svc := NewAdminPermissionService(repo, AdminPermissionModeEnforce)
	for _, actor := range []*AdminPrincipal{{UserID: 2, Role: RoleAdmin, Kind: AdminPrincipalKindJWT}, {UserID: 2, Role: RoleSuperAdmin, Kind: AdminPrincipalKindAPIKey}} {
		if err := svc.AssertMutableTarget(context.Background(), actor, 5); !errors.Is(err, ErrAdminCannotModifySuperAdmin) {
			t.Fatalf("unprotected super-admin target: %v", err)
		}
	}
	if err := svc.AssertMutableTarget(context.Background(), &AdminPrincipal{UserID: 1, Role: RoleSuperAdmin, Kind: AdminPrincipalKindJWT}, 5); err != nil {
		t.Fatal(err)
	}
}
