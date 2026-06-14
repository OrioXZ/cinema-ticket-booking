package realtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestHubBroadcastsOnlyToMatchingRoom(t *testing.T) {
	hub := NewHub()
	first := hub.Register("showtime-1", 1)
	second := hub.Register("showtime-1", 1)
	other := hub.Register("showtime-2", 1)

	hub.Broadcast("showtime-1", []byte("update"))
	for _, client := range []*Client{first, second} {
		select {
		case message := <-client.send:
			if string(message) != "update" {
				t.Fatalf("message = %q", message)
			}
		default:
			t.Fatal("matching client did not receive update")
		}
	}
	select {
	case message := <-other.send:
		t.Fatalf("other room received %q", message)
	default:
	}
}

func TestPublicProjectionDoesNotExposeUserID(t *testing.T) {
	update, ok := Project(events.DomainEvent{
		ID: "event-1", Type: events.SeatLocked, OccurredAt: time.Now(),
		ShowtimeID: "showtime-1", SeatNo: "A1", Generation: 42, UserID: "private-user",
	})
	if !ok {
		t.Fatal("Project() did not produce update")
	}
	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-user") || strings.Contains(string(data), "user_id") {
		t.Fatalf("public message exposed identity: %s", data)
	}
	if update.Revision != 42 {
		t.Fatalf("revision = %d, want 42", update.Revision)
	}
}

func TestSlowClientDoesNotBlockOthers(t *testing.T) {
	hub := NewHub()
	slow := hub.Register("showtime-1", 1)
	fast := hub.Register("showtime-1", 2)
	hub.Broadcast("showtime-1", []byte("one"))
	hub.Broadcast("showtime-1", []byte("two"))

	if message := <-fast.send; string(message) != "one" {
		t.Fatalf("first fast message = %q", message)
	}
	if message := <-fast.send; string(message) != "two" {
		t.Fatalf("second fast message = %q", message)
	}
	if _, ok := <-slow.send; !ok {
		t.Fatal("buffered slow-client message should be readable before close")
	}
	if _, ok := <-slow.send; ok {
		t.Fatal("slow client was not closed")
	}
}

func TestUnregisterIsSafeAndShutdownClosesClients(t *testing.T) {
	hub := NewHub()
	client := hub.Register("showtime-1", 1)
	hub.Unregister(client)
	hub.Unregister(client)

	remaining := hub.Register("showtime-1", 1)
	hub.Shutdown()
	hub.Shutdown()
	if _, ok := <-remaining.send; ok {
		t.Fatal("shutdown did not close client")
	}
}
