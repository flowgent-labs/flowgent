package notifier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type notifierMQTTStub struct {
	mu        sync.Mutex
	published map[string][]byte
}

func (s *notifierMQTTStub) Subscribe(context.Context, string, func(string, []byte)) error { return nil }
func (s *notifierMQTTStub) Publish(_ context.Context, topic string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.published == nil {
		s.published = make(map[string][]byte)
	}
	s.published[topic] = append([]byte(nil), payload...)
	return nil
}

func notifierTestCipher(t *testing.T) secretbox.ISecretCipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secretbox.NewAESGCMSecretCipher("test-v1", map[string]string{"test-v1": key})
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestParseNotifyEventTopic(t *testing.T) {
	namespace, flow, run, ok := parseNotifyEventTopic("flowgent/v1/team-a/flows/flow-a/runs/run-a/notify/event")
	if !ok || namespace != "team-a" || flow != "flow-a" || run != "run-a" {
		t.Fatalf("parsed = %q %q %q %v", namespace, flow, run, ok)
	}
	if _, _, _, ok := parseNotifyEventTopic("flowgent/v1/team-a/notify/event"); ok {
		t.Fatal("accepted malformed topic")
	}
}

func TestQueueMessageDecryptsAndDeliversWebhook(t *testing.T) {
	cipher := notifierTestCipher(t)
	var receivedAuthorization string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	channel := &entities.NotifyChannelInfo{
		BaseEntity: entities.BaseEntity{ID: "channel-a", Namespace: "team-a"},
		Name:       "alerts", ChannelType: entities.NotifWebhook, Enabled: true,
		Config: map[string]any{
			"url":     receiver.URL,
			"headers": map[string]string{"Authorization": "Bearer runtime-secret"},
		},
	}
	if err := channel.ProtectSecrets(context.Background(), cipher); err != nil {
		t.Fatal(err)
	}
	public, err := channel.RuntimeView()
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(entities.NewPage(
			[]*entities.NotifyChannelInfo{public}, 1, entities.PageRequest{Page: 1, Size: 1000},
		))
	}))
	defer api.Close()

	queue := &notifierMQTTStub{}
	apiClient := client.NewFlowgentClient(api.URL)
	manager := NewFlowgentNotifierManager(
		client.NewNotifierClient(apiClient, "team-a"), queue,
		client.NewGenericHttpClient(0), &config.NotifierConfig{}, cipher,
	)
	message, _ := json.Marshal(model.NotifierMessage{
		ChannelID: "channel-a", DeliveryID: "delivery-a", Namespace: "team-a",
		AgentFlowID: "flow-a", Title: "test", Body: "body",
	})
	manager.onQueueMessage("flowgent/v1/team-a/flows/flow-a/runs/delivery-a/notify/event", message)

	if receivedAuthorization != "Bearer runtime-secret" {
		t.Fatalf("authorization = %q", receivedAuthorization)
	}
	resultTopic := "flowgent/v1/team-a/flows/flow-a/runs/delivery-a/notify/result"
	var result model.NotifierDeliveryResult
	if err := json.Unmarshal(queue.published[resultTopic], &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "DELIVERED" || result.ChannelID != "channel-a" {
		t.Fatalf("result = %+v", result)
	}
}
