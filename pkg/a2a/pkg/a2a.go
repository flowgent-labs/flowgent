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
	"strings"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

// FlowgentA2AServer is the standalone A2A protocol administration server.
type FlowgentA2AServer struct {
	cfg        *config.FlowgentConfig
	httpServer *http.Server
	store      storepkg.IStore
}

// NewFlowgentA2AServer creates the A2A server with all handlers registered.
// It does not start listening.
func NewFlowgentA2AServer(cfg *config.FlowgentConfig) (*FlowgentA2AServer, error) {
	backingStore := storepkg.InitStore(cfg)
	taskStore, err := NewPersistentTaskStore(backingStore)
	if err != nil {
		_ = backingStore.Close()
		return nil, err
	}
	return &FlowgentA2AServer{
		cfg:        cfg,
		httpServer: NewHTTPServer(cfg, taskStore),
		store:      backingStore,
	}, nil
}

// NewHTTPServer creates the canonical A2A HTTP server used by both standalone
// and all-in-one modes. Callers own the supplied task store and server lifecycle.
func NewHTTPServer(cfg *config.FlowgentConfig, taskStore a2asrv.TaskStore) *http.Server {
	executor := &adminAgentHandler{apiServerURL: cfg.Runtime.APIServerURL}

	handler := a2asrv.NewHandler(executor, a2asrv.WithTaskStore(taskStore))

	card := &a2a.AgentCard{
		Name:               cfg.ServiceName + "-admin",
		Description:        "Flowgent Admin Agent — AgentFlow CRUD and FlowRun lifecycle control",
		URL:                fmt.Sprintf("http://%s:%d", cfg.A2A.Host, cfg.A2A.Port),
		Version:            "dev",
		ProtocolVersion:    string(a2a.Version),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: false},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json"},
		Security: []a2a.SecurityRequirements{
			{a2a.SecuritySchemeName("bearerAuth"): a2a.SecuritySchemeScopes{}},
		},
		SecuritySchemes: a2a.NamedSecuritySchemes{
			a2a.SecuritySchemeName("bearerAuth"): a2a.HTTPAuthSecurityScheme{
				Scheme: "Bearer", BearerFormat: "JWT or Flowgent API key",
				Description: "A Flowgent identity token; authorization is enforced by the APIServer RBAC policy.",
			},
		},
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
		_ = json.NewEncoder(w).Encode(card)
	})
	mux.HandleFunc("GET /_/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/", bearerContextMiddleware(a2asrv.NewJSONRPCHandler(handler), cfg.Auth.Authorization.Enabled, cfg.Runtime.APIServerURL))

	readTO, _ := time.ParseDuration(cfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(cfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	a2aAddr := fmt.Sprintf("%s:%d", cfg.A2A.Host, cfg.A2A.Port)
	return &http.Server{
		Addr: a2aAddr, Handler: mux,
		ReadTimeout: readTO, WriteTimeout: writeTO,
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
	err := s.httpServer.Shutdown(shutdownCtx)
	if s.store != nil {
		if closeErr := s.store.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

// ─── AdminAgentHandler (implements a2asrv.AgentExecutor) ──────

// adminAgentHandler dispatches A2A admin requests to the apiserver REST API.
type adminAgentHandler struct {
	apiServerURL string
}

// adminRequest is the expected JSON payload from admin agents.
type adminRequest struct {
	Action      string             `json:"action"`
	AgentFlowID string             `json:"agentflow_id,omitempty"`
	RunID       string             `json:"run_id,omitempty"`
	Spec        *entities.FlowInfo `json:"spec,omitempty"`
	Vars        map[string]any     `json:"vars,omitempty"`
	Namespace   string             `json:"namespace,omitempty"`
}

func (h *adminAgentHandler) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	req := parseAdminRequest(reqCtx.Message)
	if req.Namespace == "" {
		req.Namespace = "default"
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
	apiClient := client.NewFlowgentClientWithToken(h.apiServerURL, bearerTokenFromContext(ctx))
	switch req.Action {
	case "list_flows":
		flows, err := apiClient.ListFlows(ctx, req.Namespace)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(flows)
		return string(b), nil

	case "get_flow":
		spec, err := apiClient.GetFlow(ctx, req.Namespace, req.AgentFlowID)
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
		if err := apiClient.CreateFlow(ctx, req.Namespace, req.Spec); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"created","agentflow_id":"%s"}`, req.Spec.ID), nil

	case "update_flow":
		if req.Spec == nil {
			return "", fmt.Errorf("spec is required for update_flow")
		}
		if err := apiClient.UpdateFlow(ctx, req.Namespace, req.AgentFlowID, req.Spec); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"updated","agentflow_id":"%s"}`, req.AgentFlowID), nil

	case "delete_flow":
		if err := apiClient.DeleteFlow(ctx, req.Namespace, req.AgentFlowID); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"deleted","agentflow_id":"%s"}`, req.AgentFlowID), nil

	case "start_run":
		trigger := entities.TriggerInfo{Type: "a2a", Source: "admin"}
		run, err := apiClient.TriggerRun(ctx, req.Namespace, req.AgentFlowID, req.Vars, trigger)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"triggered","agentflow_id":"%s","run_id":"%s"}`, req.AgentFlowID, run.RunID), nil

	case "cancel_run":
		if err := apiClient.CancelRun(ctx, req.Namespace, req.RunID); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"status":"cancelled","run_id":"%s"}`, req.RunID), nil

	case "get_run":
		run, err := apiClient.GetRun(ctx, req.Namespace, req.RunID)
		if err != nil {
			return "", err
		}
		if run == nil {
			return `{"error":"not found"}`, nil
		}
		b, _ := json.Marshal(run)
		return string(b), nil

	case "list_runs":
		runs, err := apiClient.ListRuns(ctx, req.Namespace, "", "", "", req.AgentFlowID, 1, 50)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(runs)
		return string(b), nil

	default:
		return "", fmt.Errorf("unknown admin action: %q (available: list_flows, get_flow, create_flow, update_flow, delete_flow, start_run, cancel_run, get_run, list_runs)", req.Action)
	}
}

type bearerTokenContextKey struct{}

func bearerContextMiddleware(next http.Handler, required bool, apiServerURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			if required {
				w.Header().Set("WWW-Authenticate", `Bearer realm="flowgent-a2a"`)
				http.Error(w, "bearer authentication required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if required {
			valid, err := validateBearerCredential(r.Context(), apiServerURL, parts[1])
			if err != nil {
				http.Error(w, "authentication service unavailable", http.StatusServiceUnavailable)
				return
			}
			if !valid {
				w.Header().Set("WWW-Authenticate", `Bearer realm="flowgent-a2a"`)
				http.Error(w, "invalid or expired bearer credential", http.StatusUnauthorized)
				return
			}
		}
		ctx := context.WithValue(r.Context(), bearerTokenContextKey{}, parts[1])
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateBearerCredential(ctx context.Context, apiServerURL, token string) (bool, error) {
	if apiServerURL == "" {
		apiServerURL = "http://flowgent-apiserver:9999"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiServerURL, "/")+"/api/v1/auth/me", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("authentication service returned %d", resp.StatusCode)
	}
}

func bearerTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(bearerTokenContextKey{}).(string)
	return token
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
			applyAdminData(req, p.Data)
		case a2a.DataPart:
			applyAdminData(req, p.Data)
		case *a2a.TextPart:
			if p.Text != "" {
				req.Action = p.Text
			}
		case a2a.TextPart:
			if p.Text != "" {
				req.Action = p.Text
			}
		}
	}
	return req
}

func applyAdminData(req *adminRequest, data map[string]any) {
	if v, ok := data["action"].(string); ok {
		req.Action = v
	}
	if v, ok := data["agentflow_id"].(string); ok {
		req.AgentFlowID = v
	}
	if v, ok := data["run_id"].(string); ok {
		req.RunID = v
	}
	if v, ok := data["vars"].(map[string]any); ok {
		req.Vars = v
	}
	if v, ok := data["namespace"].(string); ok {
		req.Namespace = v
	}
	if raw, ok := data["spec"]; ok {
		b, _ := json.Marshal(raw)
		var spec entities.FlowInfo
		if json.Unmarshal(b, &spec) == nil {
			req.Spec = &spec
		}
	}
}

// Ensure utils import is used (needed by callers for signal handling).
var _ = utils.StopByPID
