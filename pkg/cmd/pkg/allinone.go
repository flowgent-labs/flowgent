// Package allinone provides the combined single-process entry point
// for local development. It starts all components in one process:
// REST API, A2A, JobManager + RunPoller, cron triggers, notifier.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/api/pkg"
	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/ldap"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/oidc"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/notifier/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
)

// allInOneState bundles shared dependencies for the all-in-one process.
type allInOneState struct {
	cfg         *config.FlowgentConfig
	store       store.IStore
	apiClient   *client.FlowgentClient
	httpClient  model.IFlowgentAPIClient
	tenant      string
	taskClient  *client.TaskStateClient
	humanClient *client.HumanApprovalClient
	logger      *utils.Logger
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

	storeImpl := store.InitStore(svcCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	apiClient := client.NewFlowgentClient(svcCfg.Runtime.APIServerURL)
	tenant := svcCfg.Tenant.DefaultTenant
	if tenant == "" {
		tenant = "default"
	}

	state := &allInOneState{
		cfg:         svcCfg,
		store:       storeImpl,
		apiClient:   apiClient,
		httpClient:  client.NewHttpClient(svcCfg, nil),
		tenant:      tenant,
		taskClient:  &client.TaskStateClient{Client: apiClient, Tenant: tenant},
		humanClient: &client.HumanApprovalClient{Client: apiClient},
		logger:      logger,
	}

	// Load agent flows (YAML + DB)
	agentFlows, subFlows := loadFlows(state, cfgPath)
	allFlows := append(agentFlows, flattenSubflows(subFlows)...)

	// Resource Manager (standalone, starts TM in-process)
	rm := createStandaloneRM(state)

	// Notifier + WS bridge (before REST so bridge is available)
	notifSvc, wsBridge := startNotifier(state)

	// REST API Server (with optional WS bridge)
	restSrv, flowHandler := startRESTServer(state, agentFlows, subFlows, wsBridge)

	// Cron triggers
	startCronScheduler(allFlows, state.apiClient, state.tenant)

	// JobManager + RunPoller
	startOrchestrator(state, rm, flowHandler.AgentFlows())

	// A2A Server
	a2aSrv := startA2AServer(state)

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
	if dbFlows, dbSubFlows, dberr := flow.LoadFromDB(context.Background(), state.store); dberr == nil {
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
		Provider:      engine.ProviderStandalone,
		PoolSize:      state.cfg.Orchestration.MaxConcurrentFlows,
		TaskState:     state.taskClient,
		ApprovalInfo: state.humanClient,
		Logger:        state.logger,
		APIServerURL:  state.cfg.Runtime.APIServerURL,
		Tenant:        state.tenant,
	})
	return rm
}

// ─── Notifier ─────────────────────────────────────────────────────

func startNotifier(state *allInOneState) (*notifier.FlowgentNotifierManager, *handler.NotifierWSBridge) {
	notifSvc := notifier.CreateNotifierService(state.apiClient, state.cfg, state.httpClient)
	if notifSvc == nil {
		return nil, nil
	}
	go func() { _ = notifSvc.Start(context.Background()) }()
	return notifSvc, handler.NewNotifierWSBridge(&notifier.NotifToWSAdapter{Svc: notifSvc})
}

// ─── REST API Server ──────────────────────────────────────────────

func startRESTServer(state *allInOneState, agentFlows []entities.FlowInfo,
	subFlows map[string]entities.FlowInfo, wsBridge *handler.NotifierWSBridge) (*http.Server, *handler.FlowDefHandler) {

	var mqttPub handler.MQTTPublisher
	if state.cfg.Messager.Type == "mqtt" && state.cfg.Messager.MQTT.Broker != "" {
		mqttPub = newRawMQTTPublisher(state.cfg.Messager.MQTT.Broker,
			state.cfg.Messager.MQTT.ClientID,
			state.cfg.Messager.MQTT.Username,
			state.cfg.Messager.MQTT.Password)
	}

	flowHandler := handler.NewFlowDefHandler(state.store, state.logger, agentFlows, subFlows, state.cfg.Tenant.NamespacePrefix, state.cfg.Tenant.DefaultTenant, mqttPub)
	agentHandler := handler.NewAgentDefHandler(state.store, state.logger)
	humanHandler := handler.NewHumanHandler(state.store, mqttPub, state.logger)
	runHandler := handler.NewFlowRunHandler(state.store, mqttPub, state.logger)
	notifHandler := handler.NewNotifierHandler(state.store, state.logger)
	llmProviderHandler := handler.NewLlmProviderHandler(state.store)
	mcpHandler := handler.NewMcpHandler(state.store)

	restMux := api.RegisterRESTRoutes(
		&handler.HealthHandler{}, flowHandler, agentHandler,
		runHandler, humanHandler, notifHandler, wsBridge, llmProviderHandler, mcpHandler)

	var restHandler http.Handler = restMux
	authSvc, err := auth.NewService(state.cfg.Auth)
	if err != nil {
		slog.Error("auth service setup failed", "error", err)
		os.Exit(1)
	}
		authSvc.Register(oidc.NewService(state.cfg.Auth.OIDC, authSvc.TokenService()))
		authSvc.Register(ldap.NewService(state.cfg.Auth.LDAP, authSvc.TokenService()))
		restHandler = authSvc.Middleware()(restMux)

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
	return restSrv, flowHandler
}

