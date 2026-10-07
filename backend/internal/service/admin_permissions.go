package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	RoleSuperAdmin = domain.RoleSuperAdmin

	AdminPermissionModeDisabled = "disabled"
	// AdminPermissionModeShadow is retained only so older configuration readers
	// and callers can continue to compile. It is a legacy input alias and is
	// always normalized to disabled; it is never an effective runtime mode.
	AdminPermissionModeShadow  = "shadow"
	AdminPermissionModeEnforce = "enforce"

	AdminPrincipalKindJWT       = "jwt"
	AdminPrincipalKindAPIKey    = "admin_api_key"
	AdminPrincipalKindWebSocket = "websocket"
	AdminPrincipalKindPlugin    = "plugin"

	AdminGrantAllow = "allow"
	AdminGrantDeny  = "deny"
)

var (
	ErrAdminPermissionUnknown        = infraerrors.BadRequest("UNKNOWN_ADMIN_PERMISSION", "unknown admin permission")
	ErrAdminPrincipalNotFound        = infraerrors.NotFound("ADMIN_PRINCIPAL_NOT_FOUND", "admin principal not found")
	ErrAdminPermissionDenied         = infraerrors.Forbidden("ADMIN_PERMISSION_DENIED", "admin permission denied")
	ErrAdminPermissionSelfGrant      = infraerrors.Forbidden("ADMIN_PERMISSION_SELF_GRANT", "administrators cannot grant permissions to themselves")
	ErrAdminPermissionPrivilege      = infraerrors.Forbidden("ADMIN_PERMISSION_PRIVILEGE", "cannot grant a permission the actor does not hold")
	ErrAdminPermissionEnforceNoBind  = infraerrors.Unauthorized("ADMIN_KEY_PRINCIPAL_REQUIRED", "legacy admin key has no explicit principal binding in enforce mode")
	ErrAdminPermissionScopeInvalid   = infraerrors.BadRequest("ADMIN_PERMISSION_SCOPE_INVALID", "administrator permission scope is invalid")
	ErrAdminPermissionTargetNotAdmin = infraerrors.NotFound("ADMIN_PERMISSION_TARGET_NOT_ADMIN", "administrator permission target is not an administrator")
	ErrLastSuperAdmin                = infraerrors.Conflict("LAST_SUPER_ADMIN", "cannot disable, demote or delete the last active super administrator")
)

type AdminPermissionDefinition struct {
	Permission  string `json:"permission"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description"`
}

type AdminGrant struct {
	Permission string         `json:"permission"`
	Effect     string         `json:"effect"`
	Scope      map[string]any `json:"scope,omitempty"`
}

type AdminCapabilities struct {
	WritesEnabled bool   `json:"writes_enabled"`
	CanWrite      bool   `json:"can_write"`
	Mode          string `json:"mode"`
	DenyReason    string `json:"deny_reason,omitempty"`
}

type AdminPrincipal struct {
	ID           string             `json:"id"`
	Kind         string             `json:"kind"`
	UserID       int64              `json:"user_id"`
	Role         string             `json:"role"`
	Version      int64              `json:"version"`
	Grants       []AdminGrant       `json:"grants,omitempty"`
	Explicit     bool               `json:"explicit"`
	Source       string             `json:"source"`
	ResolvedAt   time.Time          `json:"resolved_at"`
	Capabilities *AdminCapabilities `json:"capabilities,omitempty"`
}

func (p *AdminPrincipal) IsSuperAdmin() bool {
	return p != nil && p.Role == RoleSuperAdmin
}

type AdminPrincipalRecord struct {
	UserID  int64
	Role    string
	Status  string
	Version int64
	Grants  []AdminGrant
}

type AdminPermissionChange struct {
	ActorUserID  int64
	ActorIsSuper bool
	TargetUserID int64
	Action       string
	Permission   string
	Effect       string
	Scope        map[string]any
	Reason       string
	OldValue     map[string]any
	NewValue     map[string]any
	RequestID    string
}

type AdminPermissionAudit struct {
	ActorUserID  int64
	TargetUserID int64
	Action       string
	Permission   string
	OldValue     map[string]any
	NewValue     map[string]any
	RequestID    string
}

