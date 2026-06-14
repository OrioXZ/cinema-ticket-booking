//go:build integration

package booking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestRealMongoAndRedisConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoURI := envOrDefault("MONGO_URI", "mongodb://cinema:cinema_dev_password@127.0.0.1:27017/?authSource=admin")
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	defer mongoClient.Disconnect(context.Background())

	databaseName := envOrDefault("MONGO_INTEGRATION_DATABASE", "cinema_phase2_integration")
	database := mongoClient.Database(databaseName)
	defer database.Drop(context.Background())

	redisOptions, err := goredis.ParseURL(envOrDefault("REDIS_URI", "redis://127.0.0.1:6379/15"))
	if err != nil {
		t.Fatal(err)
	}
	redisClient := goredis.NewClient(redisOptions)
	defer redisClient.Close()

	repository := NewMongoRepository(database)
	if err := repository.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	for _, seatNo := range []string{"A1", "A2"} {
		keys := []string{
			lockKey("showtime-1", seatNo),
			generationKey("showtime-1", seatNo),
			realtimeStateKey("showtime-1", seatNo),
		}
		redisClient.Del(ctx, keys...)
		defer redisClient.Del(context.Background(), keys...)
	}

	channel := fmt.Sprintf("cinema.events.concurrency.%d", time.Now().UnixNano())
	lockRepository := NewRedisLockRepository(redisClient, channel)
	service := NewService(
		repository,
		repository,
		lockRepository,
		events.NoopPublisher{},
		log.New(io.Discard, "", 0),
	)

	const lockAttempts = 24
	var lockWins atomic.Int32
	var wait sync.WaitGroup
	wait.Add(lockAttempts)
	for i := 0; i < lockAttempts; i++ {
		go func(user int) {
			defer wait.Done()
			if _, err := service.AcquireLock(ctx, "showtime-1", "A1", fmt.Sprintf("user-%d", user)); err == nil {
				lockWins.Add(1)
			} else if !errors.Is(err, ErrSeatConflict) {
				t.Errorf("AcquireLock() error = %v", err)
			}
		}(i)
	}
	wait.Wait()
	if lockWins.Load() != 1 {
		t.Fatalf("lock winners = %d, want 1", lockWins.Load())
	}

	lock, err := service.AcquireLock(ctx, "showtime-1", "A2", "booking-user")
	if err != nil {
		t.Fatal(err)
	}
	const confirmAttempts = 12
	wait.Add(confirmAttempts)
	for i := 0; i < confirmAttempts; i++ {
		go func() {
			defer wait.Done()
			_, err := service.Confirm(ctx, "showtime-1", "A2", "booking-user", lock.OwnershipToken)
			if err != nil &&
				!errors.Is(err, ErrSeatConflict) &&
				!errors.Is(err, ErrLockNotFound) {
				t.Errorf("Confirm() error = %v", err)
			}
		}()
	}
	wait.Wait()

	booked, err := repository.ListBookedSeats(ctx, "showtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := booked["A2"]; !exists || len(booked) != 1 {
		t.Fatalf("booked seats = %#v, want only A2", booked)
	}

	err = repository.Create(ctx, Booking{
		ID: "duplicate", ShowtimeID: "showtime-1", SeatNo: "A2",
		UserID: "other", Status: BookingStatusConfirmed, CreatedAt: time.Now().UTC(),
	})
	if !errors.Is(err, ErrDuplicateBooking) {
		t.Fatalf("duplicate Create() error = %v, want ErrDuplicateBooking", err)
	}

	older := time.Now().UTC().Add(-time.Minute)
	newer := time.Now().UTC()
	for _, booking := range []Booking{
		{
			ID: "admin-old", ShowtimeID: "showtime-1", SeatNo: "A3",
			UserID: "filter-user", Status: BookingStatusConfirmed, CreatedAt: older,
		},
		{
			ID: "admin-new", ShowtimeID: "showtime-1", SeatNo: "A4",
			UserID: "filter-user", Status: BookingStatusConfirmed, CreatedAt: newer,
		},
		{
			ID: "admin-other", ShowtimeID: "showtime-1", SeatNo: "A5",
			UserID: "other-user", Status: BookingStatusConfirmed, CreatedAt: newer.Add(time.Second),
		},
	} {
		if err := repository.Create(ctx, booking); err != nil {
			t.Fatal(err)
		}
	}
	adminBookings, err := repository.ListConfirmed(ctx, "filter-user", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(adminBookings) != 2 || adminBookings[0].ID != "admin-new" ||
		adminBookings[1].ID != "admin-old" {
		t.Fatalf("admin filtered bookings = %#v", adminBookings)
	}
}

func TestRealRedisGenerationTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	redisOptions, err := goredis.ParseURL(envOrDefault("REDIS_URI", "redis://127.0.0.1:6379/15"))
	if err != nil {
		t.Fatal(err)
	}
	redisClient := goredis.NewClient(redisOptions)
	defer redisClient.Close()

	channel := fmt.Sprintf("cinema.events.transitions.%d", time.Now().UnixNano())
	subscription := redisClient.Subscribe(ctx, channel)
	defer subscription.Close()
	if _, err := subscription.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	messages := subscription.Channel()
	repository := NewRedisLockRepository(redisClient, channel)
	showtimeID := "integration-ownership"
	seatNos := []string{"A1", "A2"}
	for _, seatNo := range seatNos {
		keys := []string{
			lockKey(showtimeID, seatNo),
			generationKey(showtimeID, seatNo),
			realtimeStateKey(showtimeID, seatNo),
		}
		if err := redisClient.Del(ctx, keys...).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = redisClient.Del(context.Background(), keys...).Err()
		})
	}

	const shortTTL = 1200 * time.Millisecond
	original := SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         "A1",
		UserID:         "user-1",
		OwnershipToken: "original-token",
	}
	acquired, generation, err := repository.Acquire(
		ctx,
		original,
		shortTTL,
		transitionEvent(t, events.SeatLocked, original, ""),
	)
	if err != nil || !acquired {
		t.Fatalf("Acquire() = %v, %d, %v", acquired, generation, err)
	}
	original.Generation = generation
	assertTransition(t, messages, events.SeatLocked, generation)
	markerKey := expirationMarkerPrefix(showtimeID, "A1") + fmt.Sprint(generation)
	if exists, err := redisClient.Exists(ctx, markerKey).Result(); err != nil || exists != 1 {
		t.Fatalf("expiration marker exists = %d, %v", exists, err)
	}
	beforeWrongOwnership, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}

	wrongToken := original
	wrongToken.OwnershipToken = "wrong-token"
	if result, err := repository.Release(
		ctx,
		wrongToken,
		transitionEvent(t, events.SeatReleased, wrongToken, ""),
	); err != nil || result != ReleaseNotOwned {
		t.Fatalf("wrong-token Release() = %v, %v", result, err)
	}
	wrongUser := original
	wrongUser.UserID = "user-2"
	if result, _, err := repository.VerifyOwnership(ctx, wrongUser); err != nil || result != OwnershipNotMatched {
		t.Fatalf("wrong-user VerifyOwnership() = %v, %v", result, err)
	}
	if result, _, err := repository.VerifyOwnership(ctx, wrongToken); err != nil || result != OwnershipNotMatched {
		t.Fatalf("wrong-token VerifyOwnership() = %v, %v", result, err)
	}
	afterWrongOwnership, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if afterWrongOwnership <= 0 || afterWrongOwnership > beforeWrongOwnership {
		t.Fatalf(
			"PTTL after wrong ownership = %v, want positive and at most %v",
			afterWrongOwnership,
			beforeWrongOwnership,
		)
	}

	before, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if result, verifiedGeneration, err := repository.VerifyOwnership(ctx, original); err != nil ||
		result != OwnershipMatched || verifiedGeneration != generation {
		t.Fatalf("matching VerifyOwnership() = %v, %v", result, err)
	}
	after, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if after <= 0 || after >= before {
		t.Fatalf("PTTL after verification = %v, want positive and less than %v", after, before)
	}

	if result, err := repository.Release(
		ctx,
		original,
		transitionEvent(t, events.SeatReleased, original, ""),
	); err != nil || result != ReleaseSucceeded {
		t.Fatalf("matching Release() = %v, %v", result, err)
	}
	assertTransition(t, messages, events.SeatReleased, generation)
	if exists, err := redisClient.Exists(ctx, markerKey).Result(); err != nil || exists != 0 {
		t.Fatalf("released marker exists = %d, %v", exists, err)
	}

	newer := original
	newer.OwnershipToken = "newer-token"
	acquired, newerGeneration, err := repository.Acquire(
		ctx,
		newer,
		shortTTL,
		transitionEvent(t, events.SeatLocked, newer, ""),
	)
	if err != nil || !acquired || newerGeneration <= generation {
		t.Fatalf("newer Acquire() = %v, %v", acquired, err)
	}
	newer.Generation = newerGeneration
	assertTransition(t, messages, events.SeatLocked, newerGeneration)
	if result, err := repository.Release(
		ctx,
		original,
		transitionEvent(t, events.SeatReleased, original, ""),
	); err != nil || result != ReleaseNotOwned {
		t.Fatalf("stale-token Release() = %v, %v", result, err)
	}
	if err := repository.Confirm(
		ctx,
		newer,
		transitionEvent(t, events.BookingConfirmed, newer, "booking-1"),
	); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	assertTransition(t, messages, events.BookingConfirmed, newerGeneration)
	newerMarker := expirationMarkerPrefix(showtimeID, "A1") + fmt.Sprint(newerGeneration)
	if count, err := redisClient.Exists(ctx, lockKey(showtimeID, "A1"), newerMarker).Result(); err != nil ||
		count != 0 {
		t.Fatalf("confirmed lock/marker count = %d, %v", count, err)
	}
	published, err := repository.PublishExpiration(
		ctx,
		showtimeID,
		"A1",
		generation,
		transitionEvent(t, events.SeatLockExpired, original, ""),
	)
	if err != nil || published {
		t.Fatalf("old expiration after newer BOOKED = %v, %v", published, err)
	}

	blocked := newer
	blocked.OwnershipToken = "blocked-token"
	if acquired, _, err := repository.Acquire(
		ctx,
		blocked,
		shortTTL,
		transitionEvent(t, events.SeatLocked, blocked, ""),
	); err != nil || acquired {
		t.Fatalf("acquire after BOOKED = %v, %v", acquired, err)
	}

	expiring := SeatLock{
		ShowtimeID: showtimeID, SeatNo: "A2", UserID: "user-1", OwnershipToken: "expiring-token",
	}
	acquired, expiringGeneration, err := repository.Acquire(
		ctx,
		expiring,
		shortTTL,
		transitionEvent(t, events.SeatLocked, expiring, ""),
	)
	if err != nil || !acquired {
		t.Fatalf("expiring Acquire() = %v, %v", acquired, err)
	}
	expiring.Generation = expiringGeneration
	assertTransition(t, messages, events.SeatLocked, expiringGeneration)
	if err := redisClient.Del(ctx, lockKey(showtimeID, "A2")).Err(); err != nil {
		t.Fatal(err)
	}
	published, err = repository.PublishExpiration(
		ctx,
		showtimeID,
		"A2",
		expiringGeneration,
		transitionEvent(t, events.SeatLockExpired, expiring, ""),
	)
	if err != nil || !published {
		t.Fatalf("PublishExpiration() = %v, %v", published, err)
	}
	assertTransition(t, messages, events.SeatLockExpired, expiringGeneration)
	if err := repository.Confirm(
		ctx,
		expiring,
		transitionEvent(t, events.BookingConfirmed, expiring, "booking-2"),
	); err != nil {
		t.Fatal(err)
	}
	assertTransition(t, messages, events.BookingConfirmed, expiringGeneration)
	published, err = repository.PublishExpiration(
		ctx,
		showtimeID,
		"A2",
		expiringGeneration,
		transitionEvent(t, events.SeatLockExpired, expiring, ""),
	)
	if err != nil || published {
		t.Fatalf("expiration after BOOKED = %v, %v", published, err)
	}
}

func transitionEvent(
	t *testing.T,
	eventType events.Type,
	lock SeatLock,
	bookingID string,
) events.DomainEvent {
	t.Helper()
	event, err := events.New(
		eventType,
		lock.ShowtimeID,
		lock.SeatNo,
		lock.UserID,
		bookingID,
		"",
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func assertTransition(
	t *testing.T,
	messages <-chan *goredis.Message,
	eventType events.Type,
	generation int64,
) events.DomainEvent {
	t.Helper()
	select {
	case message := <-messages:
		event, err := events.Unmarshal([]byte(message.Payload))
		if err != nil {
			t.Fatal(err)
		}
		if event.Type != eventType || event.Generation != generation {
			t.Fatalf("event = %#v, want type %q generation %d", event, eventType, generation)
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q generation %d", eventType, generation)
		return events.DomainEvent{}
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
