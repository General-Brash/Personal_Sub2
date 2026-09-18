package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterOIDCProviderRoutes(r *gin.Engine, h *handler.Handlers) {
	if h == nil || h.OIDCProvider == nil {
		return
	}
	public := r.Group("")
	public.Use(middleware.RequireOIDCPublicHost(h.OIDCProvider.PublicHost()))
	public.GET("/.well-known/openid-configuration", h.OIDCProvider.Discovery)
	oauth := public.Group("/oauth")
	oauth.GET("/authorize", h.OIDCProvider.Authorize)
	oauth.GET("/login", h.OIDCProvider.LoginPage)
	oauth.POST("/login", h.OIDCProvider.LoginSubmit)
	oauth.GET("/consent", h.OIDCProvider.ConsentPage)
	oauth.POST("/consent", h.OIDCProvider.ConsentSubmit)
	oauth.POST("/token", h.OIDCProvider.Token)
	oauth.GET("/userinfo", h.OIDCProvider.UserInfo)
	oauth.POST("/userinfo", h.OIDCProvider.UserInfo)
	oauth.GET("/jwks", h.OIDCProvider.JWKS)
	oauth.POST("/revoke", h.OIDCProvider.Revoke)
}

func RegisterOIDCProviderAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers, stepUp middleware.StepUpAuthMiddleware) {
	if h == nil || h.Admin == nil || h.Admin.OIDCProvider == nil {
		return
	}
	group := admin.Group("/oidc-provider")
	group.Use(middleware.OIDCAdminNoStore())

	read := func(permission string) *gin.RouterGroup {
		readGroup := group.Group("")
		readGroup.Use(middleware.RequireOIDCAdminPermission(permission))
		return readGroup
	}
	read("oidc.provider.read").GET("/status", h.Admin.OIDCProvider.Status)
	read("oidc.clients.read").GET("/clients", h.Admin.OIDCProvider.ListClients)
	read("oidc.clients.read").GET("/clients/:id", h.Admin.OIDCProvider.GetClient)
	read("oidc.consents.read").GET("/consents", h.Admin.OIDCProvider.ListConsents)
	read("oidc.keys.read").GET("/keys", h.Admin.OIDCProvider.ListKeys)
	read("oidc.audit.read").GET("/audit-events", h.Admin.OIDCProvider.ListAuditEvents)

	csrf := middleware.OIDCAdminCSRFMiddleware(h.Admin.OIDCProvider.AdminCSRFToken)
	alwaysStepUp := middleware.RequireStepUpAlways(stepUp)
	write := func(permission string) *gin.RouterGroup {
		writeGroup := group.Group("")
		// Permission is checked before CSRF/step-up so global disabled/shadow
		// modes cannot turn an OIDC route into an authorization bypass.
		writeGroup.Use(middleware.RequireOIDCAdminPermission(permission))
		writeGroup.Use(csrf)
		writeGroup.Use(gin.HandlerFunc(alwaysStepUp))
		return writeGroup
	}

	write("oidc.clients.write").POST("/clients", h.Admin.OIDCProvider.CreateClient)
	write("oidc.clients.write").PUT("/clients/:id", h.Admin.OIDCProvider.UpdateClient)
	write("oidc.clients.secret.rotate").POST("/clients/:id/secrets", h.Admin.OIDCProvider.RotateSecret)
	write("oidc.clients.secret.rotate").POST("/clients/:id/secrets/:secretID/revoke", h.Admin.OIDCProvider.RevokeSecret)
	write("oidc.clients.disable").POST("/clients/:id/disable", h.Admin.OIDCProvider.SetClientEnabled)
	write("oidc.clients.disable").POST("/clients/:id/enable", h.Admin.OIDCProvider.SetClientEnabled)
	write("oidc.consents.revoke").POST("/consents/:id/revoke", h.Admin.OIDCProvider.RevokeConsent)
	write("oidc.keys.rotate").POST("/keys/rotate", h.Admin.OIDCProvider.RotateKey)
	write("oidc.keys.revoke").POST("/keys/:kid/retire", h.Admin.OIDCProvider.SetKeyStatus)
	write("oidc.keys.revoke").POST("/keys/:kid/revoke", h.Admin.OIDCProvider.SetKeyStatus)
}
