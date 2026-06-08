package channel

import (
	"context"
	"encoding/json"
	"sync"
)

type WSMessage struct {
	Type        string `json:"type"`
	AgentFlowID string `json:"agentflow_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Data        any    `json:"data,omitempty"`
}

type WSConn interface {
	ReadMessages() <-chan []byte
	Done() <-chan struct{}
	Close()
}

type WSHub struct {
	mu      sync.RWMutex
	clients map[string]WSConn
}

func NewWSHub() *WSHub { return &WSHub{clients: make(map[string]WSConn)} }

func (h *WSHub) Type() string { return "websocket" }

func (h *WSHub) Send(_ context.Context, _ string, title string, body string) error {
	msg := WSMessage{Type: "notification", Data: map[string]string{"title": title, "body": body}}
	b, _ := json.Marshal(msg)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, conn := range h.clients {
		select {
		case <-conn.Done():
			go func(cid string) { h.Unregister(cid) }(id)
		default:
			_ = b
		}
	}
	return nil
}

func (h *WSHub) Validate() error                 { return nil }
func (h *WSHub) Register(id string, conn WSConn) { h.mu.Lock(); h.clients[id] = conn; h.mu.Unlock() }
func (h *WSHub) Unregister(id string)            { h.mu.Lock(); delete(h.clients, id); h.mu.Unlock() }
