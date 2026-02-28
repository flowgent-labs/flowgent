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
//
// Mode routing:
//   - low/medium/high → session mode (shared JM+TM pool)
//   - grade → application mode (dedicated K8s namespace + JM+TM)
func (m *JobManager) Submit(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	mode := spec.EffectiveMode()
	m.logger.Info("jobmanager submit",
		"run_id", run.ID,
		"agentflow_id", spec.ID,
		"mode", mode,
		"priority", spec.Priority,
		"tenant", spec.TenantID,
		"namespace", spec.Namespace,
	)

	// Propagate mode metadata to the run
	run.Priority = spec.Priority
	run.Namespace = spec.Namespace
	run.TenantID = spec.TenantID

	if mode == model.ModeApplication {
		m.logger.Info("application mode — dedicated cluster", "agentflow_id", spec.ID, "tenant", spec.TenantID)
		// In application mode, ensure dedicated K8s resources exist.
		// The KubernetesResourceManager handles namespace + Deployment provisioning.
		if err := m.rm.Validate(ctx); err != nil {
			m.logger.Warn("application mode: resource validation failed, falling back to session", "error", err)
		}
		// The RM's Schedule() will use the run.Namespace for K8s resource placement.
	}

	master := NewJobMaster(m.store, m.rm, m.logger, m.cfg)
	return master.Execute(ctx, run, spec)
}
