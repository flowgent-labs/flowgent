//go:build ldap
// +build ldap

package apiserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	ldap_auth "github.com/flowgent-labs/flowgent/api/pkg/auth/ldap"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

const glauthAddr = "localhost:3389"
const glauthBaseDN = "dc=example,dc=com"

const testPrivateKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIICo+88pwIcpxYaJQngpUwxWR4huhj3dd9yUyGM3936eoAoGCCqGSM49
AwEHoUQDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLuPRdwgkS1x97sRQJ44gVNZW0h
6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END EC PRIVATE KEY-----`

const testPublicKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEipUhjeQH8TLc1KXQ+NZjfovpdZLu
PRdwgkS1x97sRQJ44gVNZW0h6qqAmNRYMzoW/cS87D5yB1tSV/PtFC0E9A==
-----END PUBLIC KEY-----`

type testUser struct {
	username string
	password string
	wantRole string
	domain   string
}

var testUsers = []testUser{
	{username: "jdoe", password: "password123", wantRole: "admin", domain: "corp"},
	{username: "asmith", password: "password123", wantRole: "operator", domain: "corp"},
	{username: "bwilson", password: "password123", wantRole: "", domain: "corp"},
	{username: "cjones", password: "password123", wantRole: "admin", domain: "eng"},
	{username: "dlee", password: "password123", wantRole: "admin", domain: "eng"},
}

func ldapAvailable(t *testing.T) bool {
	t.Helper()
	conn, err := ldap_auth.DialURL("ldap://"+glauthAddr, 2*time.Second, false)
	if err != nil {
		t.Skipf("GLAuth LDAP server not available at %s: %v\n  Start with: cd deploy/docker/glauth && docker compose up -d", glauthAddr, err)
		return false
	}
	conn.Close()
	return true
}

func mustTokenService(t *testing.T) *auth.TokenService {
	t.Helper()
	cfg := config.AuthConfig{
		JWTAlgorithm: "ES256", JWTPrivateKey: testPrivateKey, JWTPublicKey: testPublicKey,
		JWTValidityAK: 3600, JWTValidityRK: 86400,
	}
	ts, err := auth.NewTokenService(cfg)
	if err != nil {
		t.Fatalf("create token service: %v", err)
	}
	return ts
}

func testLDAPConfig() config.LDAPConfig {
	return config.LDAPConfig{
		Enabled: true, URL: "ldap://" + glauthAddr, BaseDN: glauthBaseDN,
		UserDN: "cn=svc-flowgent," + glauthBaseDN, Password: "password123",
		Domains:              []config.LDAPDomainConfig{{BaseDN: "dc=example,dc=com", UserSearchFilter: "(cn=%s)"}},
		UserSearchFilter:     "(cn=%s)",
		UsernameAttribute:    "cn",
		EmailAttribute:       "mail",
		DisplayNameAttribute: "givenname",
		GroupSearchBase:      "ou=groups,dc=example,dc=com",
		GroupSearchFilter:    "(uniqueMember=%s)",
		GroupNameAttribute:   "ou",
		InsecureSkipVerify:   false,
		RoleMapping:          []config.LDAPRoleMapping{{Match: "FlowgentAdmins", Role: "admin"}, {Match: "FlowgentOps", Role: "operator"}},
	}
}

func TestE2E_LDAP_Authenticate_Success(t *testing.T) {
	if !ldapAvailable(t) {
		return
	}
	cfg := testLDAPConfig()
	tokenService := mustTokenService(t)
	provider := ldap_auth.NewService(cfg, tokenService)

	for _, tu := range testUsers {
		t.Run(tu.username, func(t *testing.T) {
			body := fmt.Sprintf(`{"username":"%s","password":"%s"}`, tu.username, tu.password)
			req := httptest.NewRequest(http.MethodPost, "/auth/login/ldap", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			provider.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
			}
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result["success"] != true {
				t.Fatalf("login failed: %v", result)
			}
			accessToken, _ := result["access_token"].(string)
			if accessToken == "" {
				t.Fatal("no access_token in response")
			}
			user, _ := result["user"].(map[string]any)
			if user["username"] != tu.username {
				t.Errorf("username = %v, want %s", user["username"], tu.username)
			}
			if user["role"] != tu.wantRole {
				t.Errorf("role = %v, want %s", user["role"], tu.wantRole)
			}
		})
	}
}

