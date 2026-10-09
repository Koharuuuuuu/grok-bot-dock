package events

import (
	"encoding/json"
	"sync"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func New() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }
func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
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
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}
