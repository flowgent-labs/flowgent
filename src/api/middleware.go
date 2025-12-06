package api

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── JWT Claims & Context keys ─────────────────────────────────

type contextKey string

const (
	CtxUserID   contextKey = "user_id"
	CtxJWTClaims contextKey = "jwt_claims"
	CtxRequestID contextKey = "request_id"
)

// CustomClaims extends the standard JWT claims with a UserID field.
type CustomClaims struct {
	jwt.RegisteredClaims
	UserID   int64  `json:"uid,omitempty"`
	Username string `json:"uname,omitempty"`
	Role     string `json:"role,omitempty"`
}

// ── JWT Auth Middleware ───────────────────────────────────────

// JWTAuthConfig configures the JWT authentication middleware.
type JWTAuthConfig struct {
	// Algorithm is the expected JWT signing algorithm (e.g. "ES256", "RS256", "EdDSA").
	Algorithm string
	// PublicKey is the PEM-encoded public key for token verification.
	PublicKey string
	// PublicKeyFile is a path to a PEM file (used if PublicKey is empty).
	PublicKeyFile string
	// AnonymousPaths are URL paths that skip auth checks. Supports glob "/*" suffix.
	AnonymousPaths []string
}

// JWTAuthMiddleware returns an http.Handler that validates JWT Bearer tokens.
// On success it injects user_id and jwt_claims into the request context.
func JWTAuthMiddleware(cfg JWTAuthConfig) func(http.Handler) http.Handler {
	publicKey, err := loadPublicKey(cfg)
	if err != nil {
		panic(fmt.Sprintf("JWTAuthMiddleware: failed to load public key: %v", err))
	}
	alg := cfg.Algorithm
	if alg == "" {
		alg = "ES256"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for anonymous paths
			for _, p := range cfg.AnonymousPaths {
				if matchPath(p, r.URL.Path) {
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
			tokenString := parts[1]

			claims := &CustomClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
				if t.Method.Alg() != alg {
					return nil, fmt.Errorf("unexpected signing algorithm: %s", t.Method.Alg())
				}
				return publicKey, nil
			})
			if err != nil || !token.Valid {
				writeAuthError(w, "Invalid or expired token")
				return
			}

			// Inject claims into context
			ctx := context.WithValue(r.Context(), CtxJWTClaims, claims)
			if claims.UserID != 0 {
				ctx = context.WithValue(ctx, CtxUserID, fmt.Sprintf("%d", claims.UserID))
			}
			if claims.Subject != "" && claims.UserID == 0 {
				ctx = context.WithValue(ctx, CtxUserID, claims.Subject)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Request ID Middleware ─────────────────────────────────────

// RequestIDMiddleware injects a unique X-Request-ID header into every request.
func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := r.Header.Get("X-Request-ID")
			if rid == "" {
				rid = newRequestID()
			}
			w.Header().Set("X-Request-ID", rid)
			ctx := context.WithValue(r.Context(), CtxRequestID, rid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────

func loadPublicKey(cfg JWTAuthConfig) (any, error) {
	pemData := cfg.PublicKey
	if pemData == "" && cfg.PublicKeyFile != "" {
		b, err := os.ReadFile(cfg.PublicKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read public key file: %w", err)
		}
		pemData = string(b)
	}
	if pemData == "" {
		return nil, errors.New("no public key provided")
	}
	return parsePublicKey([]byte(pemData))
}

func parsePublicKey(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	// Try PKIX (PKCS#8) first
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	// Try certificate
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		return cert.PublicKey, nil
	}
	// Try PKCS#1 RSA
	if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return pub, nil
	}
	return nil, fmt.Errorf("unsupported public key format")
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"success":false,"message":"%s"}`, msg)
}

func matchPath(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/**" {
		pfx := pattern[:len(pattern)-2]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}

func newRequestID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())[:16]
}
