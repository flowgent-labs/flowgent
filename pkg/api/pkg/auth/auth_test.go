package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

const testPrivKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIICo+88pwIcpxYaJQngpUwxWR4huhj3dd9yUyGM3936eoAoGCCqGSM49
AwEHoUQDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLuPRdwgkS1x97sRQJ44gVNZW0h
6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END EC PRIVATE KEY-----`

const testPubKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLu
PRdwgkS1x97sRQJ44gVNZW0h6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END PUBLIC KEY-----`

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		JWTAlgorithm:  "ES256",
		JWTPrivateKey: testPrivKey,
		JWTPublicKey:  testPubKey,
		JWTValidityAK: 3600,
		JWTValidityRK: 86400,
	}
}

// ── AuthService ─────────────────────────────────────────────────

func TestNewService(t *testing.T) {
	svc, err := NewService(testAuthConfig())
	if err != nil {
		t.Fatal(err)
	}
	if svc.TokenService() == nil {
		t.Fatal("TokenService should not be nil")
	}
}

func TestAuthService_Register(t *testing.T) {
	svc, _ := NewService(testAuthConfig())
	svc.Register(&stubProvider{name: "stub", enabled: true})
	if len(svc.providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(svc.providers))
	}
}

func TestAuthService_Middleware_RoutesToProvider(t *testing.T) {
	svc, _ := NewService(testAuthConfig())
	stub := &stubProvider{name: "test", enabled: true, handlesPath: "/auth/login/test"}
	svc.Register(stub)

	req := httptest.NewRequest("GET", "/auth/login/test", nil)
	w := httptest.NewRecorder()
	svc.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(w, req)

	if !stub.called {
		t.Error("provider should have been called")
	}
}

func TestAuthService_Middleware_DisabledProvider(t *testing.T) {
	svc, _ := NewService(testAuthConfig())
	stub := &stubProvider{name: "test", enabled: false, handlesPath: "/auth/login/test"}
	svc.Register(stub)

	req := httptest.NewRequest("GET", "/auth/login/test", nil)
	w := httptest.NewRecorder()
	svc.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(w, req)

	if stub.called {
		t.Error("disabled provider should not have been called")
	}
}

func TestAuthService_Middleware_AuthenticatedIdentityEndpoint(t *testing.T) {
	svc, _ := NewService(testAuthConfig())
	token, err := svc.TokenService().IssueAccessToken(&UserInfo{UserID: "u42", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	nextCalled := false
	handler := svc.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || nextCalled {
		t.Fatalf("status = %d, nextCalled = %v", w.Code, nextCalled)
	}
	if body := w.Body.String(); !strings.Contains(body, `"id":"u42"`) || !strings.Contains(body, `"username":"alice"`) {
		t.Fatalf("identity response = %s", body)
	}
}

// ── TokenService ────────────────────────────────────────────────

func TestTokenService_IssueAccessToken(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	user := &UserInfo{UserID: "u1", Username: "test", Role: "admin"}
	token, err := ts.IssueAccessToken(user)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
}

func TestTokenService_IssueRefreshToken(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	user := &UserInfo{UserID: "u1", Username: "test"}
	token, err := ts.IssueRefreshToken(user)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
}

func TestNewTokenService_DefaultAlgorithm(t *testing.T) {
	cfg := testAuthConfig()
	cfg.JWTAlgorithm = ""
	ts, err := NewTokenService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Algorithm() != "ES256" {
		t.Errorf("expected ES256, got %s", ts.Algorithm())
	}
}

// ── Standalone Middleware ───────────────────────────────────────

func TestMiddleware_AnonymousPaths(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	cfg := config.AuthConfig{AnonymousPaths: []string{"/public/**", "/health"}}
	mw := Middleware(cfg, ts)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/public/index.html", http.StatusOK},
		{"/public/a/b", http.StatusOK},
		{"/health", http.StatusOK},
		{"/api/private", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestMiddleware_MissingAuth(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	mw := Middleware(config.AuthConfig{}, ts)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/api/private", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_BadFormat(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	mw := Middleware(config.AuthConfig{}, ts)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/api/private", nil)
	req.Header.Set("Authorization", "Basic abc123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	mw := Middleware(config.AuthConfig{}, ts)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/api/private", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	ts, _ := NewTokenService(testAuthConfig())
	user := &UserInfo{UserID: "u42", Username: "alice", Role: "operator"}
	token, _ := ts.IssueAccessToken(user)

	mw := Middleware(config.AuthConfig{}, ts)
	var capturedCtxUser string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtxUser, _ = r.Context().Value(CtxUserID).(string)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if capturedCtxUser != "u42" {
		t.Errorf("ctx user = %q, want u42", capturedCtxUser)
	}
}

// ── matchGlob ───────────────────────────────────────────────────

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"/health", "/health", true},
		{"/health", "/healthz", false},
		{"/public/**", "/public/index.html", true},
		{"/public/**", "/public/a/b/c", true},
		{"/public/**", "/private/x", false},
		{"/public/**", "/public", true},
		{"/_/healthz/**", "/_/healthz/ready", true},
		{"/_/healthz/**", "/_/healthz", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.path, func(t *testing.T) {
			if matchGlob(tt.pattern, tt.path) != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, !tt.want, tt.want)
			}
		})
	}
}

// ── Key parsing ─────────────────────────────────────────────────

func TestParsePrivateKey_Valid(t *testing.T) {
	key, err := parsePrivateKey([]byte(testPrivKey))
	if err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("key should not be nil")
	}
}

func TestParsePrivateKey_Invalid(t *testing.T) {
	_, err := parsePrivateKey([]byte("not-a-key"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePublicKey_Valid(t *testing.T) {
	key, err := parsePublicKey([]byte(testPubKey))
	if err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("key should not be nil")
	}
}

func TestParsePublicKey_Invalid(t *testing.T) {
	_, err := parsePublicKey([]byte("not-a-key"))
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── stub provider ───────────────────────────────────────────────

type stubProvider struct {
	name        string
	enabled     bool
	handlesPath string
	called      bool
}

func (s *stubProvider) Name() string                   { return s.name }
func (s *stubProvider) Enabled() bool                  { return s.enabled }
func (s *stubProvider) CanHandle(r *http.Request) bool { return r.URL.Path == s.handlesPath }
func (s *stubProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called = true
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"access_token":"tok"}`))
}
