package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/notifier"
)

type NotifierHandler struct {
	store  notifier.INotifierStore
	logger *utils.Logger
}

func NewNotifierHandler(s store.IStore, logger *utils.Logger) *NotifierHandler {
	var nStore notifier.INotifierStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		nStore = notifier.NewNotifierPostgresStore(db)
	case *sql.DB:
		nStore = notifier.NewNotifierSQLiteStore(db)
	}
	return &NotifierHandler{store: nStore, logger: logger}
}

func (h *NotifierHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.store.Select(r.Context(), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

func (h *NotifierHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var ch entities.NotifyChannelInfo
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	ch.ID = uuid.New().String()
	ch.Namespace = r.PathValue("namespace")
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &ch); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(ch)
}

func (h *NotifierHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := h.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if ch == nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

func (h *NotifierHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	var ch entities.NotifyChannelInfo
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	ch.ID = r.PathValue("id")
	ch.UpdatedAt = time.Now()
	if err := h.store.Save(r.Context(), &ch); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

func (h *NotifierHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

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
