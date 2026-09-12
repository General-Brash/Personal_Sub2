package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func adminRequestScope(c *gin.Context) map[string]any {
	scope := map[string]any{}
	for _, param := range c.Params {
		if id, err := strconv.ParseInt(param.Value, 10, 64); err == nil && id > 0 {
			scope[param.Key] = id
		}
	}
	if id, ok := scope["id"]; ok {
		path := c.FullPath()
		for _, resource := range []string{"users", "groups", "accounts", "channels", "orders", "plugins"} {
			if strings.Contains(path, "/"+resource+"/:id") {
				scope[strings.TrimSuffix(resource, "s")+"_id"] = id
				scope[resource+"_ids"] = id
			}
		}
	}
	return scope
}

func adminSuperOnly(permission string) bool {
	return strings.HasPrefix(permission, "security.") || strings.Contains(permission, "credentials.") || permission == "system.settings.manage"
}

func authorizeAdminSensitiveFields(c *gin.Context, svc *service.AdminPermissionService, basePermission string) bool {
	principal, ok := GetAdminPrincipalFromContext(c)
	if !ok {
		AbortWithError(c, 403, "PERMISSION_DENIED", "Permission denied")
		return false
	}
	check := func(permission string) bool {
		if adminSuperOnly(permission) && (!principal.IsSuperAdmin() || principal.Kind == service.AdminPrincipalKindAPIKey) {
			AbortWithError(c, 403, "SUPER_ADMIN_REQUIRED", "Super administrator identity required")
			return false
		}
		allowed, err := svc.CheckPermission(c.Request.Context(), principal, permission, adminRequestScope(c))
		if err != nil || !allowed {
			AbortWithError(c, 403, "PERMISSION_DENIED", "Permission denied for sensitive field")
			return false
		}
		return true
	}
	if adminSuperOnly(basePermission) && !check(basePermission) {
		return false
	}
	path, method := c.FullPath(), c.Request.Method
	mutates := method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
	assertTarget := func(id int64) bool {
		if id <= 0 {
			return true
		}
		if err := svc.AssertMutableTarget(c.Request.Context(), principal, id); err != nil {
			AbortWithError(c, 403, "TARGET_USER_PROTECTED", "Cannot modify this user")
			return false
		}
		return true
	}
	userResource := strings.HasPrefix(path, "/api/v1/admin/users") || strings.Contains(path, "/affiliates/users/") || strings.HasPrefix(path, "/api/v1/admin/entitlements") || strings.HasPrefix(path, "/api/v1/admin/invitations")
	if mutates && userResource {
		if strings.Contains(path, "/users/:id") {
			id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
			if !assertTarget(id) {
				return false
			}
		}
		if raw := c.Param("user_id"); raw != "" {
			id, _ := strconv.ParseInt(raw, 10, 64)
			if !assertTarget(id) {
				return false
			}
		}
	}

	checksUserMutation := mutates && userResource
	checksSensitiveRoute := checksUserMutation ||
		(path == "/api/v1/admin/users" && method == http.MethodPost) ||
		(path == "/api/v1/admin/users/:id" && method == http.MethodPut) ||
		(strings.HasPrefix(path, "/api/v1/admin/groups") && (method == http.MethodPut || method == http.MethodPost))
	if !checksSensitiveRoute {
		return true
	}
	if c.Request.Body == nil {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20+1))
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil || len(data) > 1<<20 {
		AbortWithError(c, 400, "INVALID_REQUEST", "Request body is too large or invalid")
		return false
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(data, &body) != nil {
		return true
	} // handler validates malformed bodies
	if checksUserMutation {
		var ids []int64
		if raw := body["user_ids"]; len(raw) > 0 {
			if json.Unmarshal(raw, &ids) != nil || len(ids) > 1000 {
				AbortWithError(c, 400, "INVALID_USER_IDS", "Invalid target users")
				return false
			}
		}
		for _, name := range []string{"target_user_id", "invitee_user_id"} {
			if raw := body[name]; len(raw) > 0 {
				var id int64
				if json.Unmarshal(raw, &id) == nil {
					ids = append(ids, id)
				}
			}
		}
		for _, id := range ids {
			if !assertTarget(id) {
				return false
			}
		}
	}
	fields := map[string]string{}
	if strings.HasPrefix(path, "/api/v1/admin/users") {
		fields = map[string]string{"role": "users.role.assign", "status": "users.status", "balance": "users.balance.adjust", "password": "users.credentials.write", "email": "users.credentials.write", "allowed_groups": "users.entitlement.manage", "restrict_public_groups": "users.entitlement.manage", "group_rates": "groups.rates.manage"}
	} else {
		for _, field := range []string{"rate_multiplier", "image_rate_multiplier", "video_rate_multiplier", "image_rate_independent", "peak_rate_multiplier", "peak_rate_enabled", "peak_start", "peak_end"} {
			fields[field] = "groups.rates.manage"
		}
	}
	for field, permission := range fields {
		raw, present := body[field]
		if !present || bytes.Equal(raw, []byte("null")) {
			continue
		}
		if field == "role" {
			var role string
			_ = json.Unmarshal(raw, &role)
			if role == service.RoleSuperAdmin {
				permission = "security.superadmin.assign"
			}
		}
		if !check(permission) {
			return false
		}
	}
	return true
}
