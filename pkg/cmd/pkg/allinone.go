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
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	a2apkg "github.com/flowgent-labs/flowgent/a2a/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg/authz"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/api/pkg/nodepayload"
	tracequery "github.com/flowgent-labs/flowgent/api/pkg/trace"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/notifier/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
	graphreleasestore "github.com/flowgent-labs/flowgent/storage/pkg/graphrelease"
)

// allInOneState bundles shared dependencies for the all-in-one process.
type allInOneState struct {
	cfg         *config.FlowgentConfig
	store       storage.IStorage
	apiClient   *client.FlowgentClient
	namespace   string
	taskClient  *client.NodeRunStateClient
	humanClient *client.HumanApprovalClient
	logger      *utils.Logger
	payloads    nodepayload.INodePayloadProvider
	queue       messager.IMessager
}

type allInOneRESTServers struct {
	external *http.Server
	internal *http.Server
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
	defer startOTELTracing(svcCfg, "flowgent-jobmanager")()
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	storeImpl := storage.InitStorage(svcCfg)
	defer storeImpl.(interface{ Close() error }).Close()
	payloadProvider, err := nodepayload.NewProvider(context.Background(), svcCfg.Storage.Artifacts)
	if err != nil {
		return fmt.Errorf("create noderun payload provider: %w", err)
	}
	defer payloadProvider.Close()
	queue := messager.NewQueueFromConfig(
		svcCfg,
		uniqueRawMQTTClientID(svcCfg.Messager.MQTT.ClientID+"-allinone"),
	)
	defer queue.Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	namespace := svcCfg.Runtime.Namespace.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}

	state := &allInOneState{
		cfg:         svcCfg,
		store:       storeImpl,
		apiClient:   apiClient,
		namespace:   namespace,
		taskClient:  &client.NodeRunStateClient{Client: apiClient, Namespace: namespace},
		humanClient: &client.HumanApprovalClient{Client: apiClient, Namespace: namespace},
		logger:      logger,
		payloads:    payloadProvider,
		queue:       queue,
	}

	// AgentGraph definitions are DB-backed and are loaded through the store.
	agentGraphs, subAgentGraphs := loadAgentGraphs(state)
	allAgentGraphs := append(agentGraphs, flattenSubGraphs(subAgentGraphs)...)

	// Notifier + WS bridge (before REST so bridge is available)
	notifSvc, wsBridge, err := startNotifier(state)
	if err != nil {
		return err
	}

	// REST API Server (with optional WS bridge). The standalone Resource
	// Manager resolves its DB-backed runtime registry through this internal
	// endpoint, so the API must be accepting traffic before RM construction.
	restServers, graphHandler, err := startRESTServer(state, agentGraphs, subAgentGraphs, wsBridge)
	if err != nil {
		return err
	}
	if err := waitForAllInOneREST(svcCfg); err != nil {
		return err
	}

	// The standalone Resource Manager constructs its in-process TaskManager,
	// which resolves DB-backed MCP and LLM definitions through the internal API.
	// Start it only after that API accepts traffic; otherwise all-in-one startup
	// races itself and permanently caches an empty runtime registry.
	rm, err := createStandaloneRM(state)
	if err != nil {
		return err
	}

	// Cron triggers
	startCronScheduler(allAgentGraphs, state.apiClient, state.namespace)

	// JobManager + RunPoller
	startOrchestrator(state, rm, graphHandler.AgentGraphs())

	// A2A Server
	a2aSrv, err := startA2AServer(state)
	if err != nil {
		return err
	}

	// Pprof
	pprofSrv := startPprof(state)

	return waitForShutdown(state, restServers, a2aSrv, pprofSrv, notifSvc)
}

