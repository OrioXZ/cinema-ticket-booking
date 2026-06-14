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
		redisClient.Del(ctx, lockKey("showtime-1", seatNo))
		defer redisClient.Del(context.Background(), lockKey("showtime-1", seatNo))
	}

	lockRepository := NewRedisLockRepository(redisClient)
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
}

func TestRealRedisOwnershipAndTTL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	redisOptions, err := goredis.ParseURL(envOrDefault("REDIS_URI", "redis://127.0.0.1:6379/15"))
	if err != nil {
		t.Fatal(err)
	}
	redisClient := goredis.NewClient(redisOptions)
	defer redisClient.Close()

	repository := NewRedisLockRepository(redisClient)
	showtimeID := "integration-ownership"
	seatNos := []string{"A1", "A2"}
	for _, seatNo := range seatNos {
		key := lockKey(showtimeID, seatNo)
		if err := redisClient.Del(ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = redisClient.Del(context.Background(), key).Err()
		})
	}

	const shortTTL = 1200 * time.Millisecond
	original := SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         "A1",
		UserID:         "user-1",
		OwnershipToken: "original-token",
	}
	acquired, err := repository.Acquire(ctx, original, shortTTL)
	if err != nil || !acquired {
		t.Fatalf("Acquire() = %v, %v", acquired, err)
	}
	beforeWrongOwnership, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}

	wrongToken := original
	wrongToken.OwnershipToken = "wrong-token"
	if result, err := repository.Release(ctx, wrongToken); err != nil || result != ReleaseNotOwned {
		t.Fatalf("wrong-token Release() = %v, %v", result, err)
	}
	wrongUser := original
	wrongUser.UserID = "user-2"
	if result, err := repository.VerifyOwnership(ctx, wrongUser); err != nil || result != OwnershipNotMatched {
		t.Fatalf("wrong-user VerifyOwnership() = %v, %v", result, err)
	}
	if result, err := repository.VerifyOwnership(ctx, wrongToken); err != nil || result != OwnershipNotMatched {
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
	if result, err := repository.VerifyOwnership(ctx, original); err != nil || result != OwnershipMatched {
		t.Fatalf("matching VerifyOwnership() = %v, %v", result, err)
	}
	after, err := redisClient.PTTL(ctx, lockKey(showtimeID, "A1")).Result()
	if err != nil {
		t.Fatal(err)
	}
	if after <= 0 || after >= before {
		t.Fatalf("PTTL after verification = %v, want positive and less than %v", after, before)
	}

	if result, err := repository.Release(ctx, original); err != nil || result != ReleaseSucceeded {
		t.Fatalf("matching Release() = %v, %v", result, err)
	}
	newer := original
	newer.OwnershipToken = "newer-token"
	if acquired, err := repository.Acquire(ctx, newer, shortTTL); err != nil || !acquired {
		t.Fatalf("newer Acquire() = %v, %v", acquired, err)
	}
	if result, err := repository.Release(ctx, original); err != nil || result != ReleaseNotOwned {
		t.Fatalf("stale-token Release() = %v, %v", result, err)
	}
	if result, err := repository.Release(ctx, newer); err != nil || result != ReleaseSucceeded {
		t.Fatalf("newer-owner Release() = %v, %v", result, err)
	}

	expiring := SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         "A2",
		UserID:         "user-1",
		OwnershipToken: "expiring-token",
	}
	if acquired, err := repository.Acquire(ctx, expiring, 150*time.Millisecond); err != nil || !acquired {
		t.Fatalf("expiring Acquire() = %v, %v", acquired, err)
	}
	time.Sleep(250 * time.Millisecond)
	replacement := expiring
	replacement.UserID = "user-2"
	replacement.OwnershipToken = "replacement-token"
	if acquired, err := repository.Acquire(ctx, replacement, shortTTL); err != nil || !acquired {
		t.Fatalf("post-expiry Acquire() = %v, %v", acquired, err)
	}
	if result, err := repository.Release(ctx, replacement); err != nil || result != ReleaseSucceeded {
		t.Fatalf("replacement Release() = %v, %v", result, err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
