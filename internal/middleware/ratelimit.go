package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/edumaster/backend-go/internal/auth"
	"github.com/edumaster/backend-go/internal/httpx"
)

// Store menyimpan penghitung rate limit. Implementasi memory dipakai sebagai
// fallback, sedangkan Redis dipakai ketika API berjalan di lebih dari satu
// instance sehingga batas tetap terpusat.
type Store interface {
	Increment(ctx context.Context, key string, window time.Duration) (count int, resetIn time.Duration, err error)
}

type memoryBucket struct {
	count   int
	resetAt time.Time
}

type memoryStore struct {
	mu      sync.Mutex
	buckets map[string]memoryBucket
}

func newMemoryStore() *memoryStore {
	return &memoryStore{buckets: make(map[string]memoryBucket)}
}

func (s *memoryStore) Increment(_ context.Context, key string, window time.Duration) (int, time.Duration, error) {
	now := time.Now()

	s.mu.Lock()
	current, ok := s.buckets[key]
	if !ok || now.After(current.resetAt) {
		current = memoryBucket{count: 0, resetAt: now.Add(window)}
	}
	current.count++
	s.buckets[key] = current
	if len(s.buckets) > 5000 {
		for k, b := range s.buckets {
			if now.After(b.resetAt) {
				delete(s.buckets, k)
			}
		}
	}
	resetIn := current.resetAt.Sub(now)
	s.mu.Unlock()

	return current.count, resetIn, nil
}

// RedisStore memakai fixed-window counter atomik. Kunci selalu diberi prefix
// aplikasi agar aman bila Redis dipakai bersama aplikasi lain.
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	if client == nil {
		return nil
	}
	return &RedisStore{client: client}
}

var incrementScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {count, ttl}
`)

func (s *RedisStore) Increment(ctx context.Context, key string, window time.Duration) (int, time.Duration, error) {
	result, err := incrementScript.Run(ctx, s.client, []string{key}, window.Milliseconds()).Result()
	if err != nil {
		return 0, 0, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
		return 0, 0, fmt.Errorf("respons rate limit Redis tidak valid")
	}
	count, okCount := redisInt(values[0])
	ttlMillis, okTTL := redisInt(values[1])
	if !okCount || !okTTL {
		return 0, 0, fmt.Errorf("nilai rate limit Redis tidak valid")
	}
	return int(count), time.Duration(ttlMillis) * time.Millisecond, nil
}

func redisInt(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

type Limiter struct {
	name     string
	limit    int
	window   time.Duration
	message  string
	store    Store
	fallback Store

	warnOnce sync.Once
}

// NewLimiter membuat limiter in-memory untuk deployment satu instance.
func NewLimiter(limit int, window time.Duration, message string) *Limiter {
	return NewLimiterWithStore("", limit, window, message, nil)
}

// NewLimiterWithStore membuat limiter dengan store tertentu (Redis) dan
// fallback in-memory bila store pusat gagal. name menjadi bagian kunci Redis.
func NewLimiterWithStore(name string, limit int, window time.Duration, message string, store Store) *Limiter {
	if store == nil {
		store = newMemoryStore()
	}
	return &Limiter{
		name:     name,
		limit:    limit,
		window:   window,
		message:  message,
		store:    store,
		fallback: newMemoryStore(),
	}
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := auth.RequestIP(r)
		if l.name != "" {
			key = l.name + ":" + key
		}
		key = "edumaster:ratelimit:" + key

		ctx, cancel := context.WithTimeout(r.Context(), 750*time.Millisecond)
		count, resetIn, err := l.store.Increment(ctx, key, l.window)
		cancel()
		if err != nil {
			l.warnOnce.Do(func() {
				log.Printf("[RATE LIMIT] store pusat gagal, memakai fallback in-memory: %v", err)
			})
			count, resetIn, _ = l.fallback.Increment(r.Context(), key, l.window)
		}

		if resetIn < 0 {
			resetIn = 0
		}

		w.Header().Set("RateLimit-Limit", itoa(l.limit))
		w.Header().Set("RateLimit-Remaining", itoa(max(0, l.limit-count)))
		w.Header().Set("RateLimit-Reset", itoa(int(resetIn.Seconds())))

		if count > l.limit {
			w.Header().Set("Retry-After", itoa(max(1, int(resetIn.Seconds()))))
			httpx.WriteError(w, httpx.TooManyRequests(l.message))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	pos := len(buf)
	for value > 0 {
		pos--
		buf[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
