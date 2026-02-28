package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/common/utils"
)

// WSBridge connects the HTTP WebSocket upgrade endpoint to the notification service.
type WSBridge struct {
	notifier interface {
		RegisterWS(ctx context.Context, agentFlowID string) (WSConnection, error)
		PodID() string
	}
	logger *utils.Logger
}

// WSConnection represents a single WebSocket client connection.
type WSConnection interface {
	ReadMessages() <-chan []byte
	Done() <-chan struct{}
}

// NewWSBridge creates a WebSocket bridge backed by a notification service.
// The notifier must implement RegisterWS and PodID methods.
func NewWSBridge(notifier interface {
	RegisterWS(ctx context.Context, agentFlowID string) (WSConnection, error)
	PodID() string
}, logger *utils.Logger) *WSBridge {
	return &WSBridge{notifier: notifier, logger: logger}
}

// HandleHumanApprovals upgrades the connection and streams human approval events
// using Server-Sent Events (zero-dependency approach).
func (b *WSBridge) HandleHumanApprovals(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	agentFlowID := r.URL.Query().Get("agentflow_id")
	conn, err := b.notifier.RegisterWS(r.Context(), agentFlowID)
	if err != nil {
		b.logger.Error("register ws", "error", err)
		return
	}

	ctx := r.Context()
	for {
		select {
		case msg := <-conn.ReadMessages():
			var wsMsg model.WSMessage
			if err := json.Unmarshal(msg, &wsMsg); err != nil {
				continue
			}
			data, _ := json.Marshal(wsMsg)
			w.Write([]byte("data: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-conn.Done():
			return
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}
