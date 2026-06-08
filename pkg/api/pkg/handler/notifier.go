package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/notifier"
	"github.com/jackc/pgx/v5/pgxpool"
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
	channels, err := h.store.Select(r.Context(), model.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

func (h *NotifierHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var ch model.NotifierChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	w.WriteHeader(201)
}

func (h *NotifierHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": r.PathValue("id")})
}

func (h *NotifierHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
func (h *NotifierHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
func (h *NotifierHandler) TestChannel(w http.ResponseWriter, r *http.Request)   { w.WriteHeader(200) }

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
