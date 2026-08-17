// Package allinone provides the combined single-process entry point
// for local development. It starts all components in one process:
// REST API, A2A, JobManager + RunPoller, cron triggers, notifier.
package main

import (
	"context"
	"fmt"

	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	a2apkg "github.com/flowgent-labs/flowgent/a2a/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/ldap"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/oidc"
	"github.com/flowgent-labs/flowgent/api/pkg/authz"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/api/pkg/taskpayload"
	tracequery "github.com/flowgent-labs/flowgent/api/pkg/trace"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/notifier/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	flowreleasestore "github.com/flowgent-labs/flowgent/store/pkg/flowrelease"
	iamstore "github.com/flowgent-labs/flowgent/store/pkg/iam"
	resourcepoolstore "github.com/flowgent-labs/flowgent/store/pkg/resourcepool"
)

// allInOneState bundles shared dependencies for the all-in-one process.
type allInOneState struct {
	cfg         *config.FlowgentConfig
	store       store.IStore
	apiClient   *client.FlowgentClient
	namespace   string
	taskClient  *client.TaskStateClient
	humanClient *client.HumanApprovalClient
	logger      *utils.Logger
	payloads    taskpayload.ITaskPayloadProvider
}

func StartAllInOne(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	slog.Info("Flowgent all-in-one starting", "pid", os.Getpid())
	return startAllInOne(cfgPath)
}

func StopAllInOne(pidFile string) error { return utils.StopByPID(pidFile) }

func RestartAllInOne(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return StartAllInOne(cfgPath, pidFile)
}

// startAllInOne loads config, initializes shared state, then starts each component.
func startAllInOne(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	config.LogConfig(svcCfg)
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	if svcCfg.Auth.Authorization.Enabled {
		if svcCfg.Auth.Authorization.InternalTokens == nil {
			svcCfg.Auth.Authorization.InternalTokens = make(map[string]string)
		}
		token := svcCfg.Auth.Authorization.InternalTokens["allinone"]
		if token == "" || strings.Contains(token, "${") {
			token = uuid.NewString() + uuid.NewString()
			svcCfg.Auth.Authorization.InternalTokens["allinone"] = token
		}
	}

	storeImpl := store.InitStore(svcCfg)
	defer storeImpl.(interface{ Close() error }).Close()
	payloadProvider, err := taskpayload.NewProvider(context.Background(), svcCfg.Storage.Artifacts)
	if err != nil {
		return fmt.Errorf("create task payload provider: %w", err)
	}
	defer payloadProvider.Close()

	apiClient := client.NewFlowgentClientWithToken(svcCfg.Runtime.APIServerURL, svcCfg.Auth.Authorization.InternalTokens["allinone"])
	namespace := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}

	state := &allInOneState{
		cfg:         svcCfg,
		store:       storeImpl,
		apiClient:   apiClient,
		namespace:   namespace,
		taskClient:  &client.TaskStateClient{Client: apiClient, Namespace: namespace},
		humanClient: &client.HumanApprovalClient{Client: apiClient},
		logger:      logger,
		payloads:    payloadProvider,
	}

	// Load agent flows (YAML + DB)
	agentFlows, subFlows := loadFlows(state, cfgPath)
	allFlows := append(agentFlows, flattenSubflows(subFlows)...)

	// Resource Manager (standalone, starts TM in-process)
	rm := createStandaloneRM(state)

	// Notifier + WS bridge (before REST so bridge is available)
	notifSvc, wsBridge, err := startNotifier(state)
	if err != nil {
		return err
	}

	// REST API Server (with optional WS bridge)
	restSrv, flowHandler, err := startRESTServer(state, agentFlows, subFlows, wsBridge)
	if err != nil {
		return err
	}

	// Cron triggers
	startCronScheduler(allFlows, state.apiClient, state.namespace)

	// JobManager + RunPoller
	startOrchestrator(state, rm, flowHandler.AgentFlows())

	// A2A Server
	a2aSrv, err := startA2AServer(state)
	if err != nil {
		return err
	}

	// Pprof
	pprofSrv := startPprof(state)

	return waitForShutdown(state, restSrv, a2aSrv, pprofSrv, notifSvc)
}

