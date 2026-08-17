package ldap

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
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

// ── Dial & connection adapter ─────────────────────────────────────

// DialURL parses an LDAP URL (ldap://host:port or ldaps://host:port) and connects.
//
// Enterprise AD examples:
//
//	ldaps://aa-lds-prod.us.mycompany:636    (standard LDAPS)
//	ldaps://aa-lds-prod.us.mycompany:3269   (Global Catalog SSL)
//
// When connecting to AD Global Catalog (port 3269), set referral: follow
// to handle cross-domain referrals automatically.
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
		return dialTLS("tcp", host, &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
			MinVersion:         tls.VersionTLS12,
		}, timeout)
	}
	return dial("tcp", host, timeout)
}

type goLdapConn struct{ conn *ldapv3.Conn }

func (c *goLdapConn) Bind(dn, password string) error { return c.conn.Bind(dn, password) }

func (c *goLdapConn) Search(req *SearchRequest) (*SearchResult, error) {
	scope := req.Scope
	if scope == 0 {
		scope = ldapv3.ScopeWholeSubtree
	}
	searchReq := ldapv3.NewSearchRequest(
		req.BaseDN, scope, ldapv3.NeverDerefAliases,
		req.SizeLimit, req.TimeLimit, false,
		req.Filter, req.Attributes, nil,
	)
	result, err := c.conn.Search(searchReq)
	if err != nil {
		return nil, err
	}
	entries := make([]*ldapEntry, 0, len(result.Entries))
	for _, e := range result.Entries {
		attrs := make(map[string][]string, len(e.Attributes))
		for _, a := range e.Attributes {
			attrs[a.Name] = a.Values
		}
		entries = append(entries, &ldapEntry{DN: e.DN, Attributes: attrs})
	}
	return &SearchResult{Entries: entries}, nil
}

func (c *goLdapConn) StartTLS(config *tls.Config) error { return c.conn.StartTLS(config) }
func (c *goLdapConn) Close() error                      { return c.conn.Close() }

func dial(network, addr string, timeout time.Duration) (LDAPConnection, error) {
	conn, err := ldapv3.Dial(network, addr)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial %s: %w", addr, err)
	}
	conn.SetTimeout(timeout)
	return &goLdapConn{conn: conn}, nil
}

func dialTLS(network, addr string, config *tls.Config, timeout time.Duration) (LDAPConnection, error) {
	conn, err := ldapv3.DialTLS(network, addr, config)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial TLS %s: %w", addr, err)
	}
	conn.SetTimeout(timeout)
	return &goLdapConn{conn: conn}, nil
}
