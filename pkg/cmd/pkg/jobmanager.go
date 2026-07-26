package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func StartJobManager(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
	}
	return startJobManager(cfgPath)
}

func StopJobManager(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartJobManager(cfgPath, pidFile string) error {
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

	// ── OTEL Tracing ──
	if svcCfg.Mgmt.OTEL.Enabled && svcCfg.Mgmt.OTEL.Endpoint != "" {
		otelCfg := &tracing.OTELConfig{
			Enabled:    svcCfg.Mgmt.OTEL.Enabled,
			Endpoint:   svcCfg.Mgmt.OTEL.Endpoint,
			Protocol:   svcCfg.Mgmt.OTEL.Protocol,
			Timeout:    svcCfg.Mgmt.OTEL.Timeout,
			SampleRate: svcCfg.Mgmt.OTEL.SampleRate,
		}
		if provider, err := tracing.NewProvider(context.Background(), "flowgent-jobmanager", "1.0", otelCfg, nil); err != nil {
			slog.Warn("OTEL tracer provider init failed, tracing disabled", "error", err)
		} else {
			defer provider.Shutdown(context.Background())
			slog.Info("OTEL tracing enabled", "endpoint", svcCfg.Mgmt.OTEL.Endpoint)
		}
	}

	q := messager.NewQueueFromConfig(svcCfg, jmID)
	defer q.Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Runtime.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	stateClient := &client.RunStateClient{Client: apiClient, Tenant: tenant}
	taskClient := &client.TaskStateClient{Client: apiClient, Tenant: tenant}
	humanClient := &client.HumanApprovalClient{Client: apiClient}

	var rm resourcemanager.ResourceManager
	jmNamespace := svcCfg.Runtime.Namespace

	k8sNamespace := svcCfg.Runtime.Namespace
	if k8sNamespace == "" {
		k8sNamespace = "default"
	}
	tmDeploy := svcCfg.Runtime.TMDeploy
	if tmDeploy == "" {
		tmDeploy = "flowgent-taskmanager"
	}
	tmImage := svcCfg.Runtime.TMImage
	if tmImage == "" {
		tmImage = svcCfg.Runtime.JMImage
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderKubernetes, SlotsPerTM: 4, MinTMs: 2, MaxTMs: 10,
			K8sNamespace:      k8sNamespace,
			K8sDeploymentName: tmDeploy,
			TMImage:           tmImage,
			TaskState:         taskClient,
			ApprovalInfo:     humanClient,
			Logger:            logger, Messager: q,
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

	flows := make(map[string]*entities.FlowInfo)
	if agentFlowID != "" {
		apiFlows, err := apiClient.ListFlows(context.Background(), tenant)
		if err == nil {
			var bestVer int64
			for i := range apiFlows {
				var spec entities.FlowInfo
				if json.Unmarshal(apiFlows[i].Definition, &spec) == nil && spec.ID == agentFlowID {
					if apiFlows[i].Version > bestVer {
						flows[spec.ID] = &spec
						bestVer = apiFlows[i].Version
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

	slog.Debug("jobmanager loaded flows", "count", len(flows), "agentFlowID", agentFlowID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go jobmanager.StartRunPoller(ctx, apiClient, tenant, jm, flows, jmNamespace, agentFlowID)
	slog.Info("JobManager started", "scheduler", rm.Provider(), "namespace", jmNamespace, "agentFlow", agentFlowID)
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
