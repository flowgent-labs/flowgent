// Package controller provides the distributed sharded flow driver.
// It discovers flows via apiserver REST API, shards across controller pods via
// hash-mod partitioning, and dispatches executions through dedicated per-flow
// JobManager Deployments.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const runtimeConfigurationManagedBy = "controller"

// FlowgentController is the distributed flow driver.
type FlowgentController struct {
	api       *client.FlowgentClient
	namespace string
	rm        resourcemanager.ResourceManager
	logger    *utils.Logger
	cfg       *config.FlowgentConfig
	cfgPath   string

	discovery    discovery.IDiscoveryClient
	pollInterval time.Duration
	cronTrigger  *trigger.ScheduleTrigger

	mu                      sync.Mutex
	running                 map[string]context.CancelFunc
	runtimeCleanupFirstSeen map[string]time.Time
	tmOrphanTimeout         time.Duration
}

// NewFlowgentController creates a FlowgentController instance.
func NewFlowgentController(api *client.FlowgentClient, namespace string, rm resourcemanager.ResourceManager,
	logger *utils.Logger, cfg *config.FlowgentConfig, cfgPath string,
	disc discovery.IDiscoveryClient) *FlowgentController {
	return &FlowgentController{
		api:                     api,
		namespace:               namespace,
		rm:                      rm,
		logger:                  logger,
		cfg:                     cfg,
		cfgPath:                 cfgPath,
		discovery:               disc,
		pollInterval:            10 * time.Second,
		cronTrigger:             trigger.NewScheduleTrigger(),
		running:                 make(map[string]context.CancelFunc),
		runtimeCleanupFirstSeen: make(map[string]time.Time),
		tmOrphanTimeout:         parseTMOrphanTimeout(cfg.Runtime.TMOrphanTimeout),
	}
}

// ─── Pod Discovery & Sharding ──────────────────────────────────

func (c *FlowgentController) getPeers(ctx context.Context) (peers []discovery.Peer, selfIndex int, err error) {
	labelSelector := c.cfg.Runtime.ControllerLabel
	if labelSelector == "" {
		labelSelector = "app.kubernetes.io/component=controller"
	}

	peers, err = c.discovery.DiscoverPeers(ctx, labelSelector)
	if err != nil {
		return nil, 0, fmt.Errorf("discover peers: %w", err)
	}

	self := c.discovery.Self()
	for i, p := range peers {
		if p.Name == self.Name {
			selfIndex = i
			break
		}
	}

	return peers, selfIndex, nil
}

// ownsFlow determines shard ownership from an already-fetched peer snapshot
// (see getPeers). Callers within the same reconcile tick must reuse a single
// snapshot rather than calling getPeers per-flow — see reconcile's peers
// variable — so that all ownership decisions in one tick are consistent with
// each other, and so a single K8s API List() call is reused across every
// flow/deployment instead of issuing one per flow (which would scale O(number
// of flows) API calls per pollInterval tick).
func (c *FlowgentController) ownsFlow(peers []discovery.Peer, flowID string) bool {
	if len(peers) == 0 {
		return true
	}
	self := c.discovery.Self()
	shard := discovery.ShardIndex(flowID, len(peers))
	for i, p := range peers {
		if p.Name == self.Name && i == shard {
			return true
		}
	}
	return false
}

// ─── Main Loop ─────────────────────────────────────────────────

func (c *FlowgentController) Run(ctx context.Context) error {
	peers, idx, err := c.getPeers(ctx)
	if err != nil {
		c.logger.Warn("Initial pod discovery failed, retrying", "error", err)
		peers = []discovery.Peer{c.discovery.Self()}
		idx = 0
	}

	c.logger.Info("Controller starting",
		"pod", c.discovery.Self().Name,
		"shard", fmt.Sprintf("%d/%d", idx, len(peers)),
		"poll_interval", c.pollInterval)

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	c.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Controller shutting down")
			c.stopAllFlows()
			c.cronTrigger.Stop()
			return nil
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

func (c *FlowgentController) reconcile(ctx context.Context) {
	// Fetch the peer/shard snapshot once per tick — via IDiscoveryClient this
	// is a live K8s API List() of controller pods (see discovery.K8sDiscoveryClient),
	// so pod scale-up/down/restarts between ticks are picked up automatically
	// on the very next reconcile without any restart or manual step. Reusing
	// one snapshot for the whole tick (instead of re-querying per flow) keeps
	// ownership decisions self-consistent within the tick and avoids O(N) API
	// calls (see ownsFlow).
	peers, _, err := c.getPeers(ctx)
	if err != nil {
		c.logger.Warn("Pod discovery failed this tick, falling back to owning everything", "error", err)
		peers = nil
	}

	versions, err := c.api.ListFlows(ctx, c.namespace)
	if err != nil {
		c.logger.Error("Failed to list agentflow definitions via apiserver", "error", err)
		return
	}

	seen := make(map[string]*entities.FlowInfo)
	for _, v := range versions {
		if _, exists := seen[v.FlowID]; exists {
			continue
		}
		var spec entities.FlowInfo
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			c.logger.Warn("Skipping invalid flow definition", "flow_id", v.FlowID, "error", err)
			continue
		}
		if spec.ID == "" {
			continue
		}
		if spec.ResourcePoolID == "" {
			spec.ResourcePoolID = "default"
		}
		if strings.EqualFold(spec.Kind, "skill") {
			continue
		}
		seen[v.FlowID] = &spec
	}

	activeRunFlows, activeRunSnapshotOK := c.collectActiveRunFlows(ctx, seen, peers)
	resourcePools := make(map[string]struct{})
	resourcePoolSnapshotOK := false
	if pools, poolErr := c.api.ListResourcePools(ctx, c.namespace); poolErr != nil {
		c.logger.Warn("Resource-pool snapshot failed; deployment cleanup skipped", "error", poolErr)
	} else {
		resourcePoolSnapshotOK = true
		for _, pool := range pools {
			if pool != nil && pool.Name != "" {
				resourcePools[pool.Name] = struct{}{}
			}
		}
	}

	var ownedFlows []entities.FlowInfo
	for flowID, spec := range seen {
		if !c.ownsFlow(peers, flowID) {
			continue
		}
		ownedFlows = append(ownedFlows, *spec)

		// Importing or updating a flow definition is metadata registration only.
		// Runtime infrastructure is created lazily when a real FlowRun is
		// PENDING/RUNNING/PAUSED. The Controller owns JM lifecycle, but does not keep
		// idle JMs/TMs around merely because a definition exists.
		if activeRunSnapshotOK && activeRunFlows[flowID] {
			c.ensureFlowJobManager(ctx, spec)
		}
	}

	// Re-register cron/schedule triggers (docs §4.2 "cron / interval") for
	// this pod's shard only, so each scheduled run fires exactly once across
	// all Controller replicas, and edits/removals of triggers take effect
	// without restarting the Controller.
	c.cronTrigger.Clear()
	c.cronTrigger.RegisterAgentFlows(ownedFlows, c.triggerScheduledRun)
	c.cronTrigger.Start()

	// Garbage-collect dedicated JM/TM Deployments for deleted flows and for
	// flows that no longer have active runs. If the active-run snapshot failed,
	// do not treat missing activity as authoritative; only deleted-flow cleanup
	// remains safe.
	c.gcOrphanedJMDeployments(ctx, seen, activeRunFlows, activeRunSnapshotOK, peers)
	c.gcOrphanedRuntimeDeployments(ctx, seen, activeRunFlows, activeRunSnapshotOK, peers)
	c.gcOrphanedResourcePoolDeployments(ctx, resourcePools, resourcePoolSnapshotOK, peers)
	c.gcOrphanedRuntimeConfiguration(ctx, seen, peers)
}

