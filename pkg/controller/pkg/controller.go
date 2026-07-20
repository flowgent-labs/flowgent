// Package controller provides the distributed sharded flow driver.
// It discovers flows via apiserver REST API, shards across controller pods via
// hash-mod partitioning, and dispatches executions in application mode
// (dedicated per-flow JM Deployment).
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// FlowgentController is the distributed flow driver.
type FlowgentController struct {
	api     *client.FlowgentClient
	tenant  string
	rm      resourcemanager.ResourceManager
	logger  *utils.Logger
	cfg     *config.FlowgentConfig
	cfgPath string

	discovery    discovery.IDiscoveryClient
	pollInterval time.Duration
	cronTrigger  *trigger.ScheduleTrigger

	mu                sync.Mutex
	running           map[string]context.CancelFunc
	dispatchedVersion map[string]int64
}

// NewFlowgentController creates a FlowgentController instance.
func NewFlowgentController(api *client.FlowgentClient, tenant string, rm resourcemanager.ResourceManager,
	logger *utils.Logger, cfg *config.FlowgentConfig, cfgPath string,
	disc discovery.IDiscoveryClient) *FlowgentController {
	return &FlowgentController{
		api:               api,
		tenant:            tenant,
		rm:                rm,
		logger:            logger,
		cfg:               cfg,
		cfgPath:           cfgPath,
		discovery:         disc,
		pollInterval:      10 * time.Second,
		cronTrigger:       trigger.NewScheduleTrigger(),
		running:           make(map[string]context.CancelFunc),
		dispatchedVersion: make(map[string]int64),
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

	versions, err := c.api.ListFlows(ctx, c.tenant)
	if err != nil {
		c.logger.Error("Failed to list agentflow definitions via apiserver", "error", err)
		return
	}

	seen := make(map[string]*entities.FlowInfo)
	versionOf := make(map[string]int64)
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
		seen[v.FlowID] = &spec
		versionOf[v.FlowID] = v.Version
	}

	var ownedFlows []entities.FlowInfo
	for flowID, spec := range seen {
		if !c.ownsFlow(peers, flowID) {
			continue
		}
		ownedFlows = append(ownedFlows, *spec)

		// Every flow runs in Application mode (dedicated per-flow JM
		// Deployment) — Session mode is currently disabled, see
		// entities.Priority doc comment. Idempotent
		// (apierrors.IsAlreadyExists is ignored) — safe to call on every
		// tick, independent of run dispatch below.
		c.ensureApplicationInfra(ctx, spec)

		// Only actually dispatch (create a new run) when the flow's
		// definition is new or has changed — the "on-new-definition" trigger
		// condition (docs/01-L1-Engine-Architecture.md §4.2). Without this
		// guard the Controller would create a brand-new FlowRun for every
		// known flow on every pollInterval tick, forever. Explicit runs
		// otherwise come from Path A (POST /flows/trigger) or the cron
		// triggers registered below.
		if !c.shouldDispatch(flowID, versionOf[flowID]) {
			continue
		}

		c.logger.Info("Controller dispatching flow (new/updated definition)",
			"flow_id", flowID, "priority", spec.Priority, "version", versionOf[flowID])

		go c.dispatchFlow(ctx, spec)
	}

	// Re-register cron/schedule triggers (docs §4.2 "cron / interval") for
	// this pod's shard only, so each scheduled run fires exactly once across
	// all Controller replicas, and edits/removals of triggers take effect
	// without restarting the Controller.
	c.cronTrigger.Clear()
	c.cronTrigger.RegisterAgentFlows(ownedFlows, c.triggerScheduledRun)
	c.cronTrigger.Start()

	// Garbage-collect dedicated JM Deployments for flows that were deleted.
	c.gcOrphanedJMDeployments(ctx, seen, peers)
}

// shouldDispatch implements the "on-new-definition" trigger condition: a
// flow is only auto-dispatched once per definition version, not on every
// reconcile tick.
func (c *FlowgentController) shouldDispatch(flowID string, version int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.dispatchedVersion[flowID]; ok && last == version {
		return false
	}
	c.dispatchedVersion[flowID] = version
	return true
}

// triggerScheduledRun is the ScheduleTrigger callback for a cron-triggered
// agentflow. It re-fetches the latest spec (cron fires asynchronously,
// possibly minutes after the last reconcile) and creates a run for it.
func (c *FlowgentController) triggerScheduledRun(ctx context.Context, flowID string) {
	spec, err := c.api.GetFlow(ctx, c.tenant, flowID)
	if err != nil || spec == nil {
		c.logger.Warn("cron trigger: failed to load flow spec", "flow_id", flowID, "error", err)
		return
	}
	c.createApplicationRun(ctx, spec)
}

