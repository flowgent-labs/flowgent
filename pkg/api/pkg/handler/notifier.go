package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/notifier"
)

type NotifierHandler struct {
	store        notifier.INotifierStore
	secretCipher secretbox.ISecretCipher
	mqtt         MQTTPublisher
	logger       *utils.Logger
}

func NewNotifierHandler(s storage.IStorage, cfg config.NotifierConfig, mqtt MQTTPublisher, logger *utils.Logger) (*NotifierHandler, error) {
	var nStore notifier.INotifierStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		nStore = notifier.NewNotifierPostgresStore(db)
	case *sql.DB:
		nStore = notifier.NewNotifierSQLiteStore(db)
	}
	cipher, err := newDynamicSecretCipher(cfg.SecretEncryption)
	if err != nil {
		return nil, err
	}
	return &NotifierHandler{store: nStore, secretCipher: cipher, mqtt: mqtt, logger: logger}, nil
}

// newDynamicSecretCipher is shared by every DB-backed, UI-managed secret. The
// envelope format and key rotation policy must not diverge by feature.
func newDynamicSecretCipher(cfg config.NotifierSecretEncryptionConfig) (secretbox.ISecretCipher, error) {
	if cfg.Provider != "aesgcm" {
		return nil, fmt.Errorf("dynamic secret encryption provider must be aesgcm")
	}
	return secretbox.NewAESGCMSecretCipher(cfg.ActiveKeyID, cfg.Keys)
}

func (h *NotifierHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.store.List(r.Context(), r.PathValue("namespace"), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	filtered := make([]*entities.NotifyChannelInfo, 0, len(channels.Items))
	for _, channel := range channels.Items {
		if channel != nil {
			redacted, redactErr := channel.Redacted()
			if redactErr != nil {
				http.Error(w, "notification secret metadata is invalid", http.StatusInternalServerError)
				return
			}
			filtered = append(filtered, redacted)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entities.NewPage(filtered, int64(len(filtered)), channels.Request))
}

// ListRuntimeChannels exposes encrypted envelopes to the notifier workload.
// It is protected by a dedicated internal permission and is never used by UI
// clients.
func (h *NotifierHandler) ListRuntimeChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.store.List(r.Context(), r.PathValue("namespace"), entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]*entities.NotifyChannelInfo, 0, len(channels.Items))
	for _, channel := range channels.Items {
		if channel == nil {
			continue
		}
		view, viewErr := channel.RuntimeView()
		if viewErr != nil {
			http.Error(w, "notification secret metadata is invalid", http.StatusInternalServerError)
			return
		}
		result = append(result, view)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entities.NewPage(result, int64(len(result)), channels.Request))
}

func (h *NotifierHandler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var ch entities.NotifyChannelInfo
	if err := decodeStrictJSON(r, &ch); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if ch.Namespace != "" && ch.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	ch.ID = uuid.New().String()
	ch.Namespace = namespace
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = time.Now()
	if err := validateNotifyChannel(&ch); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ch.ProtectSecrets(r.Context(), h.secretCipher); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := h.store.Save(r.Context(), &ch); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	redacted, _ := ch.Redacted()
	json.NewEncoder(w).Encode(redacted)
}