// collectActiveRunFlows returns the set of flow IDs owned by this controller
// shard that currently have at least one non-terminal run. A failed snapshot is
// not equivalent to "no active runs"; callers use the boolean to avoid deleting
// live runtime infrastructure during a transient apiserver problem.
func (c *FlowgentController) collectActiveRunFlows(ctx context.Context, seen map[string]*entities.FlowInfo, peers []discovery.Peer) (map[string]bool, bool) {
	active := make(map[string]bool)
	statuses := []entities.RunStatus{
		entities.RunPending,
		entities.RunRunning,
		entities.RunPaused,
	}
	for flowID := range seen {
		if !c.ownsFlow(peers, flowID) {
			continue
		}
		for _, status := range statuses {
			page, err := c.api.ListRuns(ctx, c.namespace, string(status), "", flowID, 1, 100)
			if err != nil {
				c.logger.Warn("active-run snapshot failed", "flow_id", flowID, "status", status, "error", err)
				return active, false
			}
			if len(page.Items) > 0 {
				active[flowID] = true
				break
			}
		}
	}
	return active, true
}

// triggerScheduledRun is the ScheduleTrigger callback for a cron-triggered
// agentflow. It re-fetches the latest spec (cron fires asynchronously,
// possibly minutes after the last reconcile) and creates a run for it.
func (c *FlowgentController) triggerScheduledRun(ctx context.Context, flowID string) {
	spec, err := c.api.GetFlow(ctx, c.namespace, flowID)
	if err != nil || spec == nil {
		c.logger.Warn("cron trigger: failed to load flow spec", "flow_id", flowID, "error", err)
		return
	}
	c.createScheduledRun(ctx, spec)
	c.ensureFlowJobManager(ctx, spec)
}

