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

// GetAccessTokenVersionFromContext never substitutes a fresh user version for
// the version that actually authenticated this request. Zero is a valid value.
func GetAccessTokenVersionFromContext(c *gin.Context) (int64, bool) {
	value, exists := c.Get(string(ContextKeyAccessTokenVersion))
	version, ok := value.(int64)
	return version, exists && ok
}
