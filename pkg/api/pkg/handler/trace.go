package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	tracequery "github.com/flowgent-labs/flowgent/api/pkg/trace"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TraceHandler serves normalized per-run OpenTelemetry trace data.
type TraceHandler struct {
	runs  flowrun.IFlowRunStore
	query tracequery.RunTraceQuery
}

func NewTraceHandler(store storage.IStorage, query tracequery.RunTraceQuery) *TraceHandler {
	var runs flowrun.IFlowRunStore
	switch db := store.DB().(type) {
	case *pgxpool.Pool:
		runs = flowrun.NewFlowRunPostgresStore(db)
	case *sql.DB:
		runs = flowrun.NewFlowRunSQLiteStore(db)
	}
	return newTraceHandler(runs, query)
}

func newTraceHandler(runs flowrun.IFlowRunStore, query tracequery.RunTraceQuery) *TraceHandler {
	return &TraceHandler{runs: runs, query: query}
}

func (h *TraceHandler) GetRunTrace(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.runs == nil || h.query == nil {
		http.Error(w, "trace query is not configured", http.StatusServiceUnavailable)
		return
	}
	run, err := h.runs.Get(r.Context(), requestRunID(r))
	if err != nil || run == nil || run.Namespace != r.PathValue("namespace") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if flowID := r.PathValue("flow_id"); flowID != "" && run.FlowName != flowID {
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
