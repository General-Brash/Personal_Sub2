package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
)

// JWTAuthMiddleware JWT 认证中间件类型
type JWTAuthMiddleware gin.HandlerFunc

// OptionalJWTAuthMiddleware 可选 JWT 认证中间件类型：匿名放行，带 token 严格校验
type OptionalJWTAuthMiddleware gin.HandlerFunc

// AdminAuthMiddleware 管理员认证中间件类型
type AdminAuthMiddleware gin.HandlerFunc

// APIKeyAuthMiddleware API Key 认证中间件类型
type APIKeyAuthMiddleware gin.HandlerFunc

// ProviderSet 中间件层的依赖注入
var ProviderSet = wire.NewSet(
	ProvideJWTAuthMiddleware,
	NewOptionalJWTAuthMiddleware,
	ProvideAdminAuthMiddleware,
	NewAPIKeyAuthMiddleware,
	NewAuditLogMiddleware,
	NewStepUpAuthMiddleware,
)

// Wire injects the live policy into every JWT, scoped-key and WS admin entry.
func ProvideAdminAuthMiddleware(auth *service.AuthService, users *service.UserService, settings *service.SettingService, audit *service.AuditLogService, permissions *service.AdminPermissionService) AdminAuthMiddleware {
	return NewAdminAuthMiddleware(auth, users, settings, audit, permissions)
}

func ProvideJWTAuthMiddleware(auth *service.AuthService, users *service.UserService, settings *service.SettingService, audit *service.AuditLogService, permissions *service.AdminPermissionService) JWTAuthMiddleware {
	return NewJWTAuthMiddleware(auth, users, settings, audit, permissions)
}
