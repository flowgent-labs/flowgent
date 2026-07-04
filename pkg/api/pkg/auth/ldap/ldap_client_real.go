//go:build ldap
// +build ldap

package ldap

import (
	"crypto/tls"
	"fmt"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// init overrides the stub dial functions with real go-ldap adapters.
func init() {
	dialLDAPFunc = realDial
	dialLDAPTLSFunc = realDialTLS
}

// ── Real go-ldap adapter ──────────────────────────────────────────

type realConn struct {
	conn *ldapv3.Conn
}

func (c *realConn) Bind(dn, password string) error {
	return c.conn.Bind(dn, password)
}

func (c *realConn) Search(req *SearchRequest) (*SearchResult, error) {
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

func (c *realConn) StartTLS(config *tls.Config) error {
	return c.conn.StartTLS(config)
}

func (c *realConn) Close() error {
	return c.conn.Close()
}

func realDial(network, addr string, timeout time.Duration) (LDAPConnection, error) {
	conn, err := ldapv3.Dial(network, addr)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial %s: %w", addr, err)
	}
	conn.SetTimeout(timeout)
	return &realConn{conn: conn}, nil
}

func realDialTLS(network, addr string, config *tls.Config, timeout time.Duration) (LDAPConnection, error) {
	conn, err := ldapv3.DialTLS(network, addr, config)
	if err != nil {
		return nil, fmt.Errorf("ldap: dial TLS %s: %w", addr, err)
	}
	conn.SetTimeout(timeout)
	return &realConn{conn: conn}, nil
}
