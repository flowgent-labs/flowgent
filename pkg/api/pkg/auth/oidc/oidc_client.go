package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	oidcprovider "github.com/coreos/go-oidc/v3/oidc"
	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"golang.org/x/oauth2"
)

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

func (c *oidcClient) buildAuthURL(ctx context.Context) (authURL, state, nonce string, err error) {
	provider, err := c.provider(ctx)
	if err != nil {
		return "", "", "", err
	}
	state = randomURLToken()
	nonce = randomURLToken()
	return c.oauthConfig(provider.Endpoint()).AuthCodeURL(state, oidcprovider.Nonce(nonce)), state, nonce, nil
}

func (c *oidcClient) exchangeCode(ctx context.Context, code string) (*oauth2.Token, error) {
	provider, err := c.provider(ctx)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.httpClient)
	token, err := c.oauthConfig(provider.Endpoint()).Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc token exchange: %w", err)
	}
	if !token.Valid() {
		return nil, fmt.Errorf("oidc token exchange returned invalid token")
	}
	return token, nil
}

func (c *oidcClient) extractUserInfo(ctx context.Context, token *oauth2.Token, nonce string) (*auth.UserInfo, error) {
	provider, err := c.provider(ctx)
	if err != nil {
		return nil, err
	}
	user := &auth.UserInfo{
		Issuer: "oidc:" + strings.TrimRight(c.cfg.IssueURL, "/"),
		Type:   entities.PrincipalUser,
		Extra:  make(map[string]any),
	}

	if rawIDToken, ok := token.Extra("id_token").(string); ok && rawIDToken != "" {
		verifier := provider.Verifier(&oidcprovider.Config{ClientID: c.cfg.ClientID})
		idToken, err := verifier.Verify(ctx, rawIDToken)
		if err != nil {
			return nil, fmt.Errorf("oidc id_token verify: %w", err)
		}
		if nonce != "" && idToken.Nonce != nonce {
			return nil, fmt.Errorf("oidc nonce mismatch")
		}
		var claims struct {
			Subject           string   `json:"sub"`
			PreferredUsername string   `json:"preferred_username"`
			Email             string   `json:"email"`
			Name              string   `json:"name"`
			Groups            []string `json:"groups"`
		}
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("oidc id_token claims: %w", err)
		}
		applyClaims(user, claims.Subject, claims.PreferredUsername, claims.Email, claims.Name, claims.Groups)
	}

	if user.UserID == "" {
		ctx = oidcprovider.ClientContext(ctx, c.httpClient)
		userInfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil {
			return nil, fmt.Errorf("oidc userinfo: %w", err)
		}
		var claims struct {
			Subject           string   `json:"sub"`
			PreferredUsername string   `json:"preferred_username"`
			Email             string   `json:"email"`
			Name              string   `json:"name"`
			Groups            []string `json:"groups"`
		}
		if err := userInfo.Claims(&claims); err != nil {
			return nil, fmt.Errorf("oidc userinfo claims: %w", err)
		}
		applyClaims(user, claims.Subject, claims.PreferredUsername, claims.Email, claims.Name, claims.Groups)
	}
	if user.UserID == "" {
		return nil, fmt.Errorf("oidc user identity is missing subject")
	}
	return user, nil
}

func (c *oidcClient) provider(ctx context.Context) (*oidcprovider.Provider, error) {
	ctx = oidcprovider.ClientContext(ctx, c.httpClient)
	return oidcprovider.NewProvider(ctx, strings.TrimRight(c.cfg.IssueURL, "/"))
}

func (c *oidcClient) oauthConfig(endpoint oauth2.Endpoint) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		RedirectURL:  c.cfg.RedirectURL,
		Scopes:       oidcScopes(c.cfg.Scope),
		Endpoint:     endpoint,
	}
}

func oidcScopes(scope string) []string {
	fields := strings.Fields(scope)
	for _, field := range fields {
		if field == "openid" {
			return fields
		}
	}
	return append([]string{"openid"}, fields...)
}

func applyClaims(user *auth.UserInfo, sub, preferredUsername, email, name string, groups []string) {
	user.UserID = sub
	user.Username = preferredUsername
	if user.Username == "" {
		user.Username = email
	}
	if user.Username == "" {
		user.Username = sub
	}
	user.Email = email
	user.DisplayName = name
	user.Groups = append([]string(nil), groups...)
}

func randomURLToken() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}
