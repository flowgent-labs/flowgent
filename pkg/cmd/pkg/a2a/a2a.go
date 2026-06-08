// Package a2a provides the standalone A2A protocol administration server.
// It exposes AgentFlow CRUD and FlowRun lifecycle control to external
// Admin Agent systems via the A2A JSON-RPC protocol.
//
// Uses the official github.com/a2aproject/a2a-go SDK:
//   - a2asrv.NewHandler + AgentExecutor for request dispatch
//   - a2asrv.NewJSONRPCHandler for HTTP JSON-RPC transport
//   - a2asrv.NewStaticAgentCardHandler for agent discovery
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── CLI entry points ──────────────────────────────────────────

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startService(cfgPath)
}

func Stop(pidFile string) error { return cmdutil.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(cfgPath, pidFile)
}

// ─── Server startup ────────────────────────────────────────────

func startService(cfgPath string) error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	_ = logger

	if !svcCfg.A2A.Enabled {
		log.Println("A2A is disabled in config")
		cmdutil.WaitSignal()
		return nil
	}

	apiClient := client.NewFlowgentClient()
	taskStore := newA2ATaskStore()

	executor := &adminAgentHandler{
		client: apiClient,
	}

	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(taskStore))

	card := &a2a.AgentCard{
		Name:        svcCfg.ServiceName + "-admin",
		Description: "Flowgent Admin Agent — AgentFlow CRUD and FlowRun lifecycle control",
		URL:         fmt.Sprintf("http://%s:%d", svcCfg.A2A.Host, svcCfg.A2A.Port),
		Version:     "dev",
		Capabilities: a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{ID: "list_flows", Name: "List AgentFlows", Description: "List all agentflow definitions"},
			{ID: "get_flow", Name: "Get AgentFlow", Description: "Get a single agentflow by ID"},
			{ID: "create_flow", Name: "Create AgentFlow", Description: "Create a new agentflow definition"},
			{ID: "update_flow", Name: "Update AgentFlow", Description: "Update an existing agentflow"},
			{ID: "delete_flow", Name: "Delete AgentFlow", Description: "Delete an agentflow definition"},
			{ID: "start_run", Name: "Start FlowRun", Description: "Trigger a new flow run"},
			{ID: "cancel_run", Name: "Cancel FlowRun", Description: "Cancel a running flow"},
			{ID: "get_run", Name: "Get FlowRun", Description: "Query run status by ID"},
			{ID: "list_runs", Name: "List FlowRuns", Description: "List recent runs"},
		},
	}

	readTO, _ := time.ParseDuration(svcCfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(svcCfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}
	shutdownTO, _ := time.ParseDuration(svcCfg.Server.ShutdownTimeout)
	if shutdownTO == 0 {
		shutdownTO = 15 * time.Second
	}

	mux := http.NewServeMux()
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/", a2asrv.NewJSONRPCHandler(handler))

	a2aAddr := fmt.Sprintf("%s:%d", svcCfg.A2A.Host, svcCfg.A2A.Port)
	srv := &http.Server{
		Addr: a2aAddr, Handler: mux,
		ReadTimeout: readTO, WriteTimeout: writeTO,
	}

	go func() {
		slog.Info("A2A admin agent server starting", "addr", a2aAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("A2A server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("A2A server shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	return srv.Shutdown(ctx)
}

// ─── AdminAgentHandler (implements a2asrv.AgentExecutor) ──────

// adminAgentHandler dispatches A2A admin requests to the apiserver REST API.
// It supports AgentFlow CRUD and FlowRun lifecycle control.
type adminAgentHandler struct {
	client *client.FlowgentClient
}

// adminRequest is the expected JSON payload from admin agents.
type adminRequest struct {
	Action      string         `json:"action"`
	AgentFlowID string         `json:"agentflow_id,omitempty"`
	RunID       string         `json:"run_id,omitempty"`
	Spec        *model.AgentFlowSpec `json:"spec,omitempty"`
	Vars        map[string]any `json:"vars,omitempty"`
	Tenant      string         `json:"tenant,omitempty"`
}

func (h *adminAgentHandler) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	req := parseAdminRequest(reqCtx.Message)
	if req.Tenant == "" {
		req.Tenant = "default"
	}

	// New task — acknowledge
	if reqCtx.StoredTask == nil {
		_ = queue.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateSubmitted, nil))
	}
	_ = queue.Write(ctx, a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateWorking, nil))

	result, err := h.dispatch(ctx, req)
	if err != nil {
		slog.Error("a2a admin action failed", "action", req.Action, "error", err)
		msg := a2a.NewMessage(a2a.MessageRoleAgent, &a2a.TextPart{Text: err.Error()})
		evt := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateFailed, msg)
		evt.Final = true
		return queue.Write(ctx, evt)
	}

	// Attach result as output artifact
	_ = queue.Write(ctx, a2a.NewArtifactEvent(reqCtx, &a2a.TextPart{Text: result}))

	evt := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCompleted, nil)
	evt.Final = true
	return queue.Write(ctx, evt)
}

