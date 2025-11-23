package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler_Healthz(t *testing.T) {
	h := &HealthHandler{}
	req := httptest.NewRequest("GET", "/_/healthz", nil)
	w := httptest.NewRecorder()
	h.Healthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOpenAPIHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/_/openapi.yaml", nil)
	w := httptest.NewRecorder()
	OpenAPIHandler(w, req)

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
	SwaggerUIHandler(w, req)

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

func TestContainsEvent(t *testing.T) {
	if !containsEvent([]string{"push", "pull_request"}, "push") {
		t.Error("push should be found")
	}
	if containsEvent([]string{"push"}, "issues") {
		t.Error("issues should not be found")
	}
}