type AdminPermissionRepository interface {
	GetAdminPrincipal(ctx context.Context, userID int64) (*AdminPrincipalRecord, error)
	ResolveAdminAPIKeyBinding(ctx context.Context, bindingKey string) (*AdminPrincipalRecord, error)
	ListAdminPermissionDefinitions(ctx context.Context) ([]AdminPermissionDefinition, error)
	UpsertAdminGrant(ctx context.Context, targetUserID int64, permission, effect string, scope map[string]any, actorUserID int64, reason string) error
	DeleteAdminGrant(ctx context.Context, targetUserID int64, permission string, actorUserID int64, reason string) error
	BumpAdminPermissionVersion(ctx context.Context, userID int64) (int64, error)
	CreateAdminPermissionAudit(ctx context.Context, event AdminPermissionAudit) error
	ApplyAdminPermissionChange(ctx context.Context, change AdminPermissionChange) (int64, error)
}

type AdminPermissionService struct {
	repo AdminPermissionRepository
	mode string
}

func NewAdminPermissionService(repo AdminPermissionRepository, mode string) *AdminPermissionService {
	return &AdminPermissionService{repo: repo, mode: NormalizeAdminPermissionMode(mode)}
}

func NormalizeAdminPermissionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AdminPermissionModeEnforce:
		return AdminPermissionModeEnforce
	case AdminPermissionModeShadow:
		return AdminPermissionModeDisabled
	default:
		return AdminPermissionModeDisabled
	}
}

func AdminPermissionModeFromEnv() string {
	return NormalizeAdminPermissionMode(os.Getenv("ADMIN_PERMISSIONS_MODE"))
}

func (s *AdminPermissionService) Mode() string {
	if s == nil {
		return AdminPermissionModeDisabled
	}
	return NormalizeAdminPermissionMode(s.mode)
}

func (s *AdminPermissionService) EnabledInEnforceMode() bool {
	return s != nil && s.mode == AdminPermissionModeEnforce
}

func (s *AdminPermissionService) ResolvePrincipal(ctx context.Context, userID int64) (*AdminPrincipal, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrAdminPrincipalNotFound
	}
	record, err := s.repo.GetAdminPrincipal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if record == nil || record.UserID <= 0 {
		return nil, ErrAdminPrincipalNotFound
	}
	if record.Status != StatusActive {
		return nil, ErrAdminPrincipalNotFound
	}
	if record.Role != RoleAdmin && record.Role != RoleSuperAdmin {
		return nil, ErrAdminPrincipalNotFound
	}
	return &AdminPrincipal{
		ID:         fmt.Sprintf("user:%d", record.UserID),
		Kind:       AdminPrincipalKindJWT,
		UserID:     record.UserID,
		Role:       record.Role,
		Version:    record.Version,
		Grants:     cloneAdminGrants(record.Grants),
		Explicit:   true,
		Source:     "users.role+admin_principal_grants",
		ResolvedAt: time.Now().UTC(),
	}, nil
}

// ResolveLegacyAPIKeyPrincipal deliberately never inherits super-admin from the
// bound user's role. The binding must carry explicit scopes.
func (s *AdminPermissionService) ResolveLegacyAPIKeyPrincipal(ctx context.Context, bindingKey string) (*AdminPrincipal, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAdminPermissionEnforceNoBind
	}
	record, err := s.repo.ResolveAdminAPIKeyBinding(ctx, strings.TrimSpace(bindingKey))
	if err != nil {
		return nil, err
	}
	if record == nil || record.UserID <= 0 {
		return nil, ErrAdminPermissionEnforceNoBind
	}
	if record.Status != StatusActive {
		return nil, ErrAdminPermissionEnforceNoBind
	}
	grants := make([]AdminGrant, 0, len(record.Grants))
	for _, grant := range record.Grants {
		if grant.Effect == "" {
			grant.Effect = AdminGrantAllow
		}
		if grant.Permission == "*" || !IsKnownAdminPermission(grant.Permission) || (grant.Effect != AdminGrantAllow && grant.Effect != AdminGrantDeny) {
			continue // legacy keys require explicit, known scopes; wildcard and malformed entries are ignored.
		}
		grants = append(grants, grant)
	}
	if len(grants) == 0 {
		return nil, ErrAdminPermissionEnforceNoBind
	}
	return &AdminPrincipal{
		ID:         "admin-key:" + bindingKey,
		Kind:       AdminPrincipalKindAPIKey,
		UserID:     record.UserID,
		Role:       RoleAdmin,
		Version:    record.Version,
		Grants:     grants,
		Explicit:   true,
		Source:     "admin_api_key_bindings",
		ResolvedAt: time.Now().UTC(),
	}, nil
}

