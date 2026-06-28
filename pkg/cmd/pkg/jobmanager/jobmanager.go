// Package jobmanager provides the JobManager daemon entry point.
// The JobManager is the control plane that parses flows, builds DAGs,
// and dispatches execution plans to TaskManagers via MQTT.
package jobmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
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
			ApprovalInfo:     humanClient,
			Logger:            logger, Messager: q,
			AutoScale:         appMode,
			MQTTBroker:        svcCfg.Messager.MQTT.Broker,
			PostgresDSN:       svcCfg.Storage.Postgres.Dsn,
			APIServerURL:      svcCfg.Runtime.APIServerURL,
			Tenant:            tenant,
		})
	}
	if rm == nil {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider:      engine.ProviderStandalone, PoolSize: 10,
			TaskState:     taskClient,
			ApprovalInfo: humanClient,
			Logger:        logger, Messager: q,
			APIServerURL:  svcCfg.Runtime.APIServerURL,
			Tenant:        tenant,
		})
	}

	jm, err := jobmanager.NewJobManager(stateClient, rm, logger, newJobManagerConfig(svcCfg))
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}

	agentFlowID := svcCfg.Runtime.AgentFlowID

	flows := make(map[string]*entities.AgentFlowInfo)
	if appMode && agentFlowID != "" {
		apiFlows, err := apiClient.ListFlows(context.Background(), tenant)
		if err == nil {
			for i := range apiFlows {
				var spec entities.AgentFlowInfo
				if json.Unmarshal(apiFlows[i].Definition, &spec) == nil && spec.ID == agentFlowID {
					flows[spec.ID] = &spec
					break
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

	slog.Debug("jobmanager loaded flows", "count", len(flows), "agentFlowID", agentFlowID, "appMode", appMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startRunPoller(ctx, apiClient, tenant, jm, flows, jmNamespace, agentFlowID)
	slog.Info("JobManager started", "scheduler", rm.Provider(), "namespace", jmNamespace, "agentFlow", agentFlowID, "autoScale", appMode)
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