func TestE2E_LDAP_Authenticate_InvalidPassword(t *testing.T) {
	if !ldapAvailable(t) {
		return
	}
	cfg := testLDAPConfig()
	provider := ldap_auth.NewService(cfg, mustTokenService(t))

	body := `{"username":"jdoe","password":"wrong_password"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login/ldap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	provider.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestE2E_LDAP_Authenticate_NonexistentUser(t *testing.T) {
	if !ldapAvailable(t) {
		return
	}
	cfg := testLDAPConfig()
	provider := ldap_auth.NewService(cfg, mustTokenService(t))

	body := `{"username":"nonexistent","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login/ldap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	provider.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestE2E_LDAP_JWTTokenValidation(t *testing.T) {
	if !ldapAvailable(t) {
		return
	}
	cfg := testLDAPConfig()
	tokenService := mustTokenService(t)
	provider := ldap_auth.NewService(cfg, tokenService)

	body := `{"username":"jdoe","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login/ldap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	provider.ServeHTTP(w, req)

	var result map[string]any
	json.NewDecoder(w.Result().Body).Decode(&result)
	w.Result().Body.Close()

	accessToken, ok := result["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatal("no access token received")
	}

	protectedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(auth.CtxUserID)
		role := r.Context().Value(auth.CtxUserRole)
		auth.WriteJSON(w, http.StatusOK, fmt.Sprintf(`{"user_id":"%s","role":"%s"}`, userID, role))
	})

	authCfg := config.AuthConfig{JWTAlgorithm: "ES256"}
	middleware := auth.Middleware(authCfg, tokenService)
	wrappedHandler := middleware(protectedHandler)

	req2 := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	w2 := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("protected endpoint: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	w3 := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Errorf("no token: expected 401, got %d", w3.Code)
	}
}

func TestE2E_LDAP_DirectSearch_MultiDomain(t *testing.T) {
	if !ldapAvailable(t) {
		return
	}
	conn, err := ldap_auth.DialURL("ldap://"+glauthAddr, 5*time.Second, false)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.Bind("cn=svc-flowgent,"+glauthBaseDN, "password123"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	req := ldap_auth.SearchRequest{
		BaseDN: glauthBaseDN, Scope: ldap_auth.ScopeWholeSubtree,
		Filter: "(cn=jdoe)", Attributes: []string{"cn", "mail", "givenName", "memberOf", "dn"},
		SizeLimit: 1, TimeLimit: 5,
	}
	result, err := conn.Search(&req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Entries) == 0 {
		t.Fatal("user jdoe not found")
	}
	entry := result.Entries[0]
	if mail := entry.Attributes["mail"]; len(mail) == 0 || mail[0] != "jdoe@corp.example.com" {
		t.Errorf("mail = %v, want [jdoe@corp.example.com]", mail)
	}
	if name := entry.Attributes["cn"]; len(name) == 0 || name[0] != "jdoe" {
		t.Errorf("cn = %v, want [jdoe]", name)
	}
}

func TestE2E_LDAP_Middleware_AnonymousPaths(t *testing.T) {
	tokenService := mustTokenService(t)
	anonymousHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	cfg := config.AuthConfig{
		JWTAlgorithm: "ES256", AnonymousPaths: []string{"/public/**", "/_/healthz", "/_/healthz/**"},
	}
	middleware := auth.Middleware(cfg, tokenService)
	wrappedHandler := middleware(anonymousHandler)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/public/index.html", http.StatusOK},
		{"/public/api/data", http.StatusOK},
		{"/_/healthz", http.StatusOK},
		{"/_/healthz/ready", http.StatusOK},
		{"/api/private", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("path=%s status=%d, want %d", tt.path, w.Code, tt.wantStatus)
			}
		})
	}
}

func TestE2E_LDAP_RoleMapping(t *testing.T) {
	cfg := testLDAPConfig()
	for _, tu := range testUsers {
		t.Run(tu.username, func(t *testing.T) {
			if !ldapAvailable(t) {
				return
			}
			tokenService := mustTokenService(t)
			provider := ldap_auth.NewService(cfg, tokenService)

			body := fmt.Sprintf(`{"username":"%s","password":"%s"}`, tu.username, tu.password)
			req := httptest.NewRequest(http.MethodPost, "/auth/login/ldap", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			provider.ServeHTTP(w, req)

			var result map[string]any
			json.NewDecoder(w.Result().Body).Decode(&result)
			w.Result().Body.Close()

			user, _ := result["user"].(map[string]any)
			role, _ := user["role"].(string)
			if role != tu.wantRole {
				t.Errorf("user=%s role=%q, want %q", tu.username, role, tu.wantRole)
			}
		})
	}
}

func TestE2E_LDAP_ServerConnectivity(t *testing.T) {
	conn, err := ldap_auth.DialURL("ldap://"+glauthAddr, 3*time.Second, false)
	if err != nil {
		t.Skipf("GLAuth not available: %v\n  Start: cd deploy/docker/glauth && docker compose up -d", err)
		return
	}
	defer conn.Close()

	err = conn.Bind("", "")
	if err != nil {
		t.Logf("anonymous bind failed (expected): %v", err)
	}
	err = conn.Bind("cn=svc-flowgent,"+glauthBaseDN, "password123")
	if err != nil {
		t.Fatalf("service account bind failed: %v", err)
	}
	for _, tu := range testUsers {
		req := ldap_auth.SearchRequest{
			BaseDN: glauthBaseDN, Scope: ldap_auth.ScopeWholeSubtree,
			Filter: fmt.Sprintf("(cn=%s)", tu.username), Attributes: []string{"cn", "mail"},
			SizeLimit: 1,
		}
		result, err := conn.Search(&req)
		if err != nil {
			t.Errorf("search %s: %v", tu.username, err)
			continue
		}
		if len(result.Entries) == 0 {
			t.Errorf("user %s not found", tu.username)
		}
	}
}
