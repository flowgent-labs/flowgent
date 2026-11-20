package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	runtimeconfig "github.com/flowgent-labs/flowgent/storage/pkg/runtimeconfig"
)

type runtimeConfigRepoStub struct {
	item *entities.RuntimeConfiguration
}

func (s *runtimeConfigRepoStub) Get(_ context.Context, namespace, scopeType, scopeID string) (*entities.RuntimeConfiguration, error) {
	if s.item == nil {
		return nil, runtimeconfig.ErrNotFound
	}
	copy := *s.item
	copy.Environment = cloneStrings(s.item.Environment)
	copy.ConfiguredSecretKeys = append([]string(nil), s.item.ConfiguredSecretKeys...)
	return &copy, nil
}

func (s *runtimeConfigRepoStub) Upsert(_ context.Context, item *entities.RuntimeConfiguration) error {
	copy := *item
	copy.Environment = cloneStrings(item.Environment)
	copy.ConfiguredSecretKeys = append([]string(nil), item.ConfiguredSecretKeys...)
	s.item = &copy
	return nil
}

func TestRuntimeConfigNamespaceSecretsAreWriteOnly(t *testing.T) {
	repo := &runtimeConfigRepoStub{}
	h := &RuntimeConfigHandler{repo: repo, secretBox: testNotificationCipher(t)}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/environment", h.UpdateNamespaceEnvironment)
	mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/secrets", h.UpdateNamespaceSecrets)
	mux.HandleFunc("GET /api/v1/{namespace}/runtime-config", h.GetNamespace)

	environmentResponse := httptest.NewRecorder()
	mux.ServeHTTP(environmentResponse, httptest.NewRequest(http.MethodPut, "/api/v1/team-a/runtime-config/environment", strings.NewReader(`{"environment":{"SHARED":"namespace"}}`)))
	if environmentResponse.Code != http.StatusOK {
		t.Fatalf("environment status=%d body=%s", environmentResponse.Code, environmentResponse.Body.String())
	}
	secretResponse := httptest.NewRecorder()
	mux.ServeHTTP(secretResponse, httptest.NewRequest(http.MethodPut, "/api/v1/team-a/runtime-config/secrets", strings.NewReader(`{"secrets":{"SHARED_TOKEN":"plain-secret"}}`)))
	if secretResponse.Code != http.StatusOK {
		t.Fatalf("secret status=%d body=%s", secretResponse.Code, secretResponse.Body.String())
	}
	if strings.Contains(secretResponse.Body.String(), "plain-secret") || repo.item == nil || repo.item.SealedSecrets == nil {
		t.Fatalf("plaintext leaked or envelope missing: response=%s item=%+v", secretResponse.Body.String(), repo.item)
	}
	encoded, _ := json.Marshal(repo.item)
	if strings.Contains(string(encoded), "plain-secret") || strings.Contains(string(encoded), "ciphertext") {
		t.Fatalf("public entity serialization exposed protected data: %s", encoded)
	}
	resolved, err := h.resolveSecrets(context.Background(), repo.item)
	if err != nil || resolved["SHARED_TOKEN"] != "plain-secret" {
		t.Fatalf("resolved=%v err=%v", resolved, err)
	}
	getResponse := httptest.NewRecorder()
	mux.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/team-a/runtime-config", nil))
	if strings.Contains(getResponse.Body.String(), "plain-secret") || !strings.Contains(getResponse.Body.String(), "SHARED_TOKEN") {
		t.Fatalf("redaction response=%s", getResponse.Body.String())
	}
}

func TestRuntimeConfigEmptyLayersSerializeCollections(t *testing.T) {
	view := runtimeConfigView(
		emptyRuntimeConfiguration("team-a", entities.RuntimeConfigScopeNamespace, "team-a"),
		emptyRuntimeConfiguration("team-a", entities.RuntimeConfigScopeNamespace, "team-a"),
		false,
	)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"environment":null`) || strings.Contains(string(encoded), `"secret_keys":null`) {
		t.Fatalf("empty runtime collections must serialize as objects and arrays: %s", encoded)
	}
}

func TestRuntimeConfigFlowOverridesNamespaceAcrossValueKinds(t *testing.T) {
	nsEnvironment := map[string]string{"SHARED": "ns", "SECRET_TO_ENV": "ns-env"}
	nsSecrets := map[string]string{"SECRET_TO_ENV": "ns-secret", "ENV_TO_SECRET": "ns-secret"}
	flowEnvironment := map[string]string{"SHARED": "flow", "SECRET_TO_ENV": "flow-env"}
	flowSecrets := map[string]string{"ENV_TO_SECRET": "flow-secret"}
	environment, secrets := mergeResolved(nsEnvironment, nsSecrets, flowEnvironment, flowSecrets)
	if environment["SHARED"] != "flow" || environment["SECRET_TO_ENV"] != "flow-env" {
		t.Fatalf("effective environment=%v", environment)
	}
	if _, exists := secrets["SECRET_TO_ENV"]; exists || secrets["ENV_TO_SECRET"] != "flow-secret" {
		t.Fatalf("effective secrets=%v", secrets)
	}
}

func TestRuntimeConfigRejectsAmbiguousOrInvalidEntries(t *testing.T) {
	update := &entities.RuntimeConfigUpdate{Environment: map[string]string{"TOKEN": "public"}, Secrets: map[string]string{"TOKEN": "secret"}}
	if err := validateRuntimeConfigUpdate(update); err == nil {
		t.Fatal("same key accepted as both environment and secret")
	}
	update = &entities.RuntimeConfigUpdate{Environment: map[string]string{"1INVALID": "x"}}
	if err := validateRuntimeConfigUpdate(update); err == nil {
		t.Fatal("invalid environment key accepted")
	}
}
