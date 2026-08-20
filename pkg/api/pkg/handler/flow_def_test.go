package handler

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
)

type capturingFlowRunStore struct {
	created *entities.FlowRunInfo
}

func (s *capturingFlowRunStore) Get(context.Context, string) (*entities.FlowRunInfo, error) {
	return nil, nil
}
func (s *capturingFlowRunStore) List(context.Context, flowrun.ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	return nil, nil
}
func (s *capturingFlowRunStore) Metrics(context.Context, flowrun.MetricRequest) (*entities.RunMetrics, error) {
	return nil, nil
}
func (s *capturingFlowRunStore) HasActiveForFlow(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *capturingFlowRunStore) Save(context.Context, *entities.FlowRunInfo) error { return nil }
func (s *capturingFlowRunStore) Delete(context.Context, string) error              { return nil }
func (s *capturingFlowRunStore) Create(_ context.Context, run *entities.FlowRunInfo) error {
	run.ID = "run-1"
	copy := *run
	s.created = &copy
	return nil
}
func (s *capturingFlowRunStore) Update(context.Context, *entities.FlowRunInfo) error { return nil }
func (s *capturingFlowRunStore) Cancel(context.Context, string) error                { return nil }

// TestRuntimeNamespace is a regression test for a bug where Trigger
// (Path A, POST /flows/trigger) always created runs with namespace="",
// so flows triggered via the canonical /trigger endpoint were never picked
// up by their dedicated per-flow JM (which only polls its namespace's
// namespace) and would hang forever.
//
// It also guards against a related bug where namespace.namespace_prefix (which
// already includes its own trailing separator, e.g. "flowgent-") was joined
// with another "-", producing a malformed "flowgent--{namespaceID}" namespace
// that would never match the namespace the Controller actually created the
// JM Deployment in (see pkg/controller/pkg/controller.go runtimeNamespace,
// which must compute an identical value).
//
// Per docs/01-L1-Engine-Architecture.md §1.3/§4.3, namespace is derived
// per-TENANT (not per-flow): every flow belonging to the same namespace shares
// one namespace, with each flow's dedicated JM Deployment disambiguated by
// name alone (flowgent-jobmanager-{namespaceId}-{flowId}).
func TestRuntimeNamespace(t *testing.T) {
	h := &FlowDefHandler{namespacePrefix: "flowgent-", defaultNamespace: "default"}

	t.Run("uses explicit namespace when set", func(t *testing.T) {
		spec := &entities.FlowInfo{
			BaseEntity:   entities.BaseEntity{ID: "my-flow"},
			K8sNamespace: "custom-ns",
		}
		if got := h.runtimeNamespace(spec); got != "custom-ns" {
			t.Errorf("runtimeNamespace() = %q, want %q", got, "custom-ns")
		}
	})

	t.Run("derives from namespacePrefix + namespace ID without double dash", func(t *testing.T) {
		spec := &entities.FlowInfo{
			BaseEntity: entities.BaseEntity{ID: "my-flow", Namespace: "acme"},
		}
		want := "flowgent-acme"
		if got := h.runtimeNamespace(spec); got != want {
			t.Errorf("runtimeNamespace() = %q, want %q", got, want)
		}
	})

	t.Run("two flows of the same namespace share one namespace", func(t *testing.T) {
		spec1 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-a", Namespace: "acme"}}
		spec2 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-b", Namespace: "acme"}}
		ns1, ns2 := h.runtimeNamespace(spec1), h.runtimeNamespace(spec2)
		if ns1 != ns2 {
			t.Errorf("expected same-namespace flows to share a namespace, got %q vs %q", ns1, ns2)
		}
	})

	t.Run("falls back to defaultNamespace when spec.Namespace is unset", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}}
		want := "flowgent-default"
		if got := h.runtimeNamespace(spec); got != want {
			t.Errorf("runtimeNamespace() = %q, want %q", got, want)
		}
	})
}

