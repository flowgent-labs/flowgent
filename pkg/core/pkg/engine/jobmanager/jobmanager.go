package jobmanager

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/lock"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const runLockTTL = 2 * time.Minute

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
	state           RunStateStore
	rm              resourcemanager.ResourceManager
	logger          *utils.Logger
	cfg             *JobManagerConfig
	knowledgeClient *client.FlowgentClient
	runLock         lock.DistributedLock
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

// SetKnowledgeClient wires the knowledge client for post-run knowledge extraction.
func (m *JobManager) SetKnowledgeClient(c *client.FlowgentClient) { m.knowledgeClient = c }

// SetRunLock wires the distributed run claim lock used by StartRunPoller.
func (m *JobManager) SetRunLock(l lock.DistributedLock) { m.runLock = l }

// Submit spawns a new JobMaster for the given run and blocks until completion.
func (m *JobManager) Submit(ctx context.Context, run *entities.FlowRunInfo, spec *entities.FlowInfo) error {
	m.logger.Info("jobmanager submit",
		"run_id", run.ID,
		"agentflow_id", spec.ID,
		"priority", spec.Priority,
		"namespace_id", spec.Namespace,
		"k8s_namespace", spec.K8sNamespace,
	)

	run.Priority = spec.Priority
	run.K8sNamespace = spec.K8sNamespace
	run.Namespace = spec.Namespace

	master := NewJobMaster(m.state, m.rm, m.logger, m.cfg)
	if m.knowledgeClient != nil {
		master.SetKnowledgeWriter(m.knowledgeClient)
	}
	return master.Execute(ctx, run, spec)
}

// StartRunPoller polls for pending AgentFlowRuns via the apiserver API
// and dispatches them via the JobManager.
func StartRunPoller(ctx context.Context, api *client.FlowgentClient, namespace string,
	jm *JobManager, flows map[string]*entities.FlowInfo,
	k8sNamespace, agentFlowID string) {

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			page, err := api.ListRuns(ctx, namespace, string(entities.RunPending), k8sNamespace, agentFlowID, 1, 50)
			if err != nil {
				slog.Warn("poller ListRuns failed", "err", err)
				continue
			}
			for _, run := range page.Items {
				if run.Status != entities.RunPending {
					continue
				}
				if k8sNamespace != "" && run.K8sNamespace != k8sNamespace {
					continue
				}
				spec := flows[run.AgentFlowID]
				if spec == nil {
					if apiSpec, err := api.GetFlow(ctx, namespace, run.AgentFlowID); err == nil && apiSpec != nil {
						spec = apiSpec
						slog.Debug("poller loaded flow spec via apiserver", "agentFlowID", run.AgentFlowID, "nodes", len(spec.Nodes))
					}
				}
				if spec == nil {
					continue
				}
				slog.Debug("poller dispatch run", "run", run.ID[:8], "flow", run.AgentFlowID, "priority", run.Priority)
				runCtx, release, claimed, err := jm.claimRun(ctx, run.ID)
				if err != nil {
					slog.Warn("poller run lock failed", "run", run.ID, "err", err)
					continue
				}
				if !claimed {
					slog.Debug("poller skip run already claimed", "run", run.ID, "flow", run.AgentFlowID)
					continue
				}
				go func(r *entities.FlowRunInfo, sp *entities.FlowInfo, runCtx context.Context, release func()) {
					defer release()
					_ = jm.Submit(runCtx, r, sp)
				}(run, spec, runCtx, release)
			}
		}
	}
}

func (m *JobManager) claimRun(ctx context.Context, runID string) (context.Context, func(), bool, error) {
	if m.runLock == nil {
		return ctx, func() {}, true, nil
	}
	key := runLockKey(runID)
	ok, err := m.runLock.TryLock(ctx, key, runLockTTL)
	if err != nil || !ok {
		return ctx, func() {}, ok, err
	}

	lockCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(runLockTTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-lockCtx.Done():
				return
			case <-ticker.C:
				if err := m.runLock.Extend(lockCtx, key, runLockTTL); err != nil {
					m.logger.Warn("jobmanager run lock lost", "run", runID, "error", err)
					cancel()
					return
				}
			}
		}
	}()

	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			<-done
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			if err := m.runLock.Release(releaseCtx, key); err != nil {
				m.logger.Warn("jobmanager run lock release failed", "run", runID, "error", err)
			}
		})
	}
	return lockCtx, release, true, nil
}

func runLockKey(runID string) string {
	return "flow-run:" + runID
}
