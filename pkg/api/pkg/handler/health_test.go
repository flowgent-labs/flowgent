package handler

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