// ensureFlowJobManager makes sure the dedicated per-flow JM Deployment
// exists for this flow, in its namespace's shared namespace. It is called only
// for active runs, not for metadata-only flow definition imports.
func (c *FlowgentController) ensureFlowJobManager(ctx context.Context, spec *entities.FlowInfo) {
	ns := c.runtimeNamespace(spec)
	namespaceID := c.dispatchNamespace(spec)
	poolID := defaultResourcePoolID(spec.ResourcePoolID)
	pool, err := c.api.GetResourcePool(ctx, namespaceID, poolID)
	if err != nil || pool == nil {
		c.logger.Error("Unable to resolve Flow resource pool", "flow_id", spec.ID, "resource_pool", poolID, "error", err)
		return
	}
	poolJSON, _ := json.Marshal(pool)
	poolChecksum := fmt.Sprintf("%x", sha256.Sum256(poolJSON))

	cfg, err := rest.InClusterConfig()
	if err != nil {
		c.logger.Error("Not in K8s cluster — cannot create dedicated JM", "flow_id", spec.ID, "error", err)
		return
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Error("Failed to create K8s client", "flow_id", spec.ID, "error", err)
		return
	}

	// The per-namespace namespace (e.g. "flowgent-{namespaceID}") is not
	// pre-created by Helm — it is created lazily here, on first dispatch of
	// any flow belonging to this namespace. Deployments(ns).Create would
	// otherwise fail with a 404 "namespaces \"...\" not found" the very
	// first time a new namespace is seen. Safe to call repeatedly: every flow
	// of the same namespace shares (and may re-touch) this same namespace.
	if _, err := clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			c.logger.Error("Failed to check runtime namespace", "flow_id", spec.ID, "namespace", ns, "error", err)
			return
		}
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID},
		}}
		if _, err := clientset.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			c.logger.Error("Failed to create runtime namespace", "flow_id", spec.ID, "namespace", ns, "error", err)
			return
		}
		c.logger.Info("Runtime namespace created", "flow_id", spec.ID, "namespace_id", namespaceID, "namespace", ns)

		// Copy the shared ConfigMap (flowgent-config) from the controller's
		// own namespace into the new namespace namespace. Without this, every
		// dedicated JM pod in the namespace namespace would fail with
		// "MountVolume.SetUp failed for volume \"config\": configmap not found".
		cmName := c.jmConfigMapName()
		if srcCM, srcErr := clientset.CoreV1().ConfigMaps(c.systemNamespace()).Get(ctx, cmName, metav1.GetOptions{}); srcErr == nil {
			dstCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:   cmName,
					Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID},
				},
				Data: srcCM.Data,
			}
			if _, err := clientset.CoreV1().ConfigMaps(ns).Create(ctx, dstCM, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
				c.logger.Warn("Failed to copy ConfigMap to namespace namespace", "flow_id", spec.ID, "namespace", ns, "configmap", cmName, "error", err)
			} else {
				c.logger.Info("ConfigMap copied to namespace namespace", "flow_id", spec.ID, "namespace", ns, "configmap", cmName)
			}
		}

		// Ensure the shared ConfigMap exists in the workload namespace on every
		// reconcile tick. When the namespace is recycled (deleted+re-created),
		// the ConfigMap is lost, so do a Get-or-Create here.
		{
			cmName := c.jmConfigMapName()
			if _, cmErr := clientset.CoreV1().ConfigMaps(ns).Get(ctx, cmName, metav1.GetOptions{}); cmErr != nil {
				srcCM, srcErr := clientset.CoreV1().ConfigMaps(c.systemNamespace()).Get(ctx, cmName, metav1.GetOptions{})
				if srcErr == nil {
					dstCM := &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:   cmName,
							Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID},
						},
						Data: srcCM.Data,
					}
					if _, err := clientset.CoreV1().ConfigMaps(ns).Create(ctx, dstCM, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
						c.logger.Warn("Failed to copy ConfigMap", "namespace", ns, "configmap", cmName, "error", err)
					} else {
						c.logger.Info("ConfigMap ensured", "namespace", ns, "configmap", cmName)
					}
				}
			}
		}
	}
	c.ensureRuntimeConfigMap(ctx, clientset, ns, namespaceID, spec.ID)
	if !c.ensureRuntimeAuthSecret(ctx, clientset, ns, namespaceID, spec.ID) {
		return
	}
	runtimeConfigMap, runtimeSecret, runtimeChecksum, ok := c.ensureFlowRuntimeConfiguration(ctx, clientset, ns, namespaceID, spec.ID)
	if !ok {
		return
	}
	c.ensureRuntimeRBAC(ctx, clientset, ns, namespaceID, spec.ID)

	jmName := resourceid.KubernetesName("flowgent-jobmanager", namespaceID, spec.ID)
	jmDeployment := c.buildJMDeployment(jmName, ns, namespaceID, spec, runtimeConfigMap, runtimeSecret, runtimeChecksum)
	jmDeployment.Spec.Template.Annotations["flowgent.io/resource-pool-checksum"] = poolChecksum

	_, err = clientset.AppsV1().Deployments(ns).Create(ctx, jmDeployment, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := clientset.AppsV1().Deployments(ns).Get(ctx, jmName, metav1.GetOptions{})
		if getErr != nil {
			c.logger.Error("Failed to read dedicated JM deployment", "flow_id", spec.ID, "error", getErr)
			return
		}
		if !jmDeploymentManagedEqual(existing, jmDeployment) {
			existing.Spec.Replicas = jmDeployment.Spec.Replicas
			existing.Spec.Template.Labels = jmDeployment.Spec.Template.Labels
			if existing.Spec.Template.Annotations == nil {
				existing.Spec.Template.Annotations = map[string]string{}
			}
			existing.Spec.Template.Annotations = jmDeployment.Spec.Template.Annotations
			existing.Spec.Template.Spec.ServiceAccountName = jmDeployment.Spec.Template.Spec.ServiceAccountName
			existing.Spec.Template.Spec.Volumes = jmDeployment.Spec.Template.Spec.Volumes
			if len(existing.Spec.Template.Spec.Containers) == 0 {
				existing.Spec.Template.Spec.Containers = jmDeployment.Spec.Template.Spec.Containers
			} else {
				desired := jmDeployment.Spec.Template.Spec.Containers[0]
				container := &existing.Spec.Template.Spec.Containers[0]
				container.Image = desired.Image
				container.ImagePullPolicy = desired.ImagePullPolicy
				container.Args = desired.Args
				container.Env = desired.Env
				container.EnvFrom = desired.EnvFrom
				container.VolumeMounts = desired.VolumeMounts
			}
			existing.Labels = jmDeployment.Labels
			if _, updateErr := clientset.AppsV1().Deployments(ns).Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
				c.logger.Error("Failed to update dedicated JM deployment", "flow_id", spec.ID, "error", updateErr)
				return
			}
			c.logger.Info("Dedicated JM deployment updated", "flow_id", spec.ID, "namespace", ns, "deployment", jmName)
		}
		return
	}
	if err != nil {
		c.logger.Error("Failed to create dedicated JM deployment", "flow_id", spec.ID, "error", err)
		return
	}
	if err == nil {
		c.logger.Info("Dedicated JM deployment created",
			"flow_id", spec.ID, "namespace", ns, "deployment", jmName)
	}
}

func jmDeploymentManagedEqual(existing, desired *appsv1.Deployment) bool {
	if existing == nil || desired == nil || len(existing.Spec.Template.Spec.Containers) == 0 || len(desired.Spec.Template.Spec.Containers) == 0 {
		return false
	}
	currentContainer := existing.Spec.Template.Spec.Containers[0]
	desiredContainer := desired.Spec.Template.Spec.Containers[0]
	return reflect.DeepEqual(existing.Labels, desired.Labels) &&
		reflect.DeepEqual(existing.Spec.Replicas, desired.Spec.Replicas) &&
		reflect.DeepEqual(existing.Spec.Template.Labels, desired.Spec.Template.Labels) &&
		reflect.DeepEqual(existing.Spec.Template.Annotations, desired.Spec.Template.Annotations) &&
		existing.Spec.Template.Spec.ServiceAccountName == desired.Spec.Template.Spec.ServiceAccountName &&
		reflect.DeepEqual(existing.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes) &&
		currentContainer.Image == desiredContainer.Image &&
		currentContainer.ImagePullPolicy == desiredContainer.ImagePullPolicy &&
		reflect.DeepEqual(currentContainer.Args, desiredContainer.Args) &&
		reflect.DeepEqual(currentContainer.Env, desiredContainer.Env) &&
		reflect.DeepEqual(currentContainer.EnvFrom, desiredContainer.EnvFrom) &&
		reflect.DeepEqual(currentContainer.VolumeMounts, desiredContainer.VolumeMounts)
}

