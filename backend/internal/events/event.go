package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const CurrentVersion = 1

type Type string

const (
	SeatLocked            Type = "seat.locked"
	SeatReleased          Type = "seat.released"
	SeatLockExpired       Type = "seat.lock_expired"
	BookingConfirmed      Type = "booking.confirmed"
	LockAcquisitionFailed Type = "lock.acquisition_failed"
)

type DomainEvent struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Type       Type      `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	ShowtimeID string    `json:"showtime_id"`
	SeatNo     string    `json:"seat_no"`
	UserID     string    `json:"user_id,omitempty"`
	BookingID  string    `json:"booking_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

type Publisher interface {
	Publish(context.Context, DomainEvent) error
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, DomainEvent) error {
	return nil
}

func New(
	eventType Type,
	showtimeID string,
	seatNo string,
	userID string,
	bookingID string,
	reason string,
	now time.Time,
) (DomainEvent, error) {
	id, err := randomID(16)
	if err != nil {
		return DomainEvent{}, fmt.Errorf("generate event ID: %w", err)
	}
	return DomainEvent{
		Version:    CurrentVersion,
		ID:         id,
		Type:       eventType,
		OccurredAt: now.UTC(),
		ShowtimeID: strings.TrimSpace(showtimeID),
		SeatNo:     strings.ToUpper(strings.TrimSpace(seatNo)),
		UserID:     strings.TrimSpace(userID),
		BookingID:  strings.TrimSpace(bookingID),
		Reason:     strings.TrimSpace(reason),
	}, nil
}

func Marshal(event DomainEvent) ([]byte, error) {
	return json.Marshal(event)
}

func Unmarshal(data []byte) (DomainEvent, error) {
	var event DomainEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return DomainEvent{}, err
	}
	if event.Version != CurrentVersion || event.ID == "" || event.Type == "" ||
		event.OccurredAt.IsZero() || event.ShowtimeID == "" || event.SeatNo == "" ||
		!event.Type.Valid() {
		return DomainEvent{}, fmt.Errorf("invalid domain event envelope")
	}
	return event, nil
}

func (eventType Type) Valid() bool {
	switch eventType {
	case SeatLocked,
		SeatReleased,
		SeatLockExpired,
		BookingConfirmed,
		LockAcquisitionFailed:
		return true
	default:
		return false
	}
}

func randomID(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
