//go:build integration

package realtime

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
)

func TestWebSocketServerRooms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := NewHub()
	handler := NewHandler(hub, []string{"http://localhost:5173"})
	router := gin.New()
	router.GET("/ws/showtimes/:showtimeId", handler.Get)
	server := httptest.NewServer(router)
	defer server.Close()
	defer hub.Shutdown()

	connect := func(showtimeID string) *websocket.Conn {
		t.Helper()
		url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/showtimes/" + showtimeID
		connection, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		return connection
	}
	first := connect("showtime-1")
	second := connect("showtime-1")
	other := connect("showtime-2")
	time.Sleep(50 * time.Millisecond)

	consumer := NewConsumer(hub)
	event := events.DomainEvent{
		Version: events.CurrentVersion, ID: "event-1", Type: events.SeatLocked,
		OccurredAt: time.Now().UTC(), ShowtimeID: "showtime-1", SeatNo: "A1",
		UserID: "private-user",
	}
	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	for _, connection := range []*websocket.Conn{first, second} {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var update SeatUpdate
		if err := json.Unmarshal(data, &update); err != nil {
			t.Fatal(err)
		}
		if update.State != "LOCKED" || strings.Contains(string(data), "private-user") {
			t.Fatalf("public update = %s", data)
		}
	}
	_ = other.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := other.ReadMessage(); err == nil {
		t.Fatal("other showtime unexpectedly received update")
	}
}
