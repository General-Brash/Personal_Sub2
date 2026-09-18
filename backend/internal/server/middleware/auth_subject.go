package middleware

import "github.com/gin-gonic/gin"

// AuthSubject is the minimal authenticated identity stored in gin context.
// Decision: {UserID int64, Concurrency int}
type AuthSubject struct {
	UserID      int64
	Concurrency int
}

func GetAuthSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	value, exists := c.Get(string(ContextKeyUser))
	if !exists {
		return AuthSubject{}, false
	}
	subject, ok := value.(AuthSubject)
	return subject, ok
}

func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(ContextKeyUserRole))
	if !exists {
		return "", false
	}
	role, ok := value.(string)
	return role, ok
}
func GetAuthTokenVersionFromContext(c *gin.Context) (int64, bool) {
	if c == nil {
		return 0, false
	}
	value, exists := c.Get(string(ContextKeyTokenVersion))
	if !exists {
		return 0, false
	}
	version, ok := value.(int64)
	return version, ok
}