// ─── Flow loading ─────────────────────────────────────────────────

func loadFlows(state *allInOneState, cfgPath string) ([]entities.FlowInfo, map[string]entities.FlowInfo) {
	agentFlows, subFlows, err := config.LoadAgentFlows(state.cfg, cfgPath)
	if err != nil {
		slog.Warn("load agent flows from YAML", "error", err)
	}
	if dbFlows, dbSubFlows, dberr := flow.LoadFromDB(context.Background(), state.store, state.namespace); dberr == nil {
		agentFlows = append(agentFlows, dbFlows...)
		for k, v := range dbSubFlows {
			subFlows[k] = v
		}
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subFlows))
	return agentFlows, subFlows
}

func flattenSubflows(m map[string]entities.FlowInfo) []entities.FlowInfo {
	var out []entities.FlowInfo
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// ─── Resource Manager ─────────────────────────────────────────────

func createStandaloneRM(state *allInOneState) resourcemanager.ResourceManager {
	rm, _ := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:     engine.ProviderStandalone,
		PoolSize:     state.cfg.Orchestration.MaxConcurrentFlows,
		TaskState:    state.taskClient,
		ApprovalInfo: state.humanClient,
		Logger:       state.logger,
		APIServerURL: state.cfg.Runtime.APIServerURL,
		Namespace:    state.namespace,
	})
	return rm
}

// ─── Notifier ─────────────────────────────────────────────────────

func startNotifier(state *allInOneState) (*notifier.FlowgentNotifierManager, *handler.NotifierWSBridge, error) {
	notifSvc, err := notifier.CreateNotifierService(state.apiClient, state.cfg, client.NewGenericHttpClient(30*time.Second))
	if err != nil {
		return nil, nil, err
	}
	go func() { _ = notifSvc.Start(context.Background()) }()
	return notifSvc, handler.NewNotifierWSBridge(&notifier.NotifToWSAdapter{Svc: notifSvc}), nil
}

// ─── REST API Server ──────────────────────────────────────────────