// ─── Cron Scheduler ───────────────────────────────────────────────

func startCronScheduler(allFlows []entities.FlowInfo, apiClient *client.FlowgentClient, tenant string) {
	cronSched := trigger.NewScheduleTrigger()
	cronSched.RegisterAgentFlows(allFlows, func(ctx context.Context, id string) {
		run := &entities.FlowRunInfo{AgentFlowID: id, Version: 1, Status: entities.RunPending}
		run.SetTrigger(entities.TriggerInfo{Type: "schedule", Source: "cron"})
		_, _ = apiClient.CreateRun(ctx, tenant, run)
	})
	cronSched.Start()
}

// ─── Orchestrator (JM + RunPoller) ────────────────────────────────

func startOrchestrator(state *allInOneState, rm resourcemanager.ResourceManager,
	flowMap map[string]*entities.FlowInfo) {

	timeout := parseDuration(state.cfg.Orchestration.FlowExecutionTimeout, 30*time.Minute)
	stateClient := &client.RunStateClient{Client: state.apiClient, Tenant: state.tenant}

	jm, err := jobmanager.NewJobManager(stateClient, rm, state.logger, &jobmanager.JobManagerConfig{
		FlowExecutionTimeout: timeout,
		MaxNodeRetries:       state.cfg.Orchestration.MaxNodeRetries,
		MaxConcurrentFlows:   state.cfg.Orchestration.MaxConcurrentFlows,
	})
	if err != nil {
		slog.Error("create jobmanager", "error", err)
		return
	}

	go jobmanager.StartRunPoller(context.Background(), state.apiClient, state.tenant, jm, flowMap, "", "")
}

// ─── A2A Server ───────────────────────────────────────────────────

func startA2AServer(state *allInOneState) *http.Server {
	if !state.cfg.A2A.Enabled {
		return nil
	}

	a2aMux := http.NewServeMux()
	a2aMux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(a2a.AgentCard{
			Name: state.cfg.ServiceName, Description: "Flowgent orchestration engine",
			URL: fmt.Sprintf("http://%s:%d", state.cfg.A2A.Host, state.cfg.A2A.Port),
			Version: "dev", Capabilities: a2a.AgentCapabilities{Streaming: false},
		})
	})
	a2aMux.HandleFunc("POST /a2a/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentFlowID string         `json:"agentflow_id"`
			Vars        map[string]any `json:"vars"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		run := &entities.FlowRunInfo{
			BaseEntity: entities.BaseEntity{ID: uuid.NewString()},
			AgentFlowID: req.AgentFlowID, Version: 1,
			Status: entities.RunPending, Vars: req.Vars,
		}
		run.SetTrigger(entities.TriggerInfo{Type: "api", Source: "a2a"})
		if _, err := state.apiClient.CreateRun(r.Context(), state.tenant, run); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(a2a.Task{
			ID: a2a.TaskID(run.ID), ContextID: run.ID,
			Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
		})
	})
	a2aMux.HandleFunc("GET /_/healthz", (&handler.HealthHandler{}).Healthz)

	readTO := parseDuration(state.cfg.Server.ReadTimeout, 30*time.Second)
	writeTO := parseDuration(state.cfg.Server.WriteTimeout, 60*time.Second)

	a2aSrv := &http.Server{
		Addr: fmt.Sprintf("%s:%d", state.cfg.A2A.Host, state.cfg.A2A.Port),
		Handler: a2aMux, ReadTimeout: readTO, WriteTimeout: writeTO,
	}
	go func() {
		slog.Info("A2A server", "addr", a2aSrv.Addr)
		_ = a2aSrv.ListenAndServe()
	}()
	return a2aSrv
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
	if token := client.Connect(); token.WaitTimeout(15*time.Second) && token.Error() != nil {
		slog.Warn("mqtt broker not available, lifecycle events disabled", "error", token.Error())
		return nil
	}
	slog.Info("MQTT lifecycle event publishing enabled", "broker", broker)
	return &rawMQTTPublisher{client: client}
}

type rawMQTTPublisher struct {
	client mqtt.Client
}

func (p *rawMQTTPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, 1, false, payload)
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}
