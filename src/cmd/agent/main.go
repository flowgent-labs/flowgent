package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/llm"
	"github.com/flowgent-labs/flowgent/src/mcp"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/store"
	"github.com/flowgent-labs/flowgent/src/util"
)

// Flowgent A2A (Agent-to-Agent) Server
// Implements the Google A2A protocol for inter-agent communication.
// Other AI agent systems can discover and invoke this agent via the A2A agent card.

var (
	Version   = "dev"
	GitCommit = "none"
)

func main() {
	log.Printf("Flowgent A2A Agent v%s (commit: %s)", Version, GitCommit)

	cfgPath := "etc/flowgent.yaml"
	if v := os.Getenv("FLOWGENT_CONFIG"); v != "" {
		cfgPath = v
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger := util.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agentFlows: %v", err)
	}

	var storeImpl store.Store
	switch serviceCfg.Storage.Type {
	case "POSTGRE":
		pgCfg := serviceCfg.Storage.Postgres
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			pgCfg.Username, pgCfg.Password, pgCfg.Host, pgCfg.Port, pgCfg.Database)
		pgStore := store.NewPostgresStore(dsn)
		pgStore.SetPoolConfig(pgCfg.MinConnections, pgCfg.MaxConnections)
		if err := pgStore.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init Postgres: %v", err)
		}
		storeImpl = pgStore
	default:
		sqliteStore := store.NewSQLiteStore(serviceCfg.Storage.SQLite.Dir)
		if err := sqliteStore.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init SQLite: %v", err)
		}
		storeImpl = sqliteStore
	}

	mcpFactory := mcp.NewFactory()
	for _, mcpDef := range serviceCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
		}
	}
	mcpMap := make(map[string]engine.MCPClient)
	for _, mcpDef := range serviceCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpMap[mcpDef.Name] = &mcpAdapter{factory: mcpFactory, name: mcpDef.Name}
		}
	}

	llmClient := llm.New(&serviceCfg.LLM)
	agents := make([]*model.AgentDef, len(serviceCfg.Orchestration.Agents))
	for i := range serviceCfg.Orchestration.Agents {
		agents[i] = &serviceCfg.Orchestration.Agents[i]
	}
	exec := engine.NewExecutor(storeImpl, mcpMap, agents, llmClient, logger)

	// Merge all agentflow specs
	allFlows := make(map[string]*model.AgentFlowSpec)
	for i := range agentFlows {
		allFlows[agentFlows[i].ID] = &agentFlows[i]
	}
	for k, v := range subAgentFlows {
		sw := v
		allFlows[k] = &sw
	}

	// ─── A2A Agent Card ────────────────────────────────
	agentCard := map[string]any{
		"name":        serviceCfg.ServiceName,
		"description": "Flowgent autonomous agentflow orchestration engine. Accepts agentflow IDs and input variables, returns execution results.",
		"url":         fmt.Sprintf("http://%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port),
		"version":     Version,
		"capabilities": map[string]any{
			"streaming": false,
			"pushNotifications": false,
		},
		"skills": []map[string]any{
			{"id": "run_agentflow", "description": "Execute an agentflow by ID with input variables"},
			{"id": "query_run", "description": "Query the status of an agentflow run"},
			{"id": "list_agentflows", "description": "List all available agentflow definitions"},
		},
	}

	// ─── Routes ─────────────────────────────────────────
	mux := http.NewServeMux()

	// A2A Agent Card (well-known discovery endpoint)
	mux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agentCard)
	})

	// A2A Task API: execute an agentflow
	mux.HandleFunc("POST /a2a/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Skill       string         `json:"skill"`
			AgentFlowID string         `json:"agentflow_id"`
			Vars        map[string]any `json:"vars,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		spec, ok := allFlows[req.AgentFlowID]
		if !ok {
			http.Error(w, "agentflow not found", http.StatusNotFound)
			return
		}

		run := &model.AgentFlowRun{
			AgentFlowID: req.AgentFlowID,
			Version:     1,
			Status:      model.RunPending,
			Vars:        req.Vars,
			Trigger:     model.TriggerInfo{Type: "api", Source: "a2a"},
		}
		if err := storeImpl.CreateAgentFlowRun(r.Context(), run); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		rt := engine.NewAgentFlowRuntime(storeImpl, logger)
		rt.SetExecutor(exec)
		if err := rt.Execute(r.Context(), run, spec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task_id":      run.ID,
			"agentflow_id": run.AgentFlowID,
			"status":       run.Status,
			"output":       run.Output,
		})
	})

	// A2A Status: query a run
	mux.HandleFunc("GET /a2a/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		run, err := storeImpl.GetAgentFlowRun(r.Context(), id)
		if err != nil || run == nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task_id":      run.ID,
			"agentflow_id": run.AgentFlowID,
			"status":       run.Status,
			"output":       run.Output,
			"error":        run.Error,
		})
	})

	// Health
	mux.HandleFunc("GET /_/healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})

	// ─── Server ─────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("A2A Agent server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// ─── Workers ────────────────────────────────────────
	q := queue.NewMemoryQueue(1000)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			runs, _ := storeImpl.ListActiveRuns(context.Background())
			for _, r := range runs {
				_ = q.Push(context.Background(), &queue.Message{ID: r.ID, TaskRunID: r.ID})
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")
	srv.Shutdown(context.Background())
}

type mcpAdapter struct {
	factory *mcp.Factory
	name    string
}

func (a *mcpAdapter) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return a.factory.CallTool(ctx, a.name, toolName, args)
}
