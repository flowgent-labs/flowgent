//go:build oidc
// +build oidc

package apiserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/oidc"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

const dexAddr = "localhost:5556"
const dexIssuer = "http://localhost:5556/dex"
const oidcCallbackPort = 58080

const oidcTestPrivKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIICo+88pwIcpxYaJQngpUwxWR4huhj3dd9yUyGM3936eoAoGCCqGSM49
AwEHoUQDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLuPRdwgkS1x97sRQJ44gVNZW0h
6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END EC PRIVATE KEY-----`

const oidcTestPubKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLu
PRdwgkS1x97sRQJ44gVNZW0h6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END PUBLIC KEY-----`

func dexAvailable(t *testing.T) bool {
	t.Helper()
	resp, err := http.Get(dexIssuer + "/.well-known/openid-configuration")
	if err != nil {
		t.Skipf("Dex OIDC server not available at %s: %v\n  Start with: cd deploy/docker/dex && podman run -d --name dex --network host --rm -v \"$(pwd)/config.yaml:/etc/dex/config.yaml:ro\" docker.io/dexidp/dex:v2.41.1 dex serve /etc/dex/config.yaml", dexIssuer, err)
		return false
	}
	resp.Body.Close()
	return true
}

func mustOIDCTokenService(t *testing.T) *auth.TokenService {
	t.Helper()
	cfg := config.AuthConfig{
		JWTAlgorithm: "ES256", JWTPrivateKey: oidcTestPrivKey, JWTPublicKey: oidcTestPubKey,
		JWTValidityAK: 3600, JWTValidityRK: 86400,
	}
	ts, err := auth.NewTokenService(cfg)
	if err != nil {
		t.Fatalf("create token service: %v", err)
	}
	return ts
}

func TestE2E_OIDC_Discovery(t *testing.T) {
	if !dexAvailable(t) {
		return
	}
	resp, err := http.Get(dexIssuer + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status = %d", resp.StatusCode)
	}
	var discovery map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	if discovery["issuer"] != dexIssuer {
		t.Errorf("issuer = %v, want %s", discovery["issuer"], dexIssuer)
	}
	if discovery["authorization_endpoint"] == "" {
		t.Error("missing authorization_endpoint")
	}
	if discovery["token_endpoint"] == "" {
		t.Error("missing token_endpoint")
	}
}

func TestE2E_OIDC_BuildAuthURL(t *testing.T) {
	if !dexAvailable(t) {
		return
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/auth/callback/oidc", oidcCallbackPort)
	cfg := config.OIDCConfig{
		Enabled: true, IssueURL: dexIssuer, ClientID: "flowgent-client",
		ClientSecret: "flowgent-secret", RedirectURL: redirectURL, Scope: "openid profile email",
	}
	ts := mustOIDCTokenService(t)
	svc := oidc.NewService(cfg, ts)

	req := httptest.NewRequest(http.MethodGet, "/auth/login/oidc", nil)
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("no Location header")
	}
	if !strings.Contains(location, dexIssuer+"/auth") {
		t.Errorf("redirect URL doesn't point to Dex auth endpoint: %s", location)
	}
	if !strings.Contains(location, "response_type=code") {
		t.Error("missing response_type=code")
	}
	if !strings.Contains(location, "client_id=flowgent-client") {
		t.Error("missing client_id")
	}
}

func TestE2E_OIDC_ServiceInterface(t *testing.T) {
	cfg := config.OIDCConfig{
		Enabled: true, IssueURL: dexIssuer, ClientID: "flowgent-client",
		ClientSecret: "flowgent-secret", RedirectURL: "http://localhost:58080/auth/callback/oidc",
		Scope: "openid profile email",
	}
	ts := mustOIDCTokenService(t)
	svc := oidc.NewService(cfg, ts)

	if svc.Name() != "oidc" {
		t.Errorf("Name = %q, want oidc", svc.Name())
	}
	if !svc.Enabled() {
		t.Error("should be enabled")
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/auth/login/oidc", nil)
	if !svc.CanHandle(loginReq) {
		t.Error("should handle GET /auth/login/oidc")
	}
	cbReq := httptest.NewRequest(http.MethodGet, "/auth/callback/oidc", nil)
	if !svc.CanHandle(cbReq) {
		t.Error("should handle GET /auth/callback/oidc")
	}
	badReq := httptest.NewRequest(http.MethodPost, "/auth/login/oidc", nil)
	if svc.CanHandle(badReq) {
		t.Error("should not handle POST /auth/login/oidc")
	}
}

func TestE2E_OIDC_FullFlow(t *testing.T) {
	if !dexAvailable(t) {
		return
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/auth/callback/oidc", oidcCallbackPort)
	oidcCfg := config.OIDCConfig{
		Enabled: true, IssueURL: dexIssuer, ClientID: "flowgent-client",
		ClientSecret: "flowgent-secret", RedirectURL: redirectURL, Scope: "openid profile email",
	}
	authCfg := config.AuthConfig{
		JWTAlgorithm: "ES256", JWTPrivateKey: oidcTestPrivKey, JWTPublicKey: oidcTestPubKey,
		JWTValidityAK: 3600, JWTValidityRK: 86400,
	}
	authSvc, err := auth.NewService(authCfg)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	oidcSvc := oidc.NewService(oidcCfg, authSvc.TokenService())
	authSvc.Register(oidcSvc)

	handler := authSvc.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"protected":"ok"}`))
	}))

	srv := &http.Server{Addr: fmt.Sprintf(":%d", oidcCallbackPort), Handler: handler}
	go srv.ListenAndServe()
	defer srv.Close()
	time.Sleep(100 * time.Millisecond)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	resp1, err := client.Get(fmt.Sprintf("http://localhost:%d/auth/login/oidc", oidcCallbackPort))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp1.Body.Close()

	body1, _ := io.ReadAll(resp1.Body)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("step 1: expected 200 after redirect chain, got %d: %s", resp1.StatusCode, string(body1))
	}
	var result map[string]any
	if err := json.Unmarshal(body1, &result); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("login failed: %v", result)
	}
	accessToken, _ := result["access_token"].(string)
	if accessToken == "" {
		t.Fatal("no access_token in response")
	}
	refreshToken, _ := result["refresh_token"].(string)
	if refreshToken == "" {
		t.Fatal("no refresh_token in response")
	}

	user, _ := result["user"].(map[string]any)
	t.Logf("OIDC login OK: user=%v", user)

	req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/api/protected", oidcCallbackPort), nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("protected request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body2, _ := io.ReadAll(resp2.Body)
		t.Errorf("step 2: protected endpoint status = %d, want 200: %s", resp2.StatusCode, string(body2))
	}
}
