package ldap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

func testLDAPCfg() config.LDAPConfig {
	return config.LDAPConfig{
		Enabled:            true,
		URL:                "ldap://localhost:389",
		BaseDN:             "dc=example,dc=com",
		BindDN:             "cn=svc,dc=example,dc=com",
		BindPassword:       "secret",
		UserSearchFilter:   "(cn=%s)",
		UsernameAttribute:  "cn",
		EmailAttribute:     "mail",
		DisplayNameAttribute: "displayName",
		GroupSearchBase:    "ou=groups,dc=example,dc=com",
		GroupSearchFilter:  "(uniqueMember=%s)",
		GroupNameAttribute: "cn",
	}
}

// ── Service basics ──────────────────────────────────────────────

func TestService_Name(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)
	if svc.Name() != "ldap" {
		t.Errorf("Name = %q, want ldap", svc.Name())
	}
}

func TestService_Enabled(t *testing.T) {
	svc := NewService(config.LDAPConfig{Enabled: true}, nil)
	if !svc.Enabled() {
		t.Error("should be enabled")
	}
	svc2 := NewService(config.LDAPConfig{Enabled: false}, nil)
	if svc2.Enabled() {
		t.Error("should not be enabled")
	}
}

func TestService_CanHandle(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)

	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/auth/login/ldap", true},
		{http.MethodGet, "/auth/login/ldap", false},
		{http.MethodPost, "/auth/login/oidc", false},
		{http.MethodPost, "/other", false},
	}

	for _, tt := range tests {
		t.Run(tt.method+"/"+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if svc.CanHandle(req) != tt.want {
				t.Errorf("CanHandle = %v, want %v", svc.CanHandle(req), tt.want)
			}
		})
	}
}

// ── parseCredentials ────────────────────────────────────────────

func TestParseCredentials_Valid(t *testing.T) {
	body := `{"username":"jdoe","password":"secret123"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	user, pass, err := parseCredentials(req)
	if err != nil {
		t.Fatal(err)
	}
	if user != "jdoe" || pass != "secret123" {
		t.Errorf("got %q/%q, want jdoe/secret123", user, pass)
	}
}

func TestParseCredentials_MissingUsername(t *testing.T) {
	body := `{"password":"secret123"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, _, err := parseCredentials(req)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCredentials_MissingPassword(t *testing.T) {
	body := `{"username":"jdoe"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	_, _, err := parseCredentials(req)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCredentials_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("not json"))
	_, _, err := parseCredentials(req)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── getAttr ─────────────────────────────────────────────────────

func TestGetAttr(t *testing.T) {
	entry := &ldapEntry{Attributes: map[string][]string{
		"cn":   {"jdoe"},
		"mail": {"jdoe@example.com"},
	}}

	if v := getAttr(entry, "cn", "fallback"); v != "jdoe" {
		t.Errorf("getAttr cn = %q, want jdoe", v)
	}
	if v := getAttr(entry, "unknown", "fallback"); v != "fallback" {
		t.Errorf("getAttr unknown = %q, want fallback", v)
	}
}

func TestGetAttr_NilEntry(t *testing.T) {
	if v := getAttr(nil, "cn", "fallback"); v != "fallback" {
		t.Errorf("getAttr nil = %q, want fallback", v)
	}
}

// ── escapeFilter ────────────────────────────────────────────────

func TestEscapeFilter(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"user*name", `user\2aname`},
		{"test(user)", `test\28user\29`},
		{"a\\b", `a\5cb`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeFilter(tt.input)
			if got != tt.want {
				t.Errorf("escapeFilter(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ── Service helpers ─────────────────────────────────────────────

func TestService_UsernameAttribute_Default(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)
	if svc.usernameAttribute() != "cn" {
		t.Errorf("default usernameAttribute = %q, want cn", svc.usernameAttribute())
	}
}

func TestService_UsernameAttribute_Custom(t *testing.T) {
	cfg := testLDAPCfg()
	cfg.UsernameAttribute = "uid"
	svc := NewService(cfg, nil)
	if svc.usernameAttribute() != "uid" {
		t.Errorf("usernameAttribute = %q, want uid", svc.usernameAttribute())
	}
}

func TestService_UserSearchFilter_Default(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)
	if svc.userSearchFilter() != "(cn=%s)" {
		t.Errorf("default userSearchFilter = %q", svc.userSearchFilter())
	}
}

func TestService_GroupSearchFilter_Default(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)
	if svc.groupSearchFilter() != "(member=%s)" {
		t.Errorf("default groupSearchFilter = %q", svc.groupSearchFilter())
	}
}

func TestService_Domains_Single(t *testing.T) {
	cfg := testLDAPCfg()
	svc := NewService(cfg, nil)
	domains := svc.domains()
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(domains))
	}
	if domains[0].BaseDN != "dc=example,dc=com" {
		t.Errorf("BaseDN = %q", domains[0].BaseDN)
	}
}

func TestService_Domains_Multi(t *testing.T) {
	cfg := testLDAPCfg()
	cfg.Domains = []config.LDAPDomainConfig{
		{BaseDN: "dc=corp,dc=com", UserSearchFilter: "(uid=%s)"},
		{BaseDN: "dc=eng,dc=com", UserSearchFilter: "(uid=%s)"},
	}
	svc := NewService(cfg, nil)
	domains := svc.domains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
}

// ── Service.RoleMapping ─────────────────────────────────────────

func TestService_ResolveRole(t *testing.T) {
	cfg := testLDAPCfg()
	cfg.RoleMapping = []config.LDAPRoleMapping{
		{Match: "Admins", Role: "admin"},
		{Match: "Ops", Role: "operator"},
	}
	svc := NewService(cfg, nil)

	tests := []struct {
		groups []string
		want   string
	}{
		{[]string{"Admins", "Users"}, "admin"},
		{[]string{"Ops"}, "operator"},
		{[]string{"Users"}, ""},
	}

	for _, tt := range tests {
		role := svc.resolveRole(tt.groups, "")
		if role != tt.want {
			t.Errorf("resolveRole(%v) = %q, want %q", tt.groups, role, tt.want)
		}
	}
}

func TestService_ResolveRole_ByDomain(t *testing.T) {
	cfg := testLDAPCfg()
	cfg.RoleMapping = []config.LDAPRoleMapping{
		{Match: "dc=corp,dc=com", Role: "corp-user"},
	}
	svc := NewService(cfg, nil)

	role := svc.resolveRole(nil, "dc=corp,dc=com")
	if role != "corp-user" {
		t.Errorf("role = %q, want corp-user", role)
	}
}

// ── Service.ServeHTTP ───────────────────────────────────────────

func TestService_ServeHTTP_InvalidBody(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)

	req := httptest.NewRequest("POST", "/auth/login/ldap", strings.NewReader("bad"))
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestService_ServeHTTP_EmptyCredentials(t *testing.T) {
	svc := NewService(config.LDAPConfig{}, nil)

	body, _ := json.Marshal(map[string]string{"username": "", "password": ""})
	req := httptest.NewRequest("POST", "/auth/login/ldap", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
