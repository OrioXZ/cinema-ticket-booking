package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	firebaseauth "firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{name: "valid", header: "Bearer token-value", want: "token-value", ok: true},
		{name: "case insensitive scheme", header: "bearer token-value", want: "token-value", ok: true},
		{name: "missing", header: ""},
		{name: "wrong scheme", header: "Basic token-value"},
		{name: "empty", header: "Bearer "},
		{name: "extra spaces", header: "Bearer  token-value"},
		{name: "leading space", header: " Bearer token-value"},
		{name: "tab", header: "Bearer\ttoken-value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := BearerToken(test.header)
			if (err == nil) != test.ok || got != test.want {
				t.Fatalf("BearerToken() = %q, %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "token-value") {
				t.Fatal("error exposed token")
			}
		})
	}
}

func TestNormalizeRole(t *testing.T) {
	if NormalizeRole("admin") != RoleAdmin {
		t.Fatal("ADMIN claim was not normalized")
	}
	for _, value := range []any{"OWNER", "", 12, nil, "USER"} {
		if got := NormalizeRole(value); got != RoleUser {
			t.Fatalf("NormalizeRole(%v) = %q, want USER", value, got)
		}
	}
}

func TestIdentityFromFirebaseToken(t *testing.T) {
	token := &firebaseauth.Token{
		UID: "firebase-user",
		Claims: map[string]any{
			"role":  "ADMIN",
			"email": " user@example.com ",
			"name":  "Cinema Admin",
		},
	}
	got := identityFromFirebaseToken(token)
	if got.UserID != "firebase-user" || got.Role != RoleAdmin ||
		got.Email != "user@example.com" || got.DisplayName != "Cinema Admin" {
		t.Fatalf("identityFromFirebaseToken() = %+v", got)
	}
	token.Claims["role"] = []string{"ADMIN"}
	if got := identityFromFirebaseToken(token); got.Role != RoleUser {
		t.Fatalf("malformed role = %q, want USER", got.Role)
	}
}

func TestFirebaseMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		verifier   TokenVerifier
		header     string
		devUser    string
		wantStatus int
		wantUser   string
		wantRole   Role
	}{
		{
			name: "user", verifier: fakeVerifier{identity: Identity{UserID: "firebase-user", Role: RoleUser}},
			header: "Bearer valid", devUser: "spoofed", wantStatus: 200, wantUser: "firebase-user", wantRole: RoleUser,
		},
		{
			name: "admin", verifier: fakeVerifier{identity: Identity{UserID: "firebase-admin", Role: RoleAdmin}},
			header: "Bearer valid", wantStatus: 200, wantUser: "firebase-admin", wantRole: RoleAdmin,
		},
		{name: "missing token", verifier: fakeVerifier{}, wantStatus: 401},
		{name: "invalid token", verifier: fakeVerifier{err: ErrInvalidToken}, header: "Bearer secret-token", wantStatus: 401},
		{name: "unavailable", verifier: fakeVerifier{err: ErrUnavailable}, header: "Bearer secret-token", wantStatus: 500},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(NewMiddleware("firebase", test.verifier).RequireAuthenticated())
			router.GET("/", func(c *gin.Context) {
				got, _ := FromContext(c)
				c.JSON(200, got)
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Authorization", test.header)
			request.Header.Set("X-User-ID", test.devUser)
			request.Header.Set("X-User-Role", "ADMIN")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantUser != "" && (!strings.Contains(response.Body.String(), test.wantUser) ||
				!strings.Contains(response.Body.String(), string(test.wantRole))) {
				t.Fatalf("response identity = %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret-token") {
				t.Fatal("response exposed bearer token")
			}
		})
	}
}

func TestDevelopmentMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		userID     string
		role       string
		wantStatus int
		wantRole   Role
	}{
		{name: "valid", userID: "user-1", role: "ADMIN", wantStatus: 200, wantRole: RoleAdmin},
		{name: "default role", userID: "user-1", wantStatus: 200, wantRole: RoleUser},
		{name: "missing user", role: "ADMIN", wantStatus: 401},
		{name: "invalid role", userID: "user-1", role: "OWNER", wantStatus: 401},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(NewMiddleware("development", nil).RequireAuthenticated())
			router.GET("/", func(c *gin.Context) {
				got, _ := FromContext(c)
				c.String(200, string(got.Role))
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-User-ID", test.userID)
			request.Header.Set("X-User-Role", test.role)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantRole != "" && response.Body.String() != string(test.wantRole) {
				t.Fatalf("role = %q, want %q", response.Body.String(), test.wantRole)
			}
		})
	}
}

func TestRequireRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		role       Role
		wantStatus int
	}{{RoleUser, 403}, {RoleAdmin, 200}} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(contextKey, Identity{UserID: "user", Role: test.role})
		}, RequireRole(RoleAdmin))
		router.GET("/", func(c *gin.Context) { c.Status(200) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != test.wantStatus {
			t.Fatalf("role %q status = %d, want %d", test.role, response.Code, test.wantStatus)
		}
	}
}

type fakeVerifier struct {
	identity Identity
	err      error
}

func (f fakeVerifier) VerifyIDToken(context.Context, string) (Identity, error) {
	if f.err != nil {
		return Identity{}, f.err
	}
	if f.identity.Role == "" {
		f.identity.Role = RoleUser
	}
	return f.identity, nil
}
