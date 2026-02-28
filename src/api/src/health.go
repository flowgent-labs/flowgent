package api

import (
	"encoding/json"
	"net/http"
)

// HealthHandler provides health check endpoints.
type HealthHandler struct{}

// Healthz responds with health status.
func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
}
