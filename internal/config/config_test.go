package config

import (
	"strings"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NODE_ENV", "development")
	t.Setenv("DATABASE_URL", "postgresql://localhost:5432/edumaster")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("CORS_ORIGIN", "https://app.example")
	t.Setenv("COOKIE_SAMESITE", "lax")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("TRUST_PROXY", "false")
}

func TestLoadAcceptsExplicitCORSOrigin(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "https://app.example" {
		t.Fatalf("CORSOrigins = %#v", cfg.CORSOrigins)
	}
	if cfg.TrustProxy {
		t.Fatal("TrustProxy should default to false")
	}
}

func TestLoadRejectsWildcardCORS(t *testing.T) {
	setValidEnv(t)
	t.Setenv("CORS_ORIGIN", "*")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "tidak boleh") {
		t.Fatalf("Load() error = %v, want wildcard rejection", err)
	}
}

func TestLoadRejectsSameSiteNoneWithoutSecureCookie(t *testing.T) {
	setValidEnv(t)
	t.Setenv("COOKIE_SAMESITE", "none")
	t.Setenv("COOKIE_SECURE", "false")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE") {
		t.Fatalf("Load() error = %v, want COOKIE_SECURE validation", err)
	}
}

func TestNormalizeOriginsRemovesTrailingSlash(t *testing.T) {
	got := normalizeOrigins([]string{"https://app.example/", "http://localhost:3000"})
	want := []string{"https://app.example", "http://localhost:3000"}
	if len(got) != len(want) {
		t.Fatalf("normalizeOrigins() = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("normalizeOrigins()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
