package realtime

import "sync"

type Client struct {
	room string
	send chan []byte
}

func (c *Client) Messages() <-chan []byte {
	return c.send
}

type Hub struct {
	mu     sync.Mutex
	rooms  map[string]map[*Client]struct{}
	closed bool
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[string]map[*Client]struct{})}
}

func (h *Hub) Register(room string, buffer int) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	client := &Client{room: room, send: make(chan []byte, buffer)}
	if h.closed {
		close(client.send)
		return client
	}
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*Client]struct{})
	}
	h.rooms[room][client] = struct{}{}
	return client
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	clients := h.rooms[client.room]
	if _, exists := clients[client]; !exists {
		return
	}
	delete(clients, client)
	close(client.send)
	if len(clients) == 0 {
		delete(h.rooms, client.room)
	}
}

func (h *Hub) Broadcast(room string, message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.rooms[room] {
		select {
		case client.send <- append([]byte(nil), message...):
		default:
			delete(h.rooms[room], client)
			close(client.send)
		}
	}
	if len(h.rooms[room]) == 0 {
		delete(h.rooms, room)
	}
}

func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for room, clients := range h.rooms {
		for client := range clients {
			close(client.send)
		}
		delete(h.rooms, room)
	}
}
