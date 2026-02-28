package jobmanager

import (
	"context"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/engine/scheduler"
	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/src/model"
)

// JobManager is the singleton JobManager (like Flink's Dispatcher in session mode).
// It receives agentflow run submissions and spawns a JobMaster per run.
type JobManager struct {
	store  engine.Store
	rm     scheduler.ResourceManager
	logger *utils.Logger
	cfg    *config.ServiceConfig
}

// NewJobManager creates the shared JobManager singleton.
// Returns an error if the RM fails validation.
func NewJobManager(store engine.Store, rm scheduler.ResourceManager, logger *utils.Logger, cfg *config.ServiceConfig) (*JobManager, error) {
	if errs := scheduler.ValidateComponents(rm, store); len(errs) > 0 {
		for _, e := range errs {
			logger.Error("component validation failed", "error", e.Error())
		}
		return nil, errs[0]
	}
	scheduler.WarnCompatibility(rm, store)
	return &JobManager{store: store, rm: rm, logger: logger, cfg: cfg}, nil
}

// Submit spawns a new JobMaster for the given run and blocks until completion.
// Each call creates an independent JobMaster with its own DAG state — safe for concurrent use.
func (m *JobManager) Submit(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	master := NewJobMaster(m.store, m.rm, m.logger, m.cfg)
	return master.Execute(ctx, run, spec)
}
