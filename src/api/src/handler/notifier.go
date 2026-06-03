package handler

import (
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"context"
	"github.com/flowgent-labs/flowgent/model/src"
)

// NotifierStore is the subset of store.Store needed by NotifierHandler.
type NotifierStore interface {
	ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
}

type NotifierHandler struct {
	store  NotifierStore
	logger *utils.Logger
}

func NewNotifierHandler(s NotifierStore, logger *utils.Logger) *NotifierHandler {
	return &NotifierHandler{store: s, logger: logger}
}

func (h *NotifierHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.store.ListNotifierChannels(r.Context(), r.PathValue("tenant"))
	if err != nil { http.Error(w, err.Error(), 500); return }
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

func (h *NotifierHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var ch model.NotifierChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil { http.Error(w, "invalid body", 400); return }
	w.WriteHeader(201)
}

func (h *NotifierHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": r.PathValue("id")})
}

func (h *NotifierHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
func (h *NotifierHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
func (h *NotifierHandler) TestChannel(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }

// NotifierWSBridge bridges WebSocket connections to the notifier service.
type NotifierWSBridge struct {
	svc NotifierService
}

type NotifierService interface {
	RegisterWS(ctx context.Context, agentFlowID string) (WSConn, error)
}

type WSConn interface {
	ReadMessages() <-chan []byte
}

func NewNotifierWSBridge(svc NotifierService) *NotifierWSBridge {
	return &NotifierWSBridge{svc: svc}
}

func (b *NotifierWSBridge) HandleHumanApprovals(w http.ResponseWriter, r *http.Request) {
	// WebSocket upgrade handled by caller
}
