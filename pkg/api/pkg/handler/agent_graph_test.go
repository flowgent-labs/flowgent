package handler

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
)

type capturingGraphRunStore struct {
	created *entities.GraphRun
}

func (s *capturingGraphRunStore) Get(context.Context, string) (*entities.GraphRun, error) {
	return nil, nil
}
func (s *capturingGraphRunStore) List(context.Context, graphrun.ListFilter) (*entities.Page[entities.GraphRun], error) {
	return nil, nil
}
func (s *capturingGraphRunStore) Metrics(context.Context, graphrun.MetricRequest) (*entities.GraphRunMetrics, error) {
	return nil, nil
}
func (s *capturingGraphRunStore) HasActiveForAgentGraph(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *capturingGraphRunStore) Save(context.Context, *entities.GraphRun) error { return nil }
func (s *capturingGraphRunStore) Delete(context.Context, string) error           { return nil }
func (s *capturingGraphRunStore) Create(_ context.Context, run *entities.GraphRun) error {
	run.ID = "run-1"
	copy := *run
	s.created = &copy
	return nil
}
func (s *capturingGraphRunStore) Update(context.Context, *entities.GraphRun) error { return nil }
func (s *capturingGraphRunStore) Cancel(context.Context, string) error             { return nil }

// TestRuntimeNamespace is a regression test for a bug where Trigger
// (Path A, POST /agent-graphs/trigger) always created runs with namespace="",
// so graphs triggered via the canonical /trigger endpoint were never picked
// up by their dedicated per-graph JM (which only polls its namespace's
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
// per-TENANT (not per-graph): every graph belonging to the same namespace shares
// one namespace, with each graph's dedicated JM Deployment disambiguated by
// name alone (flowgent-jobmanager-{namespaceId}-{graphId}).
func TestRuntimeNamespace(t *testing.T) {
	h := &AgentGraphHandler{namespacePrefix: "flowgent-", defaultNamespace: "default"}

	t.Run("uses explicit namespace when set", func(t *testing.T) {
		spec := &entities.AgentGraph{Name: "my-graph",
			BaseEntity:   entities.BaseEntity{ID: "my-graph"},
			K8sNamespace: "custom-ns",
		}
		if got := h.runtimeNamespace(spec); got != "custom-ns" {
			t.Errorf("runtimeNamespace() = %q, want %q", got, "custom-ns")
		}
	})

	t.Run("derives from namespacePrefix + namespace ID without double dash", func(t *testing.T) {
		spec := &entities.AgentGraph{Name: "my-graph",
			BaseEntity: entities.BaseEntity{ID: "my-graph", Namespace: "acme"},
		}
		want := "flowgent-acme"
		if got := h.runtimeNamespace(spec); got != want {
			t.Errorf("runtimeNamespace() = %q, want %q", got, want)
		}
	})

	t.Run("two graphs of the same namespace share one namespace", func(t *testing.T) {
		spec1 := &entities.AgentGraph{Name: "graph-a", BaseEntity: entities.BaseEntity{ID: "graph-a", Namespace: "acme"}}
		spec2 := &entities.AgentGraph{Name: "graph-b", BaseEntity: entities.BaseEntity{ID: "graph-b", Namespace: "acme"}}
		ns1, ns2 := h.runtimeNamespace(spec1), h.runtimeNamespace(spec2)
		if ns1 != ns2 {
			t.Errorf("expected same-namespace graphs to share a namespace, got %q vs %q", ns1, ns2)
		}
	})

	t.Run("falls back to defaultNamespace when spec.Namespace is unset", func(t *testing.T) {
		spec := &entities.AgentGraph{Name: "my-graph", BaseEntity: entities.BaseEntity{ID: "my-graph"}}
		want := "flowgent-default"
		if got := h.runtimeNamespace(spec); got != want {
			t.Errorf("runtimeNamespace() = %q, want %q", got, want)
		}
	})
}

