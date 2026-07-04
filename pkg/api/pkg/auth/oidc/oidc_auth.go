// Package oidc provides OpenID Connect authentication.
//
// It implements the authorization code flow: discovery → redirect → callback →
// token exchange → userinfo → JWT issuance.
package oidc

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ── Provider ──────────────────────────────────────────────────────

// Provider implements auth.AuthProvider for OpenID Connect authentication.
type Provider struct {
	cfg          config.OIDCConfig
	tokenService *auth.TokenService
	client       *oidcClient
}

// NewProvider creates an OIDC auth provider.
func NewProvider(cfg config.OIDCConfig, tokenService *auth.TokenService) *Provider {
	return &Provider{
		cfg:          cfg,
		tokenService: tokenService,
		client:       newOIDCClient(cfg),
	}
}

func (p *Provider) Name() string  { return "oidc" }
func (p *Provider) Enabled() bool { return p.cfg.Enabled }

// RegisterRoutes registers OIDC login and callback endpoints.
func (p *Provider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login/oidc", p.handleLogin)
	mux.HandleFunc("GET /auth/callback/oidc", p.handleCallback)
}

// handleLogin initiates the OIDC authorization code flow.
func (p *Provider) handleLogin(w http.ResponseWriter, r *http.Request) {
	authURL, state, err := p.client.buildAuthURL(r.Context())
	if err != nil {
		slog.Error("oidc: build auth URL failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"OIDC provider discovery failed"}`)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	slog.Debug("oidc: redirecting to provider")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback processes the OIDC authorization callback.
func (p *Provider) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Invalid OIDC state parameter"}`)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Missing authorization code"}`)
		return
	}

	tokenResp, err := p.client.exchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("oidc: token exchange failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"OIDC token exchange failed"}`)
		return
	}

	user, err := p.client.extractUserInfo(r.Context(), tokenResp)
	if err != nil {
		slog.Error("oidc: userinfo extraction failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"Failed to extract user info"}`)
		return
	}

	accessToken, err := p.tokenService.IssueAccessToken(user)
	if err != nil {
		slog.Error("oidc: JWT issuance failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"Token generation failed"}`)
		return
	}

	refreshToken, _ := p.tokenService.IssueRefreshToken(user)

	slog.Info("oidc: login successful", "user", user.Username, "email", user.Email)

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: "", Path: "/", MaxAge: -1})

	// Return tokens as JSON
	resp := map[string]any{
		"success":       true,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"user": map[string]any{
			"id":           user.UserID,
			"username":     user.Username,
			"email":        user.Email,
			"display_name": user.DisplayName,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
