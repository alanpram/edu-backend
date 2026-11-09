package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edumaster/backend-go/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		AppName:        "EDUMASTER Test",
		APIPrefix:      "/api",
		CORSOrigins:    []string{"http://localhost:3000"},
		JWTSecret:      "test-secret-with-at-least-32-characters",
		CookieName:     "edumaster_session",
		CSRFCookieName: "edumaster_csrf",
		UploadDir:      t.TempDir(),
	}
	return New(cfg, nil, nil)
}

func TestRouterHealthAndSecurityHeaders(t *testing.T) {
	server := newTestServer(t)
	router := server.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if !body.Success || body.Data.Status != "ok" {
		t.Fatalf("unexpected health body: %s", rec.Body.String())
	}
}

func TestRouterCORSAndUploadsHardening(t *testing.T) {
	server := newTestServer(t)
	router := server.Router()

	preflight := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflightRec := httptest.NewRecorder()
	router.ServeHTTP(preflightRec, preflight)

	if preflightRec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preflightRec.Code)
	}
	if got := preflightRec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}

	unknown := httptest.NewRequest(http.MethodGet, "/api/tidak-ada", nil)
	unknownRec := httptest.NewRecorder()
	router.ServeHTTP(unknownRec, unknown)
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("unknown endpoint status = %d, want 404", unknownRec.Code)
	}

	directory := httptest.NewRequest(http.MethodGet, "/uploads/", nil)
	directoryRec := httptest.NewRecorder()
	router.ServeHTTP(directoryRec, directory)
	if directoryRec.Code != http.StatusNotFound {
		t.Fatalf("directory listing status = %d, want 404", directoryRec.Code)
	}
}

func TestRouterRateLimitHeaders(t *testing.T) {
	server := newTestServer(t)
	router := server.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("RateLimit-Limit"); got == "" {
		t.Fatal("RateLimit-Limit header wajib ada")
	}
}
