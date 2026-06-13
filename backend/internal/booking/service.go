package booking

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const lockTTL = 5 * time.Minute

type Service struct {
	catalog  CatalogRepository
	bookings BookingRepository
	locks    LockRepository
	now      func() time.Time
}

func NewService(catalog CatalogRepository, bookings BookingRepository, locks LockRepository) *Service {
	return &Service{
		catalog:  catalog,
		bookings: bookings,
		locks:    locks,
		now:      time.Now,
	}
}

func (s *Service) ListShowtimes(ctx context.Context) ([]ShowtimeSummary, error) {
	return s.catalog.ListShowtimes(ctx)
}

func (s *Service) SeatMap(ctx context.Context, showtimeID string) ([]Seat, error) {
	showtime, err := s.catalog.GetShowtime(ctx, showtimeID)
	if err != nil {
		return nil, err
	}
	booked, err := s.bookings.ListBookedSeats(ctx, showtimeID)
	if err != nil {
		return nil, err
	}
	locked, err := s.locks.GetMany(ctx, showtimeID, showtime.Seats)
	if err != nil {
		return nil, err
	}

	seats := make([]Seat, 0, len(showtime.Seats))
	for _, seatNo := range showtime.Seats {
		state := SeatStateAvailable
		if _, ok := locked[seatNo]; ok {
			state = SeatStateLocked
		}
		if _, ok := booked[seatNo]; ok {
			state = SeatStateBooked
		}
		seats = append(seats, Seat{SeatNo: seatNo, State: state})
	}
	return seats, nil
}

func (s *Service) AcquireLock(ctx context.Context, showtimeID, seatNo, userID string) (SeatLock, error) {
	showtimeID = strings.TrimSpace(showtimeID)
	seatNo = strings.ToUpper(strings.TrimSpace(seatNo))
	_, err := s.validateSeat(ctx, showtimeID, seatNo)
	if err != nil {
		return SeatLock{}, err
	}

	booked, err := s.bookings.IsBooked(ctx, showtimeID, seatNo)
	if err != nil {
		return SeatLock{}, err
	}
	if booked {
		return SeatLock{}, ErrSeatConflict
	}

	token, err := randomID(32)
	if err != nil {
		return SeatLock{}, fmt.Errorf("generate ownership token: %w", err)
	}
	lock := SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         userID,
		OwnershipToken: token,
		ExpiresAt:      s.now().UTC().Add(lockTTL),
	}
	acquired, err := s.locks.Acquire(ctx, lock, lockTTL)
	if err != nil {
		return SeatLock{}, err
	}
	if !acquired {
		return SeatLock{}, ErrSeatConflict
	}

	booked, err = s.bookings.IsBooked(ctx, showtimeID, seatNo)
	if err != nil {
		_, _ = s.locks.Release(ctx, lock)
		return SeatLock{}, err
	}
	if booked {
		_, _ = s.locks.Release(ctx, lock)
		return SeatLock{}, ErrSeatConflict
	}
	return lock, nil
}

func (s *Service) ReleaseLock(ctx context.Context, showtimeID, seatNo, userID, token string) error {
	showtimeID = strings.TrimSpace(showtimeID)
	seatNo = strings.ToUpper(strings.TrimSpace(seatNo))
	if _, err := s.validateSeat(ctx, showtimeID, seatNo); err != nil {
		return err
	}
	result, err := s.locks.Release(ctx, SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         userID,
		OwnershipToken: token,
	})
	if err != nil {
		return err
	}
	switch result {
	case ReleaseSucceeded:
		return nil
	case ReleaseNotOwned:
		return ErrLockNotOwned
	default:
		return ErrLockNotFound
	}
}

func (s *Service) Confirm(ctx context.Context, showtimeID, seatNo, userID, token string) (Booking, error) {
	showtimeID = strings.TrimSpace(showtimeID)
	seatNo = strings.ToUpper(strings.TrimSpace(seatNo))
	if _, err := s.validateSeat(ctx, showtimeID, seatNo); err != nil {
		return Booking{}, err
	}
	lock := SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         userID,
		OwnershipToken: token,
	}
	ownership, err := s.locks.VerifyAndExtend(ctx, lock, lockTTL)
	if err != nil {
		return Booking{}, err
	}
	if ownership == OwnershipMissing {
		return Booking{}, ErrLockNotFound
	}
	if ownership == OwnershipNotMatched {
		return Booking{}, ErrLockNotOwned
	}

	id, err := randomID(16)
	if err != nil {
		return Booking{}, fmt.Errorf("generate booking ID: %w", err)
	}
	confirmed := Booking{
		ID:         id,
		ShowtimeID: showtimeID,
		SeatNo:     seatNo,
		UserID:     userID,
		Status:     BookingStatusConfirmed,
		CreatedAt:  s.now().UTC(),
	}
	if err := s.bookings.Create(ctx, confirmed); err != nil {
		if errors.Is(err, ErrDuplicateBooking) {
			return Booking{}, ErrSeatConflict
		}
		return Booking{}, err
	}

	_, err = s.locks.Release(ctx, lock)
	if err != nil {
		return Booking{}, fmt.Errorf("booking created but lock cleanup failed: %w", err)
	}
	return confirmed, nil
}

func (s *Service) MyBookings(ctx context.Context, userID string) ([]Booking, error) {
	return s.bookings.ListByUser(ctx, userID)
}

func (s *Service) validateSeat(ctx context.Context, showtimeID, seatNo string) (Showtime, error) {
	showtimeID = strings.TrimSpace(showtimeID)
	seatNo = strings.ToUpper(strings.TrimSpace(seatNo))
	if showtimeID == "" || seatNo == "" {
		return Showtime{}, ErrInvalidSeat
	}
	showtime, err := s.catalog.GetShowtime(ctx, showtimeID)
	if err != nil {
		return Showtime{}, err
	}
	if !showtime.HasSeat(seatNo) {
		return Showtime{}, ErrInvalidSeat
	}
	return showtime, nil
}

func randomID(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
