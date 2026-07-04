package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ── OIDC HTTP client ──────────────────────────────────────────────

// oidcClient handles all HTTP interactions with the OIDC provider.
// Separated from oidc_auth.go to keep the provider logic clean.
type oidcClient struct {
	cfg        config.OIDCConfig
	httpClient *http.Client
}

func newOIDCClient(cfg config.OIDCConfig) *oidcClient {
	return &oidcClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// buildAuthURL performs OIDC discovery and constructs the authorization URL.
func (c *oidcClient) buildAuthURL(ctx context.Context) (string, string, error) {
	issuerURL := strings.TrimRight(c.cfg.IssueURL, "/")
	authEndpoint, err := c.discoverEndpoint(ctx, issuerURL, "authorization_endpoint")
	if err != nil {
		return "", "", err
	}

	state := generateState()
	nonce := generateState()

	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s&nonce=%s",
		authEndpoint,
		url.QueryEscape(c.cfg.ClientID),
		url.QueryEscape(c.cfg.RedirectURL),
		url.QueryEscape(c.cfg.Scope),
		url.QueryEscape(state),
		url.QueryEscape(nonce),
	)
	return authURL, state, nil
}

// exchangeCode exchanges the authorization code for tokens.
func (c *oidcClient) exchangeCode(ctx context.Context, code string) (map[string]any, error) {
	issuerURL := strings.TrimRight(c.cfg.IssueURL, "/")
	tokenEndpoint, err := c.discoverEndpoint(ctx, issuerURL, "token_endpoint")
	if err != nil {
		return nil, err
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.cfg.RedirectURL},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return tokenResp, nil
}

// extractUserInfo extracts user identity from ID token claims or the userinfo endpoint.
func (c *oidcClient) extractUserInfo(ctx context.Context, tokenResp map[string]any) (*auth.UserInfo, error) {
	user := &auth.UserInfo{Extra: make(map[string]any)}

	// Try ID token claims first
	if idToken, ok := tokenResp["id_token"].(string); ok {
		if claims, err := decodeJWTBody(idToken); err == nil {
			if sub, ok := claims["sub"].(string); ok {
				user.UserID = sub
			}
			if name, ok := claims["preferred_username"].(string); ok {
				user.Username = name
			} else if name, ok := claims["email"].(string); ok {
				user.Username = name
			}
			if email, ok := claims["email"].(string); ok {
				user.Email = email
			}
			if name, ok := claims["name"].(string); ok {
				user.DisplayName = name
			}
		}
	}

	// Fallback: call userinfo endpoint
	if user.UserID == "" {
		if accessToken, ok := tokenResp["access_token"].(string); ok {
			issuerURL := strings.TrimRight(c.cfg.IssueURL, "/")
			userinfoEndpoint, err := c.discoverEndpoint(ctx, issuerURL, "userinfo_endpoint")
			if err == nil {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, userinfoEndpoint, nil)
				req.Header.Set("Authorization", "Bearer "+accessToken)
				resp, err := c.httpClient.Do(req)
				if err == nil {
					defer resp.Body.Close()
					var info map[string]any
					if json.NewDecoder(resp.Body).Decode(&info) == nil {
						if sub, ok := info["sub"].(string); ok {
							user.UserID = sub
						}
						if name, ok := info["preferred_username"].(string); ok {
							user.Username = name
						}
						if email, ok := info["email"].(string); ok {
							user.Email = email
						}
						if name, ok := info["name"].(string); ok {
							user.DisplayName = name
						}
					}
				}
			}
		}
	}

	if user.UserID == "" {
		return nil, fmt.Errorf("unable to extract user identity from token response")
	}
	return user, nil
}

// discoverEndpoint fetches a specific endpoint from the OIDC discovery document.
func (c *oidcClient) discoverEndpoint(ctx context.Context, issuerURL, key string) (string, error) {
	wellKnown := issuerURL + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch discovery doc: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery doc returned status %d", resp.StatusCode)
	}

	var discovery map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return "", fmt.Errorf("decode discovery doc: %w", err)
	}

	endpoint, ok := discovery[key].(string)
	if !ok || endpoint == "" {
		return "", fmt.Errorf("no %s in discovery doc", key)
	}
	return endpoint, nil
}

// ── Helpers ───────────────────────────────────────────────────────

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// decodeJWTBody extracts claims from a JWT body without verifying the signature.
func decodeJWTBody(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid JWT format")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}
