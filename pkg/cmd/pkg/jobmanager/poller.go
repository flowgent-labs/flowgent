package jobmanager

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/core/src/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src/agentflow"
	"github.com/flowgent-labs/flowgent/store/src/flowrun"
)

// startRunPoller polls for pending AgentFlowRuns and dispatches them via the JobManager.
func startRunPoller(ctx context.Context, s engine.Store, jm *jobmanager.JobManager,
	flows map[string]*model.AgentFlowSpec, namespace, agentFlowID string) {
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

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			page, _ := frStore.Select(ctx, model.PageRequest{Page: 1, Size: 50})
			runs := page.Items
			for _, run := range runs {
				if run.Status != model.RunPending {
					continue
				}
				if namespace == "" && run.Namespace != "" {
					continue
				}
				if namespace != "" && run.Namespace != namespace {
					continue
				}
				spec := flows[run.AgentFlowID]
				if spec == nil {
					if dbSpec, err := afStore.GetSpec(ctx, run.AgentFlowID); err == nil && dbSpec != nil {
						spec = dbSpec
						log.Printf("[poller] loaded flow spec from DB: %s (nodes=%d)", run.AgentFlowID, len(spec.Nodes))
					}
				}
				if spec == nil {
					continue
				}
				log.Printf("[poller] dispatch run=%s flow=%s priority=%s", run.ID[:8], run.AgentFlowID, run.Priority)
				go func(r *model.AgentFlowRun, sp *model.AgentFlowSpec) {
					_ = jm.Submit(ctx, r, sp)
				}(run, spec)
			}
		}
	}
}
