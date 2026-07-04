package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/ldap"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/oidc"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// SetupAuth creates the TokenService, builds all enabled auth providers,
// registers their routes on the given mux, and returns a JWT validation
// middleware. This is the single entry point for wiring authentication.
func SetupAuth(cfg config.AuthConfig, mux *http.ServeMux) (func(http.Handler) http.Handler, error) {
	tokenService, err := auth.NewTokenService(cfg)
	if err != nil {
		return nil, fmt.Errorf("auth: token service: %w", err)
	}

	providers := []auth.AuthProvider{
		oidc.NewProvider(cfg.OIDC, tokenService),
		ldap.NewProvider(cfg.LDAP, tokenService),
	}

	authPaths := registerAuthRoutes(mux, providers)

	allAnon := make([]string, len(cfg.AnonymousPaths)+len(authPaths))
	copy(allAnon, cfg.AnonymousPaths)
	copy(allAnon[len(cfg.AnonymousPaths):], authPaths)

	cfgWithAuth := cfg
	cfgWithAuth.AnonymousPaths = allAnon

	return auth.Middleware(cfgWithAuth, tokenService), nil
}

func registerAuthRoutes(mux *http.ServeMux, providers []auth.AuthProvider) []string {
	var anonPaths []string
	for _, p := range providers {
		if !p.Enabled() {
			continue
		}
		p.RegisterRoutes(mux)
		anonPaths = append(anonPaths,
			"/auth/login/"+p.Name(),
			"/auth/callback/"+p.Name(),
		)
		slog.Info("auth: provider registered", "name", p.Name())
	}
	return anonPaths
}
