// Package auth provides a unified authentication layer with pluggable providers.
// Each provider (OIDC, LDAP) lives in its own sub-package, implements the
// AuthProvider interface, and is independently configurable.
package auth

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ── Context keys ──────────────────────────────────────────────────

type contextKey string

const (
	CtxUserID    contextKey = "user_id"
	CtxJWTClaims contextKey = "jwt_claims"
	CtxUserRole  contextKey = "user_role"
)

// ── UserInfo ─────────────────────────────────────────────────────

// UserInfo is the normalized user representation returned by all auth providers.
type UserInfo struct {
	UserID      string
	Username    string
	Email       string
	DisplayName string
	Role        string
	Groups      []string
	Extra       map[string]any
}

// ── AuthProvider interface ────────────────────────────────────────

// AuthProvider is implemented by every authentication provider (OIDC, LDAP, etc.).
type AuthProvider interface {
	Name() string
	Enabled() bool
	RegisterRoutes(mux *http.ServeMux)
}

// ── JWT Token Service ─────────────────────────────────────────────

// TokenService creates and validates JWT tokens for authenticated users.
type TokenService struct {
	algorithm  string
	privateKey any
	publicKey  any
	akValidity time.Duration
	rkValidity time.Duration
}

// NewTokenService creates a TokenService from the auth configuration.
func NewTokenService(cfg config.AuthConfig) (*TokenService, error) {
	alg := cfg.JWTAlgorithm
	if alg == "" {
		alg = "ES256"
	}
	privateKey, err := parsePrivateKey([]byte(cfg.JWTPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("auth: parse private key: %w", err)
	}
	publicKey, err := parsePublicKey([]byte(cfg.JWTPublicKey))
	if err != nil {
		return nil, fmt.Errorf("auth: parse public key: %w", err)
	}
	akValidity := time.Duration(cfg.JWTValidityAK) * time.Second
	if akValidity <= 0 {
		akValidity = 3600 * time.Second
	}
	rkValidity := time.Duration(cfg.JWTValidityRK) * time.Second
	if rkValidity <= 0 {
		rkValidity = 86400 * time.Second
	}
	return &TokenService{
		algorithm:  alg,
		privateKey: privateKey,
		publicKey:  publicKey,
		akValidity: akValidity,
		rkValidity: rkValidity,
	}, nil
}

// IssueAccessToken creates a short-lived access token.
func (s *TokenService) IssueAccessToken(user *UserInfo) (string, error) {
	return s.issueToken(user, s.akValidity)
}

// IssueRefreshToken creates a long-lived refresh token.
func (s *TokenService) IssueRefreshToken(user *UserInfo) (string, error) {
	return s.issueToken(user, s.rkValidity)
}

func (s *TokenService) issueToken(user *UserInfo, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Issuer:    "flowgent",
		Subject:   user.UserID,
		Audience:  jwt.ClaimStrings{"flowgent"},
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
	}
	token := jwt.NewWithClaims(s.signingMethod(), claims)
	token.Header["uid"] = user.UserID
	token.Header["uname"] = user.Username
	token.Header["role"] = user.Role

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

func (s *TokenService) signingMethod() jwt.SigningMethod {
	switch s.algorithm {
	case "RS256":
		return jwt.SigningMethodRS256
	case "RS384":
		return jwt.SigningMethodRS384
	case "RS512":
		return jwt.SigningMethodRS512
	case "ES384":
		return jwt.SigningMethodES384
	case "ES512":
		return jwt.SigningMethodES512
	case "EdDSA":
		return jwt.SigningMethodEdDSA
	default:
		return jwt.SigningMethodES256
	}
}

// PublicKey returns the parsed public key for JWT validation middleware.
func (s *TokenService) PublicKey() any { return s.publicKey }

// Algorithm returns the configured JWT signing algorithm.
func (s *TokenService) Algorithm() string { return s.algorithm }

// ── Auth Middleware ────────────────────────────────────────────────

// Middleware returns an http.Handler wrapper that:
//  1. Passes through anonymous paths without authentication
//  2. Validates JWT Bearer tokens on all other requests
//  3. Injects UserInfo into the request context
func Middleware(cfg config.AuthConfig, tokenService *TokenService) func(http.Handler) http.Handler {
	anonymousPaths := cfg.AnonymousPaths
	alg := tokenService.Algorithm()
	publicKey := tokenService.PublicKey()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range anonymousPaths {
				if matchGlob(p, r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, "Authorization header is required")
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAuthError(w, "Invalid Authorization header format")
				return
			}

			claims := &jwt.RegisteredClaims{}
			token, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (any, error) {
				if t.Method.Alg() != alg {
					return nil, fmt.Errorf("unexpected signing algorithm: %s", t.Method.Alg())
				}
				return publicKey, nil
			})
			if err != nil || !token.Valid {
				writeAuthError(w, "Invalid or expired token")
				return
			}

			user := &UserInfo{
				UserID: claims.Subject,
				Extra:  make(map[string]any),
			}
			if uid, ok := token.Header["uid"].(string); ok {
				user.UserID = uid
			}
			if uname, ok := token.Header["uname"].(string); ok {
				user.Username = uname
			}
			if role, ok := token.Header["role"].(string); ok {
				user.Role = role
			}

			ctx := context.WithValue(r.Context(), CtxJWTClaims, claims)
			ctx = context.WithValue(ctx, CtxUserID, user.UserID)
			ctx = context.WithValue(ctx, CtxUserRole, user.Role)

			slog.Debug("auth: JWT validated", "user", user.UserID, "username", user.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Key parsing ───────────────────────────────────────────────────

func parsePrivateKey(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: failed to decode PEM block")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("auth: unsupported private key format")
}

func parsePublicKey(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("auth: failed to decode PEM block")
	}
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		return cert.PublicKey, nil
	}
	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	return nil, errors.New("auth: unsupported public key format")
}

// ── Shared helpers ────────────────────────────────────────────────

func matchGlob(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/**" {
		pfx := pattern[:len(pattern)-2]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"success":false,"message":"%s"}`, msg)
}

// WriteJSON writes a JSON response. Exported for provider sub-packages.
func WriteJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}
