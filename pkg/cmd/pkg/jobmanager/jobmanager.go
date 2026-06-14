// Package jobmanager provides the JobManager daemon entry point.
// The JobManager is the control plane that parses flows, builds DAGs,
// and dispatches execution plans to TaskManagers via MQTT.
package jobmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// Start launches the JobManager daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startJobManager(cfgPath)
}

// Stop stops the JobManager daemon.
func Stop(pidFile string) error { return utils.StopByPID(pidFile) }

// Restart restarts the JobManager daemon.
func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startJobManager(cfgPath)
}

func startJobManager(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	jmID := "jm-" + utils.Hostname()

	q := messager.NewQueueFromConfig(svcCfg, jmID)
	defer q.Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	stateClient := &client.RunStateClient{Client: apiClient, Tenant: tenant}
	taskClient := &client.TaskStateClient{Client: apiClient, Tenant: tenant}
	humanClient := &client.HumanApprovalClient{Client: apiClient}

	var rm resourcemanager.ResourceManager
	jmNamespace := svcCfg.Runtime.Namespace
	mode := "session"
	if svcCfg != nil && svcCfg.Deployment.Mode != "" {
		mode = svcCfg.Deployment.Mode
	}
	appMode := mode == "application"

	llmLoader := &client.LlmProviderClient{Client: apiClient, Tenant: tenant}

	k8sNamespace := svcCfg.Runtime.Namespace
	if k8sNamespace == "" {
		k8sNamespace = "default"
	}
	tmDeploy := svcCfg.Runtime.TMDeploy
	if tmDeploy == "" {
		tmDeploy = "flowgent-taskmanager"
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderKubernetes, SlotsPerTM: 4, MinTMs: 2, MaxTMs: 10,
			K8sNamespace:      k8sNamespace,
			K8sDeploymentName: tmDeploy,
			TaskState:         taskClient,
			HumanApproval:     humanClient,
			Logger:            logger, Queue: q,
			AutoScale: appMode,
				MQTTBroker:  svcCfg.Messaging.MQTT.Broker,
				PostgresDSN: svcCfg.Storage.Postgres.Dsn,
		})
	}
	if rm == nil {
		var agentPtrs []*config.AgentDef
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			for i := range agents {
				agentPtrs = append(agentPtrs, &agents[i])
			}
		}
		_ = mcp.NewMcpManager() // MCPs now DB-backed, loaded at runtime
		mcpMap := make(map[string]engine.MCPClient)
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider:      engine.ProviderStandalone, PoolSize: 10,
			TaskState:     taskClient,
			HumanApproval: humanClient,
			Agents:        agentPtrs, MCPClients: mcpMap,
			LLMClient: llm.NewLlmProviderManager(llmLoader),
			Logger: logger, Queue: q,
		})
	}

	jm, err := jobmanager.NewJobManager(stateClient, rm, logger, newJobManagerConfig(svcCfg))
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}

	agentFlowID := svcCfg.Runtime.AgentFlowID

	flows := make(map[string]*model.AgentFlowSpec)
	if appMode && agentFlowID != "" {
		if svcCfg != nil && svcCfg.Orchestration.AgentFlows.Standard.Enabled {
			apiFlows, err := apiClient.ListFlows(context.Background(), tenant)
			if err == nil {
				for i := range apiFlows {
					var spec model.AgentFlowSpec
					if json.Unmarshal(apiFlows[i].Definition, &spec) == nil && spec.ID == agentFlowID {
						flows[spec.ID] = &spec
						break
					}
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
	}

	log.Printf("[jm] loaded %d flows (agentFlowID=%s, appMode=%v)", len(flows), agentFlowID, appMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startRunPoller(ctx, apiClient, tenant, jm, flows, jmNamespace, agentFlowID)
	log.Printf("JobManager started (scheduler=%s, namespace=%s, agentFlow=%s, autoScale=%v)", rm.Provider(), jmNamespace, agentFlowID, appMode)
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

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
