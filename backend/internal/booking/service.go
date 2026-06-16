package booking

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

const lockTTL = 5 * time.Minute

type Service struct {
	catalog   CatalogRepository
	bookings  BookingRepository
	locks     LockRepository
	publisher events.Publisher
	logger    Logger
	now       func() time.Time
}

type Logger interface {
	Printf(format string, args ...any)
}

func NewService(
	catalog CatalogRepository,
	bookings BookingRepository,
	locks LockRepository,
	publisher events.Publisher,
	logger Logger,
) *Service {
	return &Service{
		catalog:   catalog,
		bookings:  bookings,
		locks:     locks,
		publisher: publisher,
		logger:    logger,
		now:       time.Now,
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
	projections, err := s.locks.GetProjections(ctx, showtimeID, showtime.Seats)
	if err != nil {
		return nil, err
	}
	// Read projections before active locks. If an acquire or release lands between
	// these reads, the active lock check is the safer final transient override.
	locked, err := s.locks.GetMany(ctx, showtimeID, showtime.Seats)
	if err != nil {
		return nil, err
	}

	seats := make([]Seat, 0, len(showtime.Seats))
	for _, seatNo := range showtime.Seats {
		state := SeatStateAvailable
		projection := projections[seatNo]
		revision := projection.Revision
		if projection.State == SeatStateBooked {
			state = SeatStateBooked
		}
		if lock, ok := locked[seatNo]; ok && state != SeatStateBooked {
			state = SeatStateLocked
			revision = lock.Generation
		}
		if _, ok := booked[seatNo]; ok {
			state = SeatStateBooked
		}
		seats = append(seats, Seat{SeatNo: seatNo, State: state, Revision: revision})
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
		s.publish(ctx, events.LockAcquisitionFailed, showtimeID, seatNo, userID, "", "seat already booked")
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
	lockedEvent, err := s.newEvent(
		events.SeatLocked,
		showtimeID,
		seatNo,
		userID,
		"",
		"",
	)
	if err != nil {
		return SeatLock{}, err
	}
	acquired, generation, err := s.locks.Acquire(ctx, lock, lockTTL, lockedEvent)
	if err != nil {
		return SeatLock{}, err
	}
	if !acquired {
		s.publish(ctx, events.LockAcquisitionFailed, showtimeID, seatNo, userID, "", "seat already locked")
		return SeatLock{}, ErrSeatConflict
	}
	lock.Generation = generation

	booked, err = s.bookings.IsBooked(ctx, showtimeID, seatNo)
	if err != nil {
		s.releaseAfterFailedAcquire(ctx, lock)
		return SeatLock{}, err
	}
	if booked {
		bookedEvent, eventErr := s.newEvent(
			events.BookingConfirmed,
			showtimeID,
			seatNo,
			"",
			"",
			"durable booking detected after lock acquisition",
		)
		if eventErr != nil {
			s.releaseAfterFailedAcquire(ctx, lock)
		} else if confirmErr := s.locks.MarkBookedAfterDurableCommit(ctx, lock, bookedEvent); confirmErr != nil {
			s.logger.Printf(
				"durable booking detected but Redis BOOKED correction failed for showtime %q seat %q",
				showtimeID,
				seatNo,
			)
		}
		s.publish(ctx, events.LockAcquisitionFailed, showtimeID, seatNo, userID, "", "seat already booked")
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
	releasedEvent, err := s.newEvent(
		events.SeatReleased,
		showtimeID,
		seatNo,
		userID,
		"",
		"",
	)
	if err != nil {
		return err
	}
	result, err := s.locks.Release(ctx, SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         userID,
		OwnershipToken: token,
	}, releasedEvent)
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
	ownership, generation, err := s.locks.VerifyOwnership(ctx, lock)
	if err != nil {
		return Booking{}, err
	}
	if ownership == OwnershipMissing {
		return Booking{}, ErrLockNotFound
	}
	if ownership == OwnershipNotMatched {
		return Booking{}, ErrLockNotOwned
	}
	lock.Generation = generation

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
	confirmedEvent, eventErr := s.newEvent(
		events.BookingConfirmed,
		showtimeID,
		seatNo,
		userID,
		confirmed.ID,
		"",
	)
	if eventErr != nil {
		s.logger.Printf(
			"booking committed but realtime event creation failed for showtime %q seat %q",
			showtimeID,
			seatNo,
		)
		return confirmed, nil
	}
	if err := s.locks.MarkBookedAfterDurableCommit(ctx, lock, confirmedEvent); err != nil {
		s.logger.Printf(
			"booking committed but Redis BOOKED transition failed for showtime %q seat %q",
			showtimeID,
			seatNo,
		)
	}
	return confirmed, nil
}

func (s *Service) releaseAfterFailedAcquire(ctx context.Context, lock SeatLock) {
	event, err := s.newEvent(
		events.SeatReleased,
		lock.ShowtimeID,
		lock.SeatNo,
		lock.UserID,
		"",
		"lock acquisition rolled back",
	)
	if err != nil {
		return
	}
	_, _ = s.locks.Release(ctx, lock, event)
}

func (s *Service) newEvent(
	eventType events.Type,
	showtimeID string,
	seatNo string,
	userID string,
	bookingID string,
	reason string,
) (events.DomainEvent, error) {
	event, err := events.New(
		eventType,
		showtimeID,
		seatNo,
		userID,
		bookingID,
		reason,
		s.now(),
	)
	if err != nil {
		return events.DomainEvent{}, fmt.Errorf("create %s event: %w", eventType, err)
	}
	return event, nil
}

func (s *Service) publish(
	ctx context.Context,
	eventType events.Type,
	showtimeID string,
	seatNo string,
	userID string,
	bookingID string,
	reason string,
) {
	event, err := events.New(
		eventType,
		showtimeID,
		seatNo,
		userID,
		bookingID,
		reason,
		s.now(),
	)
	if err != nil {
		s.logger.Printf(
			"domain event creation failed for type %q showtime %q seat %q",
			eventType,
			showtimeID,
			seatNo,
		)
		return
	}
	if err := s.publisher.Publish(ctx, event); err != nil {
		s.logger.Printf(
			"domain event publish failed for type %q showtime %q seat %q",
			eventType,
			showtimeID,
			seatNo,
		)
	}
}

func (s *Service) MyBookings(ctx context.Context, userID string) ([]Booking, error) {
	return s.bookings.ListByUser(ctx, userID)
}

func (s *Service) AdminBookings(ctx context.Context, userID string, limit int64) ([]Booking, error) {
	return s.bookings.ListConfirmed(ctx, strings.TrimSpace(userID), limit)
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
