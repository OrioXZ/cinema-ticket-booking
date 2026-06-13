//go:build integration

package booking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	service := NewService(repository, repository, lockRepository)

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

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