func (h *adminAgentHandler) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	event := a2a.NewStatusUpdateEvent(reqCtx, a2a.TaskStateCanceled, nil)
	event.Final = true
	return queue.Write(ctx, event)
}

// dispatch routes the admin action to the appropriate apiserver REST call.
func (h *adminAgentHandler) dispatch(ctx context.Context, req *adminRequest) (string, error) {
	switch req.Action {
	// ── AgentFlow CRUD ──
	case "list_flows":
		flows, err := h.client.ListFlows(ctx, req.Tenant)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(flows)
		return string(b), nil

	case "get_flow":
		spec, err := h.client.GetFlow(ctx, req.Tenant, req.AgentFlowID)
		if err != nil {
			return "", err
		}
		if spec == nil {
			return `{"error":"not found"}`, nil
		}
		b, _ := json.Marshal(spec)
		return string(b), nil

	case "create_flow":
		if req.Spec == nil {
			return "", fmt.Errorf("spec is required for create_flow")
		}
		if err := h.client.CreateFlow(ctx, req.Tenant, req.Spec); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"created","agentflow_id":"%s"}`, req.Spec.ID), nil

	case "update_flow":
		if req.Spec == nil {
			return "", fmt.Errorf("spec is required for update_flow")
		}
		if err := h.client.UpdateFlow(ctx, req.Tenant, req.AgentFlowID, req.Spec); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"updated","agentflow_id":"%s"}`, req.AgentFlowID), nil

	case "delete_flow":
		if err := h.client.DeleteFlow(ctx, req.Tenant, req.AgentFlowID); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"deleted","agentflow_id":"%s"}`, req.AgentFlowID), nil

	// ── FlowRun Control ──
	case "start_run":
		trigger := model.TriggerInfo{Type: "a2a", Source: "admin"}
		if _, err := h.client.TriggerRun(ctx, req.Tenant, req.AgentFlowID, req.Vars, trigger); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"triggered","agentflow_id":"%s"}`, req.AgentFlowID), nil

	case "cancel_run":
		if err := h.client.CancelRun(ctx, req.Tenant, req.RunID); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"cancelled","run_id":"%s"}`, req.RunID), nil

	case "get_run":
		run, err := h.client.GetRun(ctx, req.Tenant, req.RunID)
		if err != nil {
			return "", err
		}
		if run == nil {
			return `{"error":"not found"}`, nil
		}
		b, _ := json.Marshal(run)
		return string(b), nil

	case "list_runs":
		runs, err := h.client.ListRuns(ctx, req.Tenant, "", "", req.AgentFlowID, 1, 50)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(runs)
		return string(b), nil

	default:
		return "", fmt.Errorf("unknown admin action: %q (available: list_flows, get_flow, create_flow, update_flow, delete_flow, start_run, cancel_run, get_run, list_runs)", req.Action)
	}
}

// ─── Request Parsing ──────────────────────────────────────────

func parseAdminRequest(msg *a2a.Message) *adminRequest {
	req := &adminRequest{}
	if msg == nil {
		return req
	}
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case *a2a.DataPart:
			if v, ok := p.Data["action"].(string); ok {
				req.Action = v
			}
			if v, ok := p.Data["agentflow_id"].(string); ok {
				req.AgentFlowID = v
			}
			if v, ok := p.Data["run_id"].(string); ok {
				req.RunID = v
			}
			if v, ok := p.Data["vars"].(map[string]any); ok {
				req.Vars = v
			}
			if v, ok := p.Data["tenant"].(string); ok {
				req.Tenant = v
			}
			// spec is passed as raw JSON, re-marshalled
			if raw, ok := p.Data["spec"]; ok {
				b, _ := json.Marshal(raw)
				var spec model.AgentFlowSpec
				if json.Unmarshal(b, &spec) == nil {
					req.Spec = &spec
				}
			}
		case *a2a.TextPart:
			if p.Text != "" {
				req.Action = p.Text
			}
		}
	}
	return req
}

// ─── A2ATaskStore (implements a2asrv.TaskStore) ───────────────

// A2ATaskStore is an in-memory TaskStore for the A2A SDK.
type A2ATaskStore struct {
	mu       sync.RWMutex
	tasks    map[a2a.TaskID]*a2a.Task
	versions map[a2a.TaskID]a2a.TaskVersion
}

func newA2ATaskStore() *A2ATaskStore {
	return &A2ATaskStore{
		tasks:    make(map[a2a.TaskID]*a2a.Task),
		versions: make(map[a2a.TaskID]a2a.TaskVersion),
	}
}

func (s *A2ATaskStore) Save(ctx context.Context, task *a2a.Task, event a2a.Event, prev *a2a.Task, prevVersion a2a.TaskVersion) (a2a.TaskVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[task.ID]++
	s.tasks[task.ID] = task
	return s.versions[task.ID], nil
}

func (s *A2ATaskStore) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, 0, a2a.ErrTaskNotFound
	}
	return t, s.versions[taskID], nil
}

func (s *A2ATaskStore) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var tasks []*a2a.Task
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	return &a2a.ListTasksResponse{Tasks: tasks}, nil
}
