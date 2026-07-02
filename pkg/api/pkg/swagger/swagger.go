package swagger

import (
	_ "embed"
	"fmt"
	"net/http"
)

//go:embed openapi.yaml
var embeddedSpec []byte

// SwaggerConfig holds parameters for the Swagger UI and OpenAPI endpoints.
type SwaggerConfig struct {
	Title       string // page title
	SpecURL     string // URL path to the OpenAPI spec (e.g. "/_/openapi.yaml")
	SpecContent []byte // inline spec content; if nil, uses embedded openapi.yaml
}

// DefaultSwaggerConfig returns a SwaggerConfig pre-filled for Flowgent.
func DefaultSwaggerConfig() SwaggerConfig {
	return SwaggerConfig{
		Title:       "Flowgent API",
		SpecURL:     "/_/openapi.yaml",
		SpecContent: embeddedSpec,
	}
}

// ── Handlers ──────────────────────────────────────────────────

// NewOpenAPIHandler returns an http.HandlerFunc that serves the raw OAS document.
func NewOpenAPIHandler(cfg SwaggerConfig) http.HandlerFunc {
	content := cfg.SpecContent
	if content == nil {
		content = embeddedSpec
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(content)
	}
}

// NewSwaggerUIHandler returns an http.HandlerFunc that renders the Swagger UI page.
func NewSwaggerUIHandler(cfg SwaggerConfig) http.HandlerFunc {
	html := buildSwaggerHTML(cfg.Title, cfg.SpecURL)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}
}

// NewHealthHandler returns a simple health-check handler.
func NewHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
}

// ── Helpers ───────────────────────────────────────────────────

func buildSwaggerHTML(title, specURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.10.5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.10.5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({ url: "%s", dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.StandalonePreset] });
  </script>
</body>
</html>`, title, specURL)
}

// ── Legacy aliases (backwards-compatible) ─────────────────────

// OpenAPIHandler serves the embedded OAS spec.
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	NewOpenAPIHandler(DefaultSwaggerConfig())(w, r)
}

// SwaggerUIHandler serves the Swagger UI page.
func SwaggerUIHandler(w http.ResponseWriter, r *http.Request) {
	NewSwaggerUIHandler(DefaultSwaggerConfig())(w, r)
}

// OASInfo returns basic API metadata.
func OASInfo() map[string]any {
	return map[string]any{
		"spec_url": "/_/openapi.yaml",
		"ui_url":   "/_/swagger-ui",
		"version":  "3.1.0",
		"title":    "Flowgent AgentFlow Orchestration API",
	}
}
