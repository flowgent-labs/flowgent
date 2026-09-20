package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	guardmodel "authguard/adapters/golang/model"
	guardutil "authguard/adapters/golang/util"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	flowstore "github.com/flowgent-labs/flowgent/storage/pkg/flow"
)

const testSigningKey = "0123456789abcdef0123456789abcdef"

func TestDisabledAdapterUsesDummyScope(t *testing.T) {
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.Middleware(scopeRecorder(t, "1=1", nil)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAdapterCompilesNamespaceScope(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	accessContext := guardmodel.NewAccessContext(
		"github:alice", ActionRead,
		"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flow/demo",
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/**"},
		nil, 7, time.Minute,
	)
	encodedContext, err := guardutil.EncodeAccessContext(accessContext)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := guardutil.SignEncodedAccessContext(encodedContext, testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(guardutil.AccessContextHeader, encoded)
	recorder := httptest.NewRecorder()
	adapter.Middleware(scopeRecorder(t, `"namespace_id" = ?`, []any{"security-fixer"})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdapterCompilesExactRepositoryResourceScope(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	accessContext := guardmodel.NewAccessContext(
		"github:alice", ActionRead,
		"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/flow-a",
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/flow-a/**"},
		nil, 8, time.Minute,
	)
	encoded, err := guardutil.SignAccessContext(accessContext, testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security-fixer/flows/flow-a", nil)
	req.Header.Set(guardutil.AccessContextHeader, encoded)
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := storage.FlowgentSqlScopeForResources(r.Context(), storage.ResourcePath(
			storage.LiteralResource("flows"),
			storage.ColumnResource(`"agentflow_id"`),
		))
		if scope.Where != `"namespace_id" = ? AND "agentflow_id" = ?` {
			t.Fatalf("where = %q", scope.Where)
		}
		if len(scope.Args) != 2 || scope.Args[0] != "security-fixer" || scope.Args[1] != "flow-a" {
			t.Fatalf("args = %#v", scope.Args)
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdapterRepositoryResourceMismatchFailsClosed(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	accessContext := guardmodel.NewAccessContext(
		"github:alice", ActionRead,
		"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/flow-a",
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/flow-a/**"},
		nil, 9, time.Minute,
	)
	encoded, err := guardutil.SignAccessContext(accessContext, testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security-fixer/flows/flow-a", nil)
	req.Header.Set(guardutil.AccessContextHeader, encoded)
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := storage.FlowgentSqlScopeForResources(r.Context(), storage.ResourcePath(
			storage.LiteralResource("agents"),
			storage.ColumnResource(`"name"`),
		))
		if scope.Where != "0=1" || len(scope.Args) != 0 {
			t.Fatalf("mismatched resource scope = %#v", scope)
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdapterRepositoryScopeFiltersDeniedResource(t *testing.T) {
	db := storage.NewSQLiteConn(context.Background(), t.TempDir())
	t.Cleanup(func() { _ = db.Close() })
	repository := flowstore.NewFlowSQLiteStore(db)
	for _, flowID := range []string{"flow-a", "flow-b"} {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: flowID, Namespace: "security-fixer"}}
		if err := repository.CreateSpec(context.Background(), spec, "fixture", "authz fixture"); err != nil {
			t.Fatalf("create %s: %v", flowID, err)
		}
	}

	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	accessContext := guardmodel.NewAccessContext(
		"github:alice", ActionRead,
		"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows",
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/**"},
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/flow-b/**"},
		10, time.Minute,
	)
	encoded, err := guardutil.SignAccessContext(accessContext, testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security-fixer/flows", nil)
	req.Header.Set(guardutil.AccessContextHeader, encoded)
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := repository.Select(r.Context(), "security-fixer", entities.PageRequest{Page: 1, Size: 20})
		if err != nil {
			t.Fatal(err)
		}
		if page.TotalCount != 1 || len(page.Items) != 1 || page.Items[0].FlowID != "flow-a" {
			t.Fatalf("visible flows = %#v", page.Items)
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestEnabledAdapterRejectsMissingContext(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("missing AuthGuard context reached protected handler")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/security-fixer/flows", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAdapterRejectsActionMismatch(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	accessContext := guardmodel.NewAccessContext(
		"github:alice", ActionRead,
		"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/flows/demo",
		[]string{"urn:iam:prod:flowgent:global:example-corp:namespace/security-fixer/**"},
		nil, 7, time.Minute,
	)
	encoded, err := guardutil.SignAccessContext(accessContext, testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/security-fixer/flows/demo", nil)
	req.Header.Set(guardutil.AccessContextHeader, encoded)
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("action mismatch reached protected handler")
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestInternalMiddlewareUsesDummySDKScope(t *testing.T) {
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.InternalMiddleware(scopeRecorder(t, "1=1", nil)).ServeHTTP(
		httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/security-fixer/runs", nil),
	)
}

func TestAdapterRejectsInvalidContext(t *testing.T) {
	t.Setenv("AUTHGUARD__AUTHZ__SCOPE_DELIVERY__DIRECT_CONTEXT_HMAC_KEY", testSigningKey)
	adapter, err := NewAdapter(config.AuthGuardAdapterConfig{Enabled: true, Tenant: "example-corp"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(guardutil.AccessContextHeader, "invalid")
	recorder := httptest.NewRecorder()
	adapter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid context reached handler")
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func scopeRecorder(t *testing.T, wantWhere string, wantArgs []any) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := storage.FlowgentSqlScopeFromContext(r.Context())
		if scope.Where != wantWhere {
			t.Fatalf("where = %q, want %q", scope.Where, wantWhere)
		}
		if len(scope.Args) != len(wantArgs) {
			t.Fatalf("args = %#v, want %#v", scope.Args, wantArgs)
		}
		for i := range wantArgs {
			if scope.Args[i] != wantArgs[i] {
				t.Fatalf("args[%d] = %#v, want %#v", i, scope.Args[i], wantArgs[i])
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}
