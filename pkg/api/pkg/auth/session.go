package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const SessionCookieName = "flowgent_session"

// LoginProvider describes a browser-visible authentication backend.
type LoginProvider struct {
	Type     string `json:"type"`
	Label    string `json:"label"`
	LoginURL string `json:"login_url,omitempty"`
}

func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   ExternalScheme(r) == "https",
		SameSite: http.SameSiteLaxMode,
	}
	if ttl > 0 {
		cookie.MaxAge = int(ttl.Seconds())
		cookie.Expires = time.Now().Add(ttl)
	}
	http.SetCookie(w, cookie)
}

func ClearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   ExternalScheme(r) == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func RedirectWithSession(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	SetSessionCookie(w, r, token, ttl)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func Logout(w http.ResponseWriter, r *http.Request) {
	ClearCookie(w, r, SessionCookieName)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func SessionToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func WriteLoginJSON(w http.ResponseWriter, user *UserInfo) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"user": map[string]any{
			"id":           user.UserID,
			"username":     user.Username,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"role":         user.Role,
		},
	})
}

func ExternalScheme(r *http.Request) string {
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func ExternalHost(r *http.Request) string {
	if host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); host != "" {
		return strings.TrimSpace(strings.Split(host, ",")[0])
	}
	return r.Host
}
