package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminSessionCookieName is the host-only browser compatibility session used
// only by the OIDC Provider administration surface. The existing bearer
// header remains available for non-browser/admin API clients.
const AdminSessionCookieName = "__Host-sub2_admin_session"

func SetAdminSessionCookie(c *gin.Context, token string, maxAge int) {
	if c == nil || token == "" || maxAge <= 0 {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AdminSessionCookieName,
		Value:    token,
		MaxAge:   maxAge,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearAdminSessionCookie(c *gin.Context) {
	if c == nil {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AdminSessionCookieName,
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
