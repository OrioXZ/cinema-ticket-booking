package booking

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestAcquireLockSucceeds(t *testing.T) {
	service, _, locks := newTestService()

	lock, err := service.AcquireLock(context.Background(), "showtime-1", "a1", "user-1")
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if lock.UserID != "user-1" || lock.SeatNo != "A1" || lock.OwnershipToken == "" {
		t.Fatalf("AcquireLock() = %#v", lock)
	}
	if got := locks.count(); got != 1 {
		t.Fatalf("active lock count = %d, want 1", got)
	}
}

func TestAcquireLockConflictsWhenAlreadyLocked(t *testing.T) {
	service, _, _ := newTestService()
	if _, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1"); err != nil {
		t.Fatalf("first AcquireLock() error = %v", err)
	}
	if _, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-2"); !errors.Is(err, ErrSeatConflict) {
		t.Fatalf("second AcquireLock() error = %v, want ErrSeatConflict", err)
	}
}

func TestReleaseLockByOwner(t *testing.T) {
	service, _, locks := newTestService()
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}

	if err := service.ReleaseLock(context.Background(), "showtime-1", "A1", "user-1", lock.OwnershipToken); err != nil {
		t.Fatalf("ReleaseLock() error = %v", err)
	}
	if got := locks.count(); got != 0 {
		t.Fatalf("active lock count = %d, want 0", got)
	}
}

func TestReleaseLockRejectsWrongToken(t *testing.T) {
	service, _, locks := newTestService()
	if _, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1"); err != nil {
		t.Fatal(err)
	}

	err := service.ReleaseLock(context.Background(), "showtime-1", "A1", "user-1", "stale-token")
	if !errors.Is(err, ErrLockNotOwned) {
		t.Fatalf("ReleaseLock() error = %v, want ErrLockNotOwned", err)
	}
	if got := locks.count(); got != 1 {
		t.Fatalf("active lock count = %d, want 1", got)
	}
}

func TestLockExpirationMakesSeatAvailable(t *testing.T) {
	service, _, locks := newTestService()
	now := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	locks.now = func() time.Time { return now }

	if _, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(lockTTL + time.Millisecond)

	seats, err := service.SeatMap(context.Background(), "showtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if seats[0].State != SeatStateAvailable {
		t.Fatalf("A1 state = %s, want AVAILABLE", seats[0].State)
	}
}

func TestConfirmBookingSucceedsAndReleasesLock(t *testing.T) {
	service, bookings, locks := newTestService()
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}

	confirmed, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", lock.OwnershipToken)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Status != BookingStatusConfirmed || confirmed.UserID != "user-1" {
		t.Fatalf("Confirm() = %#v", confirmed)
	}
	if bookings.count() != 1 || locks.count() != 0 {
		t.Fatalf("booking count = %d, lock count = %d", bookings.count(), locks.count())
	}
}

func TestConfirmRejectsWrongUserOrToken(t *testing.T) {
	service, bookings, _ := newTestService()
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		userID string
		token  string
	}{
		{name: "wrong user", userID: "user-2", token: lock.OwnershipToken},
		{name: "wrong token", userID: "user-1", token: "wrong-token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Confirm(context.Background(), "showtime-1", "A1", test.userID, test.token)
			if !errors.Is(err, ErrLockNotOwned) {
				t.Fatalf("Confirm() error = %v, want ErrLockNotOwned", err)
			}
		})
	}
	if bookings.count() != 0 {
		t.Fatalf("booking count = %d, want 0", bookings.count())
	}
}

