package resourcemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flowgent-labs/flowgent/cache/src"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/model/src"
	messaging "github.com/flowgent-labs/flowgent/messaging/src"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// RMTMState is the persisted state of TM scaling for JM failover.
type RMTMState struct {
	CurrentTMs   int32     `json:"current_tms"`
	SlotsPerTM   int       `json:"slots_per_tm"`
	MinTMs       int       `json:"min_tms"`
	MaxTMs       int       `json:"max_tms"`
	PendingPlans int64     `json:"pending_plans"`
	LastActivity time.Time `json:"last_activity"`
}

// KubernetesResourceManager dispatches plans to TM pods via MQTT with elastic
// scaling. In session mode (autoScale=false), TMs are admin-managed and scaling
// is skipped. In application mode (autoScale=true), the JM auto-scales TMs.
//
// State is persisted to cache so that on JM failover the new JM can restore
// the current TM replica count and slot allocation without querying K8s.
type KubernetesResourceManager struct {
	q           messaging.Messager
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
	autoScale   bool

	mu           sync.Mutex
	pendingPlans int64
	lastActivity time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

func NewKubernetesResourceManager(cfg *ResourceManagerConfig) (*KubernetesResourceManager, error) {
	if cfg.SlotsPerTM <= 0 {
		cfg.SlotsPerTM = 4
	}
	if cfg.MinTMs <= 0 {
		cfg.MinTMs = 2
	}
	if cfg.MaxTMs <= 0 {
		cfg.MaxTMs = 10
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.PlanTimeout <= 0 {
		cfg.PlanTimeout = 5 * time.Minute
	}
	if cfg.K8sNamespace == "" {
		cfg.K8sNamespace = "default"
	}
	if cfg.K8sDeploymentName == "" {
		cfg.K8sDeploymentName = "flowgent-taskmanager"
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
		q:           nil, // set via SetQueue
		cache:       cfg.Cache,
		namespace:   cfg.K8sNamespace,
		deployName:  cfg.K8sDeploymentName,
		kubeClient:  clientset,
		autoScale:   cfg.AutoScale,
		slotsPerTM:  cfg.SlotsPerTM,
		minTMs:      cfg.MinTMs,
		maxTMs:      cfg.MaxTMs,
		currentTMs:  int32(cfg.MinTMs),
		idleTimeout: cfg.IdleTimeout,
		planTimeout: cfg.PlanTimeout,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Restore TM state from cache (JM failover recovery).
	if cfg.Cache != nil {
		rm.restoreFromCache(ctx)
	}

	if err := rm.ensureDeployment(ctx); err != nil {
		slog.Warn("kubernetes rm: deployment check failed (will retry in loop)", "err", err)
	}

	slog.Info("kubernetes rm: scaling deployment to min replicas",
		"deployment", rm.deployName, "namespace", rm.namespace, "replicas", rm.minTMs)
	if err := rm.scaleDeployment(ctx, int32(rm.minTMs)); err != nil {
		slog.Warn("kubernetes rm: initial scale failed", "err", err)
	} else {
		rm.persistToCache(ctx)
	}

	return rm, nil
}

func (s *KubernetesResourceManager) SetQueue(q messaging.Messager) { s.q = q }
func (s *KubernetesResourceManager) Provider() engine.Provider {
	return engine.ProviderKubernetes
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

func (s *KubernetesResourceManager) Schedule(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error) {
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

	payload, _ := json.Marshal(plan)
	if err := s.q.Publish(ctx, messaging.TopicExec, &messaging.Message{
		ID: plan.PlanID,
		Headers: map[string]string{
			"task_run_id": plan.AgentFlowRunID,
			"node_id":     plan.NodeID,
		},
		Payload: payload,
	}); err != nil {
		return nil, fmt.Errorf("kubernetes rm publish: %w", err)
	}
	return &model.TaskResult{Output: map[string]any{"dispatched": true}}, nil
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
	if !s.autoScale {
		return
	}
	pending := atomic.LoadInt64(&s.pendingPlans)
	currentTMs := int(atomic.LoadInt32(&s.currentTMs))
	currentSlots := currentTMs * s.slotsPerTM

	s.mu.Lock()
	idleDuration := time.Since(s.lastActivity)
	s.mu.Unlock()

	if pending > int64(currentSlots) {
		neededTMs := int((pending + int64(s.slotsPerTM) - 1) / int64(s.slotsPerTM))
		if neededTMs > s.maxTMs {
			neededTMs = s.maxTMs
		}
		if neededTMs > currentTMs {
			slog.Info("kubernetes rm scale up",
				"pending", pending, "current_tms", currentTMs, "target_tms", neededTMs)
			if err := s.scaleDeployment(ctx, int32(neededTMs)); err != nil {
				slog.Error("kubernetes rm scale up failed", "err", err)
				return
			}
			atomic.StoreInt32(&s.currentTMs, int32(neededTMs))
			s.persistToCache(ctx)
		}
		return
	}

	if idleDuration > s.idleTimeout && currentTMs > s.minTMs {
		targetTMs := currentTMs - 1
		if targetTMs < s.minTMs {
			targetTMs = s.minTMs
		}
		slog.Info("kubernetes rm scale down",
			"idle", idleDuration.Round(time.Second), "current_tms", currentTMs, "target_tms", targetTMs)
		if err := s.scaleDeployment(ctx, int32(targetTMs)); err != nil {
			slog.Error("kubernetes rm scale down failed", "err", err)
			return
		}
		atomic.StoreInt32(&s.currentTMs, int32(targetTMs))
		s.persistToCache(ctx)
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
		return fmt.Errorf("scale deployment %s/%s to %d: %w", s.namespace, s.deployName, replicas, err)
	}
	return nil
}

func (s *KubernetesResourceManager) ensureDeployment(ctx context.Context) error {
	_, err := s.kubeClient.AppsV1().Deployments(s.namespace).
		Get(ctx, s.deployName, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	replicas := int32(s.minTMs)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: s.deployName, Namespace: s.namespace,
			Labels: map[string]string{"app.kubernetes.io/component": "taskmanager", "flowgent/role": "worker"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"flowgent/role": "worker"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"flowgent/role": "worker"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "taskmanager",
						Image:           "localhost/flowgent/taskmanager:latest",
						ImagePullPolicy: corev1.PullNever,
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT_MQTT_BROKER", Value: getEnvOrDefault("FLOWGENT_MQTT_BROKER", "tcp://127.0.0.1:1883")},
							{Name: "FLOWGENT_DATABASE_URL", Value: os.Getenv("FLOWGENT_DATABASE_URL")},
						},
						Command: []string{"/app/flowgent", "taskmanager", "start"},
					}},
				},
			},
		},
	}
	_, err = s.kubeClient.AppsV1().Deployments(s.namespace).Create(ctx, deploy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create tm deployment: %w", err)
	}
	slog.Info("kubernetes rm: created tm deployment", "name", s.deployName, "replicas", replicas)
	return nil
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
	slog.Info("kubernetes rm shutdown, scaling to min", "replicas", s.minTMs)
	s.cancel()
	if err := s.scaleDeployment(ctx, int32(s.minTMs)); err != nil {
		slog.Warn("kubernetes rm shutdown scale failed", "err", err)
	}
	return nil
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ─── Cache persistence for JM failover ─────────────────

