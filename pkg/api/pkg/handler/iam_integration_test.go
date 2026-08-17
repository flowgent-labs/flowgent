package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/authz"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	iamstore "github.com/flowgent-labs/flowgent/store/pkg/iam"
)

type iamTestStore struct{ db *sql.DB }

func (s *iamTestStore) DB() any      { return s.db }
func (s *iamTestStore) Close() error { return s.db.Close() }

func TestIAMNamespaceAndResourceAuthorizationIntegration(t *testing.T) {
	store := &iamTestStore{db: storepkg.NewSQLiteConn(context.Background(), t.TempDir())}
	defer store.Close()
	repository, err := iamstore.NewRepository(store)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	cfg := config.AuthConfig{Authorization: config.AuthorizationConfig{
		Enabled: true, Enforcement: "enforce", BootstrapToken: "root-token", AuditAllow: true, AuditDeny: true,
	}}
	authorizer := authz.NewService(cfg.Authorization, repository)
	handler := NewIAMHandler(repository, authorizer)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{namespace}/iam/namespaces", handler.Namespace)
	mux.HandleFunc("POST /api/v1/{namespace}/iam/namespaces", handler.CreateNamespace)
	mux.HandleFunc("GET /api/v1/{namespace}/iam/principals", handler.ListPrincipals)
	mux.HandleFunc("POST /api/v1/{namespace}/iam/principals", handler.CreatePrincipal)
	mux.HandleFunc("DELETE /api/v1/{namespace}/iam/principals/{id}", handler.DeletePrincipal)
	mux.HandleFunc("POST /api/v1/{namespace}/iam/roles", handler.CreateRole)
	mux.HandleFunc("POST /api/v1/{namespace}/iam/bindings", handler.CreateBinding)
	mux.HandleFunc("POST /api/v1/{namespace}/iam/api-keys", handler.CreateAPIKey)
	mux.HandleFunc("DELETE /api/v1/{namespace}/iam/api-keys/{id}", handler.RevokeAPIKey)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/trace", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	authenticator, err := auth.NewService(cfg)
	if err != nil {
		t.Fatalf("auth.NewService: %v", err)
	}
	authenticator.SetCredentialAuthenticator(authorizer)
	server := httptest.NewServer(authenticator.Middleware()(authorizer.Middleware(mux)))
	defer server.Close()

	request := func(method, path string, body any) *http.Response {
		t.Helper()
		var payload bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&payload).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req, _ := http.NewRequest(method, server.URL+path, &payload)
		req.Header.Set("Authorization", "Bearer root-token")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return response
	}

	createdNamespace := request(http.MethodPost, "/api/v1/default/iam/namespaces", map[string]any{
		"id": "team-a", "name": "Team_A", "description": "Security engineering",
	})
	if createdNamespace.StatusCode != http.StatusCreated {
		t.Fatalf("create namespace status = %d", createdNamespace.StatusCode)
	}
	createdNamespace.Body.Close()

	for _, invalid := range []map[string]any{
		{"id": "1-team", "name": "Team_1"},
		{"id": "settings", "name": "Settings_Team"},
		{"id": "team.b", "name": "Team_B"},
	} {
		response := request(http.MethodPost, "/api/v1/default/iam/namespaces", invalid)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid namespace %+v status = %d, want 400", invalid, response.StatusCode)
		}
	}
	for _, duplicate := range []map[string]any{
		{"id": "TEAM-A", "name": "Team_B"},
		{"id": "team-b", "name": "team_a"},
	} {
		response := request(http.MethodPost, "/api/v1/default/iam/namespaces", duplicate)
		response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("duplicate namespace %+v status = %d, want 409", duplicate, response.StatusCode)
		}
	}

	principalResponse := request(http.MethodPost, "/api/v1/team-a/iam/principals", map[string]any{
		"type": "user", "issuer": "test", "external_id": "alice", "username": "alice", "display_name": "Alice",
	})
	if principalResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create principal status = %d", principalResponse.StatusCode)
	}
	var principal entities.IAMPrincipal
	if err := json.NewDecoder(principalResponse.Body).Decode(&principal); err != nil {
		t.Fatal(err)
	}
	principalResponse.Body.Close()
	if stored, err := repository.GetPrincipal(context.Background(), principal.ID); err != nil || stored.Type != entities.PrincipalUser {
		t.Fatalf("created principal was not persisted: principal=%+v err=%v", stored, err)
	}
	if member, err := repository.GetNamespaceMember(context.Background(), "team-a", principal.ID); err != nil || member.Status != "ACTIVE" {
		t.Fatalf("created principal membership was not persisted: member=%+v err=%v", member, err)
	}

	serviceResponse := request(http.MethodPost, "/api/v1/team-a/iam/principals", map[string]any{
		"type": "service_account", "issuer": "flowgent:local", "external_id": "security-fixer-ci",
		"username": "security-fixer-ci", "display_name": "Security Fixer CI",
	})
	if serviceResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create service account status = %d", serviceResponse.StatusCode)
	}
	var serviceAccount entities.IAMPrincipal
	if err := json.NewDecoder(serviceResponse.Body).Decode(&serviceAccount); err != nil {
		t.Fatal(err)
	}
	serviceResponse.Body.Close()

	roleResponse := request(http.MethodPost, "/api/v1/team-a/iam/roles", map[string]any{
		"name": "security-fixer-reader", "permissions": []string{"flow.read", "trace.read"},
	})
	if roleResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create role status = %d", roleResponse.StatusCode)
	}
	var role entities.IAMRole
	if err := json.NewDecoder(roleResponse.Body).Decode(&role); err != nil {
		t.Fatal(err)
	}
	roleResponse.Body.Close()

	bindingResponse := request(http.MethodPost, "/api/v1/team-a/iam/bindings", map[string]any{
		"role_id": role.ID, "subject_type": "user", "subject_id": principal.ID,
		"resource_type": "flow", "resource_id": "security-autonomy-fixer", "effect": "ALLOW", "conditions": map[string]any{},
	})
	if bindingResponse.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(bindingResponse.Body)
		t.Fatalf("create binding status = %d: %s", bindingResponse.StatusCode, body)
	}
	bindingResponse.Body.Close()
	serviceBindingResponse := request(http.MethodPost, "/api/v1/team-a/iam/bindings", map[string]any{
		"role_id": role.ID, "subject_type": "service_account", "subject_id": serviceAccount.ID,
		"resource_type": "flow", "resource_id": "security-autonomy-fixer", "effect": "ALLOW", "conditions": map[string]any{},
	})
	if serviceBindingResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create service binding status = %d", serviceBindingResponse.StatusCode)
	}
	serviceBindingResponse.Body.Close()

	keyResponse := request(http.MethodPost, "/api/v1/team-a/iam/api-keys", map[string]any{
		"name": "security-fixer-verifier", "principal_id": serviceAccount.ID,
		"permissions": []string{"flow.read", "trace.read"},
	})
	if keyResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create service API key status = %d", keyResponse.StatusCode)
	}
	var createdKey struct {
		Record entities.IAMAPIKey `json:"record"`
		Secret string             `json:"secret"`
	}
	if err := json.NewDecoder(keyResponse.Body).Decode(&createdKey); err != nil {
		t.Fatal(err)
	}
	keyResponse.Body.Close()
	if createdKey.Record.PrincipalID != serviceAccount.ID || createdKey.Secret == "" {
		t.Fatalf("API key was not issued to service account: %+v", createdKey.Record)
	}
	credentialRequest := func(method, path, credential string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return response
	}
	assertCredentialStatus := func(method, path string, want int) {
		t.Helper()
		response := credentialRequest(method, path, createdKey.Secret)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("service API key %s %s status = %d, want %d", method, path, response.StatusCode, want)
		}
	}
	assertCredentialStatus(http.MethodGet, "/api/v1/team-a/flows/security-autonomy-fixer", http.StatusNoContent)
	assertCredentialStatus(http.MethodGet, "/api/v1/team-a/flows/security-autonomy-fixer/runs/run-1/trace", http.StatusNoContent)
	assertCredentialStatus(http.MethodGet, "/api/v1/team-a/flows/other-flow", http.StatusForbidden)
	assertCredentialStatus(http.MethodPut, "/api/v1/team-a/flows/security-autonomy-fixer", http.StatusForbidden)

	allowed, err := authorizer.Authorize(context.Background(), authz.AccessRequest{
		PrincipalID: principal.ID, PrincipalType: entities.PrincipalUser, Namespace: "team-a",
		Permission: "flow.read", ResourceType: "flow", ResourceID: "security-autonomy-fixer",
	})
	if err != nil || !allowed.Allowed {
		t.Fatalf("exact-resource decision = %+v, err=%v", allowed, err)
	}
	other, _ := authorizer.Authorize(context.Background(), authz.AccessRequest{
		PrincipalID: principal.ID, PrincipalType: entities.PrincipalUser, Namespace: "team-a",
		Permission: "flow.read", ResourceType: "flow", ResourceID: "other-flow",
	})
	if other.Allowed {
		t.Fatalf("unbound resource was allowed: %+v", other)
	}
	crossNamespace, _ := authorizer.Authorize(context.Background(), authz.AccessRequest{
		PrincipalID: principal.ID, PrincipalType: entities.PrincipalUser, Namespace: "default",
		Permission: "flow.read", ResourceType: "flow", ResourceID: "security-autonomy-fixer",
	})
	if crossNamespace.Allowed {
		t.Fatalf("cross-namespace resource was allowed: %+v", crossNamespace)
	}
	accessToken, err := authenticator.TokenService().IssueAccessToken(&auth.UserInfo{
		UserID: "alice", Issuer: "test", Username: "alice", Type: entities.PrincipalUser,
	})
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	jwtRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/team-a/flows/security-autonomy-fixer", nil)
	jwtRequest.Header.Set("Authorization", "Bearer "+accessToken)
	jwtResponse, err := http.DefaultClient.Do(jwtRequest)
	if err != nil {
		t.Fatal(err)
	}
	jwtResponse.Body.Close()
	if jwtResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("external identity authorization status = %d, want 204", jwtResponse.StatusCode)
	}

	revokeResponse := request(http.MethodDelete, "/api/v1/team-a/iam/api-keys/"+createdKey.Record.ID, nil)
	if revokeResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke service API key status = %d", revokeResponse.StatusCode)
	}
	revokeResponse.Body.Close()
	revokedResponse := credentialRequest(http.MethodGet, "/api/v1/team-a/flows/security-autonomy-fixer", createdKey.Secret)
	revokedResponse.Body.Close()
	if revokedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked API key status = %d, want 401", revokedResponse.StatusCode)
	}

	listResponse := request(http.MethodGet, "/api/v1/default/iam/namespaces", nil)
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("list namespaces status = %d", listResponse.StatusCode)
	}
	var namespaces []entities.IAMNamespace
	if err := json.NewDecoder(listResponse.Body).Decode(&namespaces); err != nil {
		t.Fatal(err)
	}
	listResponse.Body.Close()
	if len(namespaces) != 2 {
		t.Fatalf("visible namespaces = %d, want 2", len(namespaces))
	}
}
