package booking

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/identity"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(router *gin.RouterGroup) {
	router.GET("/showtimes", h.listShowtimes)
	router.GET("/showtimes/:showtimeId/seats", h.seatMap)
	router.POST("/showtimes/:showtimeId/seats/:seatNo/lock", h.acquireLock)
	router.DELETE("/showtimes/:showtimeId/seats/:seatNo/lock", h.releaseLock)
	router.POST("/bookings/confirm", h.confirm)
	router.GET("/bookings/me", h.myBookings)
}

func (h *Handler) listShowtimes(c *gin.Context) {
	showtimes, err := h.service.ListShowtimes(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"showtimes": showtimes})
}

func (h *Handler) seatMap(c *gin.Context) {
	seats, err := h.service.SeatMap(c.Request.Context(), c.Param("showtimeId"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"showtime_id": c.Param("showtimeId"),
		"seats":       seats,
	})
}

func (h *Handler) acquireLock(c *gin.Context) {
	requestIdentity, ok := identity.Require(c)
	if !ok {
		return
	}
	lock, err := h.service.AcquireLock(
		c.Request.Context(),
		c.Param("showtimeId"),
		normalizeSeat(c.Param("seatNo")),
		requestIdentity.UserID,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, lock)
}

func (h *Handler) releaseLock(c *gin.Context) {
	requestIdentity, ok := identity.Require(c)
	if !ok {
		return
	}
	var request struct {
		OwnershipToken string `json:"ownership_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeRequestError(c, "ownership_token is required")
		return
	}
	err := h.service.ReleaseLock(
		c.Request.Context(),
		c.Param("showtimeId"),
		normalizeSeat(c.Param("seatNo")),
		requestIdentity.UserID,
		request.OwnershipToken,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) confirm(c *gin.Context) {
	requestIdentity, ok := identity.Require(c)
	if !ok {
		return
	}
	var request struct {
		ShowtimeID     string `json:"showtime_id" binding:"required"`
		SeatNo         string `json:"seat_no" binding:"required"`
		OwnershipToken string `json:"ownership_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeRequestError(c, "showtime_id, seat_no, and ownership_token are required")
		return
	}
	confirmed, err := h.service.Confirm(
		c.Request.Context(),
		request.ShowtimeID,
		normalizeSeat(request.SeatNo),
		requestIdentity.UserID,
		request.OwnershipToken,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, confirmed)
}

func (h *Handler) myBookings(c *gin.Context) {
	requestIdentity, ok := identity.Require(c)
	if !ok {
		return
	}
	bookings, err := h.service.MyBookings(c.Request.Context(), requestIdentity.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookings": bookings})
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeStructuredError(c, http.StatusNotFound, "SHOWTIME_NOT_FOUND", "showtime was not found")
	case errors.Is(err, ErrInvalidSeat):
		writeStructuredError(c, http.StatusBadRequest, "INVALID_SEAT", "seat is not configured for this showtime")
	case errors.Is(err, ErrLockNotOwned):
		writeStructuredError(c, http.StatusForbidden, "LOCK_NOT_OWNED", "the active lock does not match this user and ownership token")
	case errors.Is(err, ErrLockNotFound):
		writeStructuredError(c, http.StatusConflict, "LOCK_NOT_ACTIVE", "the seat lock is missing or expired")
	case errors.Is(err, ErrSeatConflict), errors.Is(err, ErrDuplicateBooking):
		writeStructuredError(c, http.StatusConflict, "SEAT_CONFLICT", "the seat is already locked or booked")
	default:
		writeStructuredError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "an internal error occurred")
	}
}

func writeRequestError(c *gin.Context, message string) {
	writeStructuredError(c, http.StatusBadRequest, "INVALID_REQUEST", message)
}

func writeStructuredError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func normalizeSeat(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
