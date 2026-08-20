package swagger

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
