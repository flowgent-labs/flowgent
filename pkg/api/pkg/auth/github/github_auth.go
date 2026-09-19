// Package github provides browser SSO through GitHub OAuth.
package github

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"golang.org/x/oauth2"
)

const stateCookieName = "github_oauth_state"

// Service implements auth.AuthProviderService for GitHub OAuth browser login.
type Service struct {
	cfg          config.GitHubAuthConfig
	tokenService *auth.TokenService
	httpClient   *http.Client
}

func NewService(cfg config.GitHubAuthConfig, tokenService *auth.TokenService) *Service {
	return &Service{
		cfg:          withDefaults(cfg),
		tokenService: tokenService,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Service) Name() string  { return "github" }
func (p *Service) Enabled() bool { return p.cfg.Enabled }

func (p *Service) CanHandle(r *http.Request) bool {
	return (r.URL.Path == "/auth/login/github" || r.URL.Path == "/auth/callback/github") &&
		r.Method == http.MethodGet
}

func (p *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/login/github":
		p.handleLogin(w, r)
	case "/auth/callback/github":
		p.handleCallback(w, r)
	}
}

func (p *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(p.cfg.ClientID) == "" || strings.TrimSpace(p.cfg.ClientSecret) == "" {
		auth.WriteJSON(w, http.StatusServiceUnavailable,
			`{"success":false,"message":"GitHub OAuth is not configured"}`)
		return
	}
	state, err := randomState()
	if err != nil {
		slog.Error("github auth: state generation failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"GitHub OAuth state generation failed"}`)
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

	http.Redirect(w, r, p.oauthConfig(p.redirectURL(r)).AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
}

func (p *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || r.URL.Query().Get("state") != stateCookie.Value {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Invalid GitHub OAuth state parameter"}`)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Missing GitHub OAuth code"}`)
		return
	}

	token, err := p.exchangeCode(r.Context(), code, p.redirectURL(r))
	if err != nil {
		slog.Error("github auth: token exchange failed", "error", err)
		auth.WriteJSON(w, http.StatusBadGateway,
			`{"success":false,"message":"GitHub OAuth token exchange failed"}`)
		return
	}
	user, err := p.fetchUser(r.Context(), token)
	if err != nil {
		slog.Error("github auth: user lookup failed", "error", err)
		auth.WriteJSON(w, http.StatusBadGateway,
			`{"success":false,"message":"GitHub user lookup failed"}`)
		return
	}
	accessToken, err := p.tokenService.IssueAccessToken(user)
	if err != nil {
		slog.Error("github auth: JWT issuance failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"Token generation failed"}`)
		return
	}

	auth.ClearCookie(w, r, stateCookieName)
	slog.Info("github auth: login successful", "user", user.Username, "email", user.Email)
	auth.RedirectWithSession(w, r, accessToken, p.tokenService.AccessTokenTTL())
}

func (p *Service) exchangeCode(ctx context.Context, code, redirectURL string) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	token, err := p.oauthConfig(redirectURL).Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", errors.New("github token response missing access_token")
	}
	return token.AccessToken, nil
}

func (p *Service) fetchUser(ctx context.Context, token string) (*auth.UserInfo, error) {
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := p.getJSON(ctx, p.cfg.UserInfoURL, token, &user); err != nil {
		return nil, err
	}
	email := user.Email
	if email == "" {
		if primary, err := p.fetchPrimaryEmail(ctx, token); err == nil {
			email = primary
		}
	}
	if user.ID == 0 || user.Login == "" {
		return nil, errors.New("github user response missing id/login")
	}
	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}
	return &auth.UserInfo{
		UserID:      "github:" + strconv.FormatInt(user.ID, 10),
		Issuer:      "github",
		Username:    user.Login,
		Email:       email,
		DisplayName: displayName,
		Type:        entities.PrincipalUser,
		Extra: map[string]any{
			"github_id":  user.ID,
			"avatar_url": user.AvatarURL,
		},
	}, nil
}

func (p *Service) fetchPrimaryEmail(ctx context.Context, token string) (string, error) {
	emailURL := strings.TrimSuffix(p.cfg.UserInfoURL, "/user") + "/user/emails"
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := p.getJSON(ctx, emailURL, token, &emails); err != nil {
		return "", err
	}
	for _, candidate := range emails {
		if candidate.Primary && candidate.Verified && candidate.Email != "" {
			return candidate.Email, nil
		}
	}
	for _, candidate := range emails {
		if candidate.Verified && candidate.Email != "" {
			return candidate.Email, nil
		}
	}
	return "", errors.New("no verified GitHub email")
}

func (p *Service) getJSON(ctx context.Context, target, token string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		decoder = json.NewDecoder(bytes.NewReader(data))
		return decoder.Decode(dest)
	}
	return nil
}

func (p *Service) redirectURL(r *http.Request) string {
	if strings.TrimSpace(p.cfg.RedirectURL) != "" {
		return p.cfg.RedirectURL
	}
	return auth.ExternalScheme(r) + "://" + auth.ExternalHost(r) + "/auth/callback/github"
}

func (p *Service) oauthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       strings.Fields(p.cfg.Scope),
		Endpoint: oauth2.Endpoint{
			AuthURL:  p.cfg.AuthURL,
			TokenURL: p.cfg.TokenURL,
		},
	}
}

func withDefaults(cfg config.GitHubAuthConfig) config.GitHubAuthConfig {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = "https://api.github.com/user"
	}
	if cfg.Scope == "" {
		cfg.Scope = "read:user user:email"
	}
	return cfg
}

func randomState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
