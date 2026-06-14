//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/audit"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/realtime"
)

func TestRealRedisPublishSubscribeDelivery(t *testing.T) {
	client := integrationRedis(t)
	channel := fmt.Sprintf("cinema.events.integration.%d", time.Now().UnixNano())
	received := make(chan events.DomainEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subscriber := events.NewRedisSubscriber(
		client,
		channel,
		func(_ context.Context, event events.DomainEvent) error {
			received <- event
			return nil
		},
		log.New(io.Discard, "", 0),
	)
	done := make(chan error, 1)
	go func() { done <- subscriber.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	publisher := events.NewRedisPublisher(client, channel)
	if err := client.Publish(context.Background(), channel, `{"type":`).Err(); err != nil {
		t.Fatal(err)
	}
	event, err := events.New(
		events.SeatLocked, "showtime-1", "A1", "user-1", "", "", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got.ID != event.ID || got.Type != events.SeatLocked {
			t.Fatalf("received event = %#v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Redis event")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("subscriber shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not stop")
	}
}

func TestExpiredSeatLockProducesRealtimeAndAuditEvents(t *testing.T) {
	client := integrationRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldConfig, err := client.ConfigGet(ctx, "notify-keyspace-events").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if value, ok := oldConfig["notify-keyspace-events"]; ok {
			_ = client.ConfigSet(context.Background(), "notify-keyspace-events", value).Err()
		}
	})

	channel := fmt.Sprintf("cinema.events.expiration.%d", time.Now().UnixNano())
	publisher := events.NewRedisPublisher(client, channel)
	logger := log.New(io.Discard, "", 0)
	listener := events.NewExpirationListener(client, client.Options().DB, publisher, logger)

	hub := realtime.NewHub()
	realtimeClient := hub.Register("showtime-expiration", 4)
	realtimeConsumer := realtime.NewConsumer(hub)
	auditRepository := &memoryAuditRepository{}
	auditConsumer := audit.NewConsumer(auditRepository)

	realtimeSubscriber := events.NewRedisSubscriber(
		client, channel, realtimeConsumer.Handle, logger,
	)
	auditSubscriber := events.NewRedisSubscriber(
		client, channel, auditConsumer.Handle, logger,
	)

	var workers sync.WaitGroup
	for _, run := range []func(context.Context) error{
		listener.Run, realtimeSubscriber.Run, auditSubscriber.Run,
	} {
		workers.Add(1)
		go func(run func(context.Context) error) {
			defer workers.Done()
			_ = run(ctx)
		}(run)
	}
	time.Sleep(150 * time.Millisecond)

	lockKey := "seat_lock:showtime-expiration:A1"
	unrelatedKey := "unrelated:expiration"
	t.Cleanup(func() {
		_ = client.Del(context.Background(), lockKey, unrelatedKey).Err()
	})
	if err := client.Set(ctx, unrelatedKey, "value", 100*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, lockKey, "value", 150*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-realtimeClient.Messages():
		var update realtime.SeatUpdate
		if err := json.Unmarshal(data, &update); err != nil {
			t.Fatal(err)
		}
		if update.State != "AVAILABLE" || update.ShowtimeID != "showtime-expiration" ||
			update.SeatNo != "A1" {
			t.Fatalf("seat update = %#v", update)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for expiration realtime update")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if auditRepository.HasType(events.SeatLockExpired) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !auditRepository.HasType(events.SeatLockExpired) {
		t.Fatal("audit consumer did not record seat.lock_expired")
	}
	if auditRepository.Count() != 1 {
		t.Fatalf("audit count = %d, unrelated expiration should be ignored", auditRepository.Count())
	}

	cancel()
	hub.Shutdown()
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("expiration workers did not stop")
	}
}

func integrationRedis(t *testing.T) *goredis.Client {
	t.Helper()
	options, err := goredis.ParseURL(envOrDefault("REDIS_URI", "redis://127.0.0.1:6379/15"))
	if err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type memoryAuditRepository struct {
	mu      sync.Mutex
	entries []audit.Log
}

func (r *memoryAuditRepository) Insert(_ context.Context, entry audit.Log) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.entries {
		if existing.EventID == entry.EventID {
			return nil
		}
	}
	r.entries = append(r.entries, entry)
	return nil
}

func (r *memoryAuditRepository) HasType(eventType events.Type) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		if entry.EventType == eventType {
			return true
		}
	}
	return false
}

func (r *memoryAuditRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