func (c *FlowgentController) dispatchFlow(ctx context.Context, spec *entities.FlowInfo) {
	flowCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.running[spec.ID] = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.running, spec.ID)
		c.mu.Unlock()
		cancel()
	}()

	// Every flow gets a dedicated per-flow JM Deployment (Application mode).
	// Session mode (a shared, Helm-deployed JM/TM pool picking up
	// namespace="" runs) is currently disabled to simplify troubleshooting —
	// see entities.Priority doc comment.
	c.createApplicationRun(flowCtx, spec)
}

// ensureApplicationInfra makes sure the dedicated per-flow JM Deployment
// exists for this flow, in its tenant's shared namespace. Idempotent — safe
// to call on every reconcile tick, independent of whether a run is
// dispatched.
func (c *FlowgentController) ensureApplicationInfra(ctx context.Context, spec *entities.FlowInfo) {
	ns := c.applicationNamespace(spec)
	tenantID := c.dispatchTenant(spec)

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

	// The per-tenant namespace (e.g. "flowgent-{tenantID}") is not
	// pre-created by Helm — it is created lazily here, on first dispatch of
	// any flow belonging to this tenant. Deployments(ns).Create would
	// otherwise fail with a 404 "namespaces \"...\" not found" the very
	// first time a new tenant is seen. Safe to call repeatedly: every flow
	// of the same tenant shares (and may re-touch) this same namespace.
	if _, err := clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			c.logger.Error("Failed to check application namespace", "flow_id", spec.ID, "namespace", ns, "error", err)
			return
		}
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: map[string]string{"flowgent.io/mode": "application", "flowgent.io/tenant": tenantID},
		}}
		if _, err := clientset.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			c.logger.Error("Failed to create application namespace", "flow_id", spec.ID, "namespace", ns, "error", err)
			return
		}
		c.logger.Info("Application namespace created", "flow_id", spec.ID, "tenant_id", tenantID, "namespace", ns)

		// Copy the shared ConfigMap (flowgent-config) from the controller's
		// own namespace into the new tenant namespace. Without this, every
		// dedicated JM pod in the tenant namespace would fail with
		// "MountVolume.SetUp failed for volume \"config\": configmap not found".
		cmName := c.jmConfigMapName()
		if srcCM, srcErr := clientset.CoreV1().ConfigMaps(c.cfg.Runtime.Namespace).Get(ctx, cmName, metav1.GetOptions{}); srcErr == nil {
			dstCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:   cmName,
					Labels: map[string]string{"flowgent.io/mode": "application", "flowgent.io/tenant": tenantID},
				},
				Data: srcCM.Data,
			}
			if _, err := clientset.CoreV1().ConfigMaps(ns).Create(ctx, dstCM, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
				c.logger.Warn("Failed to copy ConfigMap to tenant namespace", "flow_id", spec.ID, "namespace", ns, "configmap", cmName, "error", err)
			} else {
				c.logger.Info("ConfigMap copied to tenant namespace", "flow_id", spec.ID, "namespace", ns, "configmap", cmName)
			}
		}
	}

	jmName := fmt.Sprintf("flowgent-jobmanager-%s-%s", tenantID, spec.ID)
	jmDeployment := c.buildJMDeployment(jmName, ns, tenantID, spec)

	_, err = clientset.AppsV1().Deployments(ns).Create(ctx, jmDeployment, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		c.logger.Error("Failed to create dedicated JM deployment", "flow_id", spec.ID, "error", err)
		return
	}
	if err == nil {
		c.logger.Info("Dedicated JM deployment created",
			"flow_id", spec.ID, "namespace", ns, "deployment", jmName)
	}
}

// createApplicationRun creates a PENDING run scoped to the flow's tenant
// namespace, so only its dedicated JM (which polls that namespace) picks it
// up.
func (c *FlowgentController) createApplicationRun(ctx context.Context, spec *entities.FlowInfo) {
	ns := c.applicationNamespace(spec)
	tenantID := c.dispatchTenant(spec)
	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{ID: fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()), TenantID: tenantID},
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      entities.RunPending,
		Priority:    entities.PriorityHigh,
		Namespace:   ns,
		Vars:        spec.Vars,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "controller"})
	if _, err := c.api.CreateRun(ctx, tenantID, run); err != nil {
		c.logger.Error("Failed to create application run via apiserver", "flow_id", spec.ID, "error", err)
	}
}