// ensureFlowRuntimeConfiguration resolves namespace→Flow inheritance
// through the workload-only API and materializes one ConfigMap and Secret in
// the Flow runtime boundary. The digest is placed on the JM pod template so a
// configuration change rolls JM; newly created TM/Sandbox pods receive the
// same resources through their ResourceManager configuration.
func (c *FlowgentController) ensureFlowRuntimeConfiguration(ctx context.Context, clientset kubernetes.Interface, namespace, namespaceID, flowID string) (string, string, string, bool) {
	resolved, err := c.api.ResolveFlowRuntimeConfig(ctx, namespaceID, flowID)
	if err != nil {
		c.logger.Error("Failed to resolve Flow runtime configuration", "flow_id", flowID, "namespace_id", namespaceID, "error", err)
		return "", "", "", false
	}
	configMapName := resourceid.KubernetesName("flowgent-runtime-env", namespaceID, flowID)
	secretName := resourceid.KubernetesName("flowgent-runtime-secrets", namespaceID, flowID)
	labels := map[string]string{
		"flowgent.io/runtime-boundary": "flow-config", "flowgent.io/namespace": namespaceID,
		"flowgent.io/flow": flowID, "flowgent.io/managed-by": runtimeConfigurationManagedBy,
	}
	if !upsertRuntimeConfigMap(ctx, clientset, namespace, configMapName, labels, resolved.Environment) {
		c.logger.Error("Failed to synchronize Flow runtime environment", "flow_id", flowID, "namespace", namespace)
		return "", "", "", false
	}
	secretData := make(map[string][]byte, len(resolved.Secrets))
	for key, value := range resolved.Secrets {
		secretData[key] = []byte(value)
	}
	if !upsertRuntimeSecret(ctx, clientset, namespace, secretName, labels, secretData) {
		c.logger.Error("Failed to synchronize Flow runtime secrets", "flow_id", flowID, "namespace", namespace)
		return "", "", "", false
	}
	canonical, _ := json.Marshal(resolved)
	checksum := fmt.Sprintf("%x", sha256.Sum256(canonical))
	return configMapName, secretName, checksum, true
}

func upsertRuntimeConfigMap(ctx context.Context, clientset kubernetes.Interface, namespace, name string, labels, data map[string]string) bool {
	existing, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, Data: data,
		}, metav1.CreateOptions{})
		return err == nil || apierrors.IsAlreadyExists(err)
	}
	if err != nil {
		return false
	}
	if reflect.DeepEqual(existing.Data, data) && reflect.DeepEqual(existing.Labels, labels) {
		return true
	}
	existing.Data = data
	existing.Labels = labels
	_, err = clientset.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err == nil
}

func upsertRuntimeSecret(ctx context.Context, clientset kubernetes.Interface, namespace, name string, labels map[string]string, data map[string][]byte) bool {
	existing, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, Type: corev1.SecretTypeOpaque, Data: data,
		}, metav1.CreateOptions{})
		return err == nil || apierrors.IsAlreadyExists(err)
	}
	if err != nil {
		return false
	}
	if reflect.DeepEqual(existing.Data, data) && reflect.DeepEqual(existing.Labels, labels) {
		return true
	}
	existing.Data = data
	existing.Labels = labels
	existing.Type = corev1.SecretTypeOpaque
	_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err == nil
}

// ensureRuntimeAuthSecret copies only the runtime workload credentials into
// the namespace runtime boundary. Human/bootstrap and control-plane tokens are
// deliberately kept out of workload namespaces. Existing Secrets are
// updated on rotation before a new JM is admitted.
func (c *FlowgentController) ensureRuntimeAuthSecret(ctx context.Context, clientset kubernetes.Interface, namespace, namespaceID, flowID string) bool {
	if !c.cfg.Auth.Authorization.Enabled || strings.EqualFold(c.cfg.Auth.Authorization.Enforcement, "disabled") {
		return true
	}
	name := c.cfg.Runtime.InternalAuthSecret
	if name == "" {
		c.logger.Error("Runtime authorization Secret is not configured", "flow_id", flowID, "namespace", namespace)
		return false
	}
	source, err := clientset.CoreV1().Secrets(c.systemNamespace()).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		c.logger.Error("Failed to read runtime authorization Secret", "flow_id", flowID, "namespace", namespace, "secret", name, "error", err)
		return false
	}
	keys := []string{c.jobManagerAuthKey(), c.taskManagerAuthKey()}
	data := make(map[string][]byte, len(keys))
	for _, key := range keys {
		value, ok := source.Data[key]
		if !ok || len(value) == 0 {
			c.logger.Error("Runtime authorization Secret is missing a workload key", "flow_id", flowID, "namespace", namespace, "secret", name, "key", key)
			return false
		}
		data[key] = append([]byte(nil), value...)
	}
	labels := map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID}
	existing, err := clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, Type: corev1.SecretTypeOpaque, Data: data,
		}, metav1.CreateOptions{})
	} else if err == nil && (!reflect.DeepEqual(existing.Data, data) || !reflect.DeepEqual(existing.Labels, labels)) {
		existing.Data = data
		existing.Labels = labels
		existing.Type = corev1.SecretTypeOpaque
		_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	if err != nil {
		c.logger.Error("Failed to synchronize runtime authorization Secret", "flow_id", flowID, "namespace", namespace, "secret", name, "error", err)
		return false
	}
	return true
}

