package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

// OpenAPIHandler serves the OAS specification document.
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(openAPISpec)
}

// SwaggerUIHandler serves an inline Swagger UI page.
func SwaggerUIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(swaggerHTML))
}

const swaggerHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Flowgent API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.10.5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.10.5/swagger-ui-bundle.js"></script>
  <script>
    const ui = SwaggerUIBundle({url:"/_/openapi.yaml",dom_id:"#swagger-ui",presets:[SwaggerUIBundle.presets.apis,SwaggerUIStandalonePreset]});
  </script>
</body>
</html>
`

// OASInfo returns basic API metadata.
func OASInfo() map[string]any {
	return map[string]any{
		"spec_url": "/_/openapi.yaml",
		"ui_url":   "/_/swagger-ui",
		"version":  "3.1.0",
		"title":    "Flowgent AgentFlow Orchestration API",
	}
}