func startRESTServer(state *allInOneState, agentFlows []entities.FlowInfo,
	subFlows map[string]entities.FlowInfo, wsBridge *handler.NotifierWSBridge) (*http.Server, *handler.FlowDefHandler, error) {

	var mqttPub handler.MQTTPublisher
	if state.cfg.Messager.Type == "mqtt" && state.cfg.Messager.MQTT.Broker != "" {
		mqttPub = newRawMQTTPublisher(state.cfg.Messager.MQTT.Broker,
			uniqueRawMQTTClientID(state.cfg.Messager.MQTT.ClientID),
			state.cfg.Messager.MQTT.Username,
			state.cfg.Messager.MQTT.Password)
	}

	flowHandler := handler.NewFlowDefHandler(state.store, state.logger, agentFlows, subFlows, state.cfg.Runtime.Namespace.NamespacePrefix, state.cfg.Runtime.Namespace.DefaultNamespace, mqttPub)
	agentHandler := handler.NewAgentDefHandler(state.store, state.logger)
	humanHandler := handler.NewHumanHandler(state.store, mqttPub, state.logger)
	runHandler := handler.NewFlowRunHandler(state.store, state.payloads, mqttPub, state.logger)
	notifHandler, err := handler.NewNotifierHandler(state.store, state.cfg.Notifier, mqttPub, state.logger)
	if err != nil {
		return nil, nil, fmt.Errorf("notification secret encryption: %w", err)
	}
	llmProviderHandler := handler.NewLlmProviderHandler(state.store)
	mcpHandler := handler.NewMcpHandler(state.store)
	knowledgeHandler := handler.NewKnowledgeHandler(state.store)
	webhookHandler := handler.NewWebhookHandler(flowHandler, state.logger, state.cfg.Runtime.Namespace.DefaultNamespace)
	jaegerClient, traceErr := tracequery.NewJaegerClientFromConfig(state.cfg.Mgmt.OTEL)
	if traceErr != nil {
		slog.Warn("Jaeger query client init failed, trace query disabled", "error", traceErr)
		jaegerClient = nil
	}
	traceHandler := handler.NewTraceHandler(state.store, jaegerClient)
	iamRepository, err := iamstore.NewRepository(state.store)
	if err != nil {
		return nil, nil, fmt.Errorf("IAM repository: %w", err)
	}
	authorizer := authz.NewService(state.cfg.Auth.Authorization, iamRepository)
	iamHandler := handler.NewIAMHandler(iamRepository, authorizer)
	flowReleaseRepository, err := flowreleasestore.NewRepository(state.store)
	if err != nil {
		return nil, nil, fmt.Errorf("flow release repository: %w", err)
	}
	flowReleaseHandler := handler.NewFlowReleaseHandler(flowReleaseRepository, iamRepository, flowHandler)
	runtimeConfigHandler, err := handler.NewRuntimeConfigHandler(state.store, state.cfg.Notifier.SecretEncryption)
	if err != nil {
		return nil, nil, fmt.Errorf("runtime configuration secrets: %w", err)
	}
	resourcePoolRepository, err := resourcepoolstore.NewRepository(state.store)
	if err != nil {
		return nil, nil, fmt.Errorf("resource pool repository: %w", err)
	}
	resourcePoolHandler := handler.NewResourcePoolHandler(resourcePoolRepository, flowHandler.FlowStore(), flowHandler.FlowRunStore())

	restMux := api.RegisterRESTRoutes(
		&handler.HealthHandler{}, flowHandler, agentHandler,
		runHandler, humanHandler, notifHandler, wsBridge, llmProviderHandler, mcpHandler, webhookHandler, knowledgeHandler, traceHandler, iamHandler, flowReleaseHandler, runtimeConfigHandler, resourcePoolHandler)

	var restHandler http.Handler = restMux
	authSvc, err := auth.NewService(state.cfg.Auth)
	if err != nil {
		slog.Error("auth service setup failed", "error", err)
		return nil, nil, fmt.Errorf("auth service setup: %w", err)
	}
	authSvc.Register(oidc.NewService(state.cfg.Auth.OIDC, authSvc.TokenService()))
	authSvc.Register(ldap.NewService(state.cfg.Auth.LDAP, authSvc.TokenService()))
	authSvc.SetCredentialAuthenticator(authorizer)
	restHandler = authSvc.Middleware()(authorizer.Middleware(restMux))

	readTO := parseDuration(state.cfg.Server.ReadTimeout, 30*time.Second)
	writeTO := parseDuration(state.cfg.Server.WriteTimeout, 60*time.Second)

	restAddr := fmt.Sprintf("%s:%d", state.cfg.Server.Host, state.cfg.Server.Port)
	restSrv := &http.Server{
		Addr: restAddr, Handler: restHandler,
		ReadTimeout: readTO, WriteTimeout: writeTO,
		MaxHeaderBytes: state.cfg.Server.MaxBodyBytes,
	}
	go func() {
		slog.Info("REST API server", "addr", restAddr)
		_ = restSrv.ListenAndServe()
	}()
	return restSrv, flowHandler, nil
}

// ─── Cron Scheduler ───────────────────────────────────────────────

func startCronScheduler(allFlows []entities.FlowInfo, apiClient *client.FlowgentClient, namespace string) {
	cronSched := trigger.NewScheduleTrigger()
	cronSched.RegisterAgentFlows(allFlows, func(ctx context.Context, id string) {
		run := &entities.FlowRunInfo{AgentFlowID: id, Version: 1, Status: entities.RunPending}
		run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "cron"})
		_, _ = apiClient.CreateRun(ctx, namespace, run)
	})
	cronSched.Start()
}

// ─── Orchestrator (JM + RunPoller) ────────────────────────────────

