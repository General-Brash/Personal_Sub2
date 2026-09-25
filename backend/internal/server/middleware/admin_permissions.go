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
// fails closed. Legacy shadow input has already been normalized to disabled.
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
	if permissionService != nil || mode == service.AdminPermissionModeEnforce {
		AbortWithError(c, http.StatusForbidden, "ADMIN_PRINCIPAL_REQUIRED", "An explicit admin principal binding is required")
		return false
	}
	return true
}

func RequireAdminPermission(permissionService *service.AdminPermissionService, permission string) gin.HandlerFunc {
	permission = strings.TrimSpace(permission)
	return func(c *gin.Context) {
		principal, ok := GetAdminPrincipalFromContext(c)
		if !ok || principal == nil {
			AbortWithError(c, http.StatusForbidden, "PERMISSION_DENIED", "Permission denied")
			return
		}
		if !permissionServiceAuthorizes(c, permissionService, principal, permission, adminRequestScope(c)) {
			AbortWithError(c, http.StatusForbidden, "PERMISSION_DENIED", "Permission denied")
			return
		}
		c.Next()
	}
}

// RequireOIDCAdminPermission is intentionally independent from the global
// ADMIN_PERMISSIONS_MODE. OIDC administration is security-sensitive, so every
// route must have a live explicit principal and permission even when the
// global policy is disabled or shadow-only.
func RequireOIDCAdminPermission(permission string) gin.HandlerFunc {
	permission = strings.TrimSpace(permission)
	return func(c *gin.Context) {
		if permission == "" {
			AbortWithError(c, http.StatusForbidden, "OIDC_PERMISSION_DENIED", "OIDC permission is required")
			return
		}
		principal, ok := GetAdminPrincipalFromContext(c)
		permissionService := service.AdminAuthorizationService(c.Request.Context())
		if permissionService == nil || !ok || principal == nil {
			AbortWithError(c, http.StatusForbidden, "OIDC_PERMISSION_DENIED", "OIDC permission denied")
			return
		}
		allowed, err := permissionService.CheckPermission(c.Request.Context(), principal, permission, adminRequestScope(c))
		if err != nil || !allowed {
			AbortWithError(c, http.StatusForbidden, "OIDC_PERMISSION_DENIED", "OIDC permission denied")
			return
		}
		c.Next()
	}
}

func RequireAdminSuperAdmin(permissionService *service.AdminPermissionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := GetAdminPrincipalFromContext(c)
		if !ok || principal == nil || !principal.IsSuperAdmin() || principal.Kind != service.AdminPrincipalKindJWT {
			AbortWithError(c, http.StatusForbidden, "SUPER_ADMIN_REQUIRED", "Super administrator access required")
			return
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
			if mode == service.AdminPermissionModeEnforce || isAdminAPIKeyRequest(c, nil) {
				AbortWithError(c, http.StatusForbidden, "PERMISSION_NOT_MAPPED", "Admin route has no permission mapping")
				return
			}
			c.Next()
			return
		}
		mode := adminPermissionMode(c, permissionService)
		if mode == service.AdminPermissionModeEnforce || isAdminAPIKeyRequest(c, nil) {
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

func adminPermissionMode(c *gin.Context, permissionService *service.AdminPermissionService) string {
	if permissionService != nil {
		return permissionService.Mode()
	}
	if c != nil {
		if raw, ok := c.Get(string(ContextKeyAdminMode)); ok {
			if mode, ok := raw.(string); ok && mode != "" {
				return service.NormalizeAdminPermissionMode(mode)
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
		return isHumanJWTAdmin(principal)
	}
	allowed, err := permissionService.AuthorizeRequest(c.Request.Context(), principal, permission, scope)
	return err == nil && allowed
}

func isHumanJWTAdmin(principal *service.AdminPrincipal) bool {
	return principal != nil && principal.Kind == service.AdminPrincipalKindJWT &&
		(principal.Role == service.RoleAdmin || principal.Role == service.RoleSuperAdmin)
}

func isAdminAPIKeyRequest(c *gin.Context, principal *service.AdminPrincipal) bool {
	if principal != nil && principal.Kind == service.AdminPrincipalKindAPIKey {
		return true
	}
	return c != nil && c.GetString("auth_method") == service.AuditAuthMethodAdminAPIKey
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
		return SetAdminPrincipal(c, permissionService, nil)
	}
	return SetAdminPrincipal(c, permissionService, principal)
}

func attachLegacyAdminAPIKeyPrincipal(c *gin.Context, permissionService *service.AdminPermissionService) (*service.AdminPrincipal, bool) {
	if permissionService == nil {
		AbortWithError(c, http.StatusForbidden, "ADMIN_KEY_PRINCIPAL_REQUIRED", "Admin API key has no explicit principal binding")
		return nil, false
	}
	principal, err := permissionService.ResolveLegacyAPIKeyPrincipal(c.Request.Context(), "legacy_admin_api_key")
	if err != nil {
		AbortWithError(c, http.StatusUnauthorized, "ADMIN_KEY_PRINCIPAL_REQUIRED", "Admin API key has no explicit principal binding")
		return nil, false
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
