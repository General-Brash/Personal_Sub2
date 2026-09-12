package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	ContextKeyAdminPrincipal ContextKey = "admin_principal"
	ContextKeyAdminMode      ContextKey = "admin_permission_mode"
)

type AdminPermissionMiddleware gin.HandlerFunc

func NewAdminPermissionMiddleware(permissionService *service.AdminPermissionService) AdminPermissionMiddleware {
	return AdminPermissionMiddleware(RequireMappedAdminPermission(permissionService))
}

func GetAdminPrincipalFromContext(c *gin.Context) (*service.AdminPrincipal, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(string(ContextKeyAdminPrincipal))
	if !ok {
		return nil, false
	}
	principal, ok := value.(*service.AdminPrincipal)
	return principal, ok
}

// SetAdminPrincipal resolves the live principal/version. In disabled mode a
// missing table deployment still retains legacy behavior; in enforce mode it
// fails closed. Shadow mode audits the would-be denial without blocking.
func SetAdminPrincipal(c *gin.Context, permissionService *service.AdminPermissionService, principal *service.AdminPrincipal) bool {
	if c == nil {
		return false
	}
	mode := service.AdminPermissionModeDisabled
	if permissionService != nil {
		mode = permissionService.Mode()
	}
	c.Set(string(ContextKeyAdminMode), mode)
	if principal != nil {
		c.Set(string(ContextKeyAdminPrincipal), principal)
		if c.Request != nil {
			ctx := service.ContextWithAdminPrincipal(c.Request.Context(), principal)
			ctx = service.ContextWithAdminAuthorization(ctx, permissionService)
			c.Request = c.Request.WithContext(ctx)
		}
		return true
	}
	if mode == service.AdminPermissionModeEnforce {
		AbortWithError(c, http.StatusForbidden, "ADMIN_PRINCIPAL_REQUIRED", "An explicit admin principal binding is required")
		return false
	}
	return true
}

func RequireAdminPermission(permissionService *service.AdminPermissionService, permission string) gin.HandlerFunc {
	permission = strings.TrimSpace(permission)
	return func(c *gin.Context) {
		mode := adminPermissionMode(c, permissionService)
		principal, ok := GetAdminPrincipalFromContext(c)
		if !ok {
			if mode == service.AdminPermissionModeEnforce {
				AbortWithError(c, http.StatusForbidden, "PERMISSION_DENIED", "Permission denied")
				return
			}
			c.Next()
			return
		}
		if !permissionServiceAuthorizes(c, permissionService, principal, permission, adminRequestScope(c)) {
			if mode == service.AdminPermissionModeEnforce {
				AbortWithError(c, http.StatusForbidden, "PERMISSION_DENIED", "Permission denied")
				return
			}
			// Shadow mode records only the canonical permission name. A full
			// audit sink is intentionally left to the request audit middleware.
			c.Set("admin_permission_shadow_denied", permission)
		}
		c.Next()
	}
}

func RequireAdminSuperAdmin(permissionService *service.AdminPermissionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mode := adminPermissionMode(c, permissionService)
		principal, ok := GetAdminPrincipalFromContext(c)
		if !ok || principal == nil || !principal.IsSuperAdmin() {
			if mode == service.AdminPermissionModeEnforce {
				AbortWithError(c, http.StatusForbidden, "SUPER_ADMIN_REQUIRED", "Super administrator access required")
				return
			}
		}
		c.Next()
	}
}

// RequireMappedAdminPermission is the fail-closed global admin mapping. Attach
// it to the admin group after AdminAuthMiddleware. Every admin route must have
// either an exact mapping here or an explicit RequireAdminPermission route
// middleware. Unknown paths are denied in enforce mode.
func RequireMappedAdminPermission(permissionService *service.AdminPermissionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		permission, ok := LookupAdminRoutePermission(c.Request.Method, c.FullPath())
		if !ok {
			mode := adminPermissionMode(c, permissionService)
			if mode == service.AdminPermissionModeEnforce {
				AbortWithError(c, http.StatusForbidden, "PERMISSION_NOT_MAPPED", "Admin route has no permission mapping")
				return
			}
			c.Next()
			return
		}
		mode := adminPermissionMode(c, permissionService)
		if mode == service.AdminPermissionModeEnforce {
			if !authorizeAdminSensitiveFields(c, permissionService, permission) {
				return
			}
		}
		RequireAdminPermission(permissionService, permission)(c)
	}
}

// LookupAdminRoutePermission uses Gin's route pattern (c.FullPath()), not the
// raw request path, so parameterized routes cannot be bypassed by path tricks.
func LookupAdminRoutePermission(method, fullPath string) (string, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	fullPath = strings.TrimSpace(fullPath)
	if method == "" || fullPath == "" {
		return "", false
	}
	if permission, ok := adminRoutePermissionMap[method+" "+fullPath]; ok {
		return permission, true
	}
	return "", false
}