func (s *AdminPermissionService) Authorize(_ context.Context, principal *AdminPrincipal, permission string, scope map[string]any) (bool, error) {
	permission = strings.TrimSpace(permission)
	if !IsKnownAdminPermission(permission) {
		return false, ErrAdminPermissionUnknown
	}
	if principal == nil || principal.UserID <= 0 {
		return false, ErrAdminPermissionDenied
	}
	if principal.Role != RoleAdmin && principal.Role != RoleSuperAdmin {
		return false, ErrAdminPermissionDenied
	}
	// API-key principals are always ordinary administrators and can only use
	// the explicit scopes stored on their own binding. They never inherit the
	// bound user's super-admin role or grants.
	if principal.Kind == AdminPrincipalKindAPIKey && principal.Role != RoleAdmin {
		return false, ErrAdminPermissionDenied
	}
	// Super-admin is an explicit role only. It never results from an implicit
	// "first admin" lookup or a legacy API-key binding.
	if principal.IsSuperAdmin() {
		if principal.Kind != AdminPrincipalKindJWT {
			return false, ErrAdminPermissionDenied
		}
		return true, nil
	}
	allowed := false
	for _, grant := range principal.Grants {
		if grant.Permission != permission && grant.Permission != "*" {
			continue
		}
		if grant.Effect == AdminGrantDeny && len(grant.Scope) == 0 {
			return false, nil
		}
		if !adminScopeContains(grant.Scope, scope) {
			continue
		}
		switch grant.Effect {
		case AdminGrantDeny:
			return false, nil // explicit deny always wins for ordinary admins
		case AdminGrantAllow:
			allowed = true
		}
	}
	return allowed, nil
}

func (s *AdminPermissionService) Capabilities(ctx context.Context, principal *AdminPrincipal, permission string) AdminCapabilities {
	mode := s.Mode()
	cap := AdminCapabilities{Mode: mode}
	if principal == nil {
		cap.DenyReason = "principal_required"
		return cap
	}
	// Both effective modes support the ordinary write path. In disabled mode a
	// human JWT admin retains the legacy role-based compatibility behavior; in
	// enforce mode the same path is grant/scope based. API keys are always
	// explicit-scope principals.
	cap.WritesEnabled = true
	var allowed bool
	var err error
	if strings.HasPrefix(permission, "oidc.") {
		allowed, err = s.CheckPermission(ctx, principal, permission, nil)
	} else {
		allowed, err = s.AuthorizeRequest(ctx, principal, permission, nil)
	}
	if err != nil {
		cap.DenyReason = "permission_denied"
		return cap
	}
	cap.CanWrite = allowed
	if !allowed {
		cap.DenyReason = "permission_denied"
	}
	return cap
}

func (s *AdminPermissionService) CheckPermission(ctx context.Context, principal *AdminPrincipal, permission string, scope map[string]any) (bool, error) {
	if principal == nil {
		return false, ErrAdminPermissionDenied
	}
	fresh, err := s.resolveCurrentPrincipal(ctx, principal)
	if err != nil {
		return false, err
	}
	return authorizeExplicitGrant(fresh, permission, scope)
}

// authorizeExplicitGrant is used by security-sensitive surfaces that must not
// inherit the ordinary human super-admin shortcut.
func authorizeExplicitGrant(principal *AdminPrincipal, permission string, scope map[string]any) (bool, error) {
	permission = strings.TrimSpace(permission)
	if !IsKnownAdminPermission(permission) {
		return false, ErrAdminPermissionUnknown
	}
	if principal == nil || principal.UserID <= 0 || (principal.Role != RoleAdmin && principal.Role != RoleSuperAdmin) {
		return false, ErrAdminPermissionDenied
	}
	allowed := false
	for _, grant := range principal.Grants {
		if strings.HasPrefix(permission, "oidc.") {
			if grant.Permission != permission {
				continue
			}
		} else if grant.Permission != permission && grant.Permission != "*" {
			continue
		}
		if grant.Effect == AdminGrantDeny && len(grant.Scope) == 0 {
			return false, nil
		}
		if !adminScopeContains(grant.Scope, scope) {
			continue
		}
		switch grant.Effect {
		case AdminGrantDeny:
			return false, nil
		case AdminGrantAllow:
			allowed = true
		}
	}
	return allowed, nil
}

