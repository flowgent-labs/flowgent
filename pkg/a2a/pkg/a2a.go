// Package a2a provides the A2A protocol administration server.
// It exposes AgentFlow CRUD and FlowRun lifecycle control to external
// Admin Agent systems via the A2A JSON-RPC protocol.
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

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// FlowgentA2AServer is the standalone A2A protocol administration server.
type FlowgentA2AServer struct {
	cfg        *config.FlowgentConfig
	httpServer *http.Server
	taskStore  *A2ATaskStore
}

// NewFlowgentA2AServer creates the A2A server with all handlers registered.
// It does not start listening.
func NewFlowgentA2AServer(cfg *config.FlowgentConfig) *FlowgentA2AServer {
	taskStore := newA2ATaskStore()

	executor := &adminAgentHandler{
		client: client.NewFlowgentClient(cfg.Runtime.APIServerURL),
	}

	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(taskStore))

	card := &a2a.AgentCard{
		Name:        cfg.ServiceName + "-admin",
		Description: "Flowgent Admin Agent — AgentFlow CRUD and FlowRun lifecycle control",
		URL:         fmt.Sprintf("http://%s:%d", cfg.A2A.Host, cfg.A2A.Port),
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	})
	mux.Handle("/", a2asrv.NewJSONRPCHandler(handler))

	readTO, _ := time.ParseDuration(cfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(cfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	a2aAddr := fmt.Sprintf("%s:%d", cfg.A2A.Host, cfg.A2A.Port)
	srv := &http.Server{
		Addr: a2aAddr, Handler: mux,
		ReadTimeout: readTO, WriteTimeout: writeTO,
	}

	return &FlowgentA2AServer{
		cfg:        cfg,
		httpServer: srv,
		taskStore:  taskStore,
	}
}

// Start begins listening and blocks until a shutdown signal is received.
func (s *FlowgentA2AServer) Start(ctx context.Context) error {
	go func() {
		slog.Info("A2A admin agent server starting", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("A2A server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		slog.Info("A2A server context cancelled, shutting down...")
	case <-sigCh:
		slog.Info("A2A server shutting down...")
	}

	return s.Shutdown()
}

// Shutdown gracefully stops the HTTP server.
func (s *FlowgentA2AServer) Shutdown() error {
	shutdownTO := 15 * time.Second
	if s.cfg != nil {
		if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil && d > 0 {
			shutdownTO = d
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// ─── AdminAgentHandler (implements a2asrv.AgentExecutor) ──────

// adminAgentHandler dispatches A2A admin requests to the apiserver REST API.
type adminAgentHandler struct {
	client *client.FlowgentClient
}

// adminRequest is the expected JSON payload from admin agents.
type adminRequest struct {
	Action      string                   `json:"action"`
	AgentFlowID string                   `json:"agentflow_id,omitempty"`
	RunID       string                   `json:"run_id,omitempty"`
	Spec        *entities.FlowInfo  `json:"spec,omitempty"`
	Vars        map[string]any           `json:"vars,omitempty"`
	Tenant      string                   `json:"tenant,omitempty"`
}

func (h *adminAgentHandler) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	req := parseAdminRequest(reqCtx.Message)
	if req.Tenant == "" {
		req.Tenant = "default"
	}

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

func (h *adminAgentHandler) dispatch(ctx context.Context, req *adminRequest) (string, error) {
	switch req.Action {
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

	case "start_run":
		trigger := entities.TriggerInfo{Type: "a2a", Source: "admin"}
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
			if raw, ok := p.Data["spec"]; ok {
				b, _ := json.Marshal(raw)
				var spec entities.FlowInfo
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

// Ensure utils import is used (needed by callers for signal handling).
var _ = utils.StopByPID
