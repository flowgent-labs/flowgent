package handler

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// TestApplicationNamespace is a regression test for a bug where Trigger
// (Path A, POST /flows/trigger) always created runs with namespace="",
// so flows triggered via the canonical /trigger endpoint were never picked
// up by their dedicated per-flow JM (which only polls its tenant's
// namespace) and would hang forever, since every flow now runs in
// Application mode and reserves no capacity in a shared pool (Session mode
// is currently disabled — see entities.Priority doc comment).
//
// It also guards against a related bug where tenant.namespace_prefix (which
// already includes its own trailing separator, e.g. "flowgent-") was joined
// with another "-", producing a malformed "flowgent--{tenantID}" namespace
// that would never match the namespace the Controller actually created the
// JM Deployment in (see pkg/controller/pkg/controller.go applicationNamespace,
// which must compute an identical value).
//
// Per docs/01-L1-Engine-Architecture.md §1.3/§4.3, namespace is derived
// per-TENANT (not per-flow): every flow belonging to the same tenant shares
// one namespace, with each flow's dedicated JM Deployment disambiguated by
// name alone (flowgent-jobmanager-{tenantId}-{flowId}).
func TestApplicationNamespace(t *testing.T) {
	h := &FlowDefHandler{namespacePrefix: "flowgent-", defaultTenant: "default"}

	t.Run("uses explicit namespace when set", func(t *testing.T) {
		spec := &entities.FlowInfo{
			BaseEntity: entities.BaseEntity{ID: "my-flow"},
			Namespace:  "custom-ns",
		}
		if got := h.applicationNamespace(spec); got != "custom-ns" {
			t.Errorf("applicationNamespace() = %q, want %q", got, "custom-ns")
		}
	})

	t.Run("derives from namespacePrefix + tenant ID without double dash", func(t *testing.T) {
		spec := &entities.FlowInfo{
			BaseEntity: entities.BaseEntity{ID: "my-flow", TenantID: "acme"},
		}
		want := "flowgent-acme"
		if got := h.applicationNamespace(spec); got != want {
			t.Errorf("applicationNamespace() = %q, want %q", got, want)
		}
	})

	t.Run("two flows of the same tenant share one namespace", func(t *testing.T) {
		spec1 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-a", TenantID: "acme"}}
		spec2 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-b", TenantID: "acme"}}
		ns1, ns2 := h.applicationNamespace(spec1), h.applicationNamespace(spec2)
		if ns1 != ns2 {
			t.Errorf("expected same-tenant flows to share a namespace, got %q vs %q", ns1, ns2)
		}
	})

	t.Run("falls back to defaultTenant when spec.TenantID is unset", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}}
		want := "flowgent-default"
		if got := h.applicationNamespace(spec); got != want {
			t.Errorf("applicationNamespace() = %q, want %q", got, want)
		}
	})
}

// TestNewFlowDefHandlerDefaultNamespacePrefix ensures an empty
// namespacePrefix falls back to "flowgent-" rather than producing an
// unprefixed (and potentially colliding) namespace.
func TestNewFlowDefHandlerDefaultNamespacePrefix(t *testing.T) {
	h := &FlowDefHandler{}
	if h.namespacePrefix != "" {
		t.Fatalf("precondition failed: expected zero value")
	}
	// NewFlowDefHandler requires a store; exercise the same default-fallback
	// logic directly since constructing a full store here is unnecessary
	// for this unit.
	if got := defaultNamespacePrefix(""); got != "flowgent-" {
		t.Errorf("defaultNamespacePrefix(\"\") = %q, want %q", got, "flowgent-")
	}
	if got := defaultNamespacePrefix("custom-"); got != "custom-" {
		t.Errorf("defaultNamespacePrefix(\"custom-\") = %q, want %q", got, "custom-")
	}
}

// TestDefaultTenantID ensures an empty defaultTenant (e.g. because a
// deployment's ConfigMap omits the tenant: block entirely) falls back to
// "default", mirroring the fallback every cmd/ entrypoint applies to
// cfg.Runtime.Tenant.DefaultTenant (see e.g. pkg/cmd/pkg/controller/controller.go)
// — without this, an unconfigured tenant.default_tenant would silently
// diverge from the Controller's own fallback and Application-mode runs
// would never be picked up by their dedicated JM.
func TestDefaultTenantID(t *testing.T) {
	if got := defaultTenantID(""); got != "default" {
		t.Errorf("defaultTenantID(\"\") = %q, want %q", got, "default")
	}
	if got := defaultTenantID("acme"); got != "acme" {
		t.Errorf("defaultTenantID(\"acme\") = %q, want %q", got, "acme")
	}
}
