package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/lock"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
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

	defer startOTELTracing(svcCfg, "flowgent-jobmanager")()

	q := messager.NewQueueFromConfig(svcCfg, jmID)
	defer q.Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	namespace := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}

	agentFlowID := svcCfg.Runtime.AgentFlowID
	agentFlowRunID := svcCfg.Runtime.AgentFlowRunID
	runtimeMode, err := runtimeModeFromConfig(svcCfg)
	if err != nil {
		return err
	}
	clusterID := runtimeClusterID(svcCfg, runtimeMode, agentFlowRunID)
	if clusterID == "" {
		return fmt.Errorf("runtime.runtime_cluster_id is required")
	}
	svcCfg.Runtime.RuntimeClusterID = clusterID

	stateClient := &client.RunStateClient{Client: apiClient, Namespace: namespace}
	taskClient := &client.TaskStateClient{Client: apiClient, Namespace: namespace}
	humanClient := &client.HumanApprovalClient{Client: apiClient, Namespace: namespace}

	runtimeNamespace := os.Getenv("POD_NAMESPACE")
	if runtimeNamespace == "" {
		runtimeNamespace = svcCfg.Runtime.K8sNamespace
	}
	if runtimeNamespace == "" {
		runtimeNamespace = "default"
	}

	tmDeploy := svcCfg.Runtime.TMDeploy
	if tmDeploy == "" {
		tmDeploy = resourceid.KubernetesName("flowgent-taskmanager", namespace, clusterID)
	}
	sandboxDeploy := svcCfg.Runtime.SandboxDeploy
	if sandboxDeploy == "" {
		sandboxDeploy = resourceid.KubernetesName("flowgent-sandbox", namespace, clusterID)
	}
	slotsPerTM := runtimeTMSlots(svcCfg, runtimeMode)
	tmReplicas := runtimeTMReplicas(svcCfg, runtimeMode)
	tmImage := svcCfg.Runtime.TMImage
	if tmImage == "" {
		tmImage = svcCfg.Runtime.JMImage
	}
	sandboxMin, sandboxMax, sandboxSlots, sandboxResources := runtimeSandboxConfig(svcCfg, runtimeMode)

	postgresDSN := postgresDSNFromConfig(svcCfg)
	var rm resourcemanager.ResourceManager
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, err = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider:                 engine.ProviderKubernetes,
			SlotsPerTM:               slotsPerTM,
			MinTMs:                   tmReplicas,
			MaxTMs:                   tmReplicas,
			K8sNamespace:             runtimeNamespace,
			K8sDeploymentName:        tmDeploy,
			ConfigMapName:            runtimeConfigMapName(svcCfg),
			TMImage:                  tmImage,
			TMResources:              runtimeTMResources(svcCfg, runtimeMode),
			TaskState:                taskClient,
			ApprovalInfo:             humanClient,
			Logger:                   logger,
			Messager:                 q,
			MQTTBroker:               svcCfg.Messager.MQTT.Broker,
			PostgresDSN:              postgresDSN,
			APIServerURL:             svcCfg.Runtime.APIServerURL,
			Namespace:                namespace,
			CredentialEnvSecret:      svcCfg.Runtime.CredentialEnvSecret,
			OwnerNamespaceID:         namespace,
			RuntimeClusterID:         clusterID,
			OwnerFlowID:              agentFlowID,
			OwnerRunID:               agentFlowRunID,
			RuntimeMode:              runtimeMode,
			OwnerJobManagerName:      jobManagerDeploymentName(namespace, agentFlowID, agentFlowRunID),
			OwnerJobManagerNamespace: runtimeNamespace,
			ResourceOwner:            svcCfg.Runtime.ResourceOwner,
			DeleteOnShutdown:         runtimeMode == entities.RuntimeModeApplication,
			SandboxWorkspace:         svcCfg.Sandbox.Workspace,
			SandboxHostWorkspace:     svcCfg.Sandbox.HostWorkspace,
			SandboxEnabled:           svcCfg.Sandbox.Deployment.Enabled,
			SandboxImage:             svcCfg.Sandbox.Deployment.Image,
			SandboxDeploymentName:    sandboxDeploy,
			SandboxMinReplicas:       sandboxMin,
			SandboxMaxReplicas:       sandboxMax,
			SandboxSlotsPerPod:       sandboxSlots,
			SandboxResources:         sandboxResources,
			SandboxPolicy:            svcCfg.Sandbox.Policy,
		})
	} else {
		rm, err = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider:         engine.ProviderStandalone,
			PoolSize:         10,
			TaskState:        taskClient,
			ApprovalInfo:     humanClient,
			Logger:           logger,
			Messager:         q,
			APIServerURL:     svcCfg.Runtime.APIServerURL,
			Namespace:        namespace,
			RuntimeClusterID: clusterID,
		})
	}
	if err != nil {
		return fmt.Errorf("create resource manager: %w", err)
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
			for i := range apiFlows {
				if apiFlows[i].ID == agentFlowID {
					flows[apiFlows[i].ID] = &apiFlows[i]
				}
			}
		}
	}

	slog.Debug("jobmanager loaded flows", "count", len(flows), "agentFlowID", agentFlowID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go jobmanager.StartRunPoller(ctx, apiClient, namespace, jm, flows, jobmanager.RunPollerConfig{
		RuntimeNamespace: runtimeNamespace,
		AgentFlowID:      agentFlowID,
		AgentFlowRunID:   agentFlowRunID,
		RuntimeMode:      runtimeMode,
	})
	slog.Info("JobManager started",
		"scheduler", rm.Provider(),
		"namespace", runtimeNamespace,
		"agentFlow", agentFlowID,
		"run", agentFlowRunID,
		"runtime_mode", runtimeMode,
		"runtime_cluster_id", clusterID,
	)
	utils.WaitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

func runtimeModeFromConfig(cfg *config.FlowgentConfig) (entities.RuntimeMode, error) {
	mode := entities.RuntimeMode(strings.TrimSpace(cfg.Runtime.Mode))
	if mode == "" {
		return "", fmt.Errorf("runtime.mode is required")
	}
	if err := entities.ValidateRuntimeMode(mode); err != nil {
		return "", err
	}
	return mode, nil
}

func runtimeClusterID(cfg *config.FlowgentConfig, mode entities.RuntimeMode, runID string) string {
	if cfg.Runtime.RuntimeClusterID != "" {
		return cfg.Runtime.RuntimeClusterID
	}
	if mode == entities.RuntimeModeSession {
		if cfg.Runtime.Session.ClusterID != "" {
			return cfg.Runtime.Session.ClusterID
		}
		if cfg.Runtime.SessionClusterID != "" {
			return cfg.Runtime.SessionClusterID
		}
		return "session"
	}
	if runID == "" {
		return ""
	}
	return resourceid.KubernetesName("app", runID)
}

func runtimeConfigMapName(cfg *config.FlowgentConfig) string {
	if cfg.Runtime.JMConfigMap != "" {
		return cfg.Runtime.JMConfigMap
	}
	return "flowgent-config"
}

func runtimeTMReplicas(cfg *config.FlowgentConfig, mode entities.RuntimeMode) int {
	if mode == entities.RuntimeModeSession && cfg.Runtime.Session.TaskManager.Replicas > 0 {
		return cfg.Runtime.Session.TaskManager.Replicas
	}
	if mode == entities.RuntimeModeApplication && cfg.Runtime.Application.TaskManager.Replicas > 0 {
		return cfg.Runtime.Application.TaskManager.Replicas
	}
	if cfg.Runtime.TMReplicas > 0 {
		return cfg.Runtime.TMReplicas
	}
	return 1
}

func runtimeTMSlots(cfg *config.FlowgentConfig, mode entities.RuntimeMode) int {
	if mode == entities.RuntimeModeSession && cfg.Runtime.Session.TaskManager.Slots > 0 {
		return cfg.Runtime.Session.TaskManager.Slots
	}
	if mode == entities.RuntimeModeApplication && cfg.Runtime.Application.TaskManager.Slots > 0 {
		return cfg.Runtime.Application.TaskManager.Slots
	}
	if cfg.Runtime.TMSlots > 0 {
		return cfg.Runtime.TMSlots
	}
	return 4
}

func runtimeTMResources(cfg *config.FlowgentConfig, mode entities.RuntimeMode) *model.SandboxResources {
	if mode == entities.RuntimeModeSession && cfg.Runtime.Session.TaskManager.Resources != nil {
		return cfg.Runtime.Session.TaskManager.Resources
	}
	if mode == entities.RuntimeModeApplication && cfg.Runtime.Application.TaskManager.Resources != nil {
		return cfg.Runtime.Application.TaskManager.Resources
	}
	return nil
}

func runtimeSandboxConfig(cfg *config.FlowgentConfig, mode entities.RuntimeMode) (int, int, int, *model.SandboxResources) {
	minReplicas := cfg.Sandbox.Deployment.MinReplicas
	maxReplicas := cfg.Sandbox.Deployment.MaxReplicas
	slots := cfg.Sandbox.Deployment.SlotsPerPod
	resources := cfg.Sandbox.Deployment.Resources
	if mode == entities.RuntimeModeSession {
		if cfg.Runtime.Session.Sandbox.MinReplicas > 0 {
			minReplicas = cfg.Runtime.Session.Sandbox.MinReplicas
		}
		if cfg.Runtime.Session.Sandbox.MaxReplicas > 0 {
			maxReplicas = cfg.Runtime.Session.Sandbox.MaxReplicas
		}
		if cfg.Runtime.Session.Sandbox.Slots > 0 {
			slots = cfg.Runtime.Session.Sandbox.Slots
		}
		if cfg.Runtime.Session.Sandbox.Resources != nil {
			resources = cfg.Runtime.Session.Sandbox.Resources
		}
	} else {
		if cfg.Runtime.Application.Sandbox.MinReplicas > 0 {
			minReplicas = cfg.Runtime.Application.Sandbox.MinReplicas
		}
		if cfg.Runtime.Application.Sandbox.MaxReplicas > 0 {
			maxReplicas = cfg.Runtime.Application.Sandbox.MaxReplicas
		}
		if cfg.Runtime.Application.Sandbox.Slots > 0 {
			slots = cfg.Runtime.Application.Sandbox.Slots
		}
		if cfg.Runtime.Application.Sandbox.Resources != nil {
			resources = cfg.Runtime.Application.Sandbox.Resources
		}
	}
	if maxReplicas < minReplicas {
		maxReplicas = minReplicas
	}
	if slots <= 0 {
		slots = 1
	}
	return minReplicas, maxReplicas, slots, resources
}

func jobManagerDeploymentName(namespaceID, flowID, runID string) string {
	if flowID == "" {
		return ""
	}
	if namespaceID == "" {
		namespaceID = "default"
	}
	if runID == "" {
		return resourceid.KubernetesName("flowgent-jobmanager", namespaceID, flowID)
	}
	return resourceid.KubernetesName("flowgent-jobmanager", namespaceID, flowID, runID)
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
		RuntimeClusterID:     cfg.Runtime.RuntimeClusterID,
	}
}
