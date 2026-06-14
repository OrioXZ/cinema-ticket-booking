//go:build integration

package audit

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestMongoAuditRepositoryIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(
		envOrDefault("MONGO_URI", "mongodb://cinema:cinema_dev_password@127.0.0.1:27017/?authSource=admin"),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	database := client.Database(envOrDefault("MONGO_INTEGRATION_DATABASE", "cinema_phase3_audit_integration"))
	defer database.Drop(context.Background())

	repository := NewMongoRepository(database)
	if err := repository.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	consumer := NewConsumer(repository)
	event := events.DomainEvent{
		Version: events.CurrentVersion, ID: "audit-event-1",
		Type: events.BookingConfirmed, OccurredAt: time.Now().UTC(),
		ShowtimeID: "showtime-1", SeatNo: "A1", UserID: "user-1", BookingID: "booking-1",
	}
	if err := consumer.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	count, err := database.Collection(collectionName).CountDocuments(ctx, bson.M{"event_id": event.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit document count = %d, want 1", count)
	}
}

func TestRedisEventProducesMongoAuditRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(
		envOrDefault("MONGO_URI", "mongodb://cinema:cinema_dev_password@127.0.0.1:27017/?authSource=admin"),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer mongoClient.Disconnect(context.Background())
	database := mongoClient.Database("cinema_phase3_redis_audit_integration")
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
	consumer := NewConsumer(repository)
	channel := fmt.Sprintf("cinema.events.audit.%d", time.Now().UnixNano())
	subscriber := events.NewRedisSubscriber(
		redisClient, channel, consumer.Handle, log.New(io.Discard, "", 0),
	)
	subscriberCtx, stopSubscriber := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan struct{}, 1)
	go func() { done <- subscriber.Run(subscriberCtx, func() { ready <- struct{}{} }) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("audit subscriber did not become ready")
	}

	event, err := events.New(
		events.SeatReleased, "showtime-1", "A2", "user-1", "", "", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	publisher := events.NewRedisPublisher(redisClient, channel)
	if err := publisher.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		count, err := database.Collection(collectionName).CountDocuments(ctx, bson.M{"event_id": event.ID})
		if err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	count, err := database.Collection(collectionName).CountDocuments(ctx, bson.M{"event_id": event.ID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit document count = %d, want 1", count)
	}
	stopSubscriber()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("subscriber shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("audit subscriber did not stop")
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
