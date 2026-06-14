package booking

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestBookingServicePublishesDomainEvents(t *testing.T) {
	tests := []struct {
		name     string
		run      func(*Service, *fakeBookings, *fakeLocks) error
		wantType events.Type
	}{
		{
			name: "successful lock",
			run: func(service *Service, _ *fakeBookings, _ *fakeLocks) error {
				_, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
				return err
			},
			wantType: events.SeatLocked,
		},
		{
			name: "locked conflict",
			run: func(service *Service, _ *fakeBookings, locks *fakeLocks) error {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "other", OwnershipToken: "token",
				}, lockTTL)
				_, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
				return err
			},
			wantType: events.LockAcquisitionFailed,
		},
		{
			name: "booked conflict",
			run: func(service *Service, bookings *fakeBookings, _ *fakeLocks) error {
				bookings.items = append(bookings.items, Booking{
					ID: "existing", ShowtimeID: "showtime-1", SeatNo: "A1",
					Status: BookingStatusConfirmed,
				})
				_, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
				return err
			},
			wantType: events.LockAcquisitionFailed,
		},
		{
			name: "manual release",
			run: func(service *Service, _ *fakeBookings, locks *fakeLocks) error {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, lockTTL)
				return service.ReleaseLock(context.Background(), "showtime-1", "A1", "user-1", "token")
			},
			wantType: events.SeatReleased,
		},
		{
			name: "booking confirmation",
			run: func(service *Service, _ *fakeBookings, locks *fakeLocks) error {
				locks.put(SeatLock{
					ShowtimeID: "showtime-1", SeatNo: "A1",
					UserID: "user-1", OwnershipToken: "token",
				}, lockTTL)
				_, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", "token")
				return err
			},
			wantType: events.BookingConfirmed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, bookings, locks, publisher := newEventTestService()
			_ = test.run(service, bookings, locks)
			published := publisher.Events()
			if len(published) != 1 {
				t.Fatalf("published events = %#v, want one", published)
			}
			if published[0].Type != test.wantType {
				t.Fatalf("event type = %q, want %q", published[0].Type, test.wantType)
			}
			if published[0].ShowtimeID != "showtime-1" || published[0].SeatNo != "A1" {
				t.Fatalf("event normalization = %#v", published[0])
			}
		})
	}
}

func TestConfirmationCleanupDoesNotPublishSeatReleased(t *testing.T) {
	service, _, locks, publisher := newEventTestService()
	locks.put(SeatLock{
		ShowtimeID: "showtime-1", SeatNo: "A1",
		UserID: "user-1", OwnershipToken: "token",
	}, lockTTL)
	if _, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", "token"); err != nil {
		t.Fatal(err)
	}
	for _, event := range publisher.Events() {
		if event.Type == events.SeatReleased {
			t.Fatal("confirmation cleanup published seat.released")
		}
	}
}

func TestPublisherFailureDoesNotChangeSuccessfulResult(t *testing.T) {
	service, _, _, publisher := newEventTestService()
	publisher.err = errors.New("redis://:secret@redis unavailable")
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil || lock.OwnershipToken == "" {
		t.Fatalf("AcquireLock() = %#v, %v", lock, err)
	}
}

func TestPublishedEventsNeverContainOwnershipTokens(t *testing.T) {
	service, _, _, publisher := newEventTestService()
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range publisher.Events() {
		data, err := events.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), lock.OwnershipToken) {
			t.Fatal("published event exposed ownership token")
		}
	}
}

func TestInvalidSeatAndMissingShowtimeDoNotPublish(t *testing.T) {
	service, _, _, publisher := newEventTestService()
	_, _ = service.AcquireLock(context.Background(), "showtime-1", "Z9", "user-1")
	_, _ = service.AcquireLock(context.Background(), "missing", "A1", "user-1")
	if got := len(publisher.Events()); got != 0 {
		t.Fatalf("published event count = %d, want 0", got)
	}
}

func newEventTestService() (*Service, *fakeBookings, *fakeLocks, *recordingPublisher) {
	catalog := &fakeCatalog{showtime: Showtime{
		ID: "showtime-1", MovieID: "movie-1", Seats: []string{"A1", "A2", "A3"},
	}}
	bookings := &fakeBookings{}
	locks := &fakeLocks{items: make(map[string]fakeLockEntry), now: time.Now}
	publisher := &recordingPublisher{}
	service := NewService(
		catalog,
		bookings,
		locks,
		publisher,
		log.New(io.Discard, "", 0),
	)
	return service, bookings, locks, publisher
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []events.DomainEvent
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, event events.DomainEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return p.err
}

func (p *recordingPublisher) Events() []events.DomainEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]events.DomainEvent(nil), p.events...)
}
