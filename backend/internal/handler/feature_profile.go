package handler

import (
	"context"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Only the authenticated user's effective capabilities are returned. This is
// UI guidance, never a replacement for live authorization at write boundaries.
type featureProfile struct {
	EntitlementTier   string                       `json:"entitlement_tier"`
	Entitlements      *service.EntitlementSnapshot `json:"effective_entitlements,omitempty"`
	PermissionMode    string                       `json:"permission_mode"`
	PermissionVersion int64                        `json:"permission_version"`
	Permissions       []string                     `json:"permissions"`
}

func buildFeatureProfile(ctx context.Context, user *service.User, permissions *service.AdminPermissionService, entitlements *service.EntitlementService) (featureProfile, error) {
	profile := featureProfile{EntitlementTier: service.EntitlementTierStandard, PermissionMode: service.AdminPermissionModeDisabled, Permissions: []string{}}
	if user == nil {
		return profile, service.ErrUserNotFound
	}
	if entitlements != nil {
		snapshot, err := entitlements.Resolve(ctx, user.ID)
		if err != nil {
			return profile, err
		}
		profile.Entitlements = snapshot
		profile.EntitlementTier = snapshot.Tier
	}
	if permissions == nil {
		return profile, nil
	}
	profile.PermissionMode = permissions.Mode()
	if !user.IsAdmin() {
		return profile, nil
	}
	principal, err := permissions.ResolvePrincipal(ctx, user.ID)
	if err != nil {
		if profile.PermissionMode != service.AdminPermissionModeEnforce {
			return profile, nil
		}
		return profile, err
	}
	profile.PermissionVersion = principal.Version
	definitions, err := permissions.ListPermissionCatalog(ctx)
	if err != nil {
		return profile, err
	}
	for _, definition := range definitions {
		// Object-scoped permissions remain visible as actions, but the server
		// must still validate each request's object against the full grant.
		scopes := []map[string]any{nil}
		for _, grant := range principal.Grants {
			if grant.Permission == definition.Permission && grant.Effect == service.AdminGrantAllow {
				scopes = append(scopes, grant.Scope)
			}
		}
		for _, scope := range scopes {
			allowed, err := permissions.Authorize(ctx, principal, definition.Permission, scope)
			if err == nil && allowed {
				profile.Permissions = append(profile.Permissions, definition.Permission)
				break
			}
		}
	}
	sort.Strings(profile.Permissions)
	return profile, nil
}
