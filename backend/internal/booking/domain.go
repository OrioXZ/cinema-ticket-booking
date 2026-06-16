package booking

import (
	"context"
	"errors"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

const (
	BookingStatusConfirmed = "CONFIRMED"
	SeatStateAvailable     = "AVAILABLE"
	SeatStateLocked        = "LOCKED"
	SeatStateBooked        = "BOOKED"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrInvalidSeat      = errors.New("invalid seat")
	ErrSeatConflict     = errors.New("seat conflict")
	ErrLockNotFound     = errors.New("seat lock not found")
	ErrLockNotOwned     = errors.New("seat lock is not owned by requester")
	ErrDuplicateBooking = errors.New("booking already exists")
)

type Movie struct {
	ID              string    `json:"id" bson:"_id"`
	Title           string    `json:"title" bson:"title"`
	DurationMinutes int       `json:"duration_minutes" bson:"duration_minutes"`
	CreatedAt       time.Time `json:"created_at" bson:"created_at"`
}

type Showtime struct {
	ID         string    `json:"id" bson:"_id"`
	MovieID    string    `json:"movie_id" bson:"movie_id"`
	StartsAt   time.Time `json:"starts_at" bson:"starts_at"`
	Auditorium string    `json:"auditorium" bson:"auditorium"`
	Seats      []string  `json:"seats" bson:"seats"`
	CreatedAt  time.Time `json:"created_at" bson:"created_at"`
}

func (s Showtime) HasSeat(seatNo string) bool {
	for _, configured := range s.Seats {
		if configured == seatNo {
			return true
		}
	}
	return false
}

type ShowtimeSummary struct {
	Showtime Showtime `json:"showtime"`
	Movie    Movie    `json:"movie"`
}

type Booking struct {
	ID         string    `json:"id" bson:"_id"`
	ShowtimeID string    `json:"showtime_id" bson:"showtime_id"`
	SeatNo     string    `json:"seat_no" bson:"seat_no"`
	UserID     string    `json:"user_id" bson:"user_id"`
	Status     string    `json:"status" bson:"status"`
	CreatedAt  time.Time `json:"created_at" bson:"created_at"`
}

type SeatLock struct {
	ShowtimeID     string    `json:"showtime_id"`
	SeatNo         string    `json:"seat_no"`
	UserID         string    `json:"user_id"`
	OwnershipToken string    `json:"ownership_token"`
	ExpiresAt      time.Time `json:"expires_at"`
	Generation     int64     `json:"revision"`
}

type Seat struct {
	SeatNo   string `json:"seat_no"`
	State    string `json:"state"`
	Revision int64  `json:"revision"`
}

type SeatProjection struct {
	State    string
	Revision int64
}

type ReleaseResult int

const (
	ReleaseMissing ReleaseResult = iota
	ReleaseSucceeded
	ReleaseNotOwned
)

type OwnershipResult int

const (
	OwnershipMissing OwnershipResult = iota
	OwnershipMatched
	OwnershipNotMatched
)

type CatalogRepository interface {
	ListShowtimes(context.Context) ([]ShowtimeSummary, error)
	GetShowtime(context.Context, string) (Showtime, error)
}

type BookingRepository interface {
	ListBookedSeats(context.Context, string) (map[string]struct{}, error)
	IsBooked(context.Context, string, string) (bool, error)
	Create(context.Context, Booking) error
	ListByUser(context.Context, string) ([]Booking, error)
	ListConfirmed(context.Context, string, int64) ([]Booking, error)
}

type LockRepository interface {
	Acquire(context.Context, SeatLock, time.Duration, events.DomainEvent) (bool, int64, error)
	GetMany(context.Context, string, []string) (map[string]SeatLock, error)
	GetProjections(context.Context, string, []string) (map[string]SeatProjection, error)
	VerifyOwnership(context.Context, SeatLock) (OwnershipResult, int64, error)
	Release(context.Context, SeatLock, events.DomainEvent) (ReleaseResult, error)
	MarkBookedAfterDurableCommit(context.Context, SeatLock, events.DomainEvent) error
}