func (h *NotifierHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := h.store.Get(r.Context(), r.PathValue("namespace"), r.PathValue("id"))
	if err != nil {
		if isNotFoundError(err) {
			http.Error(w, "not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if ch == nil {
		http.Error(w, "not found", 404)
		return
	}
	ch, err = ch.Redacted()
	if err != nil {
		http.Error(w, "notification secret metadata is invalid", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch)
}

func (h *NotifierHandler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")
	existing, err := h.store.Get(r.Context(), namespace, id)
	if err != nil || existing == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var updates entities.NotifyChannelInfo
	if err := decodeStrictJSON(r, &updates); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	if (updates.ID != "" && updates.ID != id) || (updates.Namespace != "" && updates.Namespace != namespace) {
		http.Error(w, "resource identity mismatch", http.StatusBadRequest)
		return
	}
	resolved, err := existing.ResolveSecrets(r.Context(), h.secretCipher)
	if err != nil {
		http.Error(w, "notification secrets cannot be decrypted", http.StatusInternalServerError)
		return
	}
	next := &entities.NotifyChannelInfo{
		BaseEntity:  existing.BaseEntity,
		Name:        updates.Name,
		ChannelType: updates.ChannelType,
		Config:      make(map[string]any),
		Enabled:     updates.Enabled,
		Labels:      updates.Labels,
	}
	for key, value := range updates.Config {
		next.Config[key] = value
	}
	if next.ChannelType == existing.ChannelType {
		for _, field := range entities.NotifyChannelSecretFields(next.ChannelType) {
			if !notifySecretValuePresent(next.Config[field]) && notifySecretValuePresent(resolved.Config[field]) {
				next.Config[field] = resolved.Config[field]
			}
		}
	}
	for _, field := range updates.ClearSecretFields {
		delete(next.Config, field)
	}
	next.UpdatedAt = time.Now()
	if err := validateNotifyChannel(next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := next.ProtectSecrets(r.Context(), h.secretCipher); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := h.store.Save(r.Context(), next); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	redacted, _ := next.Redacted()
	json.NewEncoder(w).Encode(redacted)
}

func (h *NotifierHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	namespace, id := r.PathValue("namespace"), r.PathValue("id")
	channel, err := h.store.Get(r.Context(), namespace, id)
	if err != nil || channel == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func (h *NotifierHandler) TestChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelID  string `json:"channel_id"`
		DeliveryID string `json:"delivery_id"`
		Recipient  string `json:"recipient"`
		Title      string `json:"title"`
		Message    string `json:"message"`
	}
	if err := decodeStrictJSON(r, &req); err != nil || req.ChannelID == "" {
		http.Error(w, "channel_id is required", http.StatusBadRequest)
		return
	}
	channel, err := h.store.Get(r.Context(), r.PathValue("namespace"), req.ChannelID)
	if err != nil || channel == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if h.mqtt == nil {
		http.Error(w, "notification queue is unavailable", http.StatusServiceUnavailable)
		return
	}
	if req.DeliveryID == "" {
		req.DeliveryID = uuid.NewString()
	}
	if req.Title == "" {
		req.Title = "Flowgent notification test"
	}
	msg := model.NotifierMessage{
		ChannelID: req.ChannelID, DeliveryID: req.DeliveryID, Recipient: req.Recipient,
		Title: req.Title, Body: req.Message, Namespace: channel.Namespace,
		AgentFlowID: "notification-test", Timestamp: time.Now().UTC(),
	}
	payload, _ := json.Marshal(msg)
	topic := messager.NotifyEventTopic(channel.Namespace, msg.AgentFlowID, req.DeliveryID)
	if err := h.mqtt.Publish(r.Context(), topic, payload); err != nil {
		http.Error(w, "notification queue publish failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"delivery_id":  req.DeliveryID,
		"result_topic": messager.NotifyResultTopic(channel.Namespace, msg.AgentFlowID, req.DeliveryID),
	})
}

func notifySecretValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return true
}

func validateNotifyChannel(channel *entities.NotifyChannelInfo) error {
	if channel.Name == "" {
		return fmt.Errorf("name is required")
	}
	required := map[entities.NotifyChannelType][]string{
		entities.NotifTelegram: {"bot_token", "chat_id"},
		entities.NotifDingTalk: {"webhook_url"},
		entities.NotifSlack:    {"webhook_url"},
		entities.NotifEmail:    {"smtp_host", "from"},
		entities.NotifWebhook:  {"url"},
	}[channel.ChannelType]
	if required == nil {
		return fmt.Errorf("unsupported notification provider %q", channel.ChannelType)
	}
	for _, field := range required {
		if !notifySecretValuePresent(channel.Config[field]) {
			return fmt.Errorf("config.%s is required", field)
		}
	}
	return nil
}

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
