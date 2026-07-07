// Package ldap provides enterprise LDAP/AD authentication.
//
// It performs service-account bind → user search → password bind → group resolution,
// supporting multiple AD domains and role mapping from AD groups/domains to Flowgent roles.
package ldap

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// ── Provider ──────────────────────────────────────────────────────

// Service implements auth.AuthProviderService for enterprise LDAP/AD authentication.
type Service struct {
	cfg          config.LDAPConfig
	tokenService *auth.TokenService
}

// NewService creates an LDAP auth service.
func NewService(cfg config.LDAPConfig, tokenService *auth.TokenService) *Service {
	return &Service{cfg: cfg, tokenService: tokenService}
}

func (p *Service) Name() string  { return "ldap" }
func (p *Service) Enabled() bool { return p.cfg.Enabled }

// CanHandle reports whether this service handles the given request.
func (p *Service) CanHandle(r *http.Request) bool {
	return r.URL.Path == "/auth/login/ldap" && r.Method == http.MethodPost
}

// ServeHTTP handles LDAP authentication requests.
func (p *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handleLogin(w, r)
}

// handleLogin processes a username/password login against LDAP/AD.
func (p *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	username, password, err := parseCredentials(r)
	if err != nil {
		auth.WriteJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"Invalid request: username and password required"}`)
		return
	}

	user, err := p.authenticate(r, username, password)
	if err != nil {
		slog.Warn("ldap: authentication failed", "username", username, "error", err)
		auth.WriteJSON(w, http.StatusUnauthorized,
			`{"success":false,"message":"LDAP authentication failed"}`)
		return
	}

	accessToken, err := p.tokenService.IssueAccessToken(user)
	if err != nil {
		slog.Error("ldap: JWT issuance failed", "error", err)
		auth.WriteJSON(w, http.StatusInternalServerError,
			`{"success":false,"message":"Token generation failed"}`)
		return
	}

	refreshToken, _ := p.tokenService.IssueRefreshToken(user)

	slog.Info("ldap: login successful", "user", user.Username, "email", user.Email, "role", user.Role)

	auth.WriteJSON(w, http.StatusOK, fmt.Sprintf(
		`{"success":true,"access_token":"%s","refresh_token":"%s","user":{"id":"%s","username":"%s","email":"%s","display_name":"%s","role":"%s"}}`,
		accessToken, refreshToken, user.UserID, user.Username, user.Email, user.DisplayName, user.Role,
	))
}

// ── Authentication logic ──────────────────────────────────────────

// authenticate validates credentials against LDAP directory.
// Multi-domain: tries each configured domain until the user is found and authenticated.
func (p *Service) authenticate(r *http.Request, username, password string) (*auth.UserInfo, error) {
	conn, err := p.dial()
	if err != nil {
		return nil, fmt.Errorf("ldap: dial: %w", err)
	}
	defer conn.Close()

	// Step 1: Bind with service account
	if err := conn.Bind(p.cfg.BindDN, p.cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap: service bind: %w", err)
	}

	// Step 2: Search for user across configured domains
	userDN, entry, matchedDomain, err := p.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	// Step 3: Re-bind with user credentials to verify password
	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("ldap: user bind: %w", err)
	}

	// Step 4: Build UserInfo from directory attributes
	user := p.buildUserInfo(entry, username, matchedDomain)

	// Step 5: Resolve group memberships
	user.Groups = p.resolveGroups(conn, userDN, username)

	// Step 6: Map AD groups/domain to Flowgent role
	user.Role = p.resolveRole(user.Groups, matchedDomain)

	return user, nil
}

// searchUser searches for a user across all configured domains.
func (p *Service) searchUser(conn LDAPConnection, username string) (string, *ldapEntry, string, error) {
	attr := p.usernameAttribute()
	attrs := []string{attr, p.emailAttribute(), p.displayNameAttribute(), "dn", "memberOf"}

	for _, domain := range p.domains() {
		filter := fmt.Sprintf(domain.UserSearchFilter, escapeFilter(username))
		base := domain.BaseDN

		result, err := conn.Search(&SearchRequest{
			BaseDN:     base,
			Scope:      ScopeWholeSubtree,
			Filter:     filter,
			Attributes: attrs,
			SizeLimit:  1,
			TimeLimit:  p.requestTimeoutSeconds(),
		})
		if err != nil {
			slog.Debug("ldap: search failed in domain", "base", base, "error", err)
			continue
		}
		if len(result.Entries) > 0 {
			slog.Debug("ldap: user found in domain", "dn", result.Entries[0].DN, "domain", base)
			return result.Entries[0].DN, result.Entries[0], base, nil
		}
	}

	if len(p.domains()) == 0 {
		return "", nil, "", fmt.Errorf("ldap: no domains configured")
	}
	return "", nil, "", fmt.Errorf("ldap: user not found in any domain: %s", username)
}

// domains returns the configured AD domains, falling back to a single-domain
// representation derived from BaseDN and UserSearchFilter.
func (p *Service) domains() []config.LDAPDomainConfig {
	if len(p.cfg.Domains) > 0 {
		return p.cfg.Domains
	}
	if p.cfg.BaseDN != "" {
		return []config.LDAPDomainConfig{{
			BaseDN:           p.cfg.BaseDN,
			UserSearchFilter: p.userSearchFilter(),
		}}
	}
	return nil
}

func (p *Service) usernameAttribute() string {
	if p.cfg.UsernameAttribute != "" {
		return p.cfg.UsernameAttribute
	}
	return "cn"
}

func (p *Service) emailAttribute() string {
	if p.cfg.EmailAttribute != "" {
		return p.cfg.EmailAttribute
	}
	return "mail"
}

func (p *Service) displayNameAttribute() string {
	if p.cfg.DisplayNameAttribute != "" {
		return p.cfg.DisplayNameAttribute
	}
	return "cn"
}

func (p *Service) groupNameAttribute() string {
	if p.cfg.GroupNameAttribute != "" {
		return p.cfg.GroupNameAttribute
	}
	return "cn"
}

func (p *Service) userSearchFilter() string {
	if p.cfg.UserSearchFilter != "" {
		return p.cfg.UserSearchFilter
	}
	return "(cn=%s)"
}

func (p *Service) groupSearchFilter() string {
	if p.cfg.GroupSearchFilter != "" {
		return p.cfg.GroupSearchFilter
	}
	return "(member=%s)"
}

// buildUserInfo extracts user attributes from an LDAP entry.
func (p *Service) buildUserInfo(entry *ldapEntry, username, domain string) *auth.UserInfo {
	user := &auth.UserInfo{
		UserID:      getAttr(entry, p.usernameAttribute(), username),
		Username:    getAttr(entry, p.usernameAttribute(), username),
		Email:       getAttr(entry, p.emailAttribute(), ""),
		DisplayName: getAttr(entry, p.displayNameAttribute(), username),
		Extra:       make(map[string]any),
	}
	if user.UserID == "" {
		user.UserID = username
	}
	if user.Username == "" {
		user.Username = username
	}
	return user
}

// resolveRole maps AD group memberships and/or the matched domain to a Flowgent role.
func (p *Service) resolveRole(groups []string, matchedDomain string) string {
	for _, mapping := range p.cfg.RoleMapping {
		// Check group-based mappings
		for _, g := range groups {
			if g == mapping.Match {
				slog.Debug("ldap: role resolved by group", "group", g, "role", mapping.Role)
				return mapping.Role
			}
		}
		// Check domain-based mappings
		if matchedDomain != "" && mapping.Match == matchedDomain {
			slog.Debug("ldap: role resolved by domain", "domain", matchedDomain, "role", mapping.Role)
			return mapping.Role
		}
	}
	return ""
}

// resolveGroups searches for groups the user belongs to.
func (p *Service) resolveGroups(conn LDAPConnection, userDN, username string) []string {
	if p.cfg.GroupSearchBase == "" {
		return nil
	}

	groupFilter := fmt.Sprintf(p.groupSearchFilter(), escapeFilter(userDN))
	result, err := conn.Search(&SearchRequest{
		BaseDN:     p.cfg.GroupSearchBase,
		Scope:      ScopeWholeSubtree,
		Filter:     groupFilter,
		Attributes: []string{p.groupNameAttribute()},
		TimeLimit:  p.requestTimeoutSeconds(),
	})
	if err != nil {
		slog.Warn("ldap: group resolution failed", "user", username, "error", err)
		return nil
	}

	var groups []string
	for _, entry := range result.Entries {
		if name := getAttr(entry, p.groupNameAttribute(), ""); name != "" {
			groups = append(groups, name)
		}
	}
	return groups
}

// ── Connection layer ──────────────────────────────────────────────

func (p *Service) dial() (LDAPConnection, error) {
	return DialURL(p.cfg.URL, 5*time.Second, p.cfg.InsecureSkipVerify)
}

func (p *Service) requestTimeoutSeconds() int { return 10 }

// ── Helpers ───────────────────────────────────────────────────────

func getAttr(entry *ldapEntry, name, fallback string) string {
	if entry == nil {
		return fallback
	}
	vals, ok := entry.Attributes[name]
	if !ok || len(vals) == 0 {
		return fallback
	}
	return vals[0]
}

// escapeFilter escapes special characters per RFC 4515.
func escapeFilter(s string) string {
	result := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '*':
			result = append(result, '\\', '2', 'a')
		case '(':
			result = append(result, '\\', '2', '8')
		case ')':
			result = append(result, '\\', '2', '9')
		case '\\':
			result = append(result, '\\', '5', 'c')
		case 0:
			result = append(result, '\\', '0', '0')
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}

func parseCredentials(r *http.Request) (string, string, error) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", "", err
	}
	if body.Username == "" || body.Password == "" {
		return "", "", fmt.Errorf("username and password are required")
	}
	return body.Username, body.Password, nil
}
