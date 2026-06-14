package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

const contextKey = "request_identity"

var (
	ErrInvalidToken = errors.New("invalid authentication token")
	ErrUnavailable  = errors.New("authentication service unavailable")
)

type Identity struct {
	UserID      string
	Role        Role
	Email       string
	DisplayName string
}

type TokenVerifier interface {
	VerifyIDToken(context.Context, string) (Identity, error)
}

type Middleware struct {
	mode     string
	verifier TokenVerifier
}

func NewMiddleware(mode string, verifier TokenVerifier) *Middleware {
	return &Middleware{mode: strings.ToLower(strings.TrimSpace(mode)), verifier: verifier}
}

func (m *Middleware) RequireAuthenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		var (
			requestIdentity Identity
			err             error
		)
		switch m.mode {
		case "development":
			requestIdentity, err = developmentIdentity(c)
		case "firebase":
			var rawToken string
			rawToken, err = BearerToken(c.GetHeader("Authorization"))
			if err == nil {
				if m.verifier == nil {
					err = ErrUnavailable
				} else {
					requestIdentity, err = m.verifier.VerifyIDToken(c.Request.Context(), rawToken)
				}
			}
		default:
			err = ErrUnavailable
		}
		if err != nil {
			writeAuthError(c, err)
			return
		}
		if requestIdentity.UserID == "" || !requestIdentity.Role.Valid() {
			writeAuthError(c, ErrInvalidToken)
			return
		}
		c.Set(contextKey, requestIdentity)
		c.Next()
	}
}

func RequireRole(roles ...Role) gin.HandlerFunc {
	allowed := make(map[Role]struct{}, len(roles))
	for _, role := range roles {
		if role.Valid() {
			allowed[role] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		requestIdentity, ok := FromContext(c)
		if !ok {
			writeAuthError(c, ErrInvalidToken)
			return
		}
		if _, ok := allowed[requestIdentity.Role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "FORBIDDEN",
					"message": "the authenticated user does not have permission",
				},
			})
			return
		}
		c.Next()
	}
}

func FromContext(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(contextKey)
	if !ok {
		return Identity{}, false
	}
	requestIdentity, ok := value.(Identity)
	return requestIdentity, ok && requestIdentity.UserID != "" && requestIdentity.Role.Valid()
}

func NormalizeRole(value any) Role {
	role, ok := value.(string)
	if !ok {
		return RoleUser
	}
	switch Role(strings.ToUpper(strings.TrimSpace(role))) {
	case RoleAdmin:
		return RoleAdmin
	default:
		return RoleUser
	}
}

func (r Role) Valid() bool {
	return r == RoleUser || r == RoleAdmin
}

func BearerToken(header string) (string, error) {
	if header == "" {
		return "", ErrInvalidToken
	}
	parts := strings.Split(header, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" ||
		strings.TrimSpace(parts[1]) != parts[1] {
		return "", ErrInvalidToken
	}
	return parts[1], nil
}

func developmentIdentity(c *gin.Context) (Identity, error) {
	userID := strings.TrimSpace(c.GetHeader("X-User-ID"))
	if userID == "" {
		return Identity{}, ErrInvalidToken
	}
	rawRole := strings.TrimSpace(c.GetHeader("X-User-Role"))
	if rawRole == "" {
		rawRole = string(RoleUser)
	}
	role := Role(strings.ToUpper(rawRole))
	if !role.Valid() {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: userID, Role: role}, nil
}

func writeAuthError(c *gin.Context, err error) {
	if errors.Is(err, ErrUnavailable) {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "AUTH_UNAVAILABLE",
				"message": "authentication is temporarily unavailable",
			},
		})
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"code":    "INVALID_AUTH_TOKEN",
			"message": "valid authentication credentials are required",
		},
	})
}
