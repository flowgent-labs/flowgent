package github

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
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

func TestCallbackSetsHttpOnlySessionCookieAndRedirects(t *testing.T) {
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"github-token","token_type":"bearer"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer github-token" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":29530154,"login":"wl4g","name":"Mr James","email":"james@example.com","avatar_url":"https://avatars.example/u.png"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubAPI.Close()

	tokenService, err := auth.NewTokenService(config.AuthConfig{
		JWTAlgorithm:  "ES256",
		JWTPrivateKey: testPrivKey,
		JWTPublicKey:  testPubKey,
		JWTValidityAK: 3600,
		JWTValidityRK: 86400,
	})
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(config.GitHubAuthConfig{
		Enabled:      true,
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     githubAPI.URL + "/token",
		UserInfoURL:  githubAPI.URL + "/user",
	}, tokenService)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback/github?code=ok&state=expected", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "expected"})
	w := httptest.NewRecorder()

	service.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if w.Header().Get("Location") != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", w.Header().Get("Location"))
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "<script") {
		t.Fatalf("callback response must not contain inline script: %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if !hasSecureHTTPOnlyCookie(cookies, auth.SessionCookieName) {
		t.Fatalf("missing secure HttpOnly session cookie: %#v", cookies)
	}
	if !hasDeletedCookie(cookies, stateCookieName) {
		t.Fatalf("state cookie was not cleared: %#v", cookies)
	}
}

func hasSecureHTTPOnlyCookie(cookies []*http.Cookie, name string) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value != "" && cookie.HttpOnly && cookie.Secure && cookie.SameSite == http.SameSiteLaxMode {
			return true
		}
	}
	return false
}

func hasDeletedCookie(cookies []*http.Cookie, name string) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}