func waitForAllInOneREST(cfg *config.FlowgentConfig) error {
	port := cfg.Server.InternalPort
	if port <= 0 {
		port = cfg.Server.Port
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/_/healthz", port)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("all-in-one internal API did not become ready: %w", lastErr)
}

// ─── AgentGraph loading ─────────────────────────────────────────────────

func loadAgentGraphs(state *allInOneState) ([]entities.AgentGraph, map[string]entities.AgentGraph) {
	agentGraphs, subAgentGraphs, err := graph.LoadFromDB(context.Background(), state.store, state.namespace)
	if err != nil {
		slog.Warn("load agent graphs from database", "error", err)
		return nil, make(map[string]entities.AgentGraph)
	}
	slog.Info("AgentGraphs loaded", "count", len(agentGraphs)+len(subAgentGraphs))
	return agentGraphs, subAgentGraphs
}

func flattenSubGraphs(m map[string]entities.AgentGraph) []entities.AgentGraph {
	var out []entities.AgentGraph
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// ─── Resource Manager ─────────────────────────────────────────────

func createStandaloneRM(state *allInOneState) (resourcemanager.ResourceManager, error) {
	return resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:         engine.ProviderStandalone,
		PoolSize:         state.cfg.Orchestration.MaxConcurrentAgentGraphs,
		TaskState:        state.taskClient,
		ApprovalInfo:     state.humanClient,
		Logger:           state.logger,
		Messager:         state.queue,
		APIServerURL:     state.cfg.Runtime.APIServerURL,
		Namespace:        state.namespace,
		RuntimeClusterID: "local",
		SandboxWorkspace: state.cfg.Sandbox.Workspace,
		SandboxPolicy:    state.cfg.Sandbox.Policy,
	})
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

func startRESTServer(state *allInOneState, agentGraphs []entities.AgentGraph,
	subAgentGraphs map[string]entities.AgentGraph, wsBridge *handler.NotifierWSBridge) (*allInOneRESTServers, *handler.AgentGraphHandler, error) {

	var mqttPub handler.MQTTPublisher
	if state.cfg.Messager.Type == "mqtt" && state.cfg.Messager.MQTT.Broker != "" {
		mqttPub = newRawMQTTPublisher(state.cfg.Messager.MQTT.Broker,
			uniqueRawMQTTClientID(state.cfg.Messager.MQTT.ClientID),
			state.cfg.Messager.MQTT.Username,
			state.cfg.Messager.MQTT.Password)
	}

	if mqttPub == nil && state.cfg.Messager.Type == "memory" {
		mqttPub = &localOutboxPublisher{queue: state.queue}
	}
	if mqttPub != nil {
		go storage.RunOutboxPublisher(context.Background(), state.store.DB(), mqttPub.Publish)
	}
	graphHandler := handler.NewAgentGraphHandler(state.store, state.logger, agentGraphs, subAgentGraphs, state.cfg.Runtime.Namespace.NamespacePrefix, state.cfg.Runtime.Namespace.DefaultNamespace, "default", mqttPub)
	agentHandler := handler.NewAgentDefHandler(state.store, state.logger)
	skillHandler := handler.NewSkillHandler(state.store)
	humanHandler := handler.NewHumanHandler(state.store, mqttPub, state.logger)
	runHandler := handler.NewGraphRunHandler(state.store, state.payloads, mqttPub, state.logger)
	notifHandler, err := handler.NewNotifierHandler(state.store, state.cfg.Notifier, mqttPub, state.logger)
	if err != nil {
		return nil, nil, fmt.Errorf("notification secret encryption: %w", err)
	}
	llmProviderHandler := handler.NewLlmProviderHandler(state.store)
	mcpHandler := handler.NewMcpHandler(state.store)
	knowledgeHandler := handler.NewKnowledgeHandler(state.store)
	webhookHandler := handler.NewWebhookHandler(graphHandler, state.logger, state.cfg.Runtime.Namespace.DefaultNamespace)
	jaegerClient, traceErr := tracequery.NewJaegerClientFromConfig(state.cfg.Mgmt.OTEL)
	if traceErr != nil {
		slog.Warn("Jaeger query client init failed, trace query disabled", "error", traceErr)
		jaegerClient = nil
	}
	traceHandler := handler.NewTraceHandler(state.store, jaegerClient)
	graphReleaseRepository, err := graphreleasestore.NewRepository(state.store)
	if err != nil {
		return nil, nil, fmt.Errorf("graph release repository: %w", err)
	}
	graphReleaseHandler := handler.NewAgentGraphReleaseHandler(graphReleaseRepository, graphHandler)
	runtimeConfigHandler, err := handler.NewRuntimeConfigHandler(state.store, state.cfg.Notifier.SecretEncryption)
	if err != nil {
		return nil, nil, fmt.Errorf("runtime configuration secrets: %w", err)
	}
	restMux := api.RegisterRESTRoutes(
		&handler.HealthHandler{}, graphHandler, agentHandler, skillHandler,
		runHandler, humanHandler, notifHandler, wsBridge, llmProviderHandler, mcpHandler, webhookHandler, knowledgeHandler, traceHandler, graphReleaseHandler, runtimeConfigHandler)

	adapter, err := authz.NewAdapter(state.cfg.AuthGuardAdapter)
	if err != nil {
		return nil, nil, fmt.Errorf("AuthGuard adapter: %w", err)
	}
	var restHandler http.Handler = adapter.Middleware(restMux)

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
	servers := &allInOneRESTServers{external: restSrv}
	if state.cfg.Server.InternalPort > 0 {
		if state.cfg.Server.InternalPort == state.cfg.Server.Port {
			_ = restSrv.Close()
			return nil, nil, fmt.Errorf("server.internal_port must differ from server.port")
		}
		internalAddr := fmt.Sprintf("%s:%d", state.cfg.Server.Host, state.cfg.Server.InternalPort)
		servers.internal = &http.Server{
			Addr: internalAddr, Handler: adapter.InternalMiddleware(restMux),
			ReadTimeout: readTO, WriteTimeout: writeTO,
			MaxHeaderBytes: state.cfg.Server.MaxBodyBytes,
		}
		go func() {
			slog.Info("Internal control-plane API server", "addr", internalAddr)
			_ = servers.internal.ListenAndServe()
		}()
	}
	return servers, graphHandler, nil
}

// ─── Cron Scheduler ───────────────────────────────────────────────

func startCronScheduler(allAgentGraphs []entities.AgentGraph, apiClient *client.FlowgentClient, namespace string) {
	cronSched := trigger.NewScheduleTrigger()
	graphByID := make(map[string]entities.AgentGraph, len(allAgentGraphs))
	for i := range allAgentGraphs {
		graphByID[allAgentGraphs[i].ID] = allAgentGraphs[i]
	}
	cronSched.RegisterAgentGraphs(allAgentGraphs, func(ctx context.Context, id string) {
		spec, ok := graphByID[id]
		if !ok {
			slog.Warn("cron trigger skipped unknown graph", "agent_graph_id", id)
			return
		}
		if err := entities.ValidateRuntimeMode(spec.RuntimeMode); err != nil {
			slog.Warn("cron trigger skipped graph with invalid runtime_mode", "agent_graph_id", id, "runtime_mode", spec.RuntimeMode, "error", err)
			return
		}
		run := &entities.GraphRun{AgentGraphName: id, AgentGraphRevision: 1, Status: entities.GraphRunPending, RuntimeMode: spec.RuntimeMode}
		run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "cron"})
		_, _ = apiClient.CreateGraphRun(ctx, namespace, run)
	})
	cronSched.Start()
}

