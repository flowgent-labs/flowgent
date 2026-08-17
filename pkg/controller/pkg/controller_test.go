package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func testController(cfg *config.FlowgentConfig) *FlowgentController {
	return &FlowgentController{
		namespace:               "default",
		logger:                  utils.NewLogger("JSON", "ERROR"),
		cfg:                     cfg,
		running:                 make(map[string]context.CancelFunc),
		runtimeCleanupFirstSeen: make(map[string]time.Time),
		tmOrphanTimeout:         parseTMOrphanTimeout(cfg.Runtime.TMOrphanTimeout),
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
		Auth: config.AuthConfig{Authorization: config.AuthorizationConfig{Enabled: true, Enforcement: "enforce"}},
		Runtime: config.RuntimeConfig{
			JMImage:             "flowgent:test",
			JMConfigMap:         "flowgent-config",
			APIServerURL:        "http://apiserver:9999",
			CredentialEnvSecret: "flowgent-runtime-env",
			InternalAuthSecret:  "flowgent-runtime-auth",
			JobManagerAuthKey:   "jm-token",
		},
		Messager: config.MessagerConfig{
			MQTT: config.MQTTConfig{Broker: "tcp://emqx:1883"},
		},
	})

	spec := &entities.FlowInfo{
		BaseEntity:     entities.BaseEntity{ID: "my-flow"},
		ResourcePoolID: "default",
	}

	dep := c.buildJMDeployment("flowgent-jobmanager-default-my-flow", "flowgent-default", "default", spec, "runtime-env", "runtime-secret", "checksum")

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
		"FLOWGENT__RUNTIME__AGENT_FLOW_ID":                "my-flow",
		"FLOWGENT__RUNTIME__NAMESPACE__DEFAULT_NAMESPACE": "default",
		"FLOWGENT__RUNTIME__RESOURCE_POOL_ID":             "default",
		"FLOWGENT__RUNTIME__TM_DEPLOY":                    "flowgent-taskmanager-default-default",
		"FLOWGENT__MESSAGER__MQTT__BROKER":                "tcp://emqx:1883",
		"FLOWGENT__RUNTIME__API_SERVER_URL":               "http://apiserver:9999",
	}
	for k, want := range wantEnv {
		if got := env[k]; got != want {
			t.Errorf("env[%q] = %q, want %q", k, got, want)
		}
	}
	var workloadToken *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "FLOWGENT_INTERNAL_TOKEN" {
			workloadToken = &container.Env[i]
		}
	}
	if workloadToken == nil || workloadToken.ValueFrom == nil || workloadToken.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("JM workload token secret ref missing: %#v", workloadToken)
	}
	if got := workloadToken.ValueFrom.SecretKeyRef.Name; got != "flowgent-runtime-auth" {
		t.Fatalf("JM workload Secret = %q", got)
	}
	if got := workloadToken.ValueFrom.SecretKeyRef.Key; got != "jm-token" {
		t.Fatalf("JM workload Secret key = %q", got)
	}
	if len(container.EnvFrom) != 3 || container.EnvFrom[0].SecretRef == nil || container.EnvFrom[1].ConfigMapRef == nil || container.EnvFrom[2].SecretRef == nil {
		t.Fatalf("expected credential, Flow environment and Flow secret envFrom refs, got %#v", container.EnvFrom)
	}
	if got := container.EnvFrom[0].SecretRef.Name; got != "flowgent-runtime-env" {
		t.Fatalf("JM credential secret = %q, want flowgent-runtime-env", got)
	}
	if container.EnvFrom[0].SecretRef.Optional == nil || !*container.EnvFrom[0].SecretRef.Optional {
		t.Fatal("JM credential secret ref should be optional")
	}
	if got := container.EnvFrom[1].ConfigMapRef.Name; got != "runtime-env" {
		t.Fatalf("JM Flow environment ConfigMap = %q", got)
	}
	if got := container.EnvFrom[2].SecretRef.Name; got != "runtime-secret" {
		t.Fatalf("JM Flow secret = %q", got)
	}
	if got := dep.Spec.Template.Annotations["flowgent.io/runtime-config-checksum"]; got != "checksum" {
		t.Fatalf("runtime configuration checksum = %q", got)
	}
}

