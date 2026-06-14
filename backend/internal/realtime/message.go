package realtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/booking"
	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

const MessageTypeSeatUpdated = "seat.updated"

type SeatUpdate struct {
	Type       string    `json:"type"`
	EventID    string    `json:"event_id"`
	ShowtimeID string    `json:"showtime_id"`
	SeatNo     string    `json:"seat_no"`
	State      string    `json:"state"`
	Revision   int64     `json:"revision"`
	OccurredAt time.Time `json:"occurred_at"`
}

func Project(event events.DomainEvent) (SeatUpdate, bool) {
	state := ""
	switch event.Type {
	case events.SeatLocked:
		state = booking.SeatStateLocked
	case events.SeatReleased, events.SeatLockExpired:
		state = booking.SeatStateAvailable
	case events.BookingConfirmed:
		state = booking.SeatStateBooked
	default:
		return SeatUpdate{}, false
	}
	return SeatUpdate{
		Type:       MessageTypeSeatUpdated,
		EventID:    event.ID,
		ShowtimeID: event.ShowtimeID,
		SeatNo:     event.SeatNo,
		State:      state,
		Revision:   event.Generation,
		OccurredAt: event.OccurredAt.UTC(),
	}, true
}

type Consumer struct {
	hub *Hub
}

func NewConsumer(hub *Hub) *Consumer {
	return &Consumer{hub: hub}
}

func (c *Consumer) Handle(_ context.Context, event events.DomainEvent) error {
	update, ok := Project(event)
	if !ok {
		return nil
	}
	data, err := json.Marshal(update)
	if err != nil {
		return err
	}
	c.hub.Broadcast(update.ShowtimeID, data)
	return nil
}
