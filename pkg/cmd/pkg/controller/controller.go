// Package controller provides the distributed sharded flow driver daemon.
// It discovers flows via apiserver REST API, shards across controller pods via
// hash-mod partitioning, and dispatches executions in either session mode
// (shared JM) or application mode (dedicated JM+TM).
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── CLI entry points ──────────────────────────────────────────

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	logSuffix := ""
	if pidFile != "" {
		logSuffix = fmt.Sprintf(", pidfile=%s", pidFile)
	}
	slog.Info(fmt.Sprintf("Flowgent Controller starting (pid=%d%s)", os.Getpid(), logSuffix))
	return startController(cfgPath)
}

func Stop(pidFile string) error { return utils.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startController(cfgPath)
}

// ─── Controller ────────────────────────────────────────────────

// Controller is the distributed flow driver.
type Controller struct {
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

// NewController creates a Controller instance.
func NewController(api *client.FlowgentClient, tenant string, rm resourcemanager.ResourceManager,
	logger *utils.Logger, cfg *config.FlowgentConfig, cfgPath string,
	disc discovery.IDiscoveryClient) *Controller {
	return &Controller{
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

func (c *Controller) getPeers(ctx context.Context) (peers []discovery.Peer, selfIndex int, err error) {
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

func (c *Controller) ownsFlow(ctx context.Context, flowID string) bool {
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

func (c *Controller) Run(ctx context.Context) error {
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

func (c *Controller) reconcile(ctx context.Context) {
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
			"priority", spec.Priority,
			"mode", spec.EffectiveMode())

		go c.dispatchFlow(ctx, spec)
	}
}

func (c *Controller) dispatchFlow(ctx context.Context, spec *entities.AgentFlowInfo) {
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

	mode := spec.EffectiveMode()
	switch mode {
	case entities.ModeApplication:
		c.dispatchApplicationMode(flowCtx, spec)
	default:
		c.dispatchSessionMode(flowCtx, spec)
	}
}

func (c *Controller) dispatchSessionMode(ctx context.Context, spec *entities.AgentFlowInfo) {
	c.logger.Info("Session mode dispatch", "flow_id", spec.ID)

	tenant := spec.TenantID
	if tenant == "" {
		tenant = c.tenant
	}

	run := &entities.FlowRunInfo{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      entities.RunPending,
		Priority:    spec.Priority,
		TenantID:    tenant,
		Namespace:   spec.Namespace,
		Vars:        spec.Vars,
		Trigger:     entities.TriggerInfo{Type: "schedule", Source: "controller"},
	}

	created, err := c.api.CreateRun(ctx, tenant, run)
	if err != nil {
		c.logger.Error("Failed to create session run via apiserver", "flow_id", spec.ID, "error", err)
		return
	}

	c.logger.Info("Session run created", "flow_id", spec.ID, "run_id", created.ID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r, err := c.api.GetRun(ctx, tenant, created.ID)
			if err != nil || r == nil {
				continue
			}
			if isTerminalStatus(r.Status) {
				c.logger.Info("Session run completed", "flow_id", spec.ID, "run_id", created.ID, "status", r.Status)
				return
			}
		}
	}
}

func (c *Controller) dispatchApplicationMode(ctx context.Context, spec *entities.AgentFlowInfo) {
	c.logger.Info("Application mode dispatch", "flow_id", spec.ID, "namespace", spec.Namespace)

	ns := spec.Namespace
	if ns == "" {
		ns = fmt.Sprintf("%s-%s", c.cfg.Tenant.NamespacePrefix, spec.ID)
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		c.logger.Warn("Not in K8s cluster, falling back to session mode",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
		return
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Warn("Failed to create K8s client, falling back to session",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
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
		c.logger.Warn("Failed to create dedicated JM deployment, falling back to session",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
		return
	}

	c.logger.Info("Dedicated JM deployment created",
		"flow_id", spec.ID, "namespace", ns, "deployment", jmName)

	run := &entities.FlowRunInfo{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      entities.RunPending,
		Priority:    entities.PriorityGrade,
		TenantID:    tenantID,
		Namespace:   ns,
		Vars:        spec.Vars,
		Trigger:     entities.TriggerInfo{Type: "schedule", Source: "controller"},
	}
	if _, err := c.api.CreateRun(ctx, tenantID, run); err != nil {
		c.logger.Error("Failed to create application run via apiserver", "flow_id", spec.ID, "error", err)
	}
}

func (c *Controller) buildJMDeployment(name, namespace, tenantID string, spec *entities.AgentFlowInfo) *appsv1.Deployment {
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
							{Name: "FLOWGENT__DEPLOYMENT__MODE", Value: "application"},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE", Value: namespace},
							{Name: "FLOWGENT__RUNTIME__AGENT_FLOW_ID", Value: spec.ID},
						},
					}},
				},
			},
		},
	}
}

func (c *Controller) stopAllFlows() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for flowID, cancel := range c.running {
		c.logger.Info("Stopping flow dispatch", "flow_id", flowID)
		cancel()
	}
	c.running = make(map[string]context.CancelFunc)
}

// ─── Controller startup ────────────────────────────────────────

func startController(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := "JSON", "DEBUG"
	if svcCfg != nil {
		logMode, logLevel = svcCfg.Logging.Mode, svcCfg.Logging.Level
	}
	logger := utils.NewLogger(logMode, logLevel)

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	stateClient := &client.TaskStateClient{Client: apiClient, Tenant: tenant}
	humanClient := &client.HumanApprovalClient{Client: apiClient}

	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:      engine.ProviderStandalone,
		PoolSize:      svcCfg.Orchestration.MaxConcurrentFlows,
		TaskState:     stateClient,
		ApprovalInfo: humanClient,
		Logger:        logger,
		APIServerURL:  svcCfg.Runtime.APIServerURL,
		Tenant:        tenant,
	})
	if err != nil {
		return fmt.Errorf("create resource manager: %w", err)
	}

	var disc discovery.IDiscoveryClient
	if k8sDisc, err := discovery.NewK8sDiscoveryClient(); err == nil {
		disc = k8sDisc
		logger.Info("Controller using K8s discovery client")
	} else {
		disc = discovery.NewStaticDiscoveryClient(svcCfg.Runtime.PodTotal, svcCfg.Runtime.PodIndex)
		logger.Info("Controller using static discovery client (env vars)")
	}

	ctrl := NewController(apiClient, tenant, rm, logger, svcCfg, cfgPath, disc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		slog.Info("Controller received shutdown signal")
		cancel()
	}()

	if err := ctrl.Run(ctx); err != nil {
		return fmt.Errorf("controller run: %w", err)
	}
	return nil
}

func isTerminalStatus(s entities.RunStatus) bool {
	return s == entities.RunCompleted || s == entities.RunFailed || s == entities.RunCancelled
}