func (c *FlowgentController) ensureRuntimeRBAC(ctx context.Context, clientset kubernetes.Interface, namespace, namespaceID, flowID string) {
	name := "flowgent-runtime"
	if _, err := clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		_, err = clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID}},
		}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			c.logger.Warn("Failed to create runtime ServiceAccount", "flow_id", flowID, "namespace", namespace, "error", err)
			return
		}
	} else if err != nil {
		c.logger.Warn("Failed to check runtime ServiceAccount", "flow_id", flowID, "namespace", namespace, "error", err)
		return
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID}},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods", "configmaps"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list", "create", "update", "delete"}},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments/scale"}, Verbs: []string{"get", "update"}},
		},
	}
	if _, err := clientset.RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := clientset.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			c.logger.Warn("Failed to create runtime Role", "flow_id", flowID, "namespace", namespace, "error", err)
			return
		}
	} else if err == nil {
		if _, err := clientset.RbacV1().Roles(namespace).Update(ctx, role, metav1.UpdateOptions{}); err != nil {
			c.logger.Warn("Failed to update runtime Role", "flow_id", flowID, "namespace", namespace, "error", err)
			return
		}
	} else if err != nil {
		c.logger.Warn("Failed to check runtime Role", "flow_id", flowID, "namespace", namespace, "error", err)
		return
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID}},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: name},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      name,
			Namespace: namespace,
		}},
	}
	if _, err := clientset.RbacV1().RoleBindings(namespace).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err := clientset.RbacV1().RoleBindings(namespace).Create(ctx, binding, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			c.logger.Warn("Failed to create runtime RoleBinding", "flow_id", flowID, "namespace", namespace, "error", err)
		}
	} else if err == nil {
		if _, err := clientset.RbacV1().RoleBindings(namespace).Update(ctx, binding, metav1.UpdateOptions{}); err != nil {
			c.logger.Warn("Failed to update runtime RoleBinding", "flow_id", flowID, "namespace", namespace, "error", err)
		}
	} else if err != nil {
		c.logger.Warn("Failed to check runtime RoleBinding", "flow_id", flowID, "namespace", namespace, "error", err)
	}
}

func (c *FlowgentController) ensureRuntimeConfigMap(ctx context.Context, clientset kubernetes.Interface, namespace, namespaceID, flowID string) {
	cmName := c.jmConfigMapName()
	if _, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{}); err == nil {
		return
	} else if !apierrors.IsNotFound(err) {
		c.logger.Warn("Failed to check ConfigMap in runtime namespace",
			"flow_id", flowID, "namespace", namespace, "configmap", cmName, "error", err)
		return
	}
	srcCM, err := clientset.CoreV1().ConfigMaps(c.systemNamespace()).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		c.logger.Warn("Failed to load system ConfigMap for runtime namespace",
			"flow_id", flowID, "system_namespace", c.systemNamespace(), "configmap", cmName, "error", err)
		return
	}
	dstCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cmName,
			Labels: map[string]string{"flowgent.io/runtime-boundary": "namespace", "flowgent.io/namespace": namespaceID},
		},
		Data: srcCM.Data,
	}
	if _, err := clientset.CoreV1().ConfigMaps(namespace).Create(ctx, dstCM, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		c.logger.Warn("Failed to copy ConfigMap to runtime namespace",
			"flow_id", flowID, "namespace", namespace, "configmap", cmName, "error", err)
		return
	}
	c.logger.Info("ConfigMap ensured in runtime namespace", "flow_id", flowID, "namespace", namespace, "configmap", cmName)
}

// createScheduledRun creates a PENDING run scoped to the flow's namespace
// namespace, so only its dedicated JM (which polls that namespace) picks it
// up.
func (c *FlowgentController) createScheduledRun(ctx context.Context, spec *entities.FlowInfo) {
	ns := c.runtimeNamespace(spec)
	namespaceID := c.dispatchNamespace(spec)
	run := &entities.FlowRunInfo{
		BaseEntity:     entities.BaseEntity{ID: fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()), Namespace: namespaceID},
		AgentFlowID:    spec.ID,
		Version:        1,
		Status:         entities.RunPending,
		ResourcePoolID: defaultResourcePoolID(spec.ResourcePoolID),
		K8sNamespace:   ns,
		Vars:           spec.Vars,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "controller"})
	if _, err := c.api.CreateRun(ctx, namespaceID, run); err != nil {
		c.logger.Error("Failed to create scheduled run via apiserver", "flow_id", spec.ID, "error", err)
	}
}

// runtimeNamespace computes the K8s namespace for a flow's dedicated JM
// Deployment. Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, namespace
// isolation is per-TENANT namespace (not per-flow): every flow belonging to
// the same namespace shares one namespace, and each flow's dedicated JM
// Deployment is disambiguated by name alone
// (flowgent-jobmanager-{namespaceId}-{flowId} — see ensureFlowJobManager).
// namespace.namespace_prefix already includes its own trailing separator
// (default "flowgent-" — see etc/flowgent.yaml), so it is concatenated
// directly with the namespace ID, not joined with another "-" (which would
// produce a malformed "flowgent--{namespaceID}" namespace).
// pkg/api/pkg/handler/flow_def.go's runtimeNamespace must compute the
// exact same value so Trigger (Path A) and the Controller (Path B) agree on
// which namespace a given flow's dedicated JM lives in — in particular both
// sides must fall back to the same "flowgent-" namespace prefix (via
// defaultNamespacePrefix, mirroring handler.defaultNamespacePrefix) and the
// same default namespace ID (cfg.Runtime.Namespace.DefaultNamespace) when a flow spec
// doesn't carry its own Namespace. Without this shared fallback the two
// components would silently disagree on the namespace and runs would never be
// picked up by their dedicated JM.
func (c *FlowgentController) runtimeNamespace(spec *entities.FlowInfo) string {
	if spec.K8sNamespace != "" {
		return spec.K8sNamespace
	}
	return resourceid.KubernetesName(
		strings.TrimSuffix(defaultNamespacePrefix(c.cfg.Runtime.Namespace.NamespacePrefix), "-"),
		c.dispatchNamespace(spec),
	)
}

// dispatchNamespace resolves the namespace ID to use for a flow's dispatch
// (namespace + JM/run Namespace), falling back from spec.Namespace to c.namespace
// (the Controller's configured default namespace — see
// pkg/cmd/pkg/controller/controller.go, always non-empty) to, as a last
// resort, "default" — mirroring handler.defaultNamespace's fallback so the
// two components never disagree even in a misconfigured edge case.
func (c *FlowgentController) dispatchNamespace(spec *entities.FlowInfo) string {
	if spec.Namespace != "" {
		return spec.Namespace
	}
	if c.namespace != "" {
		return c.namespace
	}
	return "default"
}

