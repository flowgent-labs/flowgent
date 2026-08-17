package handler

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func testNotificationCipher(t *testing.T) secretbox.ISecretCipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secretbox.NewAESGCMSecretCipher("test-v1", map[string]string{"test-v1": key})
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

type notifierStoreStub struct {
	channels map[string]*entities.NotifyChannelInfo
	page     *entities.Page[entities.NotifyChannelInfo]
	saved    *entities.NotifyChannelInfo
	deleted  string
}

func (s *notifierStoreStub) Get(_ context.Context, id string) (*entities.NotifyChannelInfo, error) {
	if channel := s.channels[id]; channel != nil {
		return channel, nil
	}
	return nil, sql.ErrNoRows
}
func (s *notifierStoreStub) Select(context.Context, entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error) {
	return s.page, nil
}
func (s *notifierStoreStub) Save(_ context.Context, channel *entities.NotifyChannelInfo) error {
	copy := *channel
	s.saved = &copy
	return nil
}
func (s *notifierStoreStub) Delete(_ context.Context, id string) error {
	s.deleted = id
	return nil
}

func TestNotifierListFiltersNamespaceAndUsesEmptyArray(t *testing.T) {
	store := &notifierStoreStub{page: &entities.Page[entities.NotifyChannelInfo]{
		Items: []*entities.NotifyChannelInfo{
			{BaseEntity: entities.BaseEntity{ID: "a", Namespace: "tenant-a"}},
			{BaseEntity: entities.BaseEntity{ID: "b", Namespace: "tenant-b"}},
		},
		Request: entities.PageRequest{Page: 1, Size: 1000},
	}}
	handler := &NotifierHandler{store: store, secretCipher: testNotificationCipher(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels", handler.ListChannels)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/notifications/channels", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var page entities.Page[entities.NotifyChannelInfo]
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "a" {
		t.Fatalf("items = %+v", page.Items)
	}

	store.page.Items = nil
	empty := httptest.NewRecorder()
	mux.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/api/v1/tenant-a/notifications/channels", nil))
	if strings.Contains(empty.Body.String(), `"items":null`) {
		t.Fatalf("empty list serialized as null: %s", empty.Body.String())
	}
}

func TestNotifierCreateEncryptsAndRedactsSecrets(t *testing.T) {
	store := &notifierStoreStub{}
	handler := &NotifierHandler{store: store, secretCipher: testNotificationCipher(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/{namespace}/notifications/channels", handler.CreateChannel)

	response := httptest.NewRecorder()
	body := `{"name":"secure-hook","provider":"webhook","config":{"url":"https://example.com/hook","headers":{"Authorization":"Bearer super-secret"}},"enabled":true}`
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/tenant-a/notifications/channels", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	stored, _ := json.Marshal(store.saved.Config)
	if strings.Contains(string(stored), "super-secret") || strings.Contains(string(stored), "example.com/hook") {
		t.Fatalf("stored config contains plaintext: %s", stored)
	}
	if !strings.Contains(string(stored), "ciphertext") {
		t.Fatalf("stored config has no envelope: %s", stored)
	}
	if strings.Contains(response.Body.String(), "super-secret") || strings.Contains(response.Body.String(), "example.com/hook") {
		t.Fatalf("response contains plaintext: %s", response.Body.String())
	}
	var public entities.NotifyChannelInfo
	if err := json.NewDecoder(response.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if len(public.ConfiguredSecretFields) != 2 || public.SealedSecrets == nil {
		t.Fatalf("public secret metadata = %+v", public)
	}
	resolved, err := store.saved.ResolveSecrets(context.Background(), testNotificationCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config["url"] != "https://example.com/hook" {
		t.Fatalf("resolved config = %+v", resolved.Config)
	}
}

func TestNotifierMutationsRequireNamespaceOwnership(t *testing.T) {
	channel := &entities.NotifyChannelInfo{
		BaseEntity:  entities.BaseEntity{ID: "channel-a", Namespace: "tenant-a"},
		Name:        "existing",
		ChannelType: entities.NotifWebhook,
		Config:      map[string]any{"url": "https://example.com"},
		Enabled:     true,
	}
	store := &notifierStoreStub{channels: map[string]*entities.NotifyChannelInfo{channel.ID: channel}}
	handler := &NotifierHandler{store: store, secretCipher: testNotificationCipher(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels/{id}", handler.GetChannel)
	mux.HandleFunc("PUT /api/v1/{namespace}/notifications/channels/{id}", handler.UpdateChannel)
	mux.HandleFunc("DELETE /api/v1/{namespace}/notifications/channels/{id}", handler.DeleteChannel)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(method, "/api/v1/tenant-b/notifications/channels/channel-a", strings.NewReader(`{"enabled":false}`))
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s cross-tenant status = %d", method, response.Code)
		}
	}
	if store.saved != nil || store.deleted != "" {
		t.Fatalf("cross-tenant mutation reached store: saved=%+v deleted=%q", store.saved, store.deleted)
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/tenant-a/notifications/channels/channel-a", strings.NewReader(`{"enabled":false}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("owned update status = %d body=%s", response.Code, response.Body.String())
	}
	if store.saved == nil || store.saved.Namespace != "tenant-a" || store.saved.Name != "existing" || store.saved.Enabled {
		t.Fatalf("partial update did not preserve ownership/resource fields: %+v", store.saved)
	}
}
