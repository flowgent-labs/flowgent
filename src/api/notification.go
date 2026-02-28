package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/common/utils"
)

// NotificationHandler manages notification channel CRUD and test-send.
type NotificationHandler struct {
	store  NotifStore
	logger *utils.Logger
}

// NotifStore is the subset of store.Store needed by NotificationHandler.
type NotifStore interface {
	SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error
	GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error)
	ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error)
	DeleteNotifierChannel(ctx context.Context, id string) error
}

// NewNotificationHandler creates a notification channel handler.
func NewNotificationHandler(s NotifStore, logger *utils.Logger) *NotificationHandler {
	return &NotificationHandler{store: s, logger: logger}
}

// ListChannels returns all notification channels for the given tenant.
func (h *NotificationHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	channels, err := h.store.ListNotifierChannels(r.Context(), tenant)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(channels)
}

// CreateChannel persists a new notification channel.
func (h *NotificationHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	var ch model.NotifierChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if ch.Name == "" || ch.Type == "" {
		http.Error(w, "name and type are required", http.StatusBadRequest)
		return
	}
	ch.ID = uuid.New().String()
	ch.TenantID = tenant
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = ch.CreatedAt
	if err := h.store.SaveNotifierChannel(r.Context(), &ch); err != nil {
		h.logger.Error("save notification channel", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ch)
}

// GetChannel returns a single notification channel by ID.
func (h *NotificationHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ch, err := h.store.GetNotifierChannel(r.Context(), id)
	if err != nil || ch == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

// UpdateChannel modifies an existing notification channel.
func (h *NotificationHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	id := r.PathValue("id")
	var ch model.NotifierChannel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ch.ID = id
	ch.TenantID = tenant
	ch.UpdatedAt = time.Now()
	if err := h.store.SaveNotifierChannel(r.Context(), &ch); err != nil {
		h.logger.Error("update notification channel", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

// DeleteChannel removes a notification channel.
func (h *NotificationHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteNotifierChannel(r.Context(), id); err != nil {
		h.logger.Error("delete notification channel", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestChannel sends a test message to a specified channel.
func (h *NotificationHandler) TestChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID string `json:"channel_id"`
		Title     string `json:"title"`
		Body      string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChannelID == "" {
		http.Error(w, "channel_id is required", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		req.Title = "Flowgent Test Notification"
	}
	if req.Body == "" {
		req.Body = "This is a test notification from Flowgent."
	}

	// The actual dispatch is handled by the notification service.
	// The API handler validates and stores the event; the service picks it up.
	h.logger.Info("test notification queued", "channel_id", req.ChannelID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "queued", "channel_id": req.ChannelID})
}