// AuthorizeRequest is the ordinary backend authorization helper. OIDC
// management must use CheckPermission directly because it has an independent
// fail-closed boundary. For ordinary admin routes, a live human JWT admin in
// disabled mode keeps the traditional role-based behavior; enforce mode uses
// grants/scopes, and API-key principals always use their own explicit scopes.
func (s *AdminPermissionService) AuthorizeRequest(ctx context.Context, principal *AdminPrincipal, permission string, scope map[string]any) (bool, error) {
	if principal == nil {
		return false, ErrAdminPermissionDenied
	}
	fresh, err := s.resolveCurrentPrincipal(ctx, principal)
	if err != nil {
		return false, err
	}
	if fresh.Kind == AdminPrincipalKindJWT && fresh.Role == RoleAdmin && s.Mode() == AdminPermissionModeDisabled {
		return true, nil
	}
	return s.Authorize(ctx, fresh, permission, scope)
}

func (s *AdminPermissionService) resolveCurrentPrincipal(ctx context.Context, principal *AdminPrincipal) (*AdminPrincipal, error) {
	if s == nil || principal == nil || principal.UserID <= 0 {
		return nil, ErrAdminPermissionDenied
	}
	var (
		fresh *AdminPrincipal
		err   error
	)
	switch principal.Kind {
	case AdminPrincipalKindAPIKey:
		fresh, err = s.ResolveLegacyAPIKeyPrincipal(ctx, strings.TrimPrefix(principal.ID, "admin-key:"))
	case AdminPrincipalKindJWT:
		fresh, err = s.ResolvePrincipal(ctx, principal.UserID)
	default:
		return nil, ErrAdminPermissionDenied
	}
	if err != nil {
		return nil, err
	}
	if fresh == nil || fresh.Kind != principal.Kind || fresh.UserID != principal.UserID || fresh.Version != principal.Version {
		return nil, ErrAdminPermissionDenied
	}
	return fresh, nil
}

func (s *AdminPermissionService) validateTargetAdmin(ctx context.Context, actor *AdminPrincipal, targetUserID int64) (*AdminPrincipalRecord, error) {
	record, err := s.repo.GetAdminPrincipal(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if record == nil || record.UserID <= 0 {
		return nil, ErrAdminPermissionTargetNotAdmin
	}
	if record.Role != RoleAdmin && record.Role != RoleSuperAdmin {
		return nil, ErrAdminPermissionTargetNotAdmin
	}
	if record.Role == RoleSuperAdmin && !actor.IsSuperAdmin() {
		return nil, ErrAdminCannotModifySuperAdmin
	}
	return record, nil
}

func (s *AdminPermissionService) GrantPermission(ctx context.Context, actor *AdminPrincipal, targetUserID int64, permission, effect string, scope map[string]any, reason string) (resultErr error) {
	defer func() { resultErr = normalizeAdminMutationError(resultErr) }()
	permission = strings.TrimSpace(permission)
	effect = strings.ToLower(strings.TrimSpace(effect))
	reason = strings.TrimSpace(reason)
	if !IsKnownAdminPermission(permission) {
		return ErrAdminPermissionUnknown
	}
	if effect != AdminGrantAllow && effect != AdminGrantDeny {
		return fmt.Errorf("invalid grant effect %q", effect)
	}
	if actor == nil || actor.UserID <= 0 {
		return ErrAdminPermissionDenied
	}
	if actor.Kind == AdminPrincipalKindAPIKey && actor.Role == RoleSuperAdmin {
		return ErrAdminPermissionDenied
	}
	if actor.UserID == targetUserID && !allowsSuperAdminOIDCSelfGrant(actor, permission) {
		return ErrAdminPermissionSelfGrant
	}
	if reason == "" {
		return fmt.Errorf("audit reason is required")
	}
	normalizedScope, err := normalizeAdminScope(scope)
	if err != nil {
		return err
	}
	if effect == AdminGrantAllow && len(normalizedScope) == 0 {
		return ErrAdminPermissionScopeInvalid
	}
	target, err := s.validateTargetAdmin(ctx, actor, targetUserID)
	if err != nil {
		return err
	}
	ctx = ContextWithAdminMutationTargetSnapshot(ctx, target.Role, target.Status)
	if !actor.IsSuperAdmin() {
		allowed, err := s.AuthorizeRequest(ctx, actor, permission, normalizedScope)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrAdminPermissionPrivilege
		}
	}
	ctx = ContextWithAdminMutationActorID(ctx, actor.UserID)
	_, err = s.repo.ApplyAdminPermissionChange(ctx, AdminPermissionChange{
		ActorUserID: actor.UserID, ActorIsSuper: actor.IsSuperAdmin(), TargetUserID: targetUserID,
		Action: "grant", Permission: permission, Effect: effect, Scope: normalizedScope, Reason: reason,
		NewValue: map[string]any{"effect": effect, "scope": normalizedScope, "reason": reason},
	})
	return err
}

