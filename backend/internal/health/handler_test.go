package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestGetReturnsOKWhenDependenciesAreUp(t *testing.T) {
	recorder := performHealthRequest(t, nil, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestGetReturnsUnavailableWhenDependencyIsDown(t *testing.T) {
	tests := []struct {
		name        string
		mongoErr    error
		redisErr    error
		downService string
	}{
		{
			name:        "MongoDB unavailable",
			mongoErr:    errors.New("mongodb://admin:secret@mongodb:27017 unavailable"),
			downService: "mongodb",
		},
		{
			name:        "Redis unavailable",
			redisErr:    errors.New("redis://:secret@redis:6379 unavailable"),
			downService: "redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := performHealthRequest(t, tt.mongoErr, tt.redisErr)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
			}

			var body struct {
				Status   string `json:"status"`
				Services map[string]struct {
					Status string `json:"status"`
				} `json:"services"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Status != "degraded" {
				t.Fatalf("status body = %q, want degraded", body.Status)
			}
			if body.Services[tt.downService].Status != "down" {
				t.Fatalf("%s status = %q, want down", tt.downService, body.Services[tt.downService].Status)
			}

			responseBody := recorder.Body.String()
			for _, sensitive := range []string{"secret", "mongodb://", "redis://", "unavailable"} {
				if strings.Contains(responseBody, sensitive) {
					t.Fatalf("response exposes sensitive dependency detail %q: %s", sensitive, responseBody)
				}
			}
		})
	}
}

func performHealthRequest(t *testing.T, mongoErr, redisErr error) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	handler := NewHandler(
		pingerFunc(func(context.Context) error { return mongoErr }),
		pingerFunc(func(context.Context) error { return redisErr }),
		time.Second,
	)

	router := gin.New()
	router.GET("/health", handler.Get)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
