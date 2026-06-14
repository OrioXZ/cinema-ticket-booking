package realtime

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	sendBufferSize = 32
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second
	maxMessageSize = 1024
)

type Handler struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

func NewHandler(hub *Hub, allowedOrigins []string) *Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return &Handler{
		hub: hub,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(request *http.Request) bool {
				origin := request.Header.Get("Origin")
				if origin == "" {
					return true
				}
				_, ok := allowed[origin]
				return ok
			},
		},
	}
}

func (h *Handler) Get(c *gin.Context) {
	showtimeID := strings.TrimSpace(c.Param("showtimeId"))
	if showtimeID == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := h.hub.Register(showtimeID, sendBufferSize)
	go h.writePump(connection, client)
	h.readPump(connection, client)
}

func (h *Handler) readPump(connection *websocket.Conn, client *Client) {
	defer func() {
		h.hub.Unregister(client)
		_ = connection.Close()
	}()
	connection.SetReadLimit(maxMessageSize)
	_ = connection.SetReadDeadline(time.Now().Add(pongWait))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Handler) writePump(connection *websocket.Conn, client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = connection.Close()
	}()
	for {
		select {
		case message, ok := <-client.send:
			_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = connection.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := connection.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
			if err := connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
