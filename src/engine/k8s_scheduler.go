package engine

import (
	"context"
	"fmt"
	"log/slog"
)

// K8sScheduler launches a Kubernetes Job (pod) per task execution.
// Currently a stub — production wiring requires a Kubernetes client and
// RBAC configuration.
//
// In Flink terms this is the Native Kubernetes mode: each task runs in
// its own pod, enabling dynamic scaling and resource isolation per node.
type K8sScheduler struct{}

func NewK8sScheduler() *K8sScheduler { return &K8sScheduler{} }

func (s *K8sScheduler) Type() SchedulerType { return SchedulerTypeK8s }

func (s *K8sScheduler) SubmitTask(ctx context.Context, submit *TaskSubmit) (*TaskResult, error) {
	slog.Info("k8s scheduler stub — SubmitTask not yet implemented",
		"cluster_id", submit.ClusterID,
		"run_id", submit.RunID,
		"node_id", submit.NodeID,
	)
	return nil, fmt.Errorf("K8sScheduler not implemented: deploy a TaskManager pod for run=%s node=%s",
		submit.RunID, submit.NodeID)
}

func (s *K8sScheduler) Close() error { return nil }