// ─── Orchestrator (JM + RunPoller) ────────────────────────────────

func startOrchestrator(state *allInOneState, rm resourcemanager.ResourceManager,
	graphMap map[string]*entities.AgentGraph) {

	timeout := parseDuration(state.cfg.Orchestration.AgentGraphExecutionTimeout, 30*time.Minute)
	stateClient := &client.GraphRunStateClient{Client: state.apiClient, Namespace: state.namespace}

	jm, err := jobmanager.NewJobManager(stateClient, rm, state.logger, &jobmanager.JobManagerConfig{
		AgentGraphExecutionTimeout: timeout,
		MaxNodeRetries:             state.cfg.Orchestration.MaxNodeRetries,
		MaxConcurrentAgentGraphs:   state.cfg.Orchestration.MaxConcurrentAgentGraphs,
		RuntimeClusterID:           "local",
	})
	if err != nil {
		slog.Error("create jobmanager", "error", err)
		return
	}

	go jobmanager.StartGraphRunPoller(context.Background(), state.apiClient, state.namespace, jm, graphMap, jobmanager.GraphRunPollerConfig{
		RuntimeMode: entities.RuntimeModeApplication,
	})
	go jobmanager.StartGraphRunPoller(context.Background(), state.apiClient, state.namespace, jm, graphMap, jobmanager.GraphRunPollerConfig{
		RuntimeMode: entities.RuntimeModeSession,
	})
}

// ─── A2A Server ───────────────────────────────────────────────────

func startA2AServer(state *allInOneState) (*http.Server, error) {
	if !state.cfg.A2A.Enabled {
		return nil, nil
	}
	taskStore, err := a2apkg.NewPersistentTaskStore(state.store)
	if err != nil {
		return nil, fmt.Errorf("create A2A noderun store: %w", err)
	}
	a2aSrv, err := a2apkg.NewHTTPServer(state.cfg, taskStore)
	if err != nil {
		return nil, err
	}
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

func waitForShutdown(state *allInOneState, restServers *allInOneRESTServers, a2aSrv, pprofSrv *http.Server,
	notifSvc *notifier.FlowgentNotifierManager) error {

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")

	shutdownTO := parseDuration(state.cfg.Server.ShutdownTimeout, 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()

	if restServers != nil {
		if restServers.external != nil {
			restServers.external.Shutdown(ctx)
		}
		if restServers.internal != nil {
			restServers.internal.Shutdown(ctx)
		}
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

type localOutboxPublisher struct{ queue messager.IMessager }

func (p *localOutboxPublisher) Publish(ctx context.Context, topic string, data []byte) error {
	return p.queue.Publish(ctx, topic, &messager.InterMessage{Payload: data})
}
