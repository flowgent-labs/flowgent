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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// FlowgentController is the distributed flow driver.
type FlowgentController struct {
	api    *client.FlowgentClient
	tenant string
	rm     resourcemanager.ResourceManager
	logger *utils.Logger
	cfg    *config.FlowgentConfig
	cfgPath string

	discovery    discovery.IDiscoveryClient
	pollInterval time.Duration

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

// NewFlowgentController creates a FlowgentController instance.
func NewFlowgentController(api *client.FlowgentClient, tenant string, rm resourcemanager.ResourceManager,
	logger *utils.Logger, cfg *config.FlowgentConfig, cfgPath string,
	disc discovery.IDiscoveryClient) *FlowgentController {
	return &FlowgentController{
		api:          api,
		tenant:       tenant,
		rm:           rm,
		logger:       logger,
		cfg:          cfg,
		cfgPath:      cfgPath,
		discovery:    disc,
		pollInterval: 10 * time.Second,
		running:      make(map[string]context.CancelFunc),
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

func (c *FlowgentController) ownsFlow(ctx context.Context, flowID string) bool {
	peers, _, err := c.getPeers(ctx)
	if err != nil || len(peers) == 0 {
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
			return nil
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

func (c *FlowgentController) reconcile(ctx context.Context) {
	versions, err := c.api.ListFlows(ctx, c.tenant)
	if err != nil {
		c.logger.Error("Failed to list agentflow definitions via apiserver", "error", err)
		return
	}

	seen := make(map[string]*entities.AgentFlowInfo)
	for _, v := range versions {
		if _, exists := seen[v.AgentFlowID]; exists {
			continue
		}
		var spec entities.AgentFlowInfo
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			c.logger.Warn("Skipping invalid agentflow definition", "agentflow_id", v.AgentFlowID, "error", err)
			continue
		}
		if spec.ID == "" {
			continue
		}
		seen[v.AgentFlowID] = &spec
	}

	for flowID, spec := range seen {
		if !c.ownsFlow(ctx, flowID) {
			continue
		}

		c.mu.Lock()
		_, alreadyRunning := c.running[flowID]
		c.mu.Unlock()
		if alreadyRunning {
			continue
		}

		c.logger.Info("Controller dispatching flow",
			"flow_id", flowID,
			"priority", spec.Priority)

		go c.dispatchFlow(ctx, spec)
	}
}

func (c *FlowgentController) dispatchFlow(ctx context.Context, spec *entities.AgentFlowInfo) {
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

	c.dispatchApplicationMode(flowCtx, spec)
}

func (c *FlowgentController) dispatchApplicationMode(ctx context.Context, spec *entities.AgentFlowInfo) {
	c.logger.Info("Application mode dispatch", "flow_id", spec.ID, "namespace", spec.Namespace)

	ns := spec.Namespace
	if ns == "" {
		ns = fmt.Sprintf("%s-%s", c.cfg.Tenant.NamespacePrefix, spec.ID)
	}

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

	tenantID := spec.TenantID
	if tenantID == "" {
		tenantID = c.tenant
	}
	jmName := fmt.Sprintf("flowgent-jobmanager-%s-%s", tenantID, spec.ID)
	jmDeployment := c.buildJMDeployment(jmName, ns, tenantID, spec)

	_, err = clientset.AppsV1().Deployments(ns).Create(ctx, jmDeployment, metav1.CreateOptions{})
	if err != nil {
		c.logger.Error("Failed to create dedicated JM deployment", "flow_id", spec.ID, "error", err)
		return
	}

	c.logger.Info("Dedicated JM deployment created",
		"flow_id", spec.ID, "namespace", ns, "deployment", jmName)

	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{ID: fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()), TenantID: tenantID},
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      entities.RunPending,
		Priority:    entities.PriorityGrade,
		Namespace:   ns,
		Vars:        spec.Vars,
	}
	run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "controller"})
	if _, err := c.api.CreateRun(ctx, tenantID, run); err != nil {
		c.logger.Error("Failed to create application run via apiserver", "flow_id", spec.ID, "error", err)
	}
}

func (c *FlowgentController) buildJMDeployment(name, namespace, tenantID string, spec *entities.AgentFlowInfo) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app":                "flowgent-jobmanager",
		"flowgent.io/tenant": tenantID,
		"flowgent.io/flow":   spec.ID,
		"flowgent.io/mode":   "application",
	}
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
						Image: c.cfg.Runtime.JMImage,
						Args:  []string{"jobmanager", "start", "-c", "/etc/flowgent/flowgent.yaml", "--flow-id", spec.ID},
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT__RUNTIME__AGENT_FLOW_ID", Value: spec.ID},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE", Value: namespace},
						},
					}},
				},
			},
		},
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
