package resourcemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flowgent-labs/flowgent/cache/pkg"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// RMState is the persisted state of TM and sandbox scaling for JM failover.
type RMState struct {
	CurrentTMs             int32     `json:"current_tms"`
	SlotsPerTM             int       `json:"slots_per_tm"`
	MinTMs                 int       `json:"min_tms"`
	MaxTMs                 int       `json:"max_tms"`
	PendingPlans           int64     `json:"pending_plans"`
	LastActivity           time.Time `json:"last_activity"`
	SandboxReplicas        int32     `json:"sandbox_replicas,omitempty"`
	SandboxPendingTriggers int64     `json:"sandbox_pending_triggers,omitempty"`
}

// KubernetesResourceManager dispatches plans to TM pods via MQTT with elastic
// scaling. The JM auto-scales TMs based on pending plan load.
//
// When SandboxEnabled=true, the K8sRM also manages a separate sandbox Deployment
// with its own scaling loop. Both TM and sandbox pods share the same workspace PVC.
//
// State is persisted to cache so that on JM failover the new JM can restoreFromCache
// the current TM/sandbox replica counts and slot allocation without querying K8s.
// execResult is the TM→JM result published to exec/results.
type execResult struct {
	PlanID         string         `json:"plan_id"`
	AgentFlowRunID string         `json:"agentflow_run_id"`
	NodeID         string         `json:"node_id"`
	State          string         `json:"state"`
	Output         map[string]any `json:"output,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type KubernetesResourceManager struct {
	q           messager.IMessager
	cache       cache.ICache
	namespace   string
	deployName  string
	kubeClient  kubernetes.Interface
	slotsPerTM  int
	minTMs      int
	maxTMs      int
	currentTMs  int32
	idleTimeout time.Duration
	planTimeout time.Duration

	mu                     sync.Mutex
	pendingPlans           int64
	lastActivity           time.Time
	runtimeReadyMu         sync.Mutex
	runtimeReadySubscribed map[string]bool
	runtimeReadyAt         map[string]time.Time
	runtimeReadySignal     map[string]chan struct{}

	// Per-run result routing: Schedule subscribes once per run to
	// exec/results and routes messages to the waiting plan's channel.
	runResultsMu sync.Mutex
	runResults   map[string]map[string]chan execResult // runID → nodeID → chan

	// Sandbox deployment fields
	sandboxEnabled         bool
	sandboxDeployName      string
	sandboxImage           string
	sandboxSlotsPerPod     int
	sandboxMinReplicas     int
	sandboxMaxReplicas     int
	sandboxCurrentReplicas int32
	sandboxWorkspace       string
	sandboxHostWorkspace   string
	sandboxPolicy          *model.SandboxPolicy
	sandboxResources       *model.SandboxResources
	sandboxPendingTriggers int64

	mqttBroker               string
	postgresDSN              string
	apiServerURL             string
	configMapName            string
	tmImage                  string
	tmResources              *model.SandboxResources
	priorityClassName        string
	nodeSelector             map[string]string
	ownerNamespaceID         string
	runtimeClusterID         string
	ownerFlowID              string
	ownerRunID               string
	runtimeMode              entities.RuntimeMode
	ownerJobManagerName      string
	ownerJobManagerNamespace string
	deleteOnShutdown         bool
	credentialEnvSecret      string
	internalAuthSecret       string
	taskManagerAuthKey       string

	ctx    context.Context
	cancel context.CancelFunc
}

const (
	LabelNamespaceID               = "flowgent.io/namespace"
	LabelRuntimeClusterID          = "flowgent.io/runtime-cluster"
	LabelFlowID                    = "flowgent.io/flow"
	LabelRunID                     = "flowgent.io/run"
	LabelRuntimeMode               = "flowgent.io/runtime-mode"
	LabelManagedBy                 = "flowgent.io/managed-by"
	LabelParentJobManager          = "flowgent.io/parent-jobmanager"
	LabelParentJobManagerNamespace = "flowgent.io/parent-jobmanager-namespace"

	LabelValueRuntimeCluster = "runtime-cluster"
	runtimeReadyLease        = 15 * time.Second
)

func NewKubernetesResourceManager(cfg *ResourceManagerConfig) (*KubernetesResourceManager, error) {
	if cfg.SlotsPerTM <= 0 {
		cfg.SlotsPerTM = 4
	}
	if cfg.MinTMs < 0 {
		cfg.MinTMs = 0
	}
	if cfg.MaxTMs <= 0 {
		cfg.MaxTMs = 10
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.PlanTimeout <= 0 {
		cfg.PlanTimeout = 12 * time.Minute
	}
	if cfg.K8sNamespace == "" {
		cfg.K8sNamespace = "default"
	}
	if cfg.K8sDeploymentName == "" {
		cfg.K8sDeploymentName = "flowgent-taskmanager"
	}
	if cfg.SandboxDeploymentName == "" {
		cfg.SandboxDeploymentName = "flowgent-sandbox"
	}
	if cfg.ConfigMapName == "" {
		cfg.ConfigMapName = "flowgent-config"
	}
	if cfg.SandboxMinReplicas < 0 {
		cfg.SandboxMinReplicas = 0
	}
	if cfg.SandboxMaxReplicas <= 0 {
		cfg.SandboxMaxReplicas = 10
	}
	if cfg.SandboxMaxReplicas < cfg.SandboxMinReplicas {
		cfg.SandboxMaxReplicas = cfg.SandboxMinReplicas
	}
	if cfg.SandboxSlotsPerPod <= 0 {
		cfg.SandboxSlotsPerPod = 4
	}

	restCfg, err := buildRESTConfig(cfg.K8sKubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("kubernetes rm: build REST config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes rm: create clientset: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	rm := &KubernetesResourceManager{
		q:                      nil, // set via SetQueue
		cache:                  cfg.Cache,
		runResults:             make(map[string]map[string]chan execResult),
		runtimeReadySubscribed: make(map[string]bool),
		runtimeReadyAt:         make(map[string]time.Time),
		runtimeReadySignal:     make(map[string]chan struct{}),
		namespace:              cfg.K8sNamespace,
		deployName:             cfg.K8sDeploymentName,
		kubeClient:             clientset,
		slotsPerTM:             cfg.SlotsPerTM,
		minTMs:                 cfg.MinTMs,
		maxTMs:                 cfg.MaxTMs,
		currentTMs:             int32(cfg.MinTMs),
		idleTimeout:            cfg.IdleTimeout,
		planTimeout:            cfg.PlanTimeout,

		sandboxEnabled:           cfg.SandboxEnabled,
		sandboxDeployName:        cfg.SandboxDeploymentName,
		sandboxImage:             defaultIfEmpty(cfg.SandboxImage, cfg.TMImage),
		sandboxSlotsPerPod:       cfg.SandboxSlotsPerPod,
		sandboxMinReplicas:       cfg.SandboxMinReplicas,
		sandboxMaxReplicas:       cfg.SandboxMaxReplicas,
		sandboxCurrentReplicas:   int32(cfg.SandboxMinReplicas),
		sandboxWorkspace:         cfg.SandboxWorkspace,
		sandboxHostWorkspace:     defaultHostPath(cfg.SandboxHostWorkspace, cfg.SandboxWorkspace),
		sandboxPolicy:            cfg.SandboxPolicy,
		sandboxResources:         cfg.SandboxResources,
		mqttBroker:               cfg.MQTTBroker,
		postgresDSN:              cfg.PostgresDSN,
		apiServerURL:             cfg.APIServerURL,
		configMapName:            cfg.ConfigMapName,
		tmImage:                  cfg.TMImage,
		tmResources:              cfg.TMResources,
		priorityClassName:        cfg.PriorityClassName,
		nodeSelector:             cfg.NodeSelector,
		ownerNamespaceID:         cfg.OwnerNamespaceID,
		runtimeClusterID:         cfg.RuntimeClusterID,
		ownerFlowID:              cfg.OwnerFlowID,
		ownerRunID:               cfg.OwnerRunID,
		runtimeMode:              cfg.RuntimeMode,
		ownerJobManagerName:      cfg.OwnerJobManagerName,
		ownerJobManagerNamespace: cfg.OwnerJobManagerNamespace,
		deleteOnShutdown:         cfg.DeleteOnShutdown,
		credentialEnvSecret:      cfg.CredentialEnvSecret,
		internalAuthSecret:       cfg.InternalAuthSecret,
		taskManagerAuthKey:       defaultIfEmpty(cfg.TaskManagerAuthKey, "taskmanager-token"),

		ctx:    ctx,
		cancel: cancel,
	}

	// Restore state from cache (JM failover recovery).
	if cfg.Cache != nil {
		rm.restore(ctx)
	}

	if rm.minTMs > 0 {
		if err := rm.ensureDeployment(ctx); err != nil {
			slog.Warn("kubernetes rm: tm deployment check failed (will retry in loop)", "err", err)
		}
		slog.Info("kubernetes rm: scaling tm deployment to min replicas",
			"deployment", rm.deployName, "namespace", rm.namespace, "replicas", rm.minTMs)
		if err := rm.scaleDeployment(ctx, int32(rm.minTMs)); err != nil {
			slog.Warn("kubernetes rm: initial tm scale failed", "err", err)
		}
	}

	if rm.sandboxEnabled && rm.sandboxMinReplicas > 0 {
		if err := rm.ensureSandboxDeployment(ctx); err != nil {
			slog.Warn("kubernetes rm: sandbox deployment check failed (will retry in loop)", "err", err)
		}
		slog.Info("kubernetes rm: scaling sandbox deployment to min replicas",
			"deployment", rm.sandboxDeployName, "namespace", rm.namespace, "replicas", rm.sandboxMinReplicas)
		if err := rm.scaleSandboxDeployment(ctx, int32(rm.sandboxMinReplicas)); err != nil {
			slog.Warn("kubernetes rm: initial sandbox scale failed", "err", err)
		}
	}

	rm.persist(ctx)
	return rm, nil
}

func (s *KubernetesResourceManager) SetQueue(q messager.IMessager) { s.q = q }
func (s *KubernetesResourceManager) Provider() engine.Provider {
	return engine.ProviderKubernetes
}

func (s *KubernetesResourceManager) ensureRuntimeReadySubscription(ctx context.Context, role string) error {
	if s.runtimeClusterID == "" {
		return nil
	}
	s.runtimeReadyMu.Lock()
	defer s.runtimeReadyMu.Unlock()
	if s.runtimeReadySubscribed == nil {
		s.runtimeReadySubscribed = make(map[string]bool)
		s.runtimeReadyAt = make(map[string]time.Time)
		s.runtimeReadySignal = make(map[string]chan struct{})
	}
	if s.runtimeReadySubscribed[role] {
		return nil
	}
	if s.runtimeReadySignal[role] == nil {
		s.runtimeReadySignal[role] = make(chan struct{}, 1)
	}
	namespaceID := defaultIfEmpty(s.ownerNamespaceID, "default")
	topic := messager.RuntimeReadyWildcard(namespaceID, s.runtimeClusterID, role)
	if err := s.q.Subscribe(ctx, topic, func(_ string, payload []byte) {
		var ready messager.RuntimeReady
		if err := json.Unmarshal(payload, &ready); err != nil || ready.Role != role ||
			ready.Namespace != namespaceID || ready.ClusterID != s.runtimeClusterID {
			return
		}
		s.runtimeReadyMu.Lock()
		s.runtimeReadyAt[role] = time.Now()
		signal := s.runtimeReadySignal[role]
		s.runtimeReadyMu.Unlock()
		select {
		case signal <- struct{}{}:
		default:
		}
	}); err != nil {
		return err
	}
	s.runtimeReadySubscribed[role] = true
	return nil
}

func (s *KubernetesResourceManager) waitForRuntimeReady(ctx context.Context, role string) error {
	if s.runtimeClusterID == "" {
		return nil
	}
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	for {
		s.runtimeReadyMu.Lock()
		readyAt := s.runtimeReadyAt[role]
		signal := s.runtimeReadySignal[role]
		s.runtimeReadyMu.Unlock()
		if !readyAt.IsZero() && time.Since(readyAt) <= runtimeReadyLease {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s worker subscription not ready for %s/%s", role, defaultIfEmpty(s.ownerNamespaceID, "default"), s.ownerFlowID)
		case <-signal:
		}
	}
}

func (s *KubernetesResourceManager) Validate(ctx context.Context) error {
	if s.namespace == "" {
		return fmt.Errorf("kubernetes rm: K8s namespace is required")
	}
	return nil
}

func (s *KubernetesResourceManager) Start(ctx context.Context) {
	go s.scalingLoop(ctx)
	slog.Info("kubernetes rm started",
		"min_tms", s.minTMs, "max_tms", s.maxTMs,
		"slots_per_tm", s.slotsPerTM, "idle_timeout", s.idleTimeout)
}

func (s *KubernetesResourceManager) Schedule(ctx context.Context, plan *entities.ExecutionPlan) (*entities.TaskResult, error) {
	if s.q == nil {
		return nil, fmt.Errorf("kubernetes rm: queue not set")
	}
	atomic.AddInt64(&s.pendingPlans, 1)
	defer atomic.AddInt64(&s.pendingPlans, -1)
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.planTimeout)
	defer cancel()

	if err := s.ensureRuntimeReadySubscription(ctx, "taskmanager"); err != nil {
		return nil, fmt.Errorf("kubernetes rm taskmanager readiness: %w", err)
	}
	if err := s.ensureTaskManagerCapacity(ctx); err != nil {
		return nil, fmt.Errorf("kubernetes rm capacity: %w", err)
	}
	if err := s.waitForRuntimeReady(ctx, "taskmanager"); err != nil {
		return nil, fmt.Errorf("kubernetes rm taskmanager readiness: %w", err)
	}
	if plan.TaskType == entities.TaskSandbox && s.sandboxEnabled {
		atomic.AddInt64(&s.sandboxPendingTriggers, 1)
		defer atomic.AddInt64(&s.sandboxPendingTriggers, -1)
		if err := s.ensureRuntimeReadySubscription(ctx, "sandbox"); err != nil {
			return nil, fmt.Errorf("kubernetes rm sandbox readiness: %w", err)
		}
		if err := s.ensureSandboxCapacity(ctx); err != nil {
			return nil, fmt.Errorf("kubernetes rm sandbox capacity: %w", err)
		}
		if err := s.waitForRuntimeReady(ctx, "sandbox"); err != nil {
			return nil, fmt.Errorf("kubernetes rm sandbox readiness: %w", err)
		}
	}

	runID := plan.AgentFlowRunID
	resultCh := make(chan execResult, 1)

	// Subscribe to exec/results for this run (once) and register the plan's
	// result channel before publishing, so we don't miss the response.
	s.runResultsMu.Lock()
	if s.runResults[runID] == nil {
		s.runResults[runID] = make(map[string]chan execResult)
		s.q.Subscribe(ctx, messager.ExecResultsTopic(plan.Namespace, plan.AgentFlowDefinitionID, runID),
			func(topic string, payload []byte) {
				var er execResult
				if err := json.Unmarshal(payload, &er); err != nil {
					return
				}
				s.runResultsMu.Lock()
				ch, ok := s.runResults[runID][er.NodeID]
				if ok {
					delete(s.runResults[runID], er.NodeID)
				}
				s.runResultsMu.Unlock()
				if ok {
					select {
					case ch <- er:
					default:
					}
				}
			})
	}
	s.runResults[runID][plan.NodeID] = resultCh
	s.runResultsMu.Unlock()

	plan.TraceContext = make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(plan.TraceContext))
	payload, _ := json.Marshal(plan)
	if err := s.q.Publish(ctx, messager.ExecPlansTopic(plan.Namespace, plan.RuntimeClusterID, plan.AgentFlowDefinitionID, runID), &messager.InterMessage{
		ID: plan.PlanID,
		Headers: map[string]string{
			"task_run_id": plan.AgentFlowRunID,
			"node_id":     plan.NodeID,
		},
		Payload: payload,
	}); err != nil {
		s.runResultsMu.Lock()
		delete(s.runResults[runID], plan.NodeID)
		s.runResultsMu.Unlock()
		return nil, fmt.Errorf("kubernetes rm publish: %w", err)
	}

	// Wait for the actual execution result from the TM.
	select {
	case <-ctx.Done():
		s.runResultsMu.Lock()
		delete(s.runResults[runID], plan.NodeID)
		s.runResultsMu.Unlock()
		return nil, fmt.Errorf("kubernetes rm: plan %s timed out waiting for TM result", plan.PlanID)
	case er := <-resultCh:
		return &entities.TaskResult{Output: er.Output, Error: er.Error}, nil
	}
}

// ─── Scaling ────────────────────────────────────────────

func (s *KubernetesResourceManager) scalingLoop(ctx context.Context) {
	interval := s.idleTimeout / 20
	if interval < 15*time.Second {
		interval = 15 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *KubernetesResourceManager) reconcile(ctx context.Context) {
	s.reconcileTM(ctx)
	if s.sandboxEnabled {
		s.reconcileSandbox(ctx)
	}
}

func (s *KubernetesResourceManager) reconcileTM(ctx context.Context) {
	pending := atomic.LoadInt64(&s.pendingPlans)
	currentTMs := int(atomic.LoadInt32(&s.currentTMs))
	currentSlots := currentTMs * s.slotsPerTM

	s.mu.Lock()
	idleDuration := time.Since(s.lastActivity)
	s.mu.Unlock()

	if pending > int64(currentSlots) {
		neededTMs := s.desiredTMCount(pending)
		if neededTMs > currentTMs {
			slog.Info("kubernetes rm tm scale up",
				"pending", pending, "current_tms", currentTMs, "target_tms", neededTMs)
			if err := s.ensureTaskManagerCapacity(ctx); err != nil {
				slog.Error("kubernetes rm tm scale up failed", "err", err)
				return
			}
		}
		return
	}

	if idleDuration > s.idleTimeout && currentTMs > s.minTMs {
		targetTMs := currentTMs - 1
		if targetTMs < s.minTMs {
			targetTMs = s.minTMs
		}
		slog.Info("kubernetes rm tm scale down",
			"idle", idleDuration.Round(time.Second), "current_tms", currentTMs, "target_tms", targetTMs)
		if err := s.scaleDeployment(ctx, int32(targetTMs)); err != nil {
			slog.Error("kubernetes rm tm scale down failed", "err", err)
			return
		}
		atomic.StoreInt32(&s.currentTMs, int32(targetTMs))
		s.persist(ctx)
	}
}

func (s *KubernetesResourceManager) desiredTMCount(pending int64) int {
	if pending <= 0 {
		return s.minTMs
	}
	needed := int((pending + int64(s.slotsPerTM) - 1) / int64(s.slotsPerTM))
	if needed < s.minTMs {
		needed = s.minTMs
	}
	if needed > s.maxTMs {
		needed = s.maxTMs
	}
	return needed
}

func (s *KubernetesResourceManager) ensureTaskManagerCapacity(ctx context.Context) error {
	target := s.desiredTMCount(atomic.LoadInt64(&s.pendingPlans))
	if target <= 0 {
		return nil
	}

	s.mu.Lock()
	persistNeeded := false
	if err := s.ensureDeployment(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	currentReplicas := int32(0)
	if dep, err := s.kubeClient.AppsV1().Deployments(s.namespace).Get(ctx, s.deployName, metav1.GetOptions{}); err == nil && dep.Spec.Replicas != nil {
		currentReplicas = *dep.Spec.Replicas
	}
	if currentReplicas < int32(target) {
		slog.Info("kubernetes rm tm scale up",
			"pending", atomic.LoadInt64(&s.pendingPlans),
			"current_replicas", currentReplicas,
			"target_replicas", target,
			"slots_per_tm", s.slotsPerTM)
		if err := s.scaleDeployment(ctx, int32(target)); err != nil {
			s.mu.Unlock()
			return err
		}
		atomic.StoreInt32(&s.currentTMs, int32(target))
		persistNeeded = true
	} else if atomic.LoadInt32(&s.currentTMs) < int32(target) {
		atomic.StoreInt32(&s.currentTMs, currentReplicas)
		persistNeeded = true
	}
	s.mu.Unlock()
	if persistNeeded {
		s.persist(ctx)
	}

	return s.waitForReadyTaskManagers(ctx, int32(target))
}

func (s *KubernetesResourceManager) waitForReadyTaskManagers(ctx context.Context, target int32) error {
	if target <= 0 {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()

	for {
		dep, err := s.kubeClient.AppsV1().Deployments(s.namespace).Get(ctx, s.deployName, metav1.GetOptions{})
		if err == nil && dep.Status.AvailableReplicas >= target {
			return nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("wait for tm deployment %s/%s: %w", s.namespace, s.deployName, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if err != nil {
				return fmt.Errorf("tm deployment %s/%s not ready: %w", s.namespace, s.deployName, err)
			}
			return fmt.Errorf("tm deployment %s/%s available replicas < %d", s.namespace, s.deployName, target)
		case <-ticker.C:
		}
	}
}

// reconcileSandbox scales sandbox pods based on pending trigger count.
func (s *KubernetesResourceManager) reconcileSandbox(ctx context.Context) {
	pending := atomic.LoadInt64(&s.sandboxPendingTriggers)
	currentReplicas := int(atomic.LoadInt32(&s.sandboxCurrentReplicas))
	currentSlots := currentReplicas * s.sandboxSlotsPerPod

	if pending > int64(currentSlots) {
		needed := s.desiredSandboxCount(pending)
		if needed > currentReplicas {
			slog.Info("kubernetes rm sandbox scale up",
				"pending", pending, "current_replicas", currentReplicas, "target_replicas", needed)
			if err := s.ensureSandboxCapacity(ctx); err != nil {
				slog.Error("kubernetes rm sandbox scale up failed", "err", err)
				return
			}
		}
		return
	}

	// Scale down sandbox if idle (same idle timeout as TM).
	s.mu.Lock()
	idleDuration := time.Since(s.lastActivity)
	s.mu.Unlock()

	if idleDuration > s.idleTimeout && currentReplicas > s.sandboxMinReplicas {
		target := currentReplicas - 1
		if target < s.sandboxMinReplicas {
			target = s.sandboxMinReplicas
		}
		slog.Info("kubernetes rm sandbox scale down",
			"idle", idleDuration.Round(time.Second), "current_replicas", currentReplicas, "target_replicas", target)
		if err := s.scaleSandboxDeployment(ctx, int32(target)); err != nil {
			slog.Error("kubernetes rm sandbox scale down failed", "err", err)
			return
		}
		atomic.StoreInt32(&s.sandboxCurrentReplicas, int32(target))
		s.persist(ctx)
	}
}

func (s *KubernetesResourceManager) desiredSandboxCount(pending int64) int {
	if !s.sandboxEnabled {
		return 0
	}
	if pending <= 0 {
		return s.sandboxMinReplicas
	}
	needed := int((pending + int64(s.sandboxSlotsPerPod) - 1) / int64(s.sandboxSlotsPerPod))
	if needed < s.sandboxMinReplicas {
		needed = s.sandboxMinReplicas
	}
	if needed > s.sandboxMaxReplicas {
		needed = s.sandboxMaxReplicas
	}
	return needed
}

func (s *KubernetesResourceManager) ensureSandboxCapacity(ctx context.Context) error {
	target := s.desiredSandboxCount(atomic.LoadInt64(&s.sandboxPendingTriggers))
	if target <= 0 {
		return nil
	}

	s.mu.Lock()
	persistNeeded := false
	if err := s.ensureSandboxDeployment(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	currentReplicas := int32(0)
	if dep, err := s.kubeClient.AppsV1().Deployments(s.namespace).Get(ctx, s.sandboxDeployName, metav1.GetOptions{}); err == nil && dep.Spec.Replicas != nil {
		currentReplicas = *dep.Spec.Replicas
	}
	if currentReplicas < int32(target) {
		slog.Info("kubernetes rm sandbox scale up",
			"pending", atomic.LoadInt64(&s.sandboxPendingTriggers),
			"current_replicas", currentReplicas,
			"target_replicas", target,
			"slots_per_pod", s.sandboxSlotsPerPod)
		if err := s.scaleSandboxDeployment(ctx, int32(target)); err != nil {
			s.mu.Unlock()
			return err
		}
		atomic.StoreInt32(&s.sandboxCurrentReplicas, int32(target))
		persistNeeded = true
	} else if atomic.LoadInt32(&s.sandboxCurrentReplicas) < int32(target) {
		atomic.StoreInt32(&s.sandboxCurrentReplicas, currentReplicas)
		persistNeeded = true
	}
	s.mu.Unlock()
	if persistNeeded {
		s.persist(ctx)
	}

	return s.waitForReadySandbox(ctx, int32(target))
}

func (s *KubernetesResourceManager) waitForReadySandbox(ctx context.Context, target int32) error {
	if target <= 0 {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()

	for {
		dep, err := s.kubeClient.AppsV1().Deployments(s.namespace).Get(ctx, s.sandboxDeployName, metav1.GetOptions{})
		if err == nil && dep.Status.AvailableReplicas >= target {
			return nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("wait for sandbox deployment %s/%s: %w", s.namespace, s.sandboxDeployName, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if err != nil {
				return fmt.Errorf("sandbox deployment %s/%s not ready: %w", s.namespace, s.sandboxDeployName, err)
			}
			return fmt.Errorf("sandbox deployment %s/%s available replicas < %d", s.namespace, s.sandboxDeployName, target)
		case <-ticker.C:
		}
	}
}

func (s *KubernetesResourceManager) scaleDeployment(ctx context.Context, replicas int32) error {
	scale := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.deployName,
			Namespace: s.namespace,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: replicas,
		},
	}
	_, err := s.kubeClient.AppsV1().Deployments(s.namespace).
		UpdateScale(ctx, s.deployName, scale, metav1.UpdateOptions{})
	if err != nil {
		if replicas == 0 && apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("scale deployment %s/%s to %d: %w", s.namespace, s.deployName, replicas, err)
	}
	return nil
}

func (s *KubernetesResourceManager) scaleSandboxDeployment(ctx context.Context, replicas int32) error {
	scale := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.sandboxDeployName,
			Namespace: s.namespace,
		},
		Spec: autoscalingv1.ScaleSpec{
			Replicas: replicas,
		},
	}
	_, err := s.kubeClient.AppsV1().Deployments(s.namespace).
		UpdateScale(ctx, s.sandboxDeployName, scale, metav1.UpdateOptions{})
	if err != nil {
		if replicas == 0 && apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("scale sandbox deployment %s/%s to %d: %w", s.namespace, s.sandboxDeployName, replicas, err)
	}
	return nil
}

// TrackSandboxTrigger increments the pending sandbox trigger counter.
// Called by SandboxExecutor before publishing a trigger.
func (s *KubernetesResourceManager) TrackSandboxTrigger() {
	atomic.AddInt64(&s.sandboxPendingTriggers, 1)
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// CompleteSandboxTrigger decrements the pending sandbox trigger counter.
// Called by SandboxExecutor after receiving a result.
func (s *KubernetesResourceManager) CompleteSandboxTrigger() {
	atomic.AddInt64(&s.sandboxPendingTriggers, -1)
}

func (s *KubernetesResourceManager) ensureDeployment(ctx context.Context) error {
	desired := s.desiredTMDeployment()
	existing, err := s.kubeClient.AppsV1().Deployments(s.namespace).
		Get(ctx, s.deployName, metav1.GetOptions{})
	if err == nil {
		return s.reconcileDeployment(ctx, existing, desired)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = s.kubeClient.AppsV1().Deployments(s.namespace).Create(ctx, desired, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create tm deployment: %w", err)
	}
	slog.Info("kubernetes rm: created tm deployment", "name", s.deployName, "replicas", s.minTMs)
	return nil
}

func (s *KubernetesResourceManager) desiredTMDeployment() *appsv1.Deployment {
	replicas := int32(s.minTMs)
	labels := s.tmLabels()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: s.deployName, Namespace: s.namespace,
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					PriorityClassName: s.priorityClassName,
					NodeSelector:      s.nodeSelector,
					Containers: []corev1.Container{{
						Name:            "taskmanager",
						Image:           s.tmImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						EnvFrom:         s.credentialEnvFrom(),
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT__MESSAGER__MQTT__BROKER", Value: s.mqttBroker},
							{Name: "FLOWGENT__STORAGE__POSTGRES__DSN", Value: s.postgresDSN},
							{Name: "FLOWGENT__RUNTIME__API_SERVER_URL", Value: s.apiServerURL},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE__DEFAULT_NAMESPACE", Value: defaultIfEmpty(s.ownerNamespaceID, "default")},
							{Name: "FLOWGENT__RUNTIME__MODE", Value: string(s.runtimeMode)},
							{Name: "FLOWGENT__RUNTIME__RUNTIME_CLUSTER_ID", Value: s.runtimeClusterID},
							{Name: "FLOWGENT__RUNTIME__TM_SLOTS", Value: fmt.Sprintf("%d", s.slotsPerTM)},
							workloadTokenEnv(s.internalAuthSecret, s.taskManagerAuthKey),
						},
						Command:   []string{"/app/flowgent", "taskmanager", "start", "-c", "/etc/flowgent/flowgent.yaml"},
						Resources: resourceRequirements(s.tmResources, "200m", "256Mi", "2000m", "2Gi"),
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/flowgent"},
							{Name: "workspace", MountPath: s.sandboxWorkspace},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: s.configMapName},
							},
						}},
						{Name: "workspace", VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: s.sandboxHostWorkspace,
								Type: hostPathPtr(corev1.HostPathDirectoryOrCreate),
							},
						}},
					},
				},
			},
		},
	}
}

func workloadTokenEnv(secretName, key string) corev1.EnvVar {
	env := corev1.EnvVar{Name: "FLOWGENT_INTERNAL_TOKEN"}
	if secretName == "" {
		return env
	}
	env.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  key,
	}}
	return env
}

func (s *KubernetesResourceManager) credentialEnvFrom() []corev1.EnvFromSource {
	optional := true
	sources := make([]corev1.EnvFromSource, 0, 3)
	if s.credentialEnvSecret != "" {
		sources = append(sources, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: s.credentialEnvSecret},
			Optional:             &optional,
		}})
	}
	return sources
}

func (s *KubernetesResourceManager) reconcileDeployment(ctx context.Context, existing, desired *appsv1.Deployment) error {
	if existing == nil || desired == nil {
		return fmt.Errorf("deployment reconciliation requires existing and desired state")
	}
	if reflect.DeepEqual(existing.Labels, desired.Labels) &&
		reflect.DeepEqual(existing.Spec.Template, desired.Spec.Template) {
		return nil
	}
	existing.Labels = desired.Labels
	existing.Spec.Template = desired.Spec.Template
	_, err := s.kubeClient.AppsV1().Deployments(s.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (s *KubernetesResourceManager) tmLabels() map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/component": "taskmanager",
		"flowgent/role":               "worker",
	}
	if s.runtimeClusterID == "" {
		return labels
	}
	namespaceID := s.ownerNamespaceID
	if namespaceID == "" {
		namespaceID = "default"
	}
	labels[LabelNamespaceID] = namespaceID
	labels[LabelRuntimeClusterID] = s.runtimeClusterID
	if s.runtimeMode != "" {
		labels[LabelRuntimeMode] = string(s.runtimeMode)
	}
	if s.ownerFlowID != "" {
		labels[LabelFlowID] = s.ownerFlowID
	}
	if s.ownerRunID != "" {
		labels[LabelRunID] = s.ownerRunID
	}
	labels[LabelManagedBy] = LabelValueRuntimeCluster
	return labels
}

func (s *KubernetesResourceManager) sandboxLabels() map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/component": "sandbox",
		"flowgent/role":               "sandbox-worker",
	}
	if s.runtimeClusterID == "" {
		return labels
	}
	namespaceID := s.ownerNamespaceID
	if namespaceID == "" {
		namespaceID = "default"
	}
	labels[LabelNamespaceID] = namespaceID
	labels[LabelRuntimeClusterID] = s.runtimeClusterID
	if s.runtimeMode != "" {
		labels[LabelRuntimeMode] = string(s.runtimeMode)
	}
	if s.ownerFlowID != "" {
		labels[LabelFlowID] = s.ownerFlowID
	}
	if s.ownerRunID != "" {
		labels[LabelRunID] = s.ownerRunID
	}
	labels[LabelManagedBy] = LabelValueRuntimeCluster
	return labels
}

func (s *KubernetesResourceManager) ensureSandboxDeployment(ctx context.Context) error {
	desired := s.desiredSandboxDeployment()
	existing, err := s.kubeClient.AppsV1().Deployments(s.namespace).
		Get(ctx, s.sandboxDeployName, metav1.GetOptions{})
	if err == nil {
		return s.reconcileDeployment(ctx, existing, desired)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = s.kubeClient.AppsV1().Deployments(s.namespace).Create(ctx, desired, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create sandbox deployment: %w", err)
	}
	slog.Info("kubernetes rm: created sandbox deployment", "name", s.sandboxDeployName, "replicas", s.sandboxMinReplicas)
	return nil
}

func (s *KubernetesResourceManager) desiredSandboxDeployment() *appsv1.Deployment {
	replicas := int32(s.sandboxMinReplicas)
	labels := s.sandboxLabels()
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: s.sandboxDeployName, Namespace: s.namespace,
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					PriorityClassName: s.priorityClassName,
					NodeSelector:      s.nodeSelector,
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Name:            "sandbox",
						Image:           s.sandboxImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						EnvFrom:         s.credentialEnvFrom(),
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT__CONFIG__FILE", Value: "/etc/flowgent/flowgent.yaml"},
							{Name: "FLOWGENT__MESSAGER__MQTT__BROKER", Value: s.mqttBroker},
							{Name: "FLOWGENT__SANDBOX__WORKSPACE", Value: s.sandboxWorkspace},
							{Name: "FLOWGENT__SANDBOX__DEPLOYMENT__SLOTS_PER_POD", Value: fmt.Sprintf("%d", s.sandboxSlotsPerPod)},
							{Name: "FLOWGENT__RUNTIME__NAMESPACE__DEFAULT_NAMESPACE", Value: defaultIfEmpty(s.ownerNamespaceID, "default")},
							{Name: "FLOWGENT__RUNTIME__MODE", Value: string(s.runtimeMode)},
							{Name: "FLOWGENT__RUNTIME__RUNTIME_CLUSTER_ID", Value: s.runtimeClusterID},
							{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
						},
						Command: []string{"/app/flowgent", "sandbox", "start"},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "config", MountPath: "/etc/flowgent"},
							{Name: "sandbox-workspace", MountPath: s.sandboxWorkspace},
						},
						Resources: s.buildSandboxResourceRequirements(),
					}},
					Volumes: []corev1.Volume{
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: s.configMapName},
							},
						}},
						{Name: "sandbox-workspace", VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: s.sandboxHostWorkspace,
								Type: hostPathDirOrCreatePtr(),
							},
						}},
					},
				},
			},
		},
	}
}

func (s *KubernetesResourceManager) buildSandboxResourceRequirements() corev1.ResourceRequirements {
	if s.sandboxResources != nil {
		return resourceRequirements(s.sandboxResources, "200m", "256Mi", "2000m", "2Gi")
	}
	reqs := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if s.sandboxPolicy != nil && s.sandboxPolicy.DefaultResources != nil {
		if cpu := s.sandboxPolicy.DefaultResources.CPU; cpu != "" {
			reqs.Limits[corev1.ResourceCPU] = quantityOrDefault(cpu, "2000m")
		}
		if mem := s.sandboxPolicy.DefaultResources.Memory; mem != "" {
			reqs.Limits[corev1.ResourceMemory] = quantityOrDefault(mem, "2Gi")
		}
	}
	if len(reqs.Limits) == 0 {
		reqs.Limits[corev1.ResourceCPU] = resource.MustParse("2000m")
		reqs.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
		reqs.Requests[corev1.ResourceCPU] = resource.MustParse("200m")
		reqs.Requests[corev1.ResourceMemory] = resource.MustParse("256Mi")
	}
	return reqs
}

func resourceRequirements(configured *model.SandboxResources, requestCPU, requestMemory, limitCPU, limitMemory string) corev1.ResourceRequirements {
	requirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(requestCPU), corev1.ResourceMemory: resource.MustParse(requestMemory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(limitCPU), corev1.ResourceMemory: resource.MustParse(limitMemory),
		},
	}
	if configured == nil {
		return requirements
	}
	if configured.CPU != "" {
		quantity := quantityOrDefault(configured.CPU, requestCPU)
		requirements.Requests[corev1.ResourceCPU] = quantity
		requirements.Limits[corev1.ResourceCPU] = quantity
	}
	if configured.Memory != "" {
		quantity := quantityOrDefault(configured.Memory, requestMemory)
		requirements.Requests[corev1.ResourceMemory] = quantity
		requirements.Limits[corev1.ResourceMemory] = quantity
	}
	return requirements
}

func quantityOrDefault(value, fallback string) resource.Quantity {
	quantity, err := resource.ParseQuantity(value)
	if err == nil && !quantity.IsZero() && quantity.Sign() > 0 {
		return quantity
	}
	slog.Error("invalid Kubernetes resource quantity; using safe default", "value", value, "default", fallback, "error", err)
	return resource.MustParse(fallback)
}

// ─── K8s Config ─────────────────────────────────────────

func buildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	switch {
	case kubeconfigPath != "":
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	case os.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return rest.InClusterConfig()
	default:
		home, _ := os.UserHomeDir()
		if home != "" {
			p := filepath.Join(home, ".kube", "config")
			if _, err := os.Stat(p); err == nil {
				return clientcmd.BuildConfigFromFlags("", p)
			}
		}
		return nil, fmt.Errorf("no kubeconfig found: set KUBECONFIG or ensure in-cluster config")
	}
}

func (s *KubernetesResourceManager) Shutdown(ctx context.Context) error {
	slog.Info("kubernetes rm shutdown",
		"runtime_mode", s.runtimeMode,
		"runtime_cluster_id", s.runtimeClusterID,
		"delete_on_shutdown", s.deleteOnShutdown,
		"tm_replicas", s.minTMs,
		"sandbox_replicas", s.sandboxMinReplicas,
	)
	s.cancel()
	if s.deleteOnShutdown {
		if err := s.kubeClient.AppsV1().Deployments(s.namespace).Delete(ctx, s.deployName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			slog.Warn("kubernetes rm tm shutdown delete failed", "deployment", s.deployName, "namespace", s.namespace, "err", err)
		}
		if s.sandboxEnabled {
			if err := s.kubeClient.AppsV1().Deployments(s.namespace).Delete(ctx, s.sandboxDeployName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				slog.Warn("kubernetes rm sandbox shutdown delete failed", "deployment", s.sandboxDeployName, "namespace", s.namespace, "err", err)
			}
		}
		return nil
	}
	if err := s.scaleDeployment(ctx, int32(s.minTMs)); err != nil {
		slog.Warn("kubernetes rm tm shutdown scale failed", "err", err)
	}
	if s.sandboxEnabled {
		if err := s.scaleSandboxDeployment(ctx, int32(s.sandboxMinReplicas)); err != nil {
			slog.Warn("kubernetes rm sandbox shutdown scale failed", "err", err)
		}
	}
	return nil
}

// ─── Cache persistence for JM failover ─────────────────

func cacheKey(namespace, deployName string) string {
	return fmt.Sprintf("flowgent:rm:%s:%s", namespace, deployName)
}

func (s *KubernetesResourceManager) persist(ctx context.Context) {
	if s.cache == nil {
		return
	}
	state := RMState{
		CurrentTMs:             atomic.LoadInt32(&s.currentTMs),
		SlotsPerTM:             s.slotsPerTM,
		MinTMs:                 s.minTMs,
		MaxTMs:                 s.maxTMs,
		PendingPlans:           atomic.LoadInt64(&s.pendingPlans),
		SandboxReplicas:        atomic.LoadInt32(&s.sandboxCurrentReplicas),
		SandboxPendingTriggers: atomic.LoadInt64(&s.sandboxPendingTriggers),
	}
	s.mu.Lock()
	state.LastActivity = s.lastActivity
	s.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		slog.Warn("kubernetes rm: marshal state for cache", "err", err)
		return
	}
	if err := s.cache.Set(ctx, cacheKey(s.namespace, s.deployName), data, 30*time.Second); err != nil {
		slog.Debug("kubernetes rm: cache set failed", "err", err)
	}
}

func (s *KubernetesResourceManager) restore(ctx context.Context) {
	data, err := s.cache.Get(ctx, cacheKey(s.namespace, s.deployName))
	if err != nil || data == nil {
		return
	}
	var state RMState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("kubernetes rm: unmarshal cached state", "err", err)
		return
	}
	atomic.StoreInt32(&s.currentTMs, state.CurrentTMs)
	atomic.StoreInt32(&s.sandboxCurrentReplicas, state.SandboxReplicas)
	atomic.StoreInt64(&s.sandboxPendingTriggers, state.SandboxPendingTriggers)
	s.mu.Lock()
	if !state.LastActivity.IsZero() {
		s.lastActivity = state.LastActivity
	}
	s.mu.Unlock()
	atomic.StoreInt64(&s.pendingPlans, state.PendingPlans)
	slog.Info("kubernetes rm: restored state from cache",
		"current_tms", state.CurrentTMs, "sandbox_replicas", state.SandboxReplicas, "last_activity", state.LastActivity)
}

// ─── Exported helpers for integration tests ──────────────

func (s *KubernetesResourceManager) InitForTest(kubeClient kubernetes.Interface, namespace, deployName string) {
	s.kubeClient = kubeClient
	s.namespace = namespace
	s.deployName = deployName
	s.slotsPerTM = 4
	s.minTMs = 0
	s.maxTMs = 3
	s.currentTMs = 0
	s.idleTimeout = 5 * time.Minute
	s.planTimeout = 5 * time.Minute
}

func (s *KubernetesResourceManager) SetCtx(ctx context.Context, cancel context.CancelFunc) {
	s.ctx = ctx
	s.cancel = cancel
}

func (s *KubernetesResourceManager) EnsureDeployment(ctx context.Context) error {
	return s.ensureDeployment(ctx)
}

func (s *KubernetesResourceManager) ScaleDeployment(ctx context.Context, replicas int32) error {
	return s.scaleDeployment(ctx, replicas)
}

func defaultHostPath(host, fallback string) string {
	if host != "" {
		return host
	}
	return fallback
}

func hostPathPtr(t corev1.HostPathType) *corev1.HostPathType {
	return &t
}

func hostPathDirOrCreatePtr() *corev1.HostPathType {
	t := corev1.HostPathDirectoryOrCreate
	return &t
}

func defaultIfEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
