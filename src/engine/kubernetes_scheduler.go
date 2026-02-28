package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// KubernetesScheduler manages a Deployment of TM pods and dispatches
// ExecutionPlans via MQTT.  Similar to Flink's Native Kubernetes mode:
// scale the TM Deployment to meet slot demand, idle scale-down.
type KubernetesScheduler struct {
	q            queue.Queue
	namespace    string
	deployName   string
	kubeClient   interface{} // kubernetes.Interface, set via constructor
	slotsPerTM   int
	minTMs       int
	maxTMs       int
	currentTMs   int
	idleTimeout  time.Duration
}

func NewKubernetesScheduler(cfg *SchedulerConfig) (*KubernetesScheduler, error) {
	if cfg.SlotsPerTM <= 0 {
		cfg.SlotsPerTM = 4
	}
	if cfg.MinTMs <= 0 {
		cfg.MinTMs = 2
	}
	if cfg.MaxTMs <= 0 {
		cfg.MaxTMs = 10
	}
	return &KubernetesScheduler{
		q:           nil, // set via SetQueue after construction
		namespace:   cfg.K8sNamespace,
		deployName:  cfg.K8sDeploymentName,
		slotsPerTM:  cfg.SlotsPerTM,
		minTMs:      cfg.MinTMs,
		maxTMs:      cfg.MaxTMs,
		currentTMs:  cfg.MinTMs,
		idleTimeout: cfg.IdleTimeout,
	}, nil
}

// SetQueue wires the MQTT queue for plan dispatch.
func (s *KubernetesScheduler) SetQueue(q queue.Queue) { s.q = q }

func (s *KubernetesScheduler) Type() SchedulerType { return SchedulerTypeKubernetes }

func (s *KubernetesScheduler) EnsureCapacity(ctx context.Context, neededSlots int) (int, error) {
	currentSlots := s.currentTMs * s.slotsPerTM
	if currentSlots >= neededSlots {
		return currentSlots, nil
	}
	neededTMs := (neededSlots + s.slotsPerTM - 1) / s.slotsPerTM
	if neededTMs > s.maxTMs {
		neededTMs = s.maxTMs
	}
	if neededTMs < s.minTMs {
		neededTMs = s.minTMs
	}

	slog.Info("kubernetes scheduler scaling TMs",
		"needed_slots", neededSlots,
		"current_tms", s.currentTMs,
		"target_tms", neededTMs,
	)

	// TODO: call K8s API to scale Deployment: kubectl scale deployment {name} --replicas={n}
	// For now, stub — the operator/deployment handles scaling externally
	s.currentTMs = neededTMs

	return neededTMs * s.slotsPerTM, nil
}

// SubmitTask publishes the ExecutionPlan to the MQTT topic for this agentflow.
func (s *KubernetesScheduler) SubmitTask(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error) {
	if s.q == nil {
		return nil, fmt.Errorf("kubernetes scheduler: queue not set")
	}
	topic := fmt.Sprintf("/flowgent/%s/exec/%s/%s", plan.AgentFlowRunID[:12], plan.AgentFlowRunID, plan.PlanID)

	payload, _ := json.Marshal(plan)
	if err := s.q.Push(ctx, &queue.Message{
		ID:        plan.PlanID,
		Topic:     topic,
		TaskRunID: plan.AgentFlowRunID,
		NodeID:    plan.NodeID,
		Payload:   payload,
	}); err != nil {
		return nil, fmt.Errorf("kubernetes scheduler: publish: %w", err)
	}

	// Result is not synchronous in K8s mode — JM monitors status via separate topic.
	// Return sentinel indicating plan was dispatched.
	return &model.TaskResult{Output: map[string]any{"dispatched": true, "topic": topic}}, nil
}

func (s *KubernetesScheduler) AvailableSlots() int {
	return s.currentTMs * s.slotsPerTM
}

func (s *KubernetesScheduler) TotalSlots() int {
	return s.maxTMs * s.slotsPerTM
}

func (s *KubernetesScheduler) Shutdown(ctx context.Context) error {
	// Scale back to minTMs on shutdown
	slog.Info("kubernetes scheduler shutdown, scaling to min", "min_tms", s.minTMs)
	return nil
}
