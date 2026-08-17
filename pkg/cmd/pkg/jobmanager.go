package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/lock"
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
	namespace := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}
	agentFlowID := svcCfg.Runtime.AgentFlowID
	poolID := svcCfg.Runtime.ResourcePoolID
	if poolID == "" && agentFlowID != "" {
		if flow, loadErr := apiClient.GetFlow(context.Background(), namespace, agentFlowID); loadErr == nil && flow != nil {
			poolID = flow.ResourcePoolID
		}
	}
	if poolID == "" {
		poolID = "default"
	}
	pool, err := apiClient.GetResourcePool(context.Background(), namespace, poolID)
	if err != nil {
		return fmt.Errorf("load resource pool %s: %w", poolID, err)
	}
	if pool == nil {
		return fmt.Errorf("resource pool %s/%s not found", namespace, poolID)
	}

	stateClient := &client.RunStateClient{Client: apiClient, Namespace: namespace}
	taskClient := &client.TaskStateClient{Client: apiClient, Namespace: namespace}
	humanClient := &client.HumanApprovalClient{Client: apiClient}

	var rm resourcemanager.ResourceManager
	runtimeNamespace := os.Getenv("POD_NAMESPACE")
	if runtimeNamespace == "" {
		runtimeNamespace = svcCfg.Runtime.K8sNamespace
	}
	if runtimeNamespace == "" {
		runtimeNamespace = "default"
	}
	jmNamespace := runtimeNamespace

	tmDeploy := svcCfg.Runtime.TMDeploy
	if tmDeploy == "" {
		tmDeploy = resourceid.KubernetesName("flowgent-taskmanager", namespace, poolID)
	}
	sandboxDeploy := resourceid.KubernetesName("flowgent-sandbox", namespace, poolID)
	slotsPerTM := pool.SlotsPerPod
	tmImage := svcCfg.Runtime.TMImage
	if tmImage == "" {
		tmImage = svcCfg.Runtime.JMImage
	}
	postgresDSN := postgresDSNFromConfig(svcCfg)
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderKubernetes, SlotsPerTM: slotsPerTM, MinTMs: pool.Replicas, MaxTMs: pool.Replicas,
			K8sNamespace:      runtimeNamespace,
			K8sDeploymentName: tmDeploy,
			TMImage:           tmImage,
			TMResources:       pool.Resources,
			PriorityClassName: pool.PriorityClassName,
			NodeSelector:      pool.NodeSelector,
			TaskState:         taskClient,
			ApprovalInfo:      humanClient,
			Logger:            logger, Messager: q,
			MQTTBroker:               svcCfg.Messager.MQTT.Broker,
			PostgresDSN:              postgresDSN,
			APIServerURL:             svcCfg.Runtime.APIServerURL,
			Namespace:                namespace,
			CredentialEnvSecret:      svcCfg.Runtime.CredentialEnvSecret,
			InternalAuthSecret:       svcCfg.Runtime.InternalAuthSecret,
			TaskManagerAuthKey:       svcCfg.Runtime.TaskManagerAuthKey,
			OwnerNamespaceID:         namespace,
			ResourcePoolID:           poolID,
			OwnerFlowID:              agentFlowID,
			OwnerJobManagerName:      jobManagerDeploymentName(namespace, agentFlowID),
			OwnerJobManagerNamespace: runtimeNamespace,
			SandboxWorkspace:         svcCfg.Sandbox.Workspace,
			SandboxHostWorkspace:     svcCfg.Sandbox.HostWorkspace,
			SandboxEnabled:           svcCfg.Sandbox.Deployment.Enabled,
			SandboxImage:             svcCfg.Sandbox.Deployment.Image,
			SandboxDeploymentName:    sandboxDeploy,
			SandboxMinReplicas:       pool.SandboxReplicas,
			SandboxMaxReplicas:       pool.SandboxReplicas,
			SandboxSlotsPerPod:       pool.SandboxSlotsPerPod,
			SandboxResources:         pool.SandboxResources,
			SandboxPolicy:            svcCfg.Sandbox.Policy,
		})
	}
	if rm == nil {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderStandalone, PoolSize: 10,
			TaskState:    taskClient,
			ApprovalInfo: humanClient,
			Logger:       logger, Messager: q,
			APIServerURL:   svcCfg.Runtime.APIServerURL,
			Namespace:      namespace,
			ResourcePoolID: poolID,
		})
	}
	defer func() {
		if err := rm.Shutdown(context.Background()); err != nil {
			slog.Warn("resource manager shutdown failed", "err", err)
		}
	}()

	jm, err := jobmanager.NewJobManager(stateClient, rm, logger, newJobManagerConfig(svcCfg))
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}
	runLock, err := newRunLock(svcCfg, postgresDSN)
	if err != nil {
		return fmt.Errorf("create jobmanager run lock: %w", err)
	}
	jm.SetRunLock(runLock)

	flows := make(map[string]*entities.FlowInfo)
	if agentFlowID != "" {
		apiFlows, err := apiClient.ListFlows(context.Background(), namespace)
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
	go jobmanager.StartRunPoller(ctx, apiClient, namespace, jm, flows, jmNamespace, agentFlowID)
	slog.Info("JobManager started", "scheduler", rm.Provider(), "namespace", jmNamespace, "agentFlow", agentFlowID)
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

func jobManagerDeploymentName(namespaceID, flowID string) string {
	if flowID == "" {
		return ""
	}
	if namespaceID == "" {
		namespaceID = "default"
	}
	return resourceid.KubernetesName("flowgent-jobmanager", namespaceID, flowID)
}

func sandboxDeploymentName(namespaceID, flowID string) string {
	if flowID == "" {
		return "flowgent-sandbox"
	}
	if namespaceID == "" {
		namespaceID = "default"
	}
	return resourceid.KubernetesName("flowgent-sandbox", namespaceID, flowID)
}

func postgresDSNFromConfig(cfg *config.FlowgentConfig) string {
	pg := cfg.Storage.Postgres
	if pg.Dsn != "" {
		return pg.Dsn
	}
	if pg.Host == "" {
		return ""
	}
	ssl := "disable"
	if pg.UseSSL {
		ssl = "require"
	}
	if pg.Port == 0 {
		pg.Port = 5432
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, ssl)
}

func newRunLock(cfg *config.FlowgentConfig, postgresDSN string) (lock.DistributedLock, error) {
	provider := strings.ToLower(cfg.Lock.Provider)
	switch provider {
	case "", "memory":
		return lock.New(lock.Config{Type: "memory"})
	case "postgres", "postgresql", "postgre":
		if postgresDSN == "" {
			return nil, fmt.Errorf("postgres lock requires storage.postgres.dsn or postgres host config")
		}
		return lock.New(lock.Config{Type: "postgres", PGConn: postgresDSN})
	default:
		return lock.New(lock.Config{Type: provider, PGConn: postgresDSN})
	}
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