// defaultNamespacePrefix mirrors pkg/api/pkg/handler.defaultNamespacePrefix —
// see runtimeNamespace doc comment for why the two must never diverge.
func defaultNamespacePrefix(prefix string) string {
	if prefix == "" {
		return "flowgent-"
	}
	return prefix
}

func (c *FlowgentController) buildJMDeployment(name, namespace, namespaceID string, spec *entities.FlowInfo, runtimeConfigMap, runtimeSecret, runtimeChecksum string) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app":                          "flowgent-jobmanager",
		"app.kubernetes.io/component":  "jobmanager",
		"flowgent.io/namespace":        namespaceID,
		"flowgent.io/flow":             spec.ID,
		"flowgent.io/runtime-boundary": "flow-jobmanager",
		"flowgent.io/resource-pool":    defaultResourcePoolID(spec.ResourcePoolID),
	}
	image := c.cfg.Runtime.JMImage
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: map[string]string{"flowgent.io/runtime-config-checksum": runtimeChecksum}},
				Spec: corev1.PodSpec{
					ServiceAccountName: "flowgent-runtime",
					Containers: []corev1.Container{{
						Name:            "jobmanager",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Image:           image,
						Args:            []string{"jobmanager", "start", "-c", "/etc/flowgent/flowgent.yaml", "--flow-id", spec.ID},
						EnvFrom:         runtimeEnvFrom(c.cfg.Runtime.CredentialEnvSecret, runtimeConfigMap, runtimeSecret),
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT__RUNTIME__AGENT_FLOW_ID", Value: spec.ID},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE__DEFAULT_NAMESPACE", Value: namespaceID},
							{Name: "FLOWGENT__RUNTIME__SYSTEM_NAMESPACE", Value: c.systemNamespace()},
							{Name: "FLOWGENT__RUNTIME__K8S_NAMESPACE", Value: namespace},
							{Name: "FLOWGENT__RUNTIME__RESOURCE_POOL_ID", Value: defaultResourcePoolID(spec.ResourcePoolID)},
							{Name: "FLOWGENT__RUNTIME__TM_DEPLOY", Value: tmDeploymentName(namespaceID, defaultResourcePoolID(spec.ResourcePoolID))},
							{Name: "FLOWGENT__MESSAGER__MQTT__BROKER", Value: c.cfg.Messager.MQTT.Broker},
							{Name: "FLOWGENT__RUNTIME__API_SERVER_URL", Value: c.cfg.Runtime.APIServerURL},
							c.jobManagerTokenEnv(),
							{
								Name: "POD_NAMESPACE",
								ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								}},
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/flowgent"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: c.jmConfigMapName()},
							},
						}},
					},
				},
			},
		},
	}
}

func (c *FlowgentController) jobManagerTokenEnv() corev1.EnvVar {
	env := corev1.EnvVar{Name: "FLOWGENT_INTERNAL_TOKEN"}
	if !c.cfg.Auth.Authorization.Enabled || strings.EqualFold(c.cfg.Auth.Authorization.Enforcement, "disabled") || c.cfg.Runtime.InternalAuthSecret == "" {
		return env
	}
	env.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: c.cfg.Runtime.InternalAuthSecret},
		Key:                  c.jobManagerAuthKey(),
	}}
	return env
}

func (c *FlowgentController) jobManagerAuthKey() string {
	if c.cfg.Runtime.JobManagerAuthKey != "" {
		return c.cfg.Runtime.JobManagerAuthKey
	}
	return "jobmanager-token"
}

func (c *FlowgentController) taskManagerAuthKey() string {
	if c.cfg.Runtime.TaskManagerAuthKey != "" {
		return c.cfg.Runtime.TaskManagerAuthKey
	}
	return "taskmanager-token"
}

func runtimeEnvFrom(credentialSecret, flowConfigMap, flowSecret string) []corev1.EnvFromSource {
	optional := true
	sources := make([]corev1.EnvFromSource, 0, 3)
	if credentialSecret != "" {
		sources = append(sources, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: credentialSecret},
			Optional:             &optional,
		}})
	}
	if flowConfigMap != "" {
		sources = append(sources, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: flowConfigMap}, Optional: &optional,
		}})
	}
	if flowSecret != "" {
		sources = append(sources, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: flowSecret}, Optional: &optional,
		}})
	}
	return sources
}

func tmDeploymentName(namespaceID, poolID string) string {
	if namespaceID == "" {
		namespaceID = "default"
	}
	return resourceid.KubernetesName("flowgent-taskmanager", namespaceID, poolID)
}

func sandboxDeploymentName(namespaceID, flowID string) string {
	if namespaceID == "" {
		namespaceID = "default"
	}
	return resourceid.KubernetesName("flowgent-sandbox", namespaceID, flowID)
}

func defaultResourcePoolID(poolID string) string {
	if poolID == "" {
		return "default"
	}
	return poolID
}

// jmConfigMapName returns the name of the ConfigMap holding flowgent.yaml,
// which dedicated per-flow JM pods mount at /etc/flowgent. Helm sets this via
// FLOWGENT__RUNTIME__JM_CONFIG_MAP (see deploy/helm/flowgent/templates/controller.yaml);
// "flowgent-config" is the fallback for the default release name.
func (c *FlowgentController) jmConfigMapName() string {
	if c.cfg.Runtime.JMConfigMap != "" {
		return c.cfg.Runtime.JMConfigMap
	}
	return "flowgent-config"
}

func (c *FlowgentController) systemNamespace() string {
	if c.cfg.Runtime.SystemNamespace != "" {
		return c.cfg.Runtime.SystemNamespace
	}
	if podNS := os.Getenv("POD_NAMESPACE"); podNS != "" {
		return podNS
	}
	if c.cfg.Runtime.K8sNamespace != "" {
		return c.cfg.Runtime.K8sNamespace
	}
	return "flowgen-system"
}

