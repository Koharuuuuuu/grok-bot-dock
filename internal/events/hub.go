package events

import (
	"encoding/json"
	"sync"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	closed  bool
}

func New() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }
func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	if h.closed {
		close(ch)
		h.mu.Unlock()
		return ch
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Close ends every active stream and prevents new subscriptions during shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.clients {
		delete(h.clients, ch)
		close(ch)
	}
}
func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}
func (h *Hub) Publish(kind string, value any) {
	data, _ := json.Marshal(value)
	msg := append([]byte("event: "+kind+"\ndata: "), data...)
	msg = append(msg, '\n', '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}
