package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	tracequery "github.com/flowgent-labs/flowgent/api/pkg/trace"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/graphrun"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TraceHandler serves normalized per-run OpenTelemetry trace data.
type TraceHandler struct {
	runs  graphrun.IGraphRunStore
	query tracequery.RunTraceQuery
}

func NewTraceHandler(store storage.IStorage, query tracequery.RunTraceQuery) *TraceHandler {
	var runs graphrun.IGraphRunStore
	switch db := store.DB().(type) {
	case *pgxpool.Pool:
		runs = graphrun.NewGraphRunPostgresStore(db)
	case *sql.DB:
		runs = graphrun.NewGraphRunSQLiteStore(db)
	}
	return newTraceHandler(runs, query)
}

func newTraceHandler(runs graphrun.IGraphRunStore, query tracequery.RunTraceQuery) *TraceHandler {
	return &TraceHandler{runs: runs, query: query}
}

func (h *TraceHandler) GetRunTrace(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.runs == nil || h.query == nil {
		http.Error(w, "trace query is not configured", http.StatusServiceUnavailable)
		return
	}
	run, err := h.runs.Get(r.Context(), requestGraphRunID(r))
	if err != nil || run == nil || run.Namespace != r.PathValue("namespace") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if graphID := r.PathValue("agent_graph_id"); graphID != "" && run.AgentGraphName != graphID {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	result, err := h.query.QueryRun(r.Context(), run.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	json.NewEncoder(w).Encode(result)
}