func TestLogicalAndK8sNamespacesRemainDistinct(t *testing.T) {
	h := &FlowDefHandler{namespacePrefix: "flowgent-", defaultNamespace: "default"}

	for _, tc := range []struct {
		name        string
		spec        *entities.FlowInfo
		wantLogical string
		wantK8s     string
	}{
		{
			name:        "explicit tenant",
			spec:        &entities.FlowInfo{BaseEntity: entities.BaseEntity{Namespace: "acme"}},
			wantLogical: "acme",
			wantK8s:     "flowgent-acme",
		},
		{
			name:        "default tenant",
			spec:        &entities.FlowInfo{},
			wantLogical: "default",
			wantK8s:     "flowgent-default",
		},
		{
			name:        "custom physical namespace",
			spec:        &entities.FlowInfo{BaseEntity: entities.BaseEntity{Namespace: "acme"}, K8sNamespace: "isolated-acme"},
			wantLogical: "acme",
			wantK8s:     "isolated-acme",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.logicalNamespace(tc.spec); got != tc.wantLogical {
				t.Fatalf("logicalNamespace() = %q, want %q", got, tc.wantLogical)
			}
			if got := h.runtimeNamespace(tc.spec); got != tc.wantK8s {
				t.Fatalf("runtimeNamespace() = %q, want %q", got, tc.wantK8s)
			}
		})
	}
}

func TestCreateRunKeepsTenantAndK8sNamespacesDistinct(t *testing.T) {
	spec := &entities.FlowInfo{
		BaseEntity:  entities.BaseEntity{ID: "flow-a", Namespace: "tenant-a"},
		RuntimeMode: entities.RuntimeModeApplication,
	}
	runs := &capturingFlowRunStore{}
	h := &FlowDefHandler{
		agentFlows:       map[string]*entities.FlowInfo{flowCacheKey("tenant-a", spec.ID): spec},
		frStore:          runs,
		namespacePrefix:  "flowgent-",
		defaultNamespace: "default",
	}

	runID, err := h.CreateRunFromTrigger(context.Background(), spec.ID, "tenant-a", nil, entities.TriggerInfo{Type: "manual"})
	if err != nil {
		t.Fatalf("CreateRunFromTrigger() error = %v", err)
	}
	if runID != "run-1" || runs.created == nil {
		t.Fatalf("run was not persisted: id=%q run=%+v", runID, runs.created)
	}
	if runs.created.Namespace != "tenant-a" {
		t.Fatalf("logical namespace = %q, want tenant-a", runs.created.Namespace)
	}
	if runs.created.K8sNamespace != "flowgent-tenant-a" {
		t.Fatalf("K8s namespace = %q, want flowgent-tenant-a", runs.created.K8sNamespace)
	}
	if runs.created.RuntimeMode != entities.RuntimeModeApplication {
		t.Fatalf("runtime mode = %q, want application", runs.created.RuntimeMode)
	}
}

func TestCreateRunRejectsCrossTenantTrigger(t *testing.T) {
	spec := &entities.FlowInfo{
		BaseEntity:  entities.BaseEntity{ID: "flow-a", Namespace: "tenant-a"},
		RuntimeMode: entities.RuntimeModeApplication,
	}
	runs := &capturingFlowRunStore{}
	h := &FlowDefHandler{
		agentFlows:       map[string]*entities.FlowInfo{flowCacheKey("tenant-a", spec.ID): spec},
		frStore:          runs,
		namespacePrefix:  "flowgent-",
		defaultNamespace: "default",
	}

	if _, err := h.CreateRunFromTrigger(context.Background(), spec.ID, "tenant-b", nil, entities.TriggerInfo{Type: "manual"}); err != errFlowNotFound {
		t.Fatalf("cross-tenant trigger error = %v, want %v", err, errFlowNotFound)
	}
	if runs.created != nil {
		t.Fatalf("cross-tenant trigger persisted run %+v", runs.created)
	}
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

// TestDefaultNamespace ensures an empty defaultNamespace (e.g. because a
// deployment's ConfigMap omits the namespace: block entirely) falls back to
// "default", mirroring the fallback every cmd/ entrypoint applies to
// cfg.Runtime.Namespace.DefaultNamespace (see e.g. pkg/cmd/pkg/controller/controller.go)
// — without this, an unconfigured namespace.default_namespace would silently
// diverge from the Controller's own fallback and runs
// would never be picked up by their dedicated JM.
func TestDefaultNamespace(t *testing.T) {
	if got := coalesceNamespace(""); got != "default" {
		t.Errorf("coalesceNamespace(\"\") = %q, want %q", got, "default")
	}
	if got := coalesceNamespace("acme"); got != "acme" {
		t.Errorf("coalesceNamespace(\"acme\") = %q, want %q", got, "acme")
	}
}
