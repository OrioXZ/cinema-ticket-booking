package identity

import (
	"context"
	"errors"
	"strings"

	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

type FirebaseVerifier struct {
	client *firebaseauth.Client
}

func NewFirebaseVerifier(ctx context.Context, projectID string) (*FirebaseVerifier, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("FIREBASE_PROJECT_ID is required in firebase authentication mode")
	}
	credentials, err := google.FindDefaultCredentials(
		ctx,
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return nil, errors.New("Firebase application credentials are unavailable")
	}
	app, err := firebase.NewApp(
		ctx,
		&firebase.Config{ProjectID: projectID},
		option.WithCredentials(credentials),
	)
	if err != nil {
		return nil, errors.New("initialize Firebase application")
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, errors.New("initialize Firebase authentication")
	}
	return &FirebaseVerifier{client: client}, nil
}

func (v *FirebaseVerifier) VerifyIDToken(ctx context.Context, rawToken string) (Identity, error) {
	token, err := v.client.VerifyIDToken(ctx, rawToken)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Identity{}, ErrUnavailable
		}
		if firebaseauth.IsIDTokenInvalid(err) {
			return Identity{}, ErrInvalidToken
		}
		return Identity{}, ErrUnavailable
	}
	return identityFromFirebaseToken(token), nil
}

func identityFromFirebaseToken(token *firebaseauth.Token) Identity {
	if token == nil {
		return Identity{Role: RoleUser}
	}
	return Identity{
		UserID:      strings.TrimSpace(token.UID),
		Role:        NormalizeRole(token.Claims["role"]),
		Email:       stringClaim(token.Claims, "email"),
		DisplayName: stringClaim(token.Claims, "name"),
	}
}

func stringClaim(claims map[string]any, key string) string {
	value, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
