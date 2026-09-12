package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	RoleSuperAdmin = domain.RoleSuperAdmin

	AdminPermissionModeDisabled = "disabled"
	AdminPermissionModeShadow   = "shadow"
	AdminPermissionModeEnforce  = "enforce"

	AdminPrincipalKindJWT       = "jwt"
	AdminPrincipalKindAPIKey    = "admin_api_key"
	AdminPrincipalKindWebSocket = "websocket"
	AdminPrincipalKindPlugin    = "plugin"

	AdminGrantAllow = "allow"
	AdminGrantDeny  = "deny"
)

var (
	ErrAdminPermissionUnknown        = errors.New("unknown admin permission")
	ErrAdminPrincipalNotFound        = errors.New("admin principal not found")
	ErrAdminPermissionDenied         = errors.New("admin permission denied")
	ErrAdminPermissionSelfGrant      = errors.New("administrators cannot grant permissions to themselves")
	ErrAdminPermissionPrivilege      = errors.New("cannot grant a permission the actor does not hold")
	ErrAdminPermissionEnforceNoBind  = errors.New("legacy admin key has no explicit principal binding in enforce mode")
	ErrAdminPermissionScopeInvalid   = errors.New("administrator permission scope is invalid")
	ErrAdminPermissionTargetNotAdmin = errors.New("administrator permission target is not an administrator")
	ErrLastSuperAdmin                = errors.New("cannot disable, demote or delete the last active super administrator")
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
		return AdminPermissionModeShadow
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
	return s.mode
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
	// API-key principals are always ordinary administrators and can only use
	// the explicit scopes stored on their own binding. They never inherit the
	// bound user's super-admin role or grants.
	if principal.Kind == AdminPrincipalKindAPIKey && principal.Role == RoleSuperAdmin {
		return false, ErrAdminPermissionDenied
	}
	// Super-admin is an explicit role only. It never results from an implicit
	// "first admin" lookup or a legacy API-key binding.
	if principal.IsSuperAdmin() {
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
	cap := AdminCapabilities{WritesEnabled: mode == AdminPermissionModeEnforce, Mode: mode}
	if !cap.WritesEnabled {
		cap.DenyReason = "enforcement_disabled"
		return cap
	}
	allowed, err := s.CheckPermission(ctx, principal, permission, nil)
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
	fresh, err := s.ResolvePrincipal(ctx, principal.UserID)
	if err != nil {
		return false, err
	}
	if principal.Kind == AdminPrincipalKindAPIKey {
		fresh, err = s.ResolveLegacyAPIKeyPrincipal(ctx, strings.TrimPrefix(principal.ID, "admin-key:"))
		if err != nil {
			return false, err
		}
	}
	if fresh.Version != principal.Version {
		return false, ErrAdminPermissionDenied
	}
	return s.Authorize(ctx, fresh, permission, scope)
}

func (s *AdminPermissionService) validateTargetAdmin(ctx context.Context, actor *AdminPrincipal, targetUserID int64) error {
	record, err := s.repo.GetAdminPrincipal(ctx, targetUserID)
	if err != nil {
		return err
	}
	if record == nil || record.UserID <= 0 {
		return ErrAdminPermissionTargetNotAdmin
	}
	if record.Role != RoleAdmin && record.Role != RoleSuperAdmin {
		return ErrAdminPermissionTargetNotAdmin
	}
	if record.Role == RoleSuperAdmin && !actor.IsSuperAdmin() {
		return ErrAdminCannotModifySuperAdmin
	}
	return nil
}

func (s *AdminPermissionService) GrantPermission(ctx context.Context, actor *AdminPrincipal, targetUserID int64, permission, effect string, scope map[string]any, reason string) error {
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
	if actor.UserID == targetUserID {
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
	if err := s.validateTargetAdmin(ctx, actor, targetUserID); err != nil {
		return err
	}
	if !actor.IsSuperAdmin() {
		allowed, err := s.Authorize(ctx, actor, permission, normalizedScope)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrAdminPermissionPrivilege
		}
	}
	_, err = s.repo.ApplyAdminPermissionChange(ctx, AdminPermissionChange{
		ActorUserID: actor.UserID, ActorIsSuper: actor.IsSuperAdmin(), TargetUserID: targetUserID,
		Action: "grant", Permission: permission, Effect: effect, Scope: normalizedScope, Reason: reason,
		NewValue: map[string]any{"effect": effect, "scope": normalizedScope, "reason": reason},
	})
	return err
}

func (s *AdminPermissionService) RevokePermission(ctx context.Context, actor *AdminPrincipal, targetUserID int64, permission, reason string) error {
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
	if actor.UserID == targetUserID {
		return ErrAdminPermissionSelfGrant
	}
	if reason == "" {
		return fmt.Errorf("audit reason is required")
	}
	if err := s.validateTargetAdmin(ctx, actor, targetUserID); err != nil {
		return err
	}
	if !actor.IsSuperAdmin() {
		allowed, err := s.Authorize(ctx, actor, permission, nil)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrAdminPermissionPrivilege
		}
	}
	_, err := s.repo.ApplyAdminPermissionChange(ctx, AdminPermissionChange{
		ActorUserID: actor.UserID, ActorIsSuper: actor.IsSuperAdmin(), TargetUserID: targetUserID,
		Action: "revoke", Permission: permission, Reason: reason,
		OldValue: map[string]any{"reason": reason},
	})
	return err
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
	"affiliates.quota.adjust":        {},
	"affiliates.read":                {},
	"affiliates.rebate.replay":       {},
	"affiliates.relationship.create": {},
	"audit.export":                   {},
	"audit.read":                     {},
	"bank.ledger.read":               {},
	"bank.settings.read":             {},
	"bank.settings.update":           {},
	"bank.settlement.retry":          {},
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
	"invites.read":                   {},
	"mall.fulfill":                   {},
	"mall.orders.read":               {},
	"mall.products.read":             {},
	"mall.products.write":            {},
	"mall.refund":                    {},
	"models.catalog.read":            {},
	"models.catalog.write":           {},
	"models.pricing.manage":          {},
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