func AdminPermissionCatalog() []service.AdminPermissionDefinition {
	return []service.AdminPermissionDefinition{
		{Permission: "users.read", Resource: "users", Action: "read", Description: "Read users"},
		{Permission: "users.update", Resource: "users", Action: "update", Description: "Edit non-sensitive user fields"},
		{Permission: "users.status", Resource: "users", Action: "status", Sensitive: true, Description: "Enable or disable users"},
		{Permission: "users.delete", Resource: "users", Action: "delete", Sensitive: true, Description: "Delete users"},
		{Permission: "users.balance.adjust", Resource: "users", Action: "balance.adjust", Sensitive: true, Description: "Adjust user balance"},
		{Permission: "users.role.assign", Resource: "users", Action: "role.assign", Sensitive: true, Description: "Assign ordinary admin role"},
		{Permission: "users.entitlement.manage", Resource: "users", Action: "entitlement.manage", Sensitive: true, Description: "Manage user entitlement"},
		{Permission: "security.permissions.grant", Resource: "security", Action: "permissions.grant", Sensitive: true, Description: "Grant or revoke permissions"},
		{Permission: "security.superadmin.assign", Resource: "security", Action: "superadmin.assign", Sensitive: true, Description: "Assign or remove super administrators"},
		{Permission: "audit.read", Resource: "audit", Action: "read", Sensitive: true, Description: "Read audit data"},
		{Permission: "audit.export", Resource: "audit", Action: "export", Sensitive: true, Description: "Export audit data"},
	}
}

func IsSensitiveAdminPermission(permission string) bool {
	for _, item := range AdminPermissionCatalog() {
		if item.Permission == permission {
			return item.Sensitive
		}
	}
	// Unknown permissions are treated as sensitive and fail closed.
	return true
}

func adminPermissionMode(c *gin.Context, permissionService *service.AdminPermissionService) string {
	if permissionService != nil {
		return permissionService.Mode()
	}
	if c != nil {
		if raw, ok := c.Get(string(ContextKeyAdminMode)); ok {
			if mode, ok := raw.(string); ok && mode != "" {
				return mode
			}
		}
	}
	return service.AdminPermissionModeDisabled
}

func permissionServiceAuthorizes(c *gin.Context, permissionService *service.AdminPermissionService, principal *service.AdminPrincipal, permission string, scope map[string]any) bool {
	if strings.TrimSpace(permission) == "" {
		return false
	}
	if permissionService == nil {
		return principal != nil && (principal.Role == service.RoleAdmin || principal.Role == service.RoleSuperAdmin)
	}
	allowed, err := permissionService.CheckPermission(c.Request.Context(), principal, permission, scope)
	return err == nil && allowed
}

func attachJWTAdminPrincipal(c *gin.Context, permissionService *service.AdminPermissionService, user *service.User) bool {
	if c == nil || user == nil {
		return false
	}
	if permissionService == nil {
		return SetAdminPrincipal(c, nil, &service.AdminPrincipal{
			ID: "legacy-user", Kind: service.AdminPrincipalKindJWT, UserID: user.ID,
			Role: user.Role, Explicit: false, Source: "legacy_role",
		})
	}
	principal, err := permissionService.ResolvePrincipal(c.Request.Context(), user.ID)
	if err != nil {
		if permissionService.Mode() == service.AdminPermissionModeEnforce {
			AbortWithError(c, http.StatusForbidden, "ADMIN_PRINCIPAL_REQUIRED", "An explicit admin principal binding is required")
			return false
		}
		c.Set("admin_permission_shadow_denied", "principal_missing")
		return SetAdminPrincipal(c, permissionService, nil)
	}
	return SetAdminPrincipal(c, permissionService, principal)
}

func attachLegacyAdminAPIKeyPrincipal(c *gin.Context, permissionService *service.AdminPermissionService) (*service.AdminPrincipal, bool) {
	if permissionService == nil {
		return nil, true
	}
	principal, err := permissionService.ResolveLegacyAPIKeyPrincipal(c.Request.Context(), "legacy_admin_api_key")
	if err != nil {
		if permissionService.Mode() == service.AdminPermissionModeEnforce {
			AbortWithError(c, http.StatusUnauthorized, "ADMIN_KEY_PRINCIPAL_REQUIRED", "Legacy admin API key has no explicit principal binding")
			return nil, false
		}
		c.Set("admin_permission_shadow_denied", "legacy_admin_api_key_unbound")
		return nil, true
	}
	return principal, true
}

func firstAdminPermissionService(services []*service.AdminPermissionService) *service.AdminPermissionService {
	for _, candidate := range services {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}
