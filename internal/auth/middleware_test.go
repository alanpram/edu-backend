package auth

import (
	"net/http/httptest"
	"testing"
)

func TestRequestIPIgnoresForwardedHeaderWithoutTrustedMiddleware(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("X-Forwarded-For", "198.51.100.99")

	if got := RequestIP(request); got != "203.0.113.10" {
		t.Fatalf("RequestIP() = %q, want %q", got, "203.0.113.10")
	}
}

func TestRequestIPUsesResolvedContextValue(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request = request.WithContext(WithClientIP(request.Context(), "198.51.100.7"))

	if got := RequestIP(request); got != "198.51.100.7" {
		t.Fatalf("RequestIP() = %q, want %q", got, "198.51.100.7")
	}
}

func TestRemoteIPStripsPort(t *testing.T) {
	cases := map[string]string{
		"203.0.113.10:4321": "203.0.113.10",
		"[2001:db8::1]:443": "2001:db8::1",
		"203.0.113.10":      "203.0.113.10",
		"":                  "",
	}
	for input, want := range cases {
		if got := RemoteIP(input); got != want {
			t.Fatalf("RemoteIP(%q) = %q, want %q", input, got, want)
		}
	}
}