func TestEnsureRuntimeAuthSecretCopiesOnlyRuntimeKeysAndRotates(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-auth", Namespace: "system"},
		Data: map[string][]byte{
			"jm":        []byte("jm-v1"),
			"tm":        []byte("tm-v1"),
			"bootstrap": []byte("must-not-cross-boundary"),
		},
	})
	c := testController(&config.FlowgentConfig{
		Auth: config.AuthConfig{Authorization: config.AuthorizationConfig{Enabled: true, Enforcement: "enforce"}},
		Runtime: config.RuntimeConfig{
			SystemNamespace: "system", InternalAuthSecret: "runtime-auth",
			JobManagerAuthKey: "jm", TaskManagerAuthKey: "tm",
		},
	})

	if !c.ensureRuntimeAuthSecret(ctx, clientset, "flowgent-acme", "acme", "flow-a") {
		t.Fatal("initial Secret synchronization failed")
	}
	destination, err := clientset.CoreV1().Secrets("flowgent-acme").Get(ctx, "runtime-auth", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get destination Secret: %v", err)
	}
	if len(destination.Data) != 2 || string(destination.Data["jm"]) != "jm-v1" || string(destination.Data["tm"]) != "tm-v1" {
		t.Fatalf("destination data = %#v", destination.Data)
	}
	if _, leaked := destination.Data["bootstrap"]; leaked {
		t.Fatal("bootstrap credential crossed into workload namespace")
	}

	source, _ := clientset.CoreV1().Secrets("system").Get(ctx, "runtime-auth", metav1.GetOptions{})
	source.Data["tm"] = []byte("tm-v2")
	if _, err := clientset.CoreV1().Secrets("system").Update(ctx, source, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("rotate source Secret: %v", err)
	}
	if !c.ensureRuntimeAuthSecret(ctx, clientset, "flowgent-acme", "acme", "flow-a") {
		t.Fatal("rotated Secret synchronization failed")
	}
	destination, _ = clientset.CoreV1().Secrets("flowgent-acme").Get(ctx, "runtime-auth", metav1.GetOptions{})
	if got := string(destination.Data["tm"]); got != "tm-v2" {
		t.Fatalf("rotated TM token = %q", got)
	}
}

