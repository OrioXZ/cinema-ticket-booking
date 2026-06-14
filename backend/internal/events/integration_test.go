//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/audit"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/lifecycle"
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
	ready := make(chan struct{}, 1)
	go func() { done <- subscriber.Run(ctx, func() { ready <- struct{}{} }) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not become ready")
	}

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
	logger := log.New(io.Discard, "", 0)
	processor := events.NewExpirationProcessor(
		staticBookingStateReader{},
		events.NewRedisExpirationPublisher(client),
		channel,
		logger,
	)
	listener := events.NewExpirationListener(client, client.Options().DB, processor)

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

	workers, err := lifecycle.Start(ctx, 2*time.Second, []lifecycle.Worker{
		{Name: "expiration", Run: listener.Run},
		{Name: "realtime", Run: realtimeSubscriber.Run},
		{Name: "audit", Run: auditSubscriber.Run},
	})
	if err != nil {
		t.Fatal(err)
	}

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
	workers.Stop()
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

func TestRedisExpirationPublisherPublishesOnlyWhenUnlocked(t *testing.T) {
	client := integrationRedis(t)
	channel := fmt.Sprintf("cinema.events.lua.%d", time.Now().UnixNano())
	subscription := client.Subscribe(context.Background(), channel)
	defer subscription.Close()
	if _, err := subscription.Receive(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages := subscription.Channel()

	publisher := events.NewRedisExpirationPublisher(client)
	first, err := events.New(
		events.SeatLockExpired, "showtime-lua", "A1", "", "", "lock expired", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	published, err := publisher.PublishIfUnlocked(
		context.Background(),
		"seat_lock:showtime-lua:A1",
		channel,
		first,
	)
	if err != nil || !published {
		t.Fatalf("PublishIfUnlocked() = %v, %v", published, err)
	}
	var message *goredis.Message
	select {
	case message = <-messages:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for expiration event")
	}
	received, err := events.Unmarshal([]byte(message.Payload))
	if err != nil {
		t.Fatal(err)
	}
	if received.ID != first.ID || received.UserID != "" ||
		strings.Contains(message.Payload, "ownership_token") {
		t.Fatalf("expiration event = %s", message.Payload)
	}

	lockKey := "seat_lock:showtime-lua:A2"
	if err := client.Set(context.Background(), lockKey, "private-lock-value", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), lockKey).Err() })
	second, err := events.New(
		events.SeatLockExpired, "showtime-lua", "A2", "", "", "lock expired", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	published, err = publisher.PublishIfUnlocked(context.Background(), lockKey, channel, second)
	if err != nil || published {
		t.Fatalf("PublishIfUnlocked() with current lock = %v, %v", published, err)
	}
	select {
	case message := <-messages:
		t.Fatalf("unexpected second expiration event = %s", message.Payload)
	case <-time.After(150 * time.Millisecond):
	}
	if first.ID == second.ID || first.ID == "" || second.ID == "" {
		t.Fatalf("event IDs are not unique and valid: %q, %q", first.ID, second.ID)
	}
}

func TestStaleExpirationAfterNewLockProducesNoAvailableUpdate(t *testing.T) {
	client := integrationRedis(t)
	channel := fmt.Sprintf("cinema.events.race.%d", time.Now().UnixNano())
	logger := log.New(io.Discard, "", 0)
	hub := realtime.NewHub()
	defer hub.Shutdown()
	realtimeClient := hub.Register("showtime-race", 1)
	subscriber := events.NewRedisSubscriber(
		client,
		channel,
		realtime.NewConsumer(hub).Handle,
		logger,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- subscriber.Run(ctx, func() { ready <- struct{}{} }) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("realtime subscriber did not become ready")
	}

	lockKey := "seat_lock:showtime-race:A1"
	if err := client.Set(ctx, lockKey, "new-owner-private-value", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), lockKey).Err() })
	processor := events.NewExpirationProcessor(
		staticBookingStateReader{},
		events.NewRedisExpirationPublisher(client),
		channel,
		logger,
	)
	processor.Process(ctx, lockKey)

	select {
	case data := <-realtimeClient.Messages():
		t.Fatalf("stale expiration produced realtime update: %s", data)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("realtime subscriber did not stop")
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

type staticBookingStateReader struct{}

func (staticBookingStateReader) IsBooked(context.Context, string, string) (bool, error) {
	return false, nil
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
