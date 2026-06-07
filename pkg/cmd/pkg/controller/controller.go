// Package controller provides the distributed sharded flow driver daemon.
// It polls agentflow definitions from PostgreSQL, shards flows across
// controller pods via hash-mod partitioning, and dispatches executions
// in either session mode (shared JM) or application mode (dedicated JM+TM).
package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/flowgent-labs/flowgent/cmd/src/cmdutil"
	"github.com/flowgent-labs/flowgent/config/src/config"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/core/src/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/src/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/store/src/agentflow"
	"github.com/flowgent-labs/flowgent/store/src/flowrun"
)

// ─── CLI entry points ──────────────────────────────────────────

// Start launches the Controller daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	logSuffix := ""
	if pidFile != "" {
		logSuffix = fmt.Sprintf(", pidfile=%s", pidFile)
	}
	slog.Info(fmt.Sprintf("Flowgent Controller starting (pid=%d%s)", os.Getpid(), logSuffix))
	return startController(cfgPath)
}

// Stop stops the Controller daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the Controller daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startController(cfgPath)
}

// ─── Controller ────────────────────────────────────────────────

// Controller is the distributed flow driver.
type Controller struct {
	store    store.IStore
	frStore  flowrun.IFlowRunStore
	afStore  agentflow.IAgentFlowStore
	rm       resourcemanager.ResourceManager
	logger   *utils.Logger
	cfg      *config.FlowgentConfig
	cfgPath  string

	discovery    discovery.IDiscoveryClient
	pollInterval time.Duration

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

// NewController creates a Controller instance.
func NewController(s store.IStore, rm resourcemanager.ResourceManager, logger *utils.Logger,
	cfg *config.FlowgentConfig, cfgPath string, disc discovery.IDiscoveryClient) *Controller {
	var frStore flowrun.IFlowRunStore
	var afStore agentflow.IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		frStore = flowrun.NewFlowRunPostgresStore(db)
		afStore = agentflow.NewAgentFlowPostgresStore(db)
	case *sql.DB:
		frStore = flowrun.NewFlowRunSQLiteStore(db)
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
	}
	return &Controller{
		store:        s,
		frStore:      frStore,
		afStore:      afStore,
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
	labelSelector := os.Getenv("FLOWGENT_CONTROLLER_LABEL")
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
	page, err := c.afStore.Select(ctx, model.PageRequest{Page: 1, Size: 1000})
	versions := page.Items
	if err != nil {
		c.logger.Error("Failed to list agentflow definitions", "error", err)
		return
	}

	seen := make(map[string]*model.AgentFlowSpec)
	for _, v := range versions {
		if _, exists := seen[v.AgentFlowID]; exists {
			continue
		}
		var spec model.AgentFlowSpec
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

func (c *Controller) dispatchFlow(ctx context.Context, spec *model.AgentFlowSpec) {
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
	case model.ModeApplication:
		c.dispatchApplicationMode(flowCtx, spec)
	default:
		c.dispatchSessionMode(flowCtx, spec)
	}
}

func (c *Controller) dispatchSessionMode(ctx context.Context, spec *model.AgentFlowSpec) {
	c.logger.Info("Session mode dispatch", "flow_id", spec.ID)

	run := &model.AgentFlowRun{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      model.RunPending,
		Priority:    spec.Priority,
		TenantID:    spec.TenantID,
		Namespace:   spec.Namespace,
		Vars:        spec.Vars,
		Trigger:     model.TriggerInfo{Type: "schedule", Source: "controller"},
	}

	if err := c.frStore.Create(ctx, run); err != nil {
		c.logger.Error("Failed to create session run", "flow_id", spec.ID, "error", err)
		return
	}

	c.logger.Info("Session run created", "flow_id", spec.ID, "run_id", run.ID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r, err := c.frStore.Get(ctx, run.ID)
			if err != nil || r == nil {
				continue
			}
			if isTerminalStatus(r.Status) {
				c.logger.Info("Session run completed", "flow_id", spec.ID, "run_id", run.ID, "status", r.Status)
				return
			}
		}
	}
}

func (c *Controller) dispatchApplicationMode(ctx context.Context, spec *model.AgentFlowSpec) {
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
		tenantID = "default"
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

	run := &model.AgentFlowRun{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      model.RunPending,
		Priority:    model.PriorityGrade,
		TenantID:    spec.TenantID,
		Namespace:   ns,
		Vars:        spec.Vars,
		Trigger:     model.TriggerInfo{Type: "schedule", Source: "controller"},
	}
	if err := c.frStore.Create(ctx, run); err != nil {
		c.logger.Error("Failed to create application run", "flow_id", spec.ID, "error", err)
	}
}

func (c *Controller) buildJMDeployment(name, namespace, tenantID string, spec *model.AgentFlowSpec) *appsv1.Deployment {
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
						Image: os.Getenv("FLOWGENT_JM_IMAGE"),
						Args:  []string{"jobmanager", "start", "-c", "/etc/flowgent/flowgent.yaml", "--flow-id", spec.ID},
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT_DEPLOYMENT_MODE", Value: "application"},
							{Name: "FLOWGENT_NAMESPACE", Value: namespace},
							{Name: "FLOWGENT_AGENTFLOW_ID", Value: spec.ID},
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

	storeImpl := store.NewStoreManager(svcCfg)
	if _, ok := storeImpl.DB().(*pgxpool.Pool); !ok {
		return fmt.Errorf("controller requires PostgreSQL storage (set FLOWGENT_DATABASE_URL or configure storage.type=POSTGRE)")
	}

	loadedAgents, _ := config.LoadAgents(svcCfg, cfgPath)
	agentPtrs := make([]*config.AgentDef, len(loadedAgents))
	for i := range loadedAgents {
		agentPtrs[i] = &loadedAgents[i]
	}

	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:   engine.ProviderStandalone,
		PoolSize:   svcCfg.Orchestration.MaxConcurrentFlows,
		Store:      storeImpl,
		Agents:     agentPtrs,
		MCPClients: make(map[string]engine.MCPClient),
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("create resource manager: %w", err)
	}

	var disc discovery.IDiscoveryClient
	if k8sDisc, err := discovery.NewK8sDiscoveryClient(); err == nil {
		disc = k8sDisc
		logger.Info("Controller using K8s discovery client")
	} else {
		disc = discovery.NewStaticDiscoveryClient()
		logger.Info("Controller using static discovery client (env vars)")
	}

	ctrl := NewController(storeImpl, rm, logger, svcCfg, cfgPath, disc)

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

func isTerminalStatus(s model.RunStatus) bool {
	return s == model.RunCompleted || s == model.RunFailed || s == model.RunCancelled
}
