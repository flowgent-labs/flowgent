package jobmanager

import (
	"context"
	"log"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// startRunPoller polls for pending AgentFlowRuns via the apiserver API
// and dispatches them via the JobManager.
func startRunPoller(ctx context.Context, api *client.FlowgentClient, tenant string,
	jm *jobmanager.JobManager, flows map[string]*entities.AgentFlowInfo,
	namespace, agentFlowID string) {

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			page, err := api.ListRuns(ctx, tenant, string(entities.RunPending), namespace, agentFlowID, 1, 50)
			if err != nil {
				log.Printf("[poller] ListRuns error: %v", err)
				continue
			}
			for _, run := range page.Items {
				if run.Status != entities.RunPending {
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
					if apiSpec, err := api.GetFlow(ctx, tenant, run.AgentFlowID); err == nil && apiSpec != nil {
						spec = apiSpec
						log.Printf("[poller] loaded flow spec via apiserver: %s (nodes=%d)", run.AgentFlowID, len(spec.Nodes))
					}
				}
				if spec == nil {
					continue
				}
				log.Printf("[poller] dispatch run=%s flow=%s priority=%s", run.ID[:8], run.AgentFlowID, run.Priority)
				go func(r *entities.FlowRunInfo, sp *entities.AgentFlowInfo) {
					_ = jm.Submit(ctx, r, sp)
				}(run, spec)
			}
		}
	}
}
