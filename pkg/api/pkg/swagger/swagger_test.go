package swagger

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/_/openapi.yaml", nil)
	w := httptest.NewRecorder()
	NewOpenAPIHandler(DefaultSwaggerConfig())(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("expected application/yaml, got %s", ct)
	}
}

func TestSwaggerUIHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/_/swagger-ui", nil)
	w := httptest.NewRecorder()
	NewSwaggerUIHandler(DefaultSwaggerConfig())(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOASInfo(t *testing.T) {
	info := OASInfo()
	if info["title"] == "" {
		t.Error("title should not be empty")
	}
}

func TestOpenAPIUsesCanonicalResourceContracts(t *testing.T) {
	t.Parallel()
	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(embeddedSpec, &document); err != nil {
		t.Fatalf("parse embedded OpenAPI: %v", err)
	}
	required := []string{
		"/{namespace}/flows",
		"/{namespace}/runs/{run_id}/node-runs",
		"/{namespace}/runs/{run_id}/approvals/{approval_id}/{decision}",
		"/{namespace}/agents",
		"/{namespace}/skill-definitions/{name}/assets",
		"/{namespace}/mcp",
		"/{namespace}/llm/providers",
		"/{namespace}/notifications/channels",
		"/{namespace}/knowledge/candidates",
		"/{namespace}/knowledge/search",
	}
	for _, path := range required {
		if _, ok := document.Paths[path]; !ok {
			t.Errorf("canonical path %q is missing", path)
		}
	}
	for _, forbidden := range [][]byte{[]byte("/tasks"), []byte("{token}"), []byte("agentflow_id")} {
		if bytes.Contains(embeddedSpec, forbidden) {
			t.Errorf("legacy OpenAPI identifier %q is still present", forbidden)
		}
	}
	if _, writable := document.Paths["/{namespace}/knowledge"]["post"]; writable {
		t.Fatal("published Knowledge collection must not expose direct POST")
	}
}
