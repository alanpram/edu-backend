package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/edumaster/backend-go/internal/httpx"
)

const (
	DefaultSessionCookie = "edumaster_session"
	DefaultCSRFCookie    = "edumaster_csrf"
	CSRFHeader           = "X-CSRF-Token"
)

func (m *Middleware) sessionCookieName() string {
	if strings.TrimSpace(m.CookieName) != "" {
		return strings.TrimSpace(m.CookieName)
	}
	return DefaultSessionCookie
}

func (m *Middleware) csrfCookieName() string {
	if strings.TrimSpace(m.CSRFCookieName) != "" {
		return strings.TrimSpace(m.CSRFCookieName)
	}
	return DefaultCSRFCookie
}

func (m *Middleware) cookieTTL() time.Duration {
	if m.TokenTTL > 0 {
		return m.TokenTTL
	}
	return 12 * time.Hour
}

func (m *Middleware) sameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(m.CookieSameSite)) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}

func (m *Middleware) writeCookie(w http.ResponseWriter, name, value string, httpOnly bool, maxAge int, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   strings.TrimSpace(m.CookieDomain),
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: httpOnly,
		Secure:   m.CookieSecure,
		SameSite: m.sameSite(),
	})
}

// SetSessionCookies memasang cookie sesi HttpOnly dan cookie CSRF yang dapat
// dibaca JavaScript (double-submit). Nilai CSRF juga dikirim lewat header agar
// frontend tetap aman ketika API dan web berada di host yang berbeda.
func (m *Middleware) SetSessionCookies(w http.ResponseWriter, token string) string {
	ttl := m.cookieTTL()
	csrf := RandomHex(32)
	m.writeCookie(w, m.sessionCookieName(), token, true, int(ttl.Seconds()), time.Now().Add(ttl))
	m.writeCookie(w, m.csrfCookieName(), csrf, false, int(ttl.Seconds()), time.Now().Add(ttl))
	w.Header().Set(CSRFHeader, csrf)
	return csrf
}

// ClearSessionCookies menghapus cookie sesi dan CSRF.
func (m *Middleware) ClearSessionCookies(w http.ResponseWriter) {
	expired := time.Unix(0, 0)
	m.writeCookie(w, m.sessionCookieName(), "", true, -1, expired)
	m.writeCookie(w, m.csrfCookieName(), "", false, -1, expired)
}

// ExposeCSRF mengirim nilai CSRF yang sedang aktif ke frontend. Jika cookie
// CSRF belum ada (mis. sesi lama), cookie baru dibuat agar double-submit tetap
// berfungsi tanpa memaksa pengguna login ulang.
func (m *Middleware) ExposeCSRF(w http.ResponseWriter, r *http.Request) string {
	csrf := ""
	if cookie, err := r.Cookie(m.csrfCookieName()); err == nil {
		csrf = strings.TrimSpace(cookie.Value)
	}
	if csrf == "" {
		csrf = RandomHex(32)
		ttl := m.cookieTTL()
		m.writeCookie(w, m.csrfCookieName(), csrf, false, int(ttl.Seconds()), time.Now().Add(ttl))
	}
	w.Header().Set(CSRFHeader, csrf)
	return csrf
}

func (m *Middleware) tokenFromRequest(r *http.Request) (string, bool, error) {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		token, err := ExtractBearer(r.Header.Get("Authorization"))
		return token, false, err
	}
	if cookie, err := r.Cookie(m.sessionCookieName()); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token, true, nil
		}
	}
	return "", false, httpx.Unauthorized(
		"Sesi login tidak ditemukan. Silakan login kembali.",
		"MISSING_TOKEN",
	)
}

func (m *Middleware) verifyCSRF(r *http.Request) error {
	header := strings.TrimSpace(r.Header.Get(CSRFHeader))
	cookie, err := r.Cookie(m.csrfCookieName())
	if header == "" || err != nil || strings.TrimSpace(cookie.Value) == "" {
		return httpx.Forbidden(
			"Permintaan ditolak karena token CSRF tidak ditemukan. Muat ulang halaman lalu coba lagi.",
			"CSRF_TOKEN_MISSING",
		)
	}
	if subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
		return httpx.Forbidden(
			"Permintaan ditolak karena token CSRF tidak valid. Muat ulang halaman lalu coba lagi.",
			"CSRF_TOKEN_INVALID",
		)
	}
	return nil
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}
