package events

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestEventJSONRoundTrip(t *testing.T) {
	original, err := New(
		BookingConfirmed,
		" showtime-1 ",
		"a1",
		"user-1",
		"booking-1",
		"",
		time.Date(2026, time.June, 14, 12, 0, 0, 0, time.FixedZone("test", 3600)),
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != original.ID || decoded.Type != BookingConfirmed ||
		decoded.ShowtimeID != "showtime-1" || decoded.SeatNo != "A1" ||
		decoded.Generation != 1 || decoded.OccurredAt.Location() != time.UTC {
		t.Fatalf("decoded event = %#v", decoded)
	}
}

func TestMalformedEventIsRejected(t *testing.T) {
	if _, err := Unmarshal([]byte(`{"type":`)); err == nil {
		t.Fatal("Unmarshal() expected malformed JSON error")
	}
	if _, err := Unmarshal([]byte(`{"version":1}`)); err == nil {
		t.Fatal("Unmarshal() expected invalid envelope error")
	}
	if _, err := Unmarshal([]byte(
		`{"version":1,"id":"event","type":"unknown","occurred_at":"2026-06-14T12:00:00Z","showtime_id":"showtime-1","seat_no":"A1"}`,
	)); err == nil {
		t.Fatal("Unmarshal() expected unknown event type error")
	}
	if _, err := Unmarshal([]byte(
		`{"version":1,"id":"event","type":"seat.locked","occurred_at":"2026-06-14T12:00:00Z","showtime_id":"showtime-1","seat_no":"A1"}`,
	)); err == nil {
		t.Fatal("Unmarshal() expected missing generation error")
	}
}

func TestSubscriberStopsOnCanceledContext(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	subscriber := NewRedisSubscriber(
		client,
		"test.events",
		func(context.Context, DomainEvent) error { return nil },
		log.New(io.Discard, "", 0),
	)
	done := make(chan error, 1)
	go func() { done <- subscriber.Run(ctx, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not stop after cancellation")
	}
}

func TestParseExpirationMarkerKey(t *testing.T) {
	showtime, seat, generation, matched, valid := parseExpirationMarkerKey(
		"seat_lock_expiry:showtime-1:a1:42",
	)
	if showtime != "showtime-1" || seat != "A1" || generation != 42 || !matched || !valid {
		t.Fatalf(
			"parseExpirationMarkerKey() = %q, %q, %d, %v, %v",
			showtime,
			seat,
			generation,
			matched,
			valid,
		)
	}
	_, _, _, matched, valid = parseExpirationMarkerKey("unrelated:key")
	if matched || valid {
		t.Fatal("unrelated key matched")
	}
	_, _, _, matched, valid = parseExpirationMarkerKey("seat_lock_expiry:broken")
	if !matched || valid {
		t.Fatal("malformed seat-lock marker was not identified")
	}
}
