// Package jobmanager provides the JobManager daemon entry point.
// The JobManager is the control plane that parses flows, builds DAGs,
// and dispatches execution plans to TaskManagers via MQTT.
package jobmanager

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/cmd/src/cmdutil"
	"github.com/flowgent-labs/flowgent/config/src/config"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/core/src/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/src/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/src/llm"
	"github.com/flowgent-labs/flowgent/core/src/mcp"
	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// Start launches the JobManager daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
	}
	return startJobManager(cfgPath)
}

// Stop stops the JobManager daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the JobManager daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
	}
	return startJobManager(cfgPath)
}

// startJobManager is the actual JobManager startup logic.
func startJobManager(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	jmID := "jm-" + cmdutil.Hostname()

	q := cmdutil.NewQueueFromConfig(svcCfg, jmID)
	defer q.Close()

	storeImpl := store.NewStoreManager(svcCfg)

	var rm resourcemanager.ResourceManager
	jmNamespace := cmdutil.EnvOr("FLOWGENT_NAMESPACE", "")
	mode := "session"
	if svcCfg != nil && svcCfg.Deployment.Mode != "" {
		mode = svcCfg.Deployment.Mode
	}
	appMode := mode == "application"

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderKubernetes, SlotsPerTM: 4, MinTMs: 2, MaxTMs: 10,
			K8sNamespace:      cmdutil.EnvOr("KUBERNETES_NAMESPACE", "default"),
			K8sDeploymentName: cmdutil.EnvOr("FLOWGENT_TM_DEPLOY", "flowgent-taskmanager"),
			Store:             storeImpl, Logger: logger, Queue: q,
			AutoScale: appMode,
		})
	}
	if rm == nil {
		var agentPtrs []*config.AgentDef
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			for i := range agents {
				agentPtrs = append(agentPtrs, &agents[i])
			}
		}
		mcpFactory := mcp.NewMcpManager()
		for _, mcpDef := range svcCfg.Orchestration.MCPs {
			if mcpDef.Enabled {
				mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
			}
		}
		mcpMap := make(map[string]engine.MCPClient)
		for _, mcpDef := range svcCfg.Orchestration.MCPs {
			if mcpDef.Enabled {
				mcpMap[mcpDef.Name] = &cmdutil.McpAdapter{Factory: mcpFactory, Name: mcpDef.Name}
			}
		}
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderStandalone, PoolSize: 10, Store: storeImpl,
			Agents: agentPtrs, MCPClients: mcpMap, LLMClient: llm.NewLlmProviderManager(&svcCfg.LLM, storeImpl),
			Logger: logger, Queue: q,
		})
	}

	jm, err := jobmanager.NewJobManager(storeImpl, rm, logger, newJobManagerConfig(svcCfg))
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}

	agentFlowID := cmdutil.EnvOr("FLOWGENT_AGENTFLOW_ID", "")

	flows := make(map[string]*model.AgentFlowSpec)
	if appMode && agentFlowID != "" {
		if svcCfg != nil && svcCfg.Orchestration.AgentFlows.Standard.Enabled {
			if dbF, dbSF, err := cmdutil.LoadAgentFlowsFromDB(context.Background(), storeImpl); err == nil {
				for i := range dbF {
					if dbF[i].ID == agentFlowID {
						flows[dbF[i].ID] = &dbF[i]
						break
					}
				}
				if sf, ok := dbSF[agentFlowID]; ok {
					flows[agentFlowID] = &sf
				}
			}
		}
	} else if svcCfg != nil {
		if f, sf, err := config.LoadAgentFlows(svcCfg, cfgPath); err == nil {
			for i := range f {
				flows[f[i].ID] = &f[i]
			}
			for k, v := range sf {
				flows[k] = &v
			}
		}
		if svcCfg.Orchestration.AgentFlows.Standard.Enabled {
			if dbF, dbSF, err := cmdutil.LoadAgentFlowsFromDB(context.Background(), storeImpl); err == nil {
				for i := range dbF {
					flows[dbF[i].ID] = &dbF[i]
				}
				for k, v := range dbSF {
					flows[k] = &v
				}
			}
		}
	}

	log.Printf("[jm] loaded %d flows (agentFlowID=%s, appMode=%v)", len(flows), agentFlowID, appMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startRunPoller(ctx, storeImpl, jm, flows, jmNamespace, agentFlowID)
	log.Printf("JobManager started (scheduler=%s, namespace=%s, agentFlow=%s, autoScale=%v)", rm.Provider(), jmNamespace, agentFlowID, appMode)
	cmdutil.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

// ─── Helpers ───────────────────────────────────────────────────

func newJobManagerConfig(cfg *config.FlowgentConfig) *jobmanager.JobManagerConfig {
	timeout, _ := time.ParseDuration(cfg.Orchestration.FlowExecutionTimeout)
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	return &jobmanager.JobManagerConfig{
		FlowExecutionTimeout: timeout,
		MaxNodeRetries:       cfg.Orchestration.MaxNodeRetries,
		MaxConcurrentFlows:   cfg.Orchestration.MaxConcurrentFlows,
	}
}

// startRunPoller is defined in poller.go.