// gcOrphanedJMDeployments deletes dedicated JM Deployments (labeled
// flowgent.io/managed-by=jobmanager) whose flow definition no longer exists or whose
// flow has no active runs. peers is the same reconcile-tick peer snapshot used
// for dispatch ownership above, so GC and dispatch agree on which pod owns
// which flow within a tick.
func (c *FlowgentController) gcOrphanedJMDeployments(ctx context.Context, seen map[string]*entities.FlowInfo, activeRunFlows map[string]bool, activeRunSnapshotOK bool, peers []discovery.Peer) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return // not running in K8s — nothing to garbage-collect
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Error("gc: failed to create K8s client", "error", err)
		return
	}

	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"app": "flowgent-jobmanager",
		}).String(),
	})
	if err != nil {
		c.logger.Error("gc: failed to list dedicated JM deployments", "error", err)
		return
	}

	for _, d := range deployments.Items {
		flowID := d.Labels["flowgent.io/flow"]
		if flowID == "" {
			continue
		}
		if !c.ownsFlow(peers, flowID) {
			continue
		}
		reason := ""
		if _, stillExists := seen[flowID]; !stillExists {
			reason = "flow deleted"
		} else if activeRunSnapshotOK && !activeRunFlows[flowID] {
			reason = "no active runs"
		}
		if reason == "" {
			c.clearRuntimeCleanup(d.Namespace, d.Name)
			continue
		}
		if reason == "no active runs" {
			firstSeen, age := c.markRuntimeCleanup(d.Namespace, d.Name)
			timeout := c.effectiveTMOrphanTimeout()
			if age < timeout {
				c.logger.Debug("gc: no-active JM deployment is inside observation window",
					"flow_id", flowID, "deployment", d.Name, "namespace", d.Namespace,
					"first_seen", firstSeen, "age", age.Round(time.Second), "timeout", timeout)
				continue
			}
			reason = "no active runs beyond observation window"
		}
		if err := clientset.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, metav1.DeleteOptions{}); err != nil {
			c.logger.Error("gc: failed to delete orphaned JM deployment",
				"flow_id", flowID, "deployment", d.Name, "namespace", d.Namespace, "error", err)
			continue
		}
		c.clearRuntimeCleanup(d.Namespace, d.Name)
		c.deleteRuntimeDeploymentsForFlow(ctx, clientset, d.Labels[resourcemanager.LabelNamespaceID], flowID, d.Name, d.Namespace, reason)
		c.logger.Info("gc: deleted orphaned JM deployment",
			"flow_id", flowID, "deployment", d.Name, "namespace", d.Namespace, "reason", reason)
	}
}

// gcOrphanedRuntimeDeployments removes JM-owned runtime Deployments whose owning JM disappeared.
// If the flow itself was deleted or has no active run, cleanup is immediate.
// If an active flow run still exists, deletion waits for tmOrphanTimeout so a
// crashed/restarted JM has time to be re-created and resume ownership.
func (c *FlowgentController) gcOrphanedRuntimeDeployments(ctx context.Context, seen map[string]*entities.FlowInfo, activeRunFlows map[string]bool, activeRunSnapshotOK bool, peers []discovery.Peer) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Error("runtime gc: failed to create K8s client", "error", err)
		return
	}

	selector := labels.SelectorFromSet(labels.Set{
		resourcemanager.LabelManagedBy: "jobmanager", // legacy per-Flow workers only
	}).String()
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		c.logger.Error("runtime gc: failed to list JM-owned deployments", "error", err)
		return
	}

	for _, d := range deployments.Items {
		flowID := d.Labels[resourcemanager.LabelFlowID]
		if flowID == "" || !c.ownsFlow(peers, flowID) {
			continue
		}
		parentName := d.Labels[resourcemanager.LabelParentJobManager]
		parentNamespace := d.Labels[resourcemanager.LabelParentJobManagerNamespace]
		if parentName != "" && parentNamespace != "" {
			if _, err := clientset.AppsV1().Deployments(parentNamespace).Get(ctx, parentName, metav1.GetOptions{}); err == nil {
				c.clearRuntimeCleanup(d.Namespace, d.Name)
				continue
			} else if !apierrors.IsNotFound(err) {
				c.logger.Warn("runtime gc: failed to check parent JM deployment",
					"deployment", d.Name, "namespace", d.Namespace,
					"parent_jm", parentName, "parent_namespace", parentNamespace, "error", err)
				continue
			}
		}

		if _, flowStillExists := seen[flowID]; !flowStillExists {
			c.deleteDeployment(ctx, clientset, &d, "flow deleted")
			continue
		}
		if activeRunSnapshotOK && !activeRunFlows[flowID] {
			c.deleteDeployment(ctx, clientset, &d, "no active runs")
			continue
		}

		firstSeen, age := c.markRuntimeCleanup(d.Namespace, d.Name)
		timeout := c.effectiveTMOrphanTimeout()
		if age < timeout {
			c.logger.Debug("runtime gc: parent JM missing, observing before cleanup",
				"deployment", d.Name, "namespace", d.Namespace, "flow_id", flowID,
				"first_seen", firstSeen, "age", age.Round(time.Second), "timeout", timeout)
			continue
		}
		c.deleteDeployment(ctx, clientset, &d, "parent JM missing beyond timeout")
	}
}

// gcOrphanedResourcePoolDeployments removes shared workers only after an
// authoritative API snapshot confirms that their namespace-scoped pool was
// deleted. ResourcePool deletion itself is blocked while a Flow or active run
// still references the pool.
func (c *FlowgentController) gcOrphanedResourcePoolDeployments(ctx context.Context, pools map[string]struct{}, snapshotOK bool, peers []discovery.Peer) {
	if !snapshotOK {
		return
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Error("resource-pool gc: failed to create K8s client", "error", err)
		return
	}
	selector := labels.SelectorFromSet(labels.Set{
		resourcemanager.LabelManagedBy:   resourcemanager.LabelValueResourcePool,
		resourcemanager.LabelNamespaceID: c.namespace,
	}).String()
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		c.logger.Error("resource-pool gc: failed to list deployments", "error", err)
		return
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		poolID := deployment.Labels[resourcemanager.LabelResourcePoolID]
		if poolID == "" || !c.ownsFlow(peers, "resource-pool/"+poolID) {
			continue
		}
		if _, exists := pools[poolID]; exists {
			continue
		}
		if err := clientset.AppsV1().Deployments(deployment.Namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			c.logger.Error("resource-pool gc: failed to delete deployment", "resource_pool", poolID, "deployment", deployment.Name, "namespace", deployment.Namespace, "error", err)
			continue
		}
		c.logger.Info("resource-pool gc: deleted deployment", "resource_pool", poolID, "deployment", deployment.Name, "namespace", deployment.Namespace)
	}
}

