package ldap

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ── LDAP connection interface & types ─────────────────────────────

// LDAPConnection abstracts the underlying LDAP library for testability.
type LDAPConnection interface {
	Bind(dn, password string) error
	Search(req *SearchRequest) (*SearchResult, error)
	StartTLS(config *tls.Config) error
	Close() error
}

// Search scopes matching LDAP protocol constants.
const (
	ScopeBaseObject   = 0
	ScopeSingleLevel  = 1
	ScopeWholeSubtree = 2
)

// SearchRequest mirrors LDAP search parameters.
type SearchRequest struct {
	BaseDN     string
	Scope      int
	Filter     string
	Attributes []string
	SizeLimit  int
	TimeLimit  int
}

// SearchResult holds the entries returned by a search.
type SearchResult struct {
	Entries []*ldapEntry
}

type ldapEntry struct {
	DN         string
	Attributes map[string][]string
}

// ── Dial function registry ────────────────────────────────────────
//
// The default (stub) implementations are set here. When built with -tags ldap,
// ldap_client_real.go overrides these in its init() with real go-ldap adapters.

type dialFunc func(network, addr string, timeout time.Duration) (LDAPConnection, error)
type dialTLSFunc func(network, addr string, config *tls.Config, timeout time.Duration) (LDAPConnection, error)

var (
	dialLDAPFunc    dialFunc    = stubDial
	dialLDAPTLSFunc dialTLSFunc = stubDialTLS
)

// dialLDAP delegates to the registered dial function.
func dialLDAP(network, addr string, timeout time.Duration) (LDAPConnection, error) {
	return dialLDAPFunc(network, addr, timeout)
}

// dialLDAPTLS delegates to the registered TLS dial function.
func dialLDAPTLS(network, addr string, config *tls.Config, timeout time.Duration) (LDAPConnection, error) {
	return dialLDAPTLSFunc(network, addr, config, timeout)
}

// DialURL parses an LDAP URL (ldap://host:port or ldaps://host:port) and connects.
func DialURL(rawURL string, timeout time.Duration, insecureSkipVerify bool) (LDAPConnection, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ldap: invalid url %q: %w", rawURL, err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "ldaps" {
			host += ":636"
		} else {
			host += ":389"
		}
	}

	if u.Scheme == "ldaps" {
		return dialLDAPTLSFunc("tcp", host, &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
			MinVersion:         tls.VersionTLS12,
		}, timeout)
	}
	return dialLDAPFunc("tcp", host, timeout)
}

// ── Stub implementations (default, no -tags ldap) ─────────────────

type stubConn struct{}

func (s *stubConn) Bind(_, _ string) error {
	return fmt.Errorf("ldap: not available — rebuild with -tags ldap and add github.com/go-ldap/ldap/v3")
}
func (s *stubConn) Search(_ *SearchRequest) (*SearchResult, error) {
	return nil, fmt.Errorf("ldap: not available — rebuild with -tags ldap")
}
func (s *stubConn) StartTLS(_ *tls.Config) error {
	return fmt.Errorf("ldap: not available — rebuild with -tags ldap")
}
func (s *stubConn) Close() error { return nil }

func stubDial(network, addr string, timeout time.Duration) (LDAPConnection, error) {
	return nil, fmt.Errorf("ldap: not available — rebuild with -tags ldap (addr=%s)", addr)
}

func stubDialTLS(network, addr string, config *tls.Config, timeout time.Duration) (LDAPConnection, error) {
	return nil, fmt.Errorf("ldap: not available — rebuild with -tags ldap (addr=%s)", addr)
}
