package booking

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/identity"
)

func TestHandlerErrorContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		method     string
		path       string
		userID     string
		body       string
		setup      func(*fakeBookings, *fakeLocks)
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing identity",
			method:     http.MethodPost,
			path:       "/api/showtimes/showtime-1/seats/A1/lock",
			wantStatus: http.StatusUnauthorized,
			wantCode:   "IDENTITY_REQUIRED",
		},
		{
			name:       "invalid seat",
			method:     http.MethodPost,
			path:       "/api/showtimes/showtime-1/seats/Z9/lock",
			userID:     "user-1",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_SEAT",
		},
		{
			name:       "unknown showtime",
			method:     http.MethodPost,
			path:       "/api/showtimes/missing/seats/A1/lock",
			userID:     "user-1",
			wantStatus: http.StatusNotFound,
			wantCode:   "SHOWTIME_NOT_FOUND",
		},
		{
			name:   "seat conflict",
			method: http.MethodPost,
			path:   "/api/showtimes/showtime-1/seats/A1/lock",
			userID: "user-2",
			setup: func(_ *fakeBookings, locks *fakeLocks) {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, lockTTL)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "SEAT_CONFLICT",
		},
		{
			name:   "booked seat conflict",
			method: http.MethodPost,
			path:   "/api/showtimes/showtime-1/seats/A1/lock",
			userID: "user-2",
			setup: func(bookings *fakeBookings, _ *fakeLocks) {
				bookings.items = append(bookings.items, Booking{
					ID: "booking-1", ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", Status: BookingStatusConfirmed,
				})
			},
			wantStatus: http.StatusConflict,
			wantCode:   "SEAT_CONFLICT",
		},
		{
			name:       "missing lock",
			method:     http.MethodPost,
			path:       "/api/bookings/confirm",
			userID:     "user-1",
			body:       `{"showtime_id":"showtime-1","seat_no":"A1","ownership_token":"token"}`,
			wantStatus: http.StatusConflict,
			wantCode:   "LOCK_NOT_ACTIVE",
		},
		{
			name:   "expired lock",
			method: http.MethodPost,
			path:   "/api/bookings/confirm",
			userID: "user-1",
			body:   `{"showtime_id":"showtime-1","seat_no":"A1","ownership_token":"token"}`,
			setup: func(_ *fakeBookings, locks *fakeLocks) {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, -time.Second)
			},
			wantStatus: http.StatusConflict,
			wantCode:   "LOCK_NOT_ACTIVE",
		},
		{
			name:   "wrong lock owner",
			method: http.MethodPost,
			path:   "/api/bookings/confirm",
			userID: "user-2",
			body:   `{"showtime_id":"showtime-1","seat_no":"A1","ownership_token":"token"}`,
			setup: func(_ *fakeBookings, locks *fakeLocks) {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, lockTTL)
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "LOCK_NOT_OWNED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, bookings, locks := newTestRouter()
			if test.setup != nil {
				test.setup(bookings, locks)
			}
			response := performRequest(router, test.method, test.path, test.userID, test.body)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if got := responseErrorCode(t, response); got != test.wantCode {
				t.Fatalf("error code = %q, want %q", got, test.wantCode)
			}
		})
	}
}

func TestHandlerSuccessStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("lock acquisition", func(t *testing.T) {
		router, _, _ := newTestRouter()
		response := performRequest(
			router,
			http.MethodPost,
			"/api/showtimes/showtime-1/seats/A1/lock",
			"user-1",
			"",
		)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("confirmation", func(t *testing.T) {
		router, _, locks := newTestRouter()
		locks.put(SeatLock{
			ShowtimeID: "showtime-1", SeatNo: "A1",
			UserID: "user-1", OwnershipToken: "token",
		}, lockTTL)
		response := performRequest(
			router,
			http.MethodPost,
			"/api/bookings/confirm",
			"user-1",
			`{"showtime_id":"showtime-1","seat_no":"A1","ownership_token":"token"}`,
		)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("release", func(t *testing.T) {
		router, _, locks := newTestRouter()
		locks.put(SeatLock{
			ShowtimeID: "showtime-1", SeatNo: "A1",
			UserID: "user-1", OwnershipToken: "token",
		}, lockTTL)
		response := performRequest(
			router,
			http.MethodDelete,
			"/api/showtimes/showtime-1/seats/A1/lock",
			"user-1",
			`{"ownership_token":"token"}`,
		)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204; body = %s", response.Code, response.Body.String())
		}
	})
}

func TestHandlerDoesNotExposeInfrastructureErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name  string
		setup func(*fakeBookings, *fakeLocks)
	}{
		{
			name: "MongoDB error",
			setup: func(bookings *fakeBookings, locks *fakeLocks) {
				bookings.createErr = errors.New("mongodb://admin:secret@mongo durable failure")
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, lockTTL)
			},
		},
		{
			name: "Redis error",
			setup: func(_ *fakeBookings, locks *fakeLocks) {
				locks.verifyErr = errors.New("redis://:secret@redis ownership failure")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, bookings, locks := newTestRouter()
			test.setup(bookings, locks)
			response := performRequest(
				router,
				http.MethodPost,
				"/api/bookings/confirm",
				"user-1",
				`{"showtime_id":"showtime-1","seat_no":"A1","ownership_token":"token"}`,
			)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body = %s", response.Code, response.Body.String())
			}
			if got := responseErrorCode(t, response); got != "INTERNAL_ERROR" {
				t.Fatalf("error code = %q, want INTERNAL_ERROR", got)
			}
			if bytes.Contains(response.Body.Bytes(), []byte("secret")) ||
				bytes.Contains(response.Body.Bytes(), []byte("ownership failure")) ||
				bytes.Contains(response.Body.Bytes(), []byte("durable failure")) {
				t.Fatalf("response exposed infrastructure error: %s", response.Body.String())
			}
		})
	}
}

func newTestRouter() (*gin.Engine, *fakeBookings, *fakeLocks) {
	service, bookings, locks := newTestServiceWithLogger(log.New(io.Discard, "", 0))
	handler := NewHandler(service)
	router := gin.New()
	router.Use(identity.DevelopmentMiddleware())
	handler.Register(router.Group("/api"))
	return router, bookings, locks
}

func performRequest(router http.Handler, method, path, userID, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if userID != "" {
		request.Header.Set("X-User-ID", userID)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func responseErrorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, response.Body.String())
	}
	return envelope.Error.Code
}
