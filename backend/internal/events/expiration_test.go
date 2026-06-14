package events

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
)

func TestExpirationProcessorPublishesAbandonedLock(t *testing.T) {
	bookings := &fakeBookingStateReader{}
	publisher := &fakeExpirationPublisher{}
	processor := NewExpirationProcessor(
		bookings,
		publisher,
		log.New(&bytes.Buffer{}, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:a1:1")

	events := publisher.Events()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	if events[0].Type != SeatLockExpired || events[0].ShowtimeID != "showtime-1" ||
		events[0].SeatNo != "A1" {
		t.Fatalf("published event = %#v", events[0])
	}
	if events[0].UserID != "" {
		t.Fatalf("expiration event user_id = %q, want empty", events[0].UserID)
	}
}

func TestExpirationProcessorSkipsBookedSeat(t *testing.T) {
	publisher := &fakeExpirationPublisher{}
	processor := NewExpirationProcessor(
		&fakeBookingStateReader{booked: true},
		publisher,
		log.New(&bytes.Buffer{}, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A1:1")

	if len(publisher.Events()) != 0 {
		t.Fatal("booked seat published an expiration event")
	}
}

func TestExpirationProcessorSkipsWhenNewerLockExists(t *testing.T) {
	publisher := &fakeExpirationPublisher{skip: true}
	processor := NewExpirationProcessor(
		&fakeBookingStateReader{},
		publisher,
		log.New(&bytes.Buffer{}, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A1:2")

	if publisher.Calls() != 1 {
		t.Fatalf("atomic publisher calls = %d, want 1", publisher.Calls())
	}
	if len(publisher.Events()) != 0 {
		t.Fatal("newer lock allowed an expiration event")
	}
}

func TestExpirationProcessorMongoFailureIsSafeAndRecoverable(t *testing.T) {
	var logs bytes.Buffer
	bookings := &fakeBookingStateReader{
		err: errors.New("mongodb://user:secret@mongo unavailable"),
	}
	publisher := &fakeExpirationPublisher{}
	processor := NewExpirationProcessor(
		bookings,
		publisher,
		log.New(&logs, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A1:1")
	bookings.err = nil
	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A2:2")

	if len(publisher.Events()) != 1 || publisher.Events()[0].SeatNo != "A2" {
		t.Fatalf("published events after recovery = %#v", publisher.Events())
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "mongodb://") {
		t.Fatalf("log exposed connection details: %q", logs.String())
	}
}

func TestExpirationProcessorIgnoresMalformedAndUnrelatedKeys(t *testing.T) {
	var logs bytes.Buffer
	publisher := &fakeExpirationPublisher{}
	processor := NewExpirationProcessor(
		&fakeBookingStateReader{},
		publisher,
		log.New(&logs, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:broken")
	processor.Process(context.Background(), "unrelated:key")

	if publisher.Calls() != 0 {
		t.Fatalf("atomic publisher calls = %d, want 0", publisher.Calls())
	}
	if !strings.Contains(logs.String(), "malformed") {
		t.Fatalf("malformed-key log = %q", logs.String())
	}
}

func TestExpirationProcessorLuaFailureHasNoFallback(t *testing.T) {
	var logs bytes.Buffer
	publisher := &fakeExpirationPublisher{
		err: errors.New("redis://:secret@redis unavailable"),
	}
	processor := NewExpirationProcessor(
		&fakeBookingStateReader{},
		publisher,
		log.New(&logs, "", 0),
	)

	processor.Process(context.Background(), "seat_lock_expiry:showtime-1:A1:1")

	if len(publisher.Events()) != 0 {
		t.Fatal("Lua failure published a fallback event")
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "redis://") {
		t.Fatalf("log exposed connection details: %q", logs.String())
	}
}

func TestOldExpirationPausedBeforeFinalGateCannotFollowBooked(t *testing.T) {
	publisher := &barrierExpirationPublisher{
		entered: make(chan struct{}),
		resume:  make(chan struct{}),
	}
	processor := NewExpirationProcessor(
		&fakeBookingStateReader{},
		publisher,
		log.New(&bytes.Buffer{}, "", 0),
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		processor.Process(
			context.Background(),
			"seat_lock_expiry:showtime-1:A1:1",
		)
	}()

	<-publisher.entered
	publisher.mu.Lock()
	publisher.booked = true
	publisher.mu.Unlock()
	close(publisher.resume)
	<-done

	if publisher.Published() != 0 {
		t.Fatal("old expiration published AVAILABLE after BOOKED")
	}
}

type fakeBookingStateReader struct {
	booked bool
	err    error
}

func (r *fakeBookingStateReader) IsBooked(context.Context, string, string) (bool, error) {
	return r.booked, r.err
}

type fakeExpirationPublisher struct {
	mu     sync.Mutex
	events []DomainEvent
	calls  int
	skip   bool
	err    error
}

type barrierExpirationPublisher struct {
	mu        sync.Mutex
	entered   chan struct{}
	resume    chan struct{}
	booked    bool
	published int
}

func (p *barrierExpirationPublisher) PublishExpiration(
	context.Context,
	string,
	string,
	int64,
	DomainEvent,
) (bool, error) {
	close(p.entered)
	<-p.resume
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.booked {
		return false, nil
	}
	p.published++
	return true, nil
}

func (p *barrierExpirationPublisher) Published() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.published
}

func (p *fakeExpirationPublisher) PublishExpiration(
	_ context.Context,
	_ string,
	_ string,
	_ int64,
	event DomainEvent,
) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return false, p.err
	}
	if p.skip {
		return false, nil
	}
	p.events = append(p.events, event)
	return true, nil
}

func (p *fakeExpirationPublisher) Events() []DomainEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]DomainEvent(nil), p.events...)
}

func (p *fakeExpirationPublisher) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
