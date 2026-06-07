// Package a2a provides the standalone A2A protocol server daemon.
// It runs its own HTTP server, calls the apiserver REST API for run CRUD,
// and does NOT access DB, MCP, LLM, or any other infrastructure directly.
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
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// Start launches the A2A daemon.
func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startService(cfgPath)
}

// Stop stops the A2A daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the A2A daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startService(cfgPath)
}

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

	// API client for creating/querying runs via apiserver REST
	apiClient := client.NewFlowgentClient()

	readTO, _ := time.ParseDuration(svcCfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(svcCfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	mux := http.NewServeMux()

	// Agent card
	mux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(a2a.AgentCard{
			Name:         svcCfg.ServiceName,
			Description:  "Flowgent autonomous agentflow orchestration engine",
			URL:          fmt.Sprintf("http://%s:%d", svcCfg.A2A.Host, svcCfg.A2A.Port),
			Version:      "dev",
			Capabilities: a2a.AgentCapabilities{Streaming: false},
		})
	})

	// Submit task (creates PENDING run via apiserver REST API)
	mux.HandleFunc("POST /a2a/tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AgentFlowID string         `json:"agentflow_id"`
			Vars        map[string]any `json:"vars"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		trigger := model.TriggerInfo{Type: "api", Source: "a2a"}
		if err := apiClient.CreateRun(r.Context(), req.AgentFlowID, req.Vars, trigger); err != nil {
			slog.Error("a2a create run failed", "error", err)
			http.Error(w, "failed to create run", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"status": "submitted"})
	})

	// Health
	mux.HandleFunc("GET /_/healthz", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	a2aAddr := fmt.Sprintf("%s:%d", svcCfg.A2A.Host, svcCfg.A2A.Port)
	srv := &http.Server{
		Addr:         a2aAddr,
		Handler:      mux,
		ReadTimeout:  readTO,
		WriteTimeout: writeTO,
	}

	go func() {
		slog.Info("A2A server starting", "addr", a2aAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("A2A server: %v", err)
		}
	}()

	shutdownTO, _ := time.ParseDuration(svcCfg.Server.ShutdownTimeout)
	if shutdownTO == 0 {
		shutdownTO = 15 * time.Second
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("A2A server shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	return srv.Shutdown(ctx)
}
