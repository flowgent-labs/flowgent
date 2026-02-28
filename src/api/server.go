package api

import "net/http"

// RegisterRESTRoutes returns a ServeMux with all REST API routes.
// Called from launch.go; the caller wraps with auth middleware and starts the server.
func RegisterRESTRoutes(
	health *HealthHandler,
	agentFlows *AgentFlowHandler,
	human *HumanHandler,
	triggers *TriggerDispatcher,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", SwaggerUIHandler)
	mux.HandleFunc("GET /api/v1/agentflows", agentFlows.ListDefinitions)
	mux.HandleFunc("POST /api/v1/agentflows/trigger", agentFlows.Trigger)
	mux.HandleFunc("GET /api/v1/runs", agentFlows.ListRuns)
	mux.HandleFunc("GET /api/v1/runs/{id}", agentFlows.GetRun)
	mux.HandleFunc("GET /api/v1/runs/{run_id}/tasks", agentFlows.GetTaskRuns)
	mux.HandleFunc("POST /api/v1/webhooks/{provider}", triggers.Webhook)
	mux.HandleFunc("POST /api/v1/webhooks/github", triggers.Webhook)
	mux.HandleFunc("POST /api/v1/human/{token}/approve", human.Approve)
	mux.HandleFunc("POST /api/v1/human/{token}/reject", human.Reject)
	return mux
}
