package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestFromEventMapsAuditDocument(t *testing.T) {
	occurred := time.Date(2026, time.June, 14, 12, 0, 0, 0, time.UTC)
	processed := occurred.Add(time.Second)
	event := events.DomainEvent{
		Version: events.CurrentVersion, ID: "event-1", Type: events.BookingConfirmed,
		OccurredAt: occurred, ShowtimeID: "showtime-1", SeatNo: "A1",
		UserID: "user-1", BookingID: "booking-1",
	}
	entry := FromEvent(event, processed)
	if entry.EventID != event.ID || entry.EventType != event.Type ||
		entry.BookingID != event.BookingID || !entry.ProcessedAt.Equal(processed) {
		t.Fatalf("audit log = %#v", entry)
	}
}

func TestConsumerIgnoresUnauditedEvent(t *testing.T) {
	repository := &fakeRepository{}
	consumer := NewConsumer(repository)
	err := consumer.Handle(context.Background(), events.DomainEvent{Type: events.SeatLocked})
	if err != nil || len(repository.entries) != 0 {
		t.Fatalf("Handle() = %v, entries = %#v", err, repository.entries)
	}
}

func TestConsumerReturnsRepositoryError(t *testing.T) {
	repository := &fakeRepository{err: errors.New("mongo unavailable")}
	consumer := NewConsumer(repository)
	err := consumer.Handle(context.Background(), events.DomainEvent{
		ID: "event-1", Type: events.SeatReleased, OccurredAt: time.Now(),
		ShowtimeID: "showtime-1", SeatNo: "A1",
	})
	if err == nil {
		t.Fatal("Handle() expected repository error")
	}
}

type fakeRepository struct {
	entries []Log
	err     error
}

func (f *fakeRepository) Insert(_ context.Context, entry Log) error {
	if f.err != nil {
		return f.err
	}
	for _, existing := range f.entries {
		if existing.EventID == entry.EventID {
			return nil
		}
	}
	f.entries = append(f.entries, entry)
	return nil
}
