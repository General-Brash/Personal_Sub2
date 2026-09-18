package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequireOIDCPublicHost keeps the Provider endpoint family bound to the
// configured issuer host. The application must not construct issuer-bearing
// redirects or metadata for an arbitrary Host header, even when a reverse
// proxy is misconfigured or the backend is reached directly.
func RequireOIDCPublicHost(publicHost string) gin.HandlerFunc {
	expected := strings.TrimSpace(publicHost)
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || !matchesOIDCPublicHost(c.Request.Host, expected) {
			c.Header("Cache-Control", "no-store, no-cache, max-age=0, private")
			c.Header("Pragma", "no-cache")
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

// OIDCAdminNoStore prevents browsers, proxies, and CDNs from retaining
// Provider client, consent, key, audit, and one-time-secret responses.
func OIDCAdminNoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, max-age=0, private")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func matchesOIDCPublicHost(rawHost, expectedHost string) bool {
	rawHost = strings.TrimSpace(rawHost)
	expectedHost = strings.TrimSpace(expectedHost)
	if rawHost == "" || expectedHost == "" {
		return false
	}
	if strings.EqualFold(rawHost, expectedHost) {
		return true
	}
	host, _, err := net.SplitHostPort(rawHost)
	return err == nil && strings.EqualFold(host, expectedHost)
}
