package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edumaster/backend-go/internal/auth"
)

func TestClientIPIgnoresForwardedHeaderWhenProxyIsNotTrusted(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.99")

	got := ""
	handler := ClientIP(false)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = auth.RequestIP(r)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if got != "203.0.113.10" {
		t.Fatalf("resolved IP = %q, want %q", got, "203.0.113.10")
	}
}

func TestClientIPUsesRightmostValidForwardedAddress(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.99, 203.0.113.20")

	got := ""
	handler := ClientIP(true)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = auth.RequestIP(r)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if got != "203.0.113.20" {
		t.Fatalf("resolved IP = %q, want %q", got, "203.0.113.20")
	}
}

func TestCORSNeverReflectsWildcardWithCredentials(t *testing.T) {
	handler := CORS([]string{"https://app.example"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest("GET", "/", nil)
	allowedRequest.Header.Set("Origin", "https://app.example")
	handler.ServeHTTP(allowed, allowedRequest)
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q", got)
	}

	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest("GET", "/", nil)
	deniedRequest.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(denied, deniedRequest)
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow origin = %q", got)
	}
}

func TestCORSWildcardIsNotAllowed(t *testing.T) {
	handler := CORS([]string{"*"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/", nil)
	request.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("wildcard should not be reflected, got %q", got)
	}
}
