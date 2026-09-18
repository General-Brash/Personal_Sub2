package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OIDCAdminCSRFMiddleware is a fail-closed synchronizer-token gate for OIDC
// client/secret/key writes. The expected token is derived from the current
// authenticated JWT subject/session/epoch; it is never stored in the database
// or emitted to logs. Admin API keys have no session binding and are rejected.
func OIDCAdminCSRFMiddleware(expectedToken func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if c.GetString("auth_method") != service.AuditAuthMethodJWT {
			AbortWithError(c, http.StatusForbidden, "OIDC_ADMIN_SESSION_REQUIRED", "OIDC admin writes require a bound administrator session")
			return
		}
		expected := ""
		if expectedToken != nil {
			expected = expectedToken(c)
		}
		cookie, err := c.Cookie("__Host-sub2_oidc_admin_csrf")
		header := c.GetHeader("X-CSRF-Token")
		if expected == "" || err != nil || cookie == "" || header == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(cookie)) != 1 || subtle.ConstantTimeCompare([]byte(expected), []byte(header)) != 1 {
			AbortWithError(c, http.StatusForbidden, "OIDC_CSRF_REQUIRED", "OIDC admin CSRF token required")
			return
		}
		c.Next()
	}
}