func (s *AdminPermissionService) RevokePermission(ctx context.Context, actor *AdminPrincipal, targetUserID int64, permission, reason string) (resultErr error) {
	defer func() { resultErr = normalizeAdminMutationError(resultErr) }()
	permission = strings.TrimSpace(permission)
	reason = strings.TrimSpace(reason)
	if !IsKnownAdminPermission(permission) {
		return ErrAdminPermissionUnknown
	}
	if actor == nil || actor.UserID <= 0 {
		return ErrAdminPermissionDenied
	}
	if actor.Kind == AdminPrincipalKindAPIKey && actor.Role == RoleSuperAdmin {
		return ErrAdminPermissionDenied
	}
	if actor.UserID == targetUserID && !allowsSuperAdminOIDCSelfGrant(actor, permission) {
		return ErrAdminPermissionSelfGrant
	}
	if reason == "" {
		return fmt.Errorf("audit reason is required")
	}
	target, err := s.validateTargetAdmin(ctx, actor, targetUserID)
	if err != nil {
		return err
	}
	ctx = ContextWithAdminMutationTargetSnapshot(ctx, target.Role, target.Status)
	if !actor.IsSuperAdmin() {
		allowed, err := s.AuthorizeRequest(ctx, actor, permission, nil)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrAdminPermissionPrivilege
		}
	}
	ctx = ContextWithAdminMutationActorID(ctx, actor.UserID)
	_, err = s.repo.ApplyAdminPermissionChange(ctx, AdminPermissionChange{
		ActorUserID: actor.UserID, ActorIsSuper: actor.IsSuperAdmin(), TargetUserID: targetUserID,
		Action: "revoke", Permission: permission, Reason: reason,
		OldValue: map[string]any{"reason": reason},
	})
	return err
}

// allowsSuperAdminOIDCSelfGrant is the only exception to the self-grant ban.
// oidc.* never inherits the super-admin shortcut (authorizeExplicitGrant), so a
// single-super-admin deployment could otherwise never obtain it. Only a human
// JWT super administrator may grant or revoke oidc.* on itself; the routes
// always require step-up and the repository rechecks the locked actor row.
func allowsSuperAdminOIDCSelfGrant(actor *AdminPrincipal, permission string) bool {
	return actor != nil && actor.Kind == AdminPrincipalKindJWT && actor.IsSuperAdmin() && strings.HasPrefix(permission, "oidc.")
}

func cloneAdminGrants(in []AdminGrant) []AdminGrant {
	out := make([]AdminGrant, len(in))
	copy(out, in)
	return out
}

func normalizeAdminScope(scope map[string]any) (map[string]any, error) {
	if len(scope) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(scope))
	for rawKey, rawValue := range scope {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return nil, ErrAdminPermissionScopeInvalid
		}
		value, ok := normalizeAdminScopeValue(rawValue)
		if !ok {
			return nil, ErrAdminPermissionScopeInvalid
		}
		out[key] = value
	}
	if len(out) == 1 {
		if value, ok := out["*"].(string); ok && value == "*" {
			return out, nil
		}
	}
	return out, nil
}

