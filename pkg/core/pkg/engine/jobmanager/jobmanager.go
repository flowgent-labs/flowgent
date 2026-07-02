package jobmanager

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// JobManagerConfig is the startup configuration for a JobManager, extracted
// from the full FlowgentConfig to decouple JM from YAML layout changes.
type JobManagerConfig struct {
	FlowExecutionTimeout time.Duration
	MaxNodeRetries       int
	MaxConcurrentFlows   int
}

// JobManager is the singleton JobManager (like Flink's Dispatcher in session mode).
// It receives agentflow run submissions and spawns a JobMaster per run.
type JobManager struct {
	state  RunStateStore
	rm     resourcemanager.ResourceManager
	logger *utils.Logger
	cfg    *JobManagerConfig
}

// NewJobManager creates the shared JobManager singleton.
func NewJobManager(state RunStateStore, rm resourcemanager.ResourceManager, logger *utils.Logger, cfg *JobManagerConfig) (*JobManager, error) {
	if errs := resourcemanager.ValidateComponents(rm); len(errs) > 0 {
		for _, e := range errs {
			logger.Error("component validation failed", "error", e.Error())
		}
		return nil, errs[0]
	}
	return &JobManager{state: state, rm: rm, logger: logger, cfg: cfg}, nil
}

// Submit spawns a new JobMaster for the given run and blocks until completion.
func (m *JobManager) Submit(ctx context.Context, run *entities.FlowRunInfo, spec *entities.AgentFlowInfo) error {
	m.logger.Info("jobmanager submit",
		"run_id", run.ID,
		"agentflow_id", spec.ID,
		"priority", spec.Priority,
		"tenant", spec.TenantID,
		"namespace", spec.Namespace,
	)

	run.Priority = spec.Priority
	run.Namespace = spec.Namespace
	run.TenantID = spec.TenantID

	master := NewJobMaster(m.state, m.rm, m.logger, m.cfg)
	return master.Execute(ctx, run, spec)
}