func (c *FlowgentController) deleteRuntimeDeploymentsForFlow(ctx context.Context, clientset kubernetes.Interface, namespaceID, flowID, parentName, parentNamespace, reason string) {
	selector := labels.SelectorFromSet(labels.Set{
		resourcemanager.LabelManagedBy: "jobmanager", // legacy per-Flow workers only
		resourcemanager.LabelFlowID:    flowID,
	}).String()
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		c.logger.Error("runtime gc: failed to list runtime deployments for flow", "flow_id", flowID, "error", err)
		return
	}
	for _, d := range deployments.Items {
		if namespaceID != "" && d.Labels[resourcemanager.LabelNamespaceID] != namespaceID {
			continue
		}
		if parentName != "" && d.Labels[resourcemanager.LabelParentJobManager] != parentName {
			continue
		}
		if parentNamespace != "" && d.Labels[resourcemanager.LabelParentJobManagerNamespace] != parentNamespace {
			continue
		}
		c.deleteDeployment(ctx, clientset, &d, reason)
	}
}

// gcOrphanedRuntimeConfiguration removes per-Flow materialized configuration
// after its Flow definition is deleted. Resources remain while an idle Flow
// exists so the next run can reuse them, but deleted Flows must not leave
// plaintext runtime Secret material in the workload namespace.
func (c *FlowgentController) gcOrphanedRuntimeConfiguration(ctx context.Context, seen map[string]*entities.FlowInfo, peers []discovery.Peer) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Error("runtime config gc: failed to create K8s client", "error", err)
		return
	}
	c.deleteOrphanedRuntimeConfiguration(ctx, clientset, seen, peers)
}

func (c *FlowgentController) deleteOrphanedRuntimeConfiguration(ctx context.Context, clientset kubernetes.Interface, seen map[string]*entities.FlowInfo, peers []discovery.Peer) {
	selector := labels.SelectorFromSet(labels.Set{
		"flowgent.io/runtime-boundary": "flow-config",
		"flowgent.io/managed-by":       runtimeConfigurationManagedBy,
	}).String()

	configMaps, err := clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		c.logger.Error("runtime config gc: failed to list ConfigMaps", "error", err)
	} else {
		for i := range configMaps.Items {
			item := &configMaps.Items[i]
			if !c.runtimeConfigurationIsOrphan(item.Labels, seen, peers) {
				continue
			}
			if err := clientset.CoreV1().ConfigMaps(item.Namespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				c.logger.Error("runtime config gc: failed to delete ConfigMap", "namespace", item.Namespace, "configmap", item.Name, "error", err)
				continue
			}
			c.logger.Info("runtime config gc: deleted orphaned ConfigMap", "namespace", item.Namespace, "configmap", item.Name)
		}
	}

	secrets, err := clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		c.logger.Error("runtime config gc: failed to list Secrets", "error", err)
		return
	}
	for i := range secrets.Items {
		item := &secrets.Items[i]
		if !c.runtimeConfigurationIsOrphan(item.Labels, seen, peers) {
			continue
		}
		if err := clientset.CoreV1().Secrets(item.Namespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			c.logger.Error("runtime config gc: failed to delete Secret", "namespace", item.Namespace, "secret", item.Name, "error", err)
			continue
		}
		c.logger.Info("runtime config gc: deleted orphaned Secret", "namespace", item.Namespace, "secret", item.Name)
	}
}

func (c *FlowgentController) runtimeConfigurationIsOrphan(resourceLabels map[string]string, seen map[string]*entities.FlowInfo, peers []discovery.Peer) bool {
	flowID := resourceLabels["flowgent.io/flow"]
	if flowID == "" || resourceLabels["flowgent.io/namespace"] != c.namespace {
		return false
	}
	if _, exists := seen[flowID]; exists {
		return false
	}
	return c.ownsFlow(peers, flowID)
}

func (c *FlowgentController) deleteDeployment(ctx context.Context, clientset kubernetes.Interface, d *appsv1.Deployment, reason string) {
	if err := clientset.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			c.clearRuntimeCleanup(d.Namespace, d.Name)
			return
		}
		c.logger.Error("runtime gc: failed to delete deployment",
			"deployment", d.Name, "namespace", d.Namespace, "reason", reason, "error", err)
		return
	}
	c.clearRuntimeCleanup(d.Namespace, d.Name)
	c.logger.Info("runtime gc: deleted deployment",
		"deployment", d.Name, "namespace", d.Namespace, "reason", reason)
}

func (c *FlowgentController) markRuntimeCleanup(namespace, name string) (time.Time, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runtimeCleanupFirstSeen == nil {
		c.runtimeCleanupFirstSeen = make(map[string]time.Time)
	}
	key := namespace + "/" + name
	firstSeen, ok := c.runtimeCleanupFirstSeen[key]
	if !ok {
		firstSeen = time.Now()
		c.runtimeCleanupFirstSeen[key] = firstSeen
	}
	return firstSeen, time.Since(firstSeen)
}

func (c *FlowgentController) clearRuntimeCleanup(namespace, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.runtimeCleanupFirstSeen, namespace+"/"+name)
}

func (c *FlowgentController) effectiveTMOrphanTimeout() time.Duration {
	if c.tmOrphanTimeout > 0 {
		return c.tmOrphanTimeout
	}
	return 3 * time.Minute
}

func parseTMOrphanTimeout(raw string) time.Duration {
	if raw == "" {
		return 3 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return d
}

func (c *FlowgentController) stopAllFlows() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for flowID, cancel := range c.running {
		c.logger.Info("Stopping flow dispatch", "flow_id", flowID)
		cancel()
	}
	c.running = make(map[string]context.CancelFunc)
}

func isTerminalStatus(s entities.RunStatus) bool {
	return s == entities.RunCompleted || s == entities.RunFailed || s == entities.RunCancelled
}