func TestEnsureRuntimeRBACAllowsPoolDeploymentReconciliation(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	c := testController(&config.FlowgentConfig{})

	c.ensureRuntimeRBAC(ctx, clientset, "flowgent-acme", "acme", "flow-a")
	role, err := clientset.RbacV1().Roles("flowgent-acme").Get(ctx, "flowgent-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get runtime Role: %v", err)
	}
	for _, rule := range role.Rules {
		if containsString(rule.APIGroups, "apps") && containsString(rule.Resources, "deployments") && containsString(rule.Verbs, "update") {
			return
		}
	}
	t.Fatalf("runtime Role cannot reconcile pool Deployments: %#v", role.Rules)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestDeleteOrphanedRuntimeConfiguration(t *testing.T) {
	ctx := context.Background()
	managedLabels := func(namespaceID, flowID string) map[string]string {
		return map[string]string{
			"flowgent.io/runtime-boundary": "flow-config",
			"flowgent.io/managed-by":       runtimeConfigurationManagedBy,
			"flowgent.io/namespace":        namespaceID,
			"flowgent.io/flow":             flowID,
		}
	}
	clientset := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "orphan-env", Namespace: "flowgent-default", Labels: managedLabels("default", "deleted-flow")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orphan-secrets", Namespace: "flowgent-default", Labels: managedLabels("default", "deleted-flow")}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "live-env", Namespace: "flowgent-default", Labels: managedLabels("default", "live-flow")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "live-secrets", Namespace: "flowgent-default", Labels: managedLabels("default", "live-flow")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "flowgent-other", Labels: managedLabels("other", "deleted-flow")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unmanaged", Namespace: "flowgent-default", Labels: map[string]string{"flowgent.io/flow": "deleted-flow"}}},
	)
	c := testController(&config.FlowgentConfig{})
	c.deleteOrphanedRuntimeConfiguration(ctx, clientset, map[string]*entities.FlowInfo{
		"live-flow": {BaseEntity: entities.BaseEntity{ID: "live-flow"}},
	}, nil)

	if _, err := clientset.CoreV1().ConfigMaps("flowgent-default").Get(ctx, "orphan-env", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphan ConfigMap was not deleted: %v", err)
	}
	if _, err := clientset.CoreV1().Secrets("flowgent-default").Get(ctx, "orphan-secrets", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("orphan Secret was not deleted: %v", err)
	}
	for namespace, name := range map[string]string{
		"flowgent-default/configmaps": "live-env",
		"flowgent-default/secrets":    "live-secrets",
		"flowgent-other/secrets":      "other-namespace",
		"flowgent-default/unmanaged":  "unmanaged",
	} {
		var err error
		switch namespace {
		case "flowgent-default/configmaps":
			_, err = clientset.CoreV1().ConfigMaps("flowgent-default").Get(ctx, name, metav1.GetOptions{})
		case "flowgent-other/secrets":
			_, err = clientset.CoreV1().Secrets("flowgent-other").Get(ctx, name, metav1.GetOptions{})
		default:
			_, err = clientset.CoreV1().Secrets("flowgent-default").Get(ctx, name, metav1.GetOptions{})
		}
		if err != nil {
			t.Fatalf("retained resource %s/%s missing: %v", namespace, name, err)
		}
	}
}

// TestRuntimeNamespace verifies namespace is derived per-NAMESPACE (not
// per-flow), per docs/01-L1-Engine-Architecture.md §1.3 ("each namespace gets
// its own namespace") / §4.3 ("namespace={namespace}") — flows belonging to the
// same namespace must resolve to the same namespace, since each flow's
// dedicated JM Deployment is only disambiguated by name
// (flowgent-jobmanager-{namespaceId}-{flowId}), not by a separate namespace.
// It also mirrors pkg/api/pkg/handler.TestRuntimeNamespace, which must
// never diverge from this one (see runtimeNamespace doc comment).
func TestRuntimeNamespace(t *testing.T) {
	c := testController(&config.FlowgentConfig{
		Runtime: config.RuntimeConfig{Namespace: config.NamespaceConfig{NamespacePrefix: "flowgent-"}},
	})

	t.Run("uses explicit namespace when set", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}, K8sNamespace: "custom-ns"}
		if got := c.runtimeNamespace(spec); got != "custom-ns" {
			t.Errorf("runtimeNamespace() = %q, want %q", got, "custom-ns")
		}
	})

	t.Run("derives from namespacePrefix + namespace ID without double dash", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow", Namespace: "acme"}}
		if got := c.runtimeNamespace(spec); got != "flowgent-acme" {
			t.Errorf("runtimeNamespace() = %q, want %q", got, "flowgent-acme")
		}
	})

	t.Run("two flows of the same namespace share one namespace", func(t *testing.T) {
		spec1 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-a", Namespace: "acme"}}
		spec2 := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "flow-b", Namespace: "acme"}}
		if ns1, ns2 := c.runtimeNamespace(spec1), c.runtimeNamespace(spec2); ns1 != ns2 {
			t.Errorf("expected same-namespace flows to share a namespace, got %q vs %q", ns1, ns2)
		}
	})

	t.Run("falls back to controller's default namespace when spec.Namespace is unset", func(t *testing.T) {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "my-flow"}}
		if got := c.runtimeNamespace(spec); got != "flowgent-default" {
			t.Errorf("runtimeNamespace() = %q, want %q", got, "flowgent-default")
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