// applicationNamespace computes the K8s namespace for a flow's dedicated JM
// Deployment. Per §1.3/§4.3 of docs/01-L1-Engine-Architecture.md, tenant
// isolation is per-TENANT namespace (not per-flow): every flow belonging to
// the same tenant shares one namespace, and each flow's dedicated JM
// Deployment is disambiguated by name alone
// (flowgent-jobmanager-{tenantId}-{flowId} — see ensureApplicationInfra).
// tenant.namespace_prefix already includes its own trailing separator
// (default "flowgent-" — see etc/flowgent.yaml), so it is concatenated
// directly with the tenant ID, not joined with another "-" (which would
// produce a malformed "flowgent--{tenantID}" namespace).
// pkg/api/pkg/handler/flow_def.go's applicationNamespace must compute the
// exact same value so Trigger (Path A) and the Controller (Path B) agree on
// which namespace a given flow's dedicated JM lives in — in particular both
// sides must fall back to the same "flowgent-" namespace prefix (via
// defaultNamespacePrefix, mirroring handler.defaultNamespacePrefix) and the
// same default tenant ID (cfg.Tenant.DefaultTenant) when a flow spec
// doesn't carry its own TenantID. Without this shared fallback the two
// components would silently disagree on the namespace and Application-mode
// runs would never be picked up by their dedicated JM.
func (c *FlowgentController) applicationNamespace(spec *entities.FlowInfo) string {
	if spec.Namespace != "" {
		return spec.Namespace
	}
	return defaultNamespacePrefix(c.cfg.Tenant.NamespacePrefix) + c.dispatchTenant(spec)
}

// dispatchTenant resolves the tenant ID to use for a flow's dispatch
// (namespace + JM/run TenantID), falling back from spec.TenantID to c.tenant
// (the Controller's configured default tenant — see
// pkg/cmd/pkg/controller/controller.go, always non-empty) to, as a last
// resort, "default" — mirroring handler.defaultTenantID's fallback so the
// two components never disagree even in a misconfigured edge case.
func (c *FlowgentController) dispatchTenant(spec *entities.FlowInfo) string {
	if spec.TenantID != "" {
		return spec.TenantID
	}
	if c.tenant != "" {
		return c.tenant
	}
	return "default"
}

// defaultNamespacePrefix mirrors pkg/api/pkg/handler.defaultNamespacePrefix —
// see applicationNamespace doc comment for why the two must never diverge.
func defaultNamespacePrefix(prefix string) string {
	if prefix == "" {
		return "flowgent-"
	}
	return prefix
}

func (c *FlowgentController) buildJMDeployment(name, namespace, tenantID string, spec *entities.FlowInfo) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app":                "flowgent-jobmanager",
		"flowgent.io/tenant": tenantID,
		"flowgent.io/flow":   spec.ID,
		"flowgent.io/mode":   "application",
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
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "jobmanager",
						Image: image,
						Args:  []string{"jobmanager", "start", "-c", "/etc/flowgent/flowgent.yaml", "--flow-id", spec.ID},
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT__RUNTIME__AGENT_FLOW_ID", Value: spec.ID},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE", Value: namespace},
							{Name: "FLOWGENT__MESSAGER__MQTT__BROKER", Value: c.cfg.Messager.MQTT.Broker},
							{Name: "FLOWGENT__RUNTIME__API_SERVER_URL", Value: c.cfg.Runtime.APIServerURL},
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

// gcOrphanedJMDeployments deletes dedicated JM Deployments (labeled
// flowgent.io/mode=application) whose flow no longer exists. This is the
// Controller-side half of "Flow completes → Controller cleans up Deployment"
// (docs/01-L1-Engine-Architecture.md §9.3). peers is the same reconcile-tick
// peer snapshot used for dispatch ownership above, so GC and dispatch agree
// on which pod owns which flow within a tick.
func (c *FlowgentController) gcOrphanedJMDeployments(ctx context.Context, seen map[string]*entities.FlowInfo, peers []discovery.Peer) {
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
		LabelSelector: "flowgent.io/mode=application",
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
		if _, stillExists := seen[flowID]; stillExists {
			continue // flow still active — still requires its dedicated JM
		}
		if err := clientset.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, metav1.DeleteOptions{}); err != nil {
			c.logger.Error("gc: failed to delete orphaned JM deployment",
				"flow_id", flowID, "deployment", d.Name, "namespace", d.Namespace, "error", err)
			continue
		}
		c.logger.Info("gc: deleted orphaned JM deployment",
			"flow_id", flowID, "deployment", d.Name, "namespace", d.Namespace, "reason", "flow deleted")
	}
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
