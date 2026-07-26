package controller

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func testController(cfg *config.FlowgentConfig) *FlowgentController {
	return &FlowgentController{
		namespace:            "default",
		logger:            utils.NewLogger("JSON", "ERROR"),
		cfg:               cfg,
		running:           make(map[string]context.CancelFunc),
		dispatchedVersion: make(map[string]int64),
	}
}

func TestJmConfigMapName(t *testing.T) {
	t.Run("uses configured name", func(t *testing.T) {
		c := testController(&config.FlowgentConfig{
			Runtime: config.RuntimeConfig{JMConfigMap: "flowgent-config"},
		})
		if got := c.jmConfigMapName(); got != "flowgent-config" {
			t.Errorf("jmConfigMapName() = %q, want %q", got, "flowgent-config")
		}
	})

	t.Run("falls back to default when unset", func(t *testing.T) {
		c := testController(&config.FlowgentConfig{})
		if got := c.jmConfigMapName(); got != "flowgent-config" {
			t.Errorf("jmConfigMapName() = %q, want fallback %q", got, "flowgent-config")
		}
	})
}

// TestBuildJMDeploymentMountsConfigAndEnv is a regression test for a bug where
// buildJMDeployment created a JM Deployment that referenced -c /etc/flowgent/flowgent.yaml
// but never mounted a ConfigMap there, and never propagated the MQTT broker /
// apiserver URL — so the dedicated JM pod could never actually start.
func TestBuildJMDeploymentMountsConfigAndEnv(t *testing.T) {
	c := testController(&config.FlowgentConfig{
		Runtime: config.RuntimeConfig{
			JMImage:      "flowgent:test",
			JMConfigMap:  "flowgent-config",
			APIServerURL: "http://apiserver:9999",
		},
		Messager: config.MessagerConfig{
			MQTT: config.MQTTConfig{Broker: "tcp://emqx:1883"},
		},
	})

	spec := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "my-flow"},
		Priority:   entities.PriorityHigh,
	}

	dep := c.buildJMDeployment("flowgent-jobmanager-default-my-flow", "flowgent-default", "default", spec)

	if len(dep.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(dep.Spec.Template.Spec.Containers))
	}
	container := dep.Spec.Template.Spec.Containers[0]

	foundMount := false
	for _, vm := range container.VolumeMounts {
		if vm.Name == "config" && vm.MountPath == "/etc/flowgent" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("JM container missing config VolumeMount at /etc/flowgent")
	}

	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "config" && v.ConfigMap != nil && v.ConfigMap.Name == "flowgent-config" {
			foundVolume = true
		}
	}
	if !foundVolume {
		t.Error("Deployment missing config Volume backed by ConfigMap flowgent-config")
	}

	env := map[string]string{}
	for _, e := range container.Env {
		env[e.Name] = e.Value
	}
	wantEnv := map[string]string{
		"FLOWGENT__RUNTIME__AGENT_FLOW_ID":  "my-flow",
		"FLOWGENT__RUNTIME__NAMESPACE":      "flowgent-default",
		"FLOWGENT__MESSAGER__MQTT__BROKER":  "tcp://emqx:1883",
		"FLOWGENT__RUNTIME__API_SERVER_URL": "http://apiserver:9999",
	}
	for k, want := range wantEnv {
		if got := env[k]; got != want {
			t.Errorf("env[%q] = %q, want %q", k, got, want)
		}
	}
}

// TestApplicationNamespace verifies namespace is derived per-NAMESPACE (not
// per-flow), per docs/01-L1-Engine-Architecture.md §1.3 ("each namespace gets
// its own namespace") / §4.3 ("namespace={namespace}") — flows belonging to the
// same namespace must resolve to the same namespace, since each flow's
// dedicated JM Deployment is only disambiguated by name
// (flowgent-jobmanager-{namespaceId}-{flowId}), not by a separate namespace.
// It also mirrors pkg/api/pkg/handler.TestApplicationNamespace, which must
// never diverge from this one (see applicationNamespace doc comment).
func TestApplicationNamespace(t *testing.T) {
	c := testController(&config.FlowgentConfig{
		Runtime: config.RuntimeConfig{Namespace: config.NamespaceConfig{NamespacePrefix: "flowgent-"}},
	})

	t.Run("uses explicit namespace when set", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}, K8sNamespace: "custom-ns"}
		if got := c.applicationNamespace(spec); got != "custom-ns" {
			t.Errorf("applicationNamespace() = %q, want %q", got, "custom-ns")
		}
	})

	t.Run("derives from namespacePrefix + namespace ID without double dash", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow", Namespace: "acme"}}
		if got := c.applicationNamespace(spec); got != "flowgent-acme" {
			t.Errorf("applicationNamespace() = %q, want %q", got, "flowgent-acme")
		}
	})

	t.Run("two flows of the same namespace share one namespace", func(t *testing.T) {
		spec1 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-a", Namespace: "acme"}}
		spec2 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-b", Namespace: "acme"}}
		if ns1, ns2 := c.applicationNamespace(spec1), c.applicationNamespace(spec2); ns1 != ns2 {
			t.Errorf("expected same-namespace flows to share a namespace, got %q vs %q", ns1, ns2)
		}
	})

	t.Run("falls back to controller's default namespace when spec.Namespace is unset", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}}
		if got := c.applicationNamespace(spec); got != "flowgent-default" {
			t.Errorf("applicationNamespace() = %q, want %q", got, "flowgent-default")
		}
	})
}