func TestOwnershipVerificationDoesNotExtendExpiry(t *testing.T) {
	service, _, locks := newTestService()
	now := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	locks.now = func() time.Time { return now }
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	originalExpiry := locks.expiry("showtime-1", "A1")

	now = now.Add(time.Minute)
	result, _, err := locks.VerifyOwnership(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	if result != OwnershipMatched {
		t.Fatalf("VerifyOwnership() = %v, want OwnershipMatched", result)
	}
	if got := locks.expiry("showtime-1", "A1"); !got.Equal(originalExpiry) {
		t.Fatalf("expiry = %v, want unchanged %v", got, originalExpiry)
	}
}

func TestFailedConfirmationsDoNotExtendExpiry(t *testing.T) {
	service, _, locks := newTestService()
	now := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	locks.now = func() time.Time { return now }
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	originalExpiry := locks.expiry("showtime-1", "A1")

	for attempt := 0; attempt < 3; attempt++ {
		now = now.Add(time.Minute)
		_, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", "wrong-token")
		if !errors.Is(err, ErrLockNotOwned) {
			t.Fatalf("Confirm() error = %v, want ErrLockNotOwned", err)
		}
		if got := locks.expiry("showtime-1", "A1"); !got.Equal(originalExpiry) {
			t.Fatalf("attempt %d expiry = %v, want unchanged %v", attempt, got, originalExpiry)
		}
	}

	now = originalExpiry.Add(time.Millisecond)
	_, err = service.Confirm(context.Background(), "showtime-1", "A1", "user-1", lock.OwnershipToken)
	if !errors.Is(err, ErrLockNotFound) {
		t.Fatalf("Confirm() after expiry error = %v, want ErrLockNotFound", err)
	}
}

func TestConfirmReturnsBookingWhenLockCleanupFails(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	service, bookings, locks := newTestServiceWithLogger(logger)
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	locks.releaseErr = errors.New("redis://:secret@redis cleanup unavailable")

	confirmed, err := service.Confirm(
		context.Background(),
		"showtime-1",
		"A1",
		"user-1",
		lock.OwnershipToken,
	)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Status != BookingStatusConfirmed || bookings.count() != 1 {
		t.Fatalf("confirmed = %#v, booking count = %d", confirmed, bookings.count())
	}
	if !strings.Contains(logs.String(), "Redis BOOKED transition failed") {
		t.Fatalf("cleanup log = %q", logs.String())
	}
	if strings.Contains(logs.String(), lock.OwnershipToken) || strings.Contains(logs.String(), "secret") {
		t.Fatal("cleanup log exposed ownership token or credentials")
	}
}

func TestCommittedBookingWithFailedCleanupCannotExpireToAvailable(t *testing.T) {
	service, bookings, locks := newTestService()
	lock, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	locks.releaseErr = errors.New("cleanup unavailable")
	if _, err := service.Confirm(
		context.Background(),
		"showtime-1",
		"A1",
		"user-1",
		lock.OwnershipToken,
	); err != nil {
		t.Fatal(err)
	}

	expirations := &fakeExpirationPublisher{}
	processor := events.NewExpirationProcessor(
		bookings,
		expirations,
		log.New(&bytes.Buffer{}, "", 0),
	)
	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A1:1")

	if expirations.calls != 0 {
		t.Fatalf("expiration publish calls = %d, want 0", expirations.calls)
	}
	if expirations.timeoutAudits != 0 || expirations.availableUpdates != 0 {
		t.Fatalf(
			"timeout audits = %d, available updates = %d, want both 0",
			expirations.timeoutAudits,
			expirations.availableUpdates,
		)
	}
	seats, err := service.SeatMap(context.Background(), "showtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if seats[0].State != SeatStateBooked {
		t.Fatalf("A1 state = %s, want BOOKED", seats[0].State)
	}
}

func TestDuplicateBookingIsConflict(t *testing.T) {
	service, bookings, _ := newTestService()
	bookings.items = append(bookings.items, Booking{
		ID: "existing", ShowtimeID: "showtime-1", SeatNo: "A1", UserID: "other",
	})
	locks := service.locks.(*fakeLocks)
	locks.put(SeatLock{
		ShowtimeID: "showtime-1", SeatNo: "A1", UserID: "user-1", OwnershipToken: "token",
	}, lockTTL)

	_, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", "token")
	if !errors.Is(err, ErrSeatConflict) {
		t.Fatalf("Confirm() error = %v, want ErrSeatConflict", err)
	}
}

func TestConcurrentLockAttemptsHaveExactlyOneWinner(t *testing.T) {
	service, _, _ := newTestService()
	const attempts = 32
	var successes atomic.Int32
	var wait sync.WaitGroup
	wait.Add(attempts)

	for i := 0; i < attempts; i++ {
		go func() {
			defer wait.Done()
			if _, err := service.AcquireLock(context.Background(), "showtime-1", "A1", "user"); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrSeatConflict) {
				t.Errorf("AcquireLock() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful lock attempts = %d, want 1", got)
	}
}

func TestConcurrentConfirmationsDoNotDoubleBook(t *testing.T) {
	service, bookings, locks := newTestService()
	locks.put(SeatLock{
		ShowtimeID: "showtime-1", SeatNo: "A1", UserID: "user-1", OwnershipToken: "token",
	}, lockTTL)

	const attempts = 16
	var wait sync.WaitGroup
	wait.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wait.Done()
			_, err := service.Confirm(context.Background(), "showtime-1", "A1", "user-1", "token")
			if err != nil &&
				!errors.Is(err, ErrSeatConflict) &&
				!errors.Is(err, ErrLockNotFound) &&
				!errors.Is(err, ErrLockNotOwned) {
				t.Errorf("Confirm() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if got := bookings.count(); got != 1 {
		t.Fatalf("booking count = %d, want 1", got)
	}
}

func TestSeatMapResolvesAvailableLockedAndBooked(t *testing.T) {
	service, bookings, locks := newTestService()
	bookings.items = append(bookings.items, Booking{
		ID: "booking-1", ShowtimeID: "showtime-1", SeatNo: "A3", Status: BookingStatusConfirmed,
	})
	locks.put(SeatLock{
		ShowtimeID: "showtime-1", SeatNo: "A2", UserID: "user-1", OwnershipToken: "token",
	}, lockTTL)
	locks.put(SeatLock{
		ShowtimeID: "showtime-1", SeatNo: "A3", UserID: "user-2", OwnershipToken: "token",
	}, lockTTL)

	seats, err := service.SeatMap(context.Background(), "showtime-1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, seat := range seats {
		got[seat.SeatNo] = seat.State
	}
	if got["A1"] != SeatStateAvailable || got["A2"] != SeatStateLocked || got["A3"] != SeatStateBooked {
		t.Fatalf("seat states = %#v", got)
	}
}

func newTestService() (*Service, *fakeBookings, *fakeLocks) {
	return newTestServiceWithLogger(log.New(&bytes.Buffer{}, "", 0))
}

func newTestServiceWithLogger(logger Logger) (*Service, *fakeBookings, *fakeLocks) {
	catalog := &fakeCatalog{showtime: Showtime{
		ID: "showtime-1", MovieID: "movie-1", Seats: []string{"A1", "A2", "A3"},
	}}
	bookings := &fakeBookings{}
	locks := &fakeLocks{
		items: make(map[string]fakeLockEntry),
		now:   time.Now,
	}
	return NewService(catalog, bookings, locks, events.NoopPublisher{}, logger), bookings, locks
}

type fakeCatalog struct {
	showtime Showtime
}

func (f *fakeCatalog) ListShowtimes(context.Context) ([]ShowtimeSummary, error) {
	return []ShowtimeSummary{{Showtime: f.showtime, Movie: Movie{ID: f.showtime.MovieID}}}, nil
}

func (f *fakeCatalog) GetShowtime(_ context.Context, id string) (Showtime, error) {
	if id != f.showtime.ID {
		return Showtime{}, ErrNotFound
	}
	return f.showtime, nil
}

type fakeBookings struct {
	mu        sync.Mutex
	items     []Booking
	createErr error
}

type fakeExpirationPublisher struct {
	calls            int
	timeoutAudits    int
	availableUpdates int
}

func (p *fakeExpirationPublisher) PublishExpiration(
	context.Context, string, string, int64, events.DomainEvent,
) (bool, error) {
	p.calls++
	p.timeoutAudits++
	p.availableUpdates++
	return true, nil
}

func (f *fakeBookings) ListBookedSeats(_ context.Context, showtimeID string) (map[string]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make(map[string]struct{})
	for _, item := range f.items {
		if item.ShowtimeID == showtimeID {
			result[item.SeatNo] = struct{}{}
		}
	}
	return result, nil
}

func (f *fakeBookings) IsBooked(_ context.Context, showtimeID, seatNo string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range f.items {
		if item.ShowtimeID == showtimeID && item.SeatNo == seatNo {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeBookings) Create(_ context.Context, booking Booking) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	for _, item := range f.items {
		if item.ShowtimeID == booking.ShowtimeID && item.SeatNo == booking.SeatNo {
			return ErrDuplicateBooking
		}
	}
	f.items = append(f.items, booking)
	return nil
}

func (f *fakeBookings) ListByUser(_ context.Context, userID string) ([]Booking, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []Booking
	for _, item := range f.items {
		if item.UserID == userID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakeBookings) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.items)
}

type fakeLockEntry struct {
	lock      SeatLock
	expiresAt time.Time
}

type fakeLocks struct {
	mu         sync.Mutex
	items      map[string]fakeLockEntry
	now        func() time.Time
	generation int64
	publisher  events.Publisher
	verifyErr  error
	releaseErr error
}

func (f *fakeLocks) Acquire(
	ctx context.Context,
	lock SeatLock,
	ttl time.Duration,
	event events.DomainEvent,
) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeExpired()
	key := lockKey(lock.ShowtimeID, lock.SeatNo)
	if _, exists := f.items[key]; exists {
		return false, 0, nil
	}
	f.generation++
	lock.Generation = f.generation
	f.items[key] = fakeLockEntry{lock: lock, expiresAt: f.now().Add(ttl)}
	event.Generation = lock.Generation
	if f.publisher != nil {
		_ = f.publisher.Publish(ctx, event)
	}
	return true, lock.Generation, nil
}

func (f *fakeLocks) Get(_ context.Context, showtimeID, seatNo string) (*SeatLock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeExpired()
	entry, exists := f.items[lockKey(showtimeID, seatNo)]
	if !exists {
		return nil, nil
	}
	lock := entry.lock
	return &lock, nil
}

func (f *fakeLocks) GetMany(_ context.Context, showtimeID string, seatNos []string) (map[string]SeatLock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeExpired()
	result := make(map[string]SeatLock)
	for _, seatNo := range seatNos {
		if entry, exists := f.items[lockKey(showtimeID, seatNo)]; exists {
			result[seatNo] = entry.lock
		}
	}
	return result, nil
}

func (f *fakeLocks) VerifyOwnership(
	_ context.Context,
	lock SeatLock,
) (OwnershipResult, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.verifyErr != nil {
		return OwnershipMissing, 0, f.verifyErr
	}
	f.removeExpired()
	key := lockKey(lock.ShowtimeID, lock.SeatNo)
	entry, exists := f.items[key]
	if !exists {
		return OwnershipMissing, 0, nil
	}
	if entry.lock.UserID != lock.UserID || entry.lock.OwnershipToken != lock.OwnershipToken {
		return OwnershipNotMatched, 0, nil
	}
	return OwnershipMatched, entry.lock.Generation, nil
}

func (f *fakeLocks) Release(
	ctx context.Context,
	lock SeatLock,
	event events.DomainEvent,
) (ReleaseResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return ReleaseMissing, f.releaseErr
	}
	f.removeExpired()
	key := lockKey(lock.ShowtimeID, lock.SeatNo)
	entry, exists := f.items[key]
	if !exists {
		return ReleaseMissing, nil
	}
	if entry.lock.UserID != lock.UserID || entry.lock.OwnershipToken != lock.OwnershipToken {
		return ReleaseNotOwned, nil
	}
	delete(f.items, key)
	event.Generation = entry.lock.Generation
	if f.publisher != nil {
		_ = f.publisher.Publish(ctx, event)
	}
	return ReleaseSucceeded, nil
}

func (f *fakeLocks) Confirm(
	ctx context.Context,
	lock SeatLock,
	event events.DomainEvent,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return f.releaseErr
	}
	key := lockKey(lock.ShowtimeID, lock.SeatNo)
	generation := lock.Generation
	if entry, exists := f.items[key]; exists {
		if entry.lock.Generation > generation {
			generation = entry.lock.Generation
		}
		delete(f.items, key)
	}
	if generation == 0 {
		generation = f.generation
	}
	event.Generation = generation
	if f.publisher != nil {
		return f.publisher.Publish(ctx, event)
	}
	return nil
}

func (f *fakeLocks) put(lock SeatLock, ttl time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lock.Generation == 0 {
		f.generation++
		lock.Generation = f.generation
	} else if lock.Generation > f.generation {
		f.generation = lock.Generation
	}
	f.items[lockKey(lock.ShowtimeID, lock.SeatNo)] = fakeLockEntry{
		lock: lock, expiresAt: f.now().Add(ttl),
	}
}

func (f *fakeLocks) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeExpired()
	return len(f.items)
}

func (f *fakeLocks) expiry(showtimeID, seatNo string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeExpired()
	return f.items[lockKey(showtimeID, seatNo)].expiresAt
}

func (f *fakeLocks) removeExpired() {
	now := f.now()
	for key, entry := range f.items {
		if !entry.expiresAt.After(now) {
			delete(f.items, key)
		}
	}
}