func normalizeAdminScopeValue(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		return v, v != ""
	case bool, float64, float32, int, int32, int64, uint, uint32, uint64:
		return v, true
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			normalized, ok := normalizeAdminScopeValue(item)
			if !ok {
				return nil, false
			}
			out = append(out, normalized)
		}
		return out, true
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item == "" {
				return nil, false
			}
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

func adminScopeContains(grantScope, requested map[string]any) bool {
	// Missing or malformed scopes are never interpreted as an unrestricted
	// grant. Global allow must be explicit as {"*":"*"}.
	if len(grantScope) == 0 {
		return false
	}
	if len(grantScope) == 1 {
		if value, ok := grantScope["*"].(string); ok && value == "*" {
			return true
		}
	}
	if len(requested) == 0 {
		return false
	}
	for key, granted := range grantScope {
		wanted, ok := requested[key]
		if !ok || !adminScopeValueContains(granted, wanted) {
			return false
		}
	}
	return true
}

func adminScopeValueContains(granted, wanted any) bool {
	if granted == nil || wanted == nil {
		return false
	}
	if value, ok := granted.(string); ok && value == "*" {
		return true
	}
	switch g := granted.(type) {
	case []any:
		for _, item := range g {
			if item == "*" || fmt.Sprint(item) == fmt.Sprint(wanted) {
				return true
			}
		}
	case []int64:
		for _, item := range g {
			if fmt.Sprint(item) == fmt.Sprint(wanted) {
				return true
			}
		}
	default:
		return fmt.Sprint(granted) == fmt.Sprint(wanted)
	}
	return false
}

func (s *AdminPermissionService) ListPermissionCatalog(ctx context.Context) ([]AdminPermissionDefinition, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAdminPermissionUnknown
	}
	return s.repo.ListAdminPermissionDefinitions(ctx)
}

func (s *AdminPermissionService) GetUserPermissionState(ctx context.Context, userID int64) (*AdminPrincipal, error) {
	return s.ResolvePrincipal(ctx, userID)
}

var knownAdminPermissions = map[string]struct{}{
	"checkin.settings.read":          {},
	"checkin.settings.update":        {},
	"accounts.catalog.read":          {},
	"accounts.catalog.write":         {},
	"accounts.credentials.read":      {},
	"accounts.credentials.write":     {},
	"affiliates.read":                {},
	"affiliates.relationship.create": {},
	"audit.export":                   {},
	"audit.read":                     {},
	"oidc.provider.read":             {},
	"oidc.clients.read":              {},
	"oidc.clients.write":             {},
	"oidc.clients.secret.rotate":     {},
	"oidc.clients.disable":           {},
	"oidc.consents.read":             {},
	"oidc.consents.revoke":           {},
	"oidc.keys.read":                 {},
	"oidc.keys.rotate":               {},
	"oidc.keys.revoke":               {},
	"oidc.audit.read":                {},
	"bank.ledger.read":               {},
	"bank.settings.read":             {},
	"bank.settings.update":           {},
	"channels.catalog.read":          {},
	"channels.catalog.write":         {},
	"channels.credentials.read":      {},
	"channels.credentials.write":     {},
	"groups.create":                  {},
	"groups.delete":                  {},
	"groups.dynamic_rates.manage":    {},
	"groups.rates.manage":            {},
	"groups.read":                    {},
	"groups.update":                  {},
	"invites.quota.adjust":           {},
	"mall.fulfill":                   {},
	"mall.orders.read":               {},
	"mall.products.read":             {},
	"mall.products.write":            {},
	"mall.refund":                    {},
	"models.catalog.read":            {},
	"models.catalog.write":           {},
	"ops.manage":                     {},
	"ops.read":                       {},
	"plugins.execute":                {},
	"security.permissions.grant":     {},
	"security.superadmin.assign":     {},
	"system.settings.manage":         {},
	"users.balance.adjust":           {},
	"users.delete":                   {},
	"users.entitlement.manage":       {},
	"users.read":                     {},
	"users.role.assign":              {},
	"users.status":                   {},
	"users.update":                   {},
}

func IsKnownAdminPermission(permission string) bool {
	_, ok := knownAdminPermissions[permission]
	return ok
}
