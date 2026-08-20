package authz

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type fakeRepository struct {
	groups      []string
	bindings    []*entities.IAMRoleBinding
	roles       []*entities.IAMRole
	keys        map[string]*entities.IAMAPIKey
	principals  map[string]*entities.IAMPrincipal
	audits      []*entities.IAMAuditEvent
	auditCtxErr error
}

func (f *fakeRepository) ListPrincipalGroups(context.Context, string, string) ([]string, error) {
	return append([]string(nil), f.groups...), nil
}
func (f *fakeRepository) FindBindings(_ context.Context, namespace string, principalType entities.PrincipalType, principalID string, groups []string) ([]*entities.IAMRoleBinding, error) {
	groupSet := map[string]bool{}
	for _, group := range groups {
		groupSet[group] = true
	}
	var result []*entities.IAMRoleBinding
	for _, binding := range f.bindings {
		if binding.Namespace != "*" && binding.Namespace != namespace {
			continue
		}
		if binding.SubjectType == principalType && binding.SubjectID == principalID || binding.SubjectType == entities.PrincipalGroup && groupSet[binding.SubjectID] {
			result = append(result, binding)
		}
	}
	return result, nil
}
func (f *fakeRepository) ListRoles(context.Context, string) ([]*entities.IAMRole, error) {
	return f.roles, nil
}
func (f *fakeRepository) GetAPIKey(_ context.Context, id string) (*entities.IAMAPIKey, error) {
	key := f.keys[id]
	if key == nil {
		return nil, errors.New("not found")
	}
	copy := *key
	return &copy, nil
}
func (f *fakeRepository) SaveAPIKey(_ context.Context, key *entities.IAMAPIKey) error {
	copy := *key
	f.keys[key.ID] = &copy
	return nil
}
func (f *fakeRepository) GetPrincipal(_ context.Context, id string) (*entities.IAMPrincipal, error) {
	principal := f.principals[id]
	if principal == nil {
		return nil, errors.New("not found")
	}
	return principal, nil
}
func (f *fakeRepository) FindPrincipalByIdentity(_ context.Context, issuer, externalID string) (*entities.IAMPrincipal, error) {
	for _, principal := range f.principals {
		if principal.Issuer == issuer && principal.ExternalID == externalID {
			return principal, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeRepository) AppendAudit(ctx context.Context, event *entities.IAMAuditEvent) error {
	f.auditCtxErr = ctx.Err()
	f.audits = append(f.audits, event)
	return nil
}

func TestAuditSurvivesRequestCancellation(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(config.AuthorizationConfig{Enabled: true, AuditAllow: true}, repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service.audit(ctx, &auth.UserInfo{UserID: "alice", Type: entities.PrincipalUser}, RoutePolicy{
		Namespace: "team-a", Permission: "flow.read", ResourceType: "flow", ResourceID: "flow-a",
	}, Decision{Allowed: true, Reason: "role_binding"}, "request-1", "127.0.0.1")
	if repo.auditCtxErr != nil || len(repo.audits) != 1 {
		t.Fatalf("audit context error=%v events=%d", repo.auditCtxErr, len(repo.audits))
	}
}

func TestAuthorizeAdditionalUsesBodyDerivedResourceScope(t *testing.T) {
	repo := &fakeRepository{
		roles: []*entities.IAMRole{{
			BaseEntity:  entities.BaseEntity{ID: "flow-user", Status: "ACTIVE"},
			Permissions: []string{"flow.use"},
		}},
		bindings: []*entities.IAMRoleBinding{{
			BaseEntity: entities.BaseEntity{ID: "flow-a-only", Namespace: "team-a", Status: "ACTIVE"},
			RoleID:     "flow-user", SubjectType: entities.PrincipalUser, SubjectID: "breakglass:root",
			ResourceType: "flow", ResourceID: "flow-a", Effect: "ALLOW",
		}},
	}
	service := NewService(config.AuthorizationConfig{
		Enabled: true, Enforcement: "enforce", BootstrapToken: "root-token", AuditAllow: true, AuditDeny: true,
	}, repo)
	authService, err := auth.NewService(config.AuthConfig{})
	if err != nil {
		t.Fatal(err)
	}
	authService.SetCredentialAuthenticator(service)
	handler := authService.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resourceID := r.URL.Query().Get("flow")
		allowed, checkErr := service.AuthorizeAdditional(r, "team-a", "flow.use", "flow", resourceID)
		if checkErr != nil {
			http.Error(w, checkErr.Error(), http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func(flow string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/bind?flow="+flow, nil)
		req.Header.Set("Authorization", "Bearer root-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request("flow-a"); response.Code != http.StatusNoContent {
		t.Fatalf("flow-a status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request("flow-b"); response.Code != http.StatusForbidden {
		t.Fatalf("flow-b status=%d body=%s", response.Code, response.Body.String())
	}
	if len(repo.audits) != 2 || repo.audits[0].ResourceID != "flow-a" || repo.audits[1].ResourceID != "flow-b" {
		t.Fatalf("additional authorization audits=%#v", repo.audits)
	}
}

func TestAuthorizeNamespaceAndResourceScopes(t *testing.T) {
	repo := &fakeRepository{
		roles: []*entities.IAMRole{
			{BaseEntity: entities.BaseEntity{ID: "reader", Status: "ACTIVE"}, Permissions: []string{"flow.read"}},
			{BaseEntity: entities.BaseEntity{ID: "writer", Status: "ACTIVE"}, Permissions: []string{"flow.*"}},
		},
		bindings: []*entities.IAMRoleBinding{
			{BaseEntity: entities.BaseEntity{ID: "read", Namespace: "team-a", Status: "ACTIVE"}, RoleID: "reader", SubjectType: entities.PrincipalUser, SubjectID: "alice", ResourceType: "namespace", ResourceID: "*", Effect: "ALLOW"},
			{BaseEntity: entities.BaseEntity{ID: "write", Namespace: "team-a", Status: "ACTIVE"}, RoleID: "writer", SubjectType: entities.PrincipalUser, SubjectID: "alice", ResourceType: "flow", ResourceID: "security-fixer", Effect: "ALLOW"},
		},
	}
	service := NewService(config.AuthorizationConfig{Enabled: true, Enforcement: "enforce"}, repo)

	read, err := service.Authorize(context.Background(), AccessRequest{PrincipalID: "alice", PrincipalType: entities.PrincipalUser, Namespace: "team-a", Permission: "flow.read", ResourceType: "flow", ResourceID: "other"})
	if err != nil || !read.Allowed {
		t.Fatalf("namespace read decision=%+v err=%v", read, err)
	}
	writeExact, _ := service.Authorize(context.Background(), AccessRequest{PrincipalID: "alice", PrincipalType: entities.PrincipalUser, Namespace: "team-a", Permission: "flow.write", ResourceType: "flow", ResourceID: "security-fixer"})
	if !writeExact.Allowed {
		t.Fatalf("exact resource write denied: %+v", writeExact)
	}
	writeOther, _ := service.Authorize(context.Background(), AccessRequest{PrincipalID: "alice", PrincipalType: entities.PrincipalUser, Namespace: "team-a", Permission: "flow.write", ResourceType: "flow", ResourceID: "other"})
	if writeOther.Allowed {
		t.Fatalf("unbound resource write allowed: %+v", writeOther)
	}
	crossNamespace, _ := service.Authorize(context.Background(), AccessRequest{PrincipalID: "alice", PrincipalType: entities.PrincipalUser, Namespace: "team-b", Permission: "flow.read", ResourceType: "flow", ResourceID: "other"})
	if crossNamespace.Allowed {
		t.Fatalf("cross-namespace read allowed: %+v", crossNamespace)
	}
}

func TestAuthorizeExplicitDenyPrecedesAllow(t *testing.T) {
	repo := &fakeRepository{
		roles: []*entities.IAMRole{{BaseEntity: entities.BaseEntity{ID: "role", Status: "ACTIVE"}, Permissions: []string{"run.trigger"}}},
		bindings: []*entities.IAMRoleBinding{
			{BaseEntity: entities.BaseEntity{ID: "allow", Namespace: "team-a", Status: "ACTIVE"}, RoleID: "role", SubjectType: entities.PrincipalGroup, SubjectID: "operators", ResourceType: "namespace", ResourceID: "*", Effect: "ALLOW"},
			{BaseEntity: entities.BaseEntity{ID: "deny", Namespace: "team-a", Status: "ACTIVE"}, RoleID: "role", SubjectType: entities.PrincipalUser, SubjectID: "alice", ResourceType: "flow", ResourceID: "restricted", Effect: "DENY"},
		},
		groups: []string{"operators"},
	}
	decision, err := NewService(config.AuthorizationConfig{Enabled: true}, repo).Authorize(context.Background(), AccessRequest{
		PrincipalID: "alice", PrincipalType: entities.PrincipalUser, Namespace: "team-a",
		Permission: "run.trigger", ResourceType: "flow", ResourceID: "restricted",
	})
	if err != nil || decision.Allowed || decision.Reason != "explicit_deny" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestAuthenticateAPIKeyAndAttenuation(t *testing.T) {
	digest := apiKeyDigest("key-id", "secret")
	repo := &fakeRepository{
		keys: map[string]*entities.IAMAPIKey{
			"key-id": {
				BaseEntity: entities.BaseEntity{ID: "key-id", Status: "ACTIVE"}, PrincipalID: "service:ci",
				SecretHash: hex.EncodeToString(digest[:]), Permissions: []string{"flow.read"}, AllowedNamespaces: []string{"team-a"},
			},
		},
		principals: map[string]*entities.IAMPrincipal{
			"service:ci": {BaseEntity: entities.BaseEntity{ID: "service:ci", Status: "ACTIVE"}, Type: entities.PrincipalServiceAccount, Username: "ci"},
		},
	}
	service := NewService(config.AuthorizationConfig{Enabled: true}, repo)
	user, err := service.AuthenticateCredential(context.Background(), "fgk_key-id_secret")
	if err != nil || user.UserID != "service:ci" || user.CredentialID != "key-id" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if repo.keys["key-id"].LastUsedAt == nil {
		t.Fatal("last-used timestamp was not persisted")
	}
	if _, err := service.AuthenticateCredential(context.Background(), "fgk_key-id_wrong"); err == nil {
		t.Fatal("wrong API key secret accepted")
	}
	decision, _ := service.Authorize(context.Background(), AccessRequest{
		PrincipalID: user.UserID, PrincipalType: user.Type, DirectPermissions: user.DirectPermissions,
		AllowedNamespaces: user.AllowedNamespaces, Namespace: "team-b", Permission: "flow.read", Now: time.Now(),
	})
	if decision.Allowed || decision.Reason != "credential_namespace_attenuation" {
		t.Fatalf("attenuation decision=%+v", decision)
	}
}

func TestAuthenticateCredentialRejectsUnresolvedSecretPlaceholder(t *testing.T) {
	service := NewService(config.AuthorizationConfig{
		Enabled: true, BootstrapToken: "${FLOWGENT_AUTH_BOOTSTRAP_TOKEN}",
		InternalTokens: map[string]string{"controller": "${FLOWGENT_AUTH_CONTROLLER_TOKEN}"},
	}, &fakeRepository{})
	if _, err := service.AuthenticateCredential(context.Background(), "${FLOWGENT_AUTH_BOOTSTRAP_TOKEN}"); err == nil {
		t.Fatal("unresolved bootstrap placeholder was accepted as a credential")
	}
	if _, err := service.AuthenticateCredential(context.Background(), "${FLOWGENT_AUTH_CONTROLLER_TOKEN}"); err == nil {
		t.Fatal("unresolved workload placeholder was accepted as a credential")
	}
}

func TestPolicyForRequest(t *testing.T) {
	tests := []struct {
		method, path, permission, resourceType, resource string
	}{
		{"GET", "/api/v1/team-a/flows/f1", "flow.read", "flow", "f1"},
		{"GET", "/api/v1/team-a/flows/watch", "flow.read", "flow", "*"},
		{"POST", "/api/v1/team-a/flows/f1/trigger", "run.trigger", "flow", "f1"},
		{"GET", "/api/v1/team-a/flows/f1/runs/r1/trace", "trace.read", "flow", "f1"},
		{"POST", "/api/v1/team-a/flows/f1/runs/r1/approvals/a1/approve", "approval.resolve", "flow", "f1"},
		{"POST", "/api/v1/team-a/runs/r1/approvals", "approval.internal.create", "approval", "r1"},
		{"GET", "/api/v1/team-a/approvals", "approval.platform.read", "approval", "*"},
		{"GET", "/api/v1/team-a/flows/f1/iam/bindings", "flow.access.manage", "flow", "f1"},
		{"GET", "/api/v1/team-a/runtime-config", "namespace.config.read", "namespace", "team-a"},
		{"PUT", "/api/v1/team-a/runtime-config/environment", "namespace.config.manage", "namespace", "team-a"},
		{"PUT", "/api/v1/team-a/runtime-config/secrets", "namespace.secret.manage", "namespace", "team-a"},
		{"GET", "/api/v1/team-a/flows/f1/runtime-config", "flow.config.read", "flow", "f1"},
		{"PUT", "/api/v1/team-a/flows/f1/runtime-config/environment", "flow.config.manage", "flow", "f1"},
		{"PUT", "/api/v1/team-a/flows/f1/runtime-config/secrets", "flow.secret.manage", "flow", "f1"},
		{"GET", "/api/v1/team-a/flows/f1/runtime-config/resolved", "flow.runtime.use", "flow", "f1"},
		{"POST", "/api/v1/team-a/flow-releases", "flow_release.publish", "flow_release", "*"},
		{"POST", "/api/v1/team-a/flow-releases/rel1/install", "flow_release.install", "flow_release", "rel1"},
		{"PUT", "/api/v1/team-a/runs/r1", "run.internal.update", "run", "r1"},
		{"GET", "/api/v1/team-a/runs/r1/trace", "trace.read", "trace", "r1"},
		{"PUT", "/api/v1/team-a/runs/r1/tasks/t1", "task.internal.write", "task", "t1"},
		{"PUT", "/api/v1/team-a/notifications/channels/c1", "notification.secret.manage", "notification", "c1"},
		{"GET", "/api/v1/team-a/notifications/runtime/channels", "notification.internal.deliver", "notification", "*"},
		{"GET", "/api/v1/team-a/llm/providers/l1", "llm_provider.read", "llm_provider", "l1"},
		{"GET", "/api/v1/team-a/ws/human-approvals", "approval.read", "approval", "*"},
		{"GET", "/api/v1/team-a/iam/roles", "iam.role.read", "roles", "*"},
	}
	for _, test := range tests {
		req, _ := http.NewRequest(test.method, test.path, nil)
		policy, ok := PolicyForRequest(req)
		if !ok || policy.Permission != test.permission || policy.ResourceType != test.resourceType || policy.ResourceID != test.resource {
			t.Errorf("%s %s policy=%+v ok=%v", test.method, test.path, policy, ok)
		}
	}
}