func startOrchestrator(state *allInOneState, rm resourcemanager.ResourceManager,
	flowMap map[string]*entities.FlowInfo) {

	timeout := parseDuration(state.cfg.Orchestration.FlowExecutionTimeout, 30*time.Minute)
	stateClient := &client.RunStateClient{Client: state.apiClient, Namespace: state.namespace}

	jm, err := jobmanager.NewJobManager(stateClient, rm, state.logger, &jobmanager.JobManagerConfig{
		FlowExecutionTimeout: timeout,
		MaxNodeRetries:       state.cfg.Orchestration.MaxNodeRetries,
		MaxConcurrentFlows:   state.cfg.Orchestration.MaxConcurrentFlows,
	})
	if err != nil {
		slog.Error("create jobmanager", "error", err)
		return
	}

	go jobmanager.StartRunPoller(context.Background(), state.apiClient, state.namespace, jm, flowMap, "", "")
}

// ─── A2A Server ───────────────────────────────────────────────────

func startA2AServer(state *allInOneState) (*http.Server, error) {
	if !state.cfg.A2A.Enabled {
		return nil, nil
	}
	taskStore, err := a2apkg.NewPersistentTaskStore(state.store)
	if err != nil {
		return nil, fmt.Errorf("create A2A task store: %w", err)
	}
	a2aSrv := a2apkg.NewHTTPServer(state.cfg, taskStore)
	go func() {
		slog.Info("A2A server", "addr", a2aSrv.Addr)
		if err := a2aSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("A2A server stopped", "error", err)
		}
	}()
	return a2aSrv, nil
}

// ─── Pprof ────────────────────────────────────────────────────────

func startPprof(state *allInOneState) *http.Server {
	if !state.cfg.Mgmt.Enabled || !state.cfg.Mgmt.PProf.Enabled {
		return nil
	}
	ppMux := http.NewServeMux()
	ppMux.HandleFunc("GET /debug/pprof/", pprof.Index)
	ppMux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	ppMux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	ppMux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	ppMux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	ppSrv := &http.Server{Addr: fmt.Sprintf("%s:%d", state.cfg.Mgmt.Host, state.cfg.Mgmt.Port), Handler: ppMux}
	go func() { ppSrv.ListenAndServe() }()
	return ppSrv
}

// ─── Shutdown ─────────────────────────────────────────────────────

func waitForShutdown(state *allInOneState, restSrv, a2aSrv, pprofSrv *http.Server,
	notifSvc *notifier.FlowgentNotifierManager) error {

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")

	shutdownTO := parseDuration(state.cfg.Server.ShutdownTimeout, 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()

	if restSrv != nil {
		restSrv.Shutdown(ctx)
	}
	if a2aSrv != nil {
		a2aSrv.Shutdown(ctx)
	}
	if pprofSrv != nil {
		pprofSrv.Close()
	}
	if notifSvc != nil {
		notifSvc.Shutdown()
	}
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────

func parseDuration(s string, defaultDur time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d == 0 {
		return defaultDur
	}
	return d
}

// newRawMQTTPublisher creates a direct MQTT publisher that sends raw payload
// bytes without wrapping in InterMessage (avoids base64 encoding of []byte).
func newRawMQTTPublisher(broker, clientID, username, password string) handler.MQTTPublisher {
	if broker == "" {
		return nil
	}
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetCleanSession(true).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second)
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}
	client := mqtt.NewClient(opts)
	if token := client.Connect(); !token.WaitTimeout(15 * time.Second) {
		slog.Warn("mqtt broker connect timeout, lifecycle events disabled")
		return nil
	} else if token.Error() != nil {
		slog.Warn("mqtt broker not available, lifecycle events disabled", "error", token.Error())
		return nil
	}
	slog.Info("MQTT lifecycle event publishing enabled", "broker", broker, "client_id", clientID)
	return &rawMQTTPublisher{client: client}
}

type rawMQTTPublisher struct {
	client mqtt.Client
}

func (p *rawMQTTPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, 1, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout")
	}
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

func uniqueRawMQTTClientID(base string) string {
	if base == "" {
		base = "flowgent-allinone"
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, host)
}