func cacheKey(namespace, deployName string) string {
	return fmt.Sprintf("flowgent:rm:%s:%s", namespace, deployName)
}

func (s *KubernetesResourceManager) persistToCache(ctx context.Context) {
	if s.cache == nil {
		return
	}
	state := RMTMState{
		CurrentTMs:   atomic.LoadInt32(&s.currentTMs),
		SlotsPerTM:   s.slotsPerTM,
		MinTMs:       s.minTMs,
		MaxTMs:       s.maxTMs,
		PendingPlans: atomic.LoadInt64(&s.pendingPlans),
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

func (s *KubernetesResourceManager) restoreFromCache(ctx context.Context) {
	data, err := s.cache.Get(ctx, cacheKey(s.namespace, s.deployName))
	if err != nil || data == nil {
		return
	}
	var state RMTMState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("kubernetes rm: unmarshal cached state", "err", err)
		return
	}
	atomic.StoreInt32(&s.currentTMs, state.CurrentTMs)
	s.mu.Lock()
	if !state.LastActivity.IsZero() {
		s.lastActivity = state.LastActivity
	}
	s.mu.Unlock()
	atomic.StoreInt64(&s.pendingPlans, state.PendingPlans)
	slog.Info("kubernetes rm: restored state from cache",
		"current_tms", state.CurrentTMs, "last_activity", state.LastActivity)
}

// ─── Exported helpers for integration tests ──────────────

func (s *KubernetesResourceManager) InitForTest(kubeClient kubernetes.Interface, namespace, deployName string) {
	s.kubeClient = kubeClient
	s.namespace = namespace
	s.deployName = deployName
	s.slotsPerTM = 4
	s.minTMs = 1
	s.maxTMs = 3
	s.currentTMs = 1
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
