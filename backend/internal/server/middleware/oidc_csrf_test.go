package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestOIDCAdminCSRFMiddlewareBindsJWTSubjectSessionAndEpoch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-pepper-with-at-least-32-bytes"
	router := gin.New()
	router.Use(func(c *gin.Context) {
		userID := int64(7)
		if c.GetHeader("X-Test-User") == "8" {
			userID = 8
		}
		c.Set(string(ContextKeyUser), AuthSubject{UserID: userID})
		c.Set(string(ContextKeySessionID), c.GetHeader("X-Test-Session"))
		c.Set(string(ContextKeyTokenVersion), int64(3))
		c.Set("auth_method", c.GetHeader("X-Test-Auth"))
		c.Next()
	})
	router.POST("/oidc", OIDCAdminCSRFMiddleware(func(c *gin.Context) string {
		subject, _ := GetAuthSubjectFromContext(c)
		version, _ := GetAuthTokenVersionFromContext(c)
		return service.OIDCAdminCSRFToken(secret, subject.UserID, c.GetString(string(ContextKeySessionID)), version)
	}), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	token := service.OIDCAdminCSRFToken(secret, 7, "session-a", 3)
	request := func(auth, user, session, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/oidc", nil)
		req.Header.Set("X-Test-Auth", auth)
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Session", session)
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: "__Host-sub2_oidc_admin_csrf", Value: csrf})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if got := request(service.AuditAuthMethodJWT, "7", "session-a", token).Code; got != http.StatusNoContent {
		t.Fatalf("valid bound token status = %d, want %d", got, http.StatusNoContent)
	}
	for name, args := range map[string][4]string{
		"different subject": {service.AuditAuthMethodJWT, "8", "session-a", token},
		"different session": {service.AuditAuthMethodJWT, "7", "session-b", token},
		"admin api key":     {service.AuditAuthMethodAdminAPIKey, "7", "session-a", token},
	} {
		if got := request(args[0], args[1], args[2], args[3]).Code; got != http.StatusForbidden {
			t.Fatalf("%s status = %d, want %d", name, got, http.StatusForbidden)
		}
	}
}
