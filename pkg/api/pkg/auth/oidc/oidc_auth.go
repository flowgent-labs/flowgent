// Package oidc provides OpenID Connect authentication.
//
// It implements the authorization code flow: discovery → redirect → callback →
// token exchange → userinfo → JWT issuance.
package oidc

import (
	"log/slog"
	"net/http"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

const (
	stateCookieName = "oidc_state"
	nonceCookieName = "oidc_nonce"
)

// ── Provider ──────────────────────────────────────────────────────

// Service implements auth.AuthProviderService for OpenID Connect authentication.
type Service struct {
	cfg          config.OIDCConfig
	tokenService *auth.TokenService
	client       *oidcClient
}

// NewService creates an OIDC auth service.
func NewService(cfg config.OIDCConfig, tokenService *auth.TokenService) *Service {
	return &Service{
		cfg:          cfg,
		tokenService: tokenService,
		client:       newOIDCClient(cfg),
	}
}

func (p *Service) Name() string  { return "oidc" }
func (p *Service) Enabled() bool { return p.cfg.Enabled }

// CanHandle reports whether this service handles the given request.
func (p *Service) CanHandle(r *http.Request) bool {
	return (r.URL.Path == "/auth/login/oidc" || r.URL.Path == "/auth/callback/oidc") &&
		r.Method == http.MethodGet
}

// ServeHTTP routes OIDC requests to the appropriate handler.
func (p *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/login/oidc":
		p.handleLogin(w, r)
	case "/auth/callback/oidc":
		p.handleCallback(w, r)
	}
}

// handleLogin initiates the OIDC authorization code flow.
func (p *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	authURL, state, nonce, err := p.client.buildAuthURL(r.Context())
	if err != nil {
		slog.Error("oidc: build auth URL failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"OIDC provider discovery failed"}`)
		return
	}

	isTLS := auth.ExternalScheme(r) == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     nonceCookieName,
		Value:    nonce,
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	slog.Debug("oidc: redirecting to provider")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback processes the OIDC authorization callback.
func (p *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Invalid OIDC state parameter"}`)
		return
	}
	nonceCookie, err := r.Cookie(nonceCookieName)
	if err != nil || nonceCookie.Value == "" {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Missing OIDC nonce"}`)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Missing authorization code"}`)
		return
	}

	token, err := p.client.exchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("oidc: token exchange failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"OIDC token exchange failed"}`)
		return
	}

	user, err := p.client.extractUserInfo(r.Context(), token, nonceCookie.Value)
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

	slog.Info("oidc: login successful", "user", user.Username, "email", user.Email)

	auth.ClearCookie(w, r, stateCookieName)
	auth.ClearCookie(w, r, nonceCookieName)

	auth.RedirectWithSession(w, r, accessToken, p.tokenService.AccessTokenTTL())
}
