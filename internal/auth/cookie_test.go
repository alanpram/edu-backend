package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyCSRF(t *testing.T) {
	m := &Middleware{CSRFCookieName: DefaultCSRFCookie}

	newRequest := func(header, cookie string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		if header != "" {
			r.Header.Set(CSRFHeader, header)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: DefaultCSRFCookie, Value: cookie})
		}
		return r
	}

	if err := m.verifyCSRF(newRequest("same-token", "same-token")); err != nil {
		t.Fatalf("token cocok seharusnya lolos, got %v", err)
	}

	err := m.verifyCSRF(newRequest("", "cookie-token"))
	if err == nil || !strings.Contains(err.Error(), "token CSRF tidak ditemukan") {
		t.Fatalf("header kosong harus ditolak, got %v", err)
	}

	err = m.verifyCSRF(newRequest("wrong-token", "cookie-token"))
	if err == nil || !strings.Contains(err.Error(), "token CSRF tidak valid") {
		t.Fatalf("token berbeda harus ditolak, got %v", err)
	}
}

func TestTokenFromRequest(t *testing.T) {
	m := &Middleware{
		CookieName:     DefaultSessionCookie,
		CSRFCookieName: DefaultCSRFCookie,
	}

	bearer := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	bearer.Header.Set("Authorization", "Bearer jwt-value")
	token, fromCookie, err := m.tokenFromRequest(bearer)
	if err != nil || token != "jwt-value" || fromCookie {
		t.Fatalf("bearer tidak terbaca: token=%q fromCookie=%v err=%v", token, fromCookie, err)
	}

	cookie := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	cookie.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "cookie-jwt"})
	token, fromCookie, err = m.tokenFromRequest(cookie)
	if err != nil || token != "cookie-jwt" || !fromCookie {
		t.Fatalf("cookie tidak terbaca: token=%q fromCookie=%v err=%v", token, fromCookie, err)
	}

	empty := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if _, _, err := m.tokenFromRequest(empty); err == nil {
		t.Fatal("request tanpa token harus menghasilkan error")
	}
}

func TestSetSessionCookies(t *testing.T) {
	m := &Middleware{CSRFCookieName: DefaultCSRFCookie, CookieSameSite: "lax"}
	rec := httptest.NewRecorder()

	csrf := m.SetSessionCookies(rec, "jwt-value")
	if len(csrf) != 64 {
		t.Fatalf("csrf harus 64 hex, got %d", len(csrf))
	}
	if got := rec.Header().Get(CSRFHeader); got != csrf {
		t.Fatalf("header CSRF tidak sama: got=%q want=%q", got, csrf)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("harus ada 2 cookie, got %d", len(cookies))
	}
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	session := byName[DefaultSessionCookie]
	if session == nil || !session.HttpOnly || session.Value != "jwt-value" {
		t.Fatalf("cookie sesi tidak sesuai: %+v", session)
	}
	csrfCookie := byName[DefaultCSRFCookie]
	if csrfCookie == nil || csrfCookie.HttpOnly || csrfCookie.Value != csrf {
		t.Fatalf("cookie CSRF tidak sesuai: %+v", csrfCookie)
	}
	if session.SameSite != http.SameSiteLaxMode || csrfCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite harus Lax: session=%v csrf=%v", session.SameSite, csrfCookie.SameSite)
	}
}

func TestIsUnsafeMethod(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if isUnsafeMethod(method) {
			t.Fatalf("%s seharusnya bukan unsafe method", method)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !isUnsafeMethod(method) {
			t.Fatalf("%s seharusnya unsafe method", method)
		}
	}
}