func TestLogicalAndK8sNamespacesRemainDistinct(t *testing.T) {
	h := &AgentGraphHandler{namespacePrefix: "flowgent-", defaultNamespace: "default"}

	for _, tc := range []struct {
		name        string
		spec        *entities.AgentGraph
		wantLogical string
		wantK8s     string
	}{
		{
			name:        "explicit tenant",
			spec:        &entities.AgentGraph{BaseEntity: entities.BaseEntity{Namespace: "acme"}},
			wantLogical: "acme",
			wantK8s:     "flowgent-acme",
		},
		{
			name:        "default tenant",
			spec:        &entities.AgentGraph{},
			wantLogical: "default",
			wantK8s:     "flowgent-default",
		},
		{
			name:        "custom physical namespace",
			spec:        &entities.AgentGraph{BaseEntity: entities.BaseEntity{Namespace: "acme"}, K8sNamespace: "isolated-acme"},
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
	spec := &entities.AgentGraph{Name: "graph-a",
		BaseEntity:  entities.BaseEntity{ID: "graph-a", Namespace: "tenant-a"},
		RuntimeMode: entities.RuntimeModeApplication,
	}
	runs := &capturingGraphRunStore{}
	h := &AgentGraphHandler{
		agentGraphs:      map[string]*entities.AgentGraph{graphCacheKey("tenant-a", spec.ID): spec},
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
	spec := &entities.AgentGraph{Name: "graph-a",
		BaseEntity:  entities.BaseEntity{ID: "graph-a", Namespace: "tenant-a"},
		RuntimeMode: entities.RuntimeModeApplication,
	}
	runs := &capturingGraphRunStore{}
	h := &AgentGraphHandler{
		agentGraphs:      map[string]*entities.AgentGraph{graphCacheKey("tenant-a", spec.ID): spec},
		frStore:          runs,
		namespacePrefix:  "flowgent-",
		defaultNamespace: "default",
	}

	if _, err := h.CreateRunFromTrigger(context.Background(), spec.ID, "tenant-b", nil, entities.TriggerInfo{Type: "manual"}); err != errAgentGraphNotFound {
		t.Fatalf("cross-tenant trigger error = %v, want %v", err, errAgentGraphNotFound)
	}
	if runs.created != nil {
		t.Fatalf("cross-tenant trigger persisted run %+v", runs.created)
	}
}

func TestApplyStableAgentGraphIdentity(t *testing.T) {
	existing := &entities.AgentGraph{
		BaseEntity: entities.BaseEntity{ID: "2f2cf079-63db-4ef3-a972-f576de41b23e"},
		Name:       "security-fixer",
	}

	t.Run("stable ID route preserves omitted name", func(t *testing.T) {
		updates := &entities.AgentGraph{}
		applyStableAgentGraphIdentity(existing, updates)
		if updates.ID != existing.ID {
			t.Fatalf("stable ID = %q, want %q", updates.ID, existing.ID)
		}
		if updates.Name != existing.Name {
			t.Fatalf("name = %q, want preserved name %q", updates.Name, existing.Name)
		}
	})

	t.Run("explicit rename remains independent of stable ID", func(t *testing.T) {
		updates := &entities.AgentGraph{Name: "security-fixer-v2"}
		applyStableAgentGraphIdentity(existing, updates)
		if updates.ID != existing.ID {
			t.Fatalf("stable ID = %q, want %q", updates.ID, existing.ID)
		}
		if updates.Name != "security-fixer-v2" {
			t.Fatalf("name = %q, want explicit rename", updates.Name)
		}
	})
}

func TestAgentGraphCacheAliasesFollowRenameAndDelete(t *testing.T) {
	const namespace = "tenant-a"
	existing := &entities.AgentGraph{
		BaseEntity: entities.BaseEntity{ID: "2f2cf079-63db-4ef3-a972-f576de41b23e"},
		Name:       "security-fixer",
	}
	h := &AgentGraphHandler{agentGraphs: make(map[string]*entities.AgentGraph)}
	h.replaceCachedDefinition(namespace, existing)

	if h.agentGraphs[graphCacheKey(namespace, existing.Name)] == nil {
		t.Fatal("name alias was not cached")
	}
	if h.agentGraphs[graphCacheKey(namespace, existing.ID)] == nil {
		t.Fatal("stable ID alias was not cached")
	}

	renamed := *existing
	renamed.Name = "security-fixer-v2"
	h.replaceCachedDefinition(namespace, &renamed, existing.Name, existing.ID)
	if h.agentGraphs[graphCacheKey(namespace, existing.Name)] != nil {
		t.Fatal("stale name alias survived rename")
	}
	if h.agentGraphs[graphCacheKey(namespace, renamed.Name)] == nil ||
		h.agentGraphs[graphCacheKey(namespace, renamed.ID)] == nil {
		t.Fatal("renamed definition aliases were not cached")
	}

	h.removeCachedDefinition(namespace, &renamed, renamed.ID)
	if len(h.agentGraphs) != 0 {
		t.Fatalf("cache aliases survived delete: %#v", h.agentGraphs)
	}
}

// TestNewAgentGraphHandlerDefaultNamespacePrefix ensures an empty
// namespacePrefix falls back to "flowgent-" rather than producing an
// unprefixed (and potentially colliding) namespace.
func TestNewAgentGraphHandlerDefaultNamespacePrefix(t *testing.T) {
	h := &AgentGraphHandler{}
	if h.namespacePrefix != "" {
		t.Fatalf("precondition failed: expected zero value")
	}
	// NewAgentGraphHandler requires a store; exercise the same default-fallback
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
