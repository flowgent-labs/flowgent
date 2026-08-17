package oidc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ── Service basics ──────────────────────────────────────────────

func TestService_Name(t *testing.T) {
	svc := NewService(config.OIDCConfig{}, nil)
	if svc.Name() != "oidc" {
		t.Errorf("Name = %q, want oidc", svc.Name())
	}
}

func TestService_Enabled(t *testing.T) {
	svc := NewService(config.OIDCConfig{Enabled: true}, nil)
	if !svc.Enabled() {
		t.Error("should be enabled")
	}
	svc2 := NewService(config.OIDCConfig{Enabled: false}, nil)
	if svc2.Enabled() {
		t.Error("should not be enabled")
	}
}

func TestService_CanHandle(t *testing.T) {
	svc := NewService(config.OIDCConfig{}, nil)

	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/auth/login/oidc", true},
		{http.MethodGet, "/auth/callback/oidc", true},
		{http.MethodPost, "/auth/login/oidc", false},
		{http.MethodGet, "/auth/login/ldap", false},
		{http.MethodGet, "/other", false},
	}

	for _, tt := range tests {
		t.Run(tt.method+"/"+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if svc.CanHandle(req) != tt.want {
				t.Errorf("CanHandle = %v, want %v", svc.CanHandle(req), tt.want)
			}
		})
	}
}

// ── OIDC Client ─────────────────────────────────────────────────

func TestNewOIDCClient(t *testing.T) {
	cfg := config.OIDCConfig{
		IssueURL: "http://localhost:8080/realms/master",
		ClientID: "flowgent",
		Scope:    "openid profile email",
	}
	client := newOIDCClient(cfg)
	if client == nil {
		t.Fatal("client should not be nil")
	}
}
