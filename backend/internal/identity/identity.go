package identity

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const contextKey = "request_identity"

type Identity struct {
	UserID string
	Role   string
}

func DevelopmentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := strings.TrimSpace(c.GetHeader("X-User-ID"))
		if userID != "" {
			role := strings.ToUpper(strings.TrimSpace(c.GetHeader("X-User-Role")))
			if role == "" {
				role = "USER"
			}
			c.Set(contextKey, Identity{UserID: userID, Role: role})
		}
		c.Next()
	}
}

func Require(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(contextKey)
	if !ok {
		writeUnauthorized(c)
		return Identity{}, false
	}

	requestIdentity, ok := value.(Identity)
	if !ok || requestIdentity.UserID == "" {
		writeUnauthorized(c)
		return Identity{}, false
	}
	return requestIdentity, true
}

func writeUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"code":    "IDENTITY_REQUIRED",
			"message": "X-User-ID is required during Phase 2 development",
		},
	})
}
