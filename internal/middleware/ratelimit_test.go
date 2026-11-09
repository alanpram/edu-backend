package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterMemoryEnforcesLimitAndHeaders(t *testing.T) {
	limiter := NewLimiter(2, time.Minute, "terlalu banyak")
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := request()
	second := request()
	third := request()

	if first.Code != http.StatusNoContent || second.Code != http.StatusNoContent {
		t.Fatalf("dua request pertama harus lolos, got %d dan %d", first.Code, second.Code)
	}
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("request ketiga harus 429, got %d", third.Code)
	}
	if third.Header().Get("Retry-After") == "" {
		t.Fatal("response 429 harus menyertakan Retry-After")
	}
	if got := third.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Fatalf("RateLimit-Remaining = %q, want 0", got)
	}
}

func TestLimiterFallsBackWhenStoreFails(t *testing.T) {
	fallbackLimiter := NewLimiterWithStore("api", 1, time.Minute, "terlalu banyak", failingStore{})
	handler := fallbackLimiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.7:4321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := request(); code != http.StatusNoContent {
		t.Fatalf("request pertama harus lolos lewat fallback, got %d", code)
	}
	if code := request(); code != http.StatusTooManyRequests {
		t.Fatalf("request kedua harus 429 lewat fallback, got %d", code)
	}
}

func TestMemoryStoreResetsAfterWindow(t *testing.T) {
	store := newMemoryStore()
	key := "edumaster:ratelimit:test"

	count, _, err := store.Increment(context.Background(), key, 20*time.Millisecond)
	if err != nil || count != 1 {
		t.Fatalf("increment pertama = %d, err=%v", count, err)
	}
	time.Sleep(30 * time.Millisecond)
	count, _, err = store.Increment(context.Background(), key, 20*time.Millisecond)
	if err != nil || count != 1 {
		t.Fatalf("increment setelah window = %d, err=%v", count, err)
	}
}

type failingStore struct{}

func (failingStore) Increment(context.Context, string, time.Duration) (int, time.Duration, error) {
	return 0, 0, errors.New("redis unavailable")
}