// TestOwnsFlowUsesPassedPeerSnapshot verifies ownsFlow shards purely off the
// peer slice the caller passes in (see reconcile's single per-tick
// getPeers() snapshot) rather than re-querying discovery itself — this is
// what lets one reconcile tick fetch the live K8s pod list exactly once and
// reuse it for every flow's ownership check and for GC, instead of issuing
// one API List() call per flow.
func TestOwnsFlowUsesPassedPeerSnapshot(t *testing.T) {
	c := testController(&config.FlowgentConfig{})
	c.discovery = fakeDiscovery{self: discovery.Peer{Name: "controller-0"}}

	// No peers discovered (e.g. transient discovery error) → fail open and
	// own everything, so flows still get dispatched by a lone pod.
	if !c.ownsFlow(nil, "flow-a") {
		t.Fatal("ownsFlow(nil peers, ...) should fail open (own everything)")
	}

	peers := []discovery.Peer{{Name: "controller-0"}, {Name: "controller-1"}}
	got0 := c.ownsFlow(peers, "flow-a")
	got1 := discovery.ShardIndex("flow-a", len(peers)) == 0
	if got0 != got1 {
		t.Errorf("ownsFlow(peers, %q) = %v, want %v (shard index consistency)", "flow-a", got0, got1)
	}
}

type fakeDiscovery struct {
	self discovery.Peer
}

func (f fakeDiscovery) DiscoverPeers(ctx context.Context, labelSelector string) ([]discovery.Peer, error) {
	return nil, nil
}
func (f fakeDiscovery) Self() discovery.Peer { return f.self }
func (f fakeDiscovery) IsLeader(ctx context.Context, labelSelector string) (bool, error) {
	return true, nil
}
func (f fakeDiscovery) WatchPeers(ctx context.Context, labelSelector string) (<-chan []discovery.Peer, error) {
	return nil, nil
}

// TestShouldDispatchOnNewDefinitionOnly is a regression test for a critical
// bug where reconcile() unconditionally re-dispatched (i.e. created a brand
// new FlowRun for) every known agentflow on every pollInterval (10s) tick,
// forever — because the only gate was c.running[flowID], which is deleted
// almost immediately after dispatchFlow returns (dispatch is a fast,
// synchronous REST/K8s call, not a long-running execution). In a real
// deployment this would create an unbounded, ever-growing stream of runs for
// every flow that simply exists in the system.
//
// shouldDispatch implements the documented "on-new-definition" trigger
// condition (docs/01-L1-Engine-Architecture.md §4.2): a flow must only be
// auto-dispatched once per definition version.
func TestShouldDispatchOnNewDefinitionOnly(t *testing.T) {
	c := testController(&config.FlowgentConfig{})

	if !c.shouldDispatch("flow-a", 1) {
		t.Fatal("first sighting of flow-a@v1 should dispatch")
	}
	for i := 0; i < 5; i++ {
		if c.shouldDispatch("flow-a", 1) {
			t.Fatalf("repeated reconcile tick %d for unchanged flow-a@v1 must NOT re-dispatch", i)
		}
	}
	if !c.shouldDispatch("flow-a", 2) {
		t.Fatal("flow-a@v2 (new definition) should dispatch again")
	}
	if c.shouldDispatch("flow-a", 2) {
		t.Fatal("repeated reconcile tick for unchanged flow-a@v2 must NOT re-dispatch")
	}
	if !c.shouldDispatch("flow-b", 1) {
		t.Fatal("first sighting of a different flow-b@v1 should dispatch independently of flow-a")
	}
}
