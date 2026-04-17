package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// — Interface ──────────────────────────────────────────────────────────────────

// RateLimiter is the contract for any rate-limiting backend.
// Swap InMemoryRateLimiter for RedisRateLimiter (or any other) without touching
// the middleware or application code — just inject a different implementation.
type RateLimiter interface {
	// Allow returns true if the request identified by key is within the limit.
	// Returns false if the limit has been exceeded (caller should respond 429).
	Allow(key string) bool
}

// — In-Memory implementation ───────────────────────────────────────────────────

type windowEntry struct {
	count   int
	resetAt time.Time
}

// InMemoryRateLimiter implements RateLimiter using a fixed window per key.
// State is stored in a sync.Map; a background goroutine periodically evicts
// expired entries to prevent unbounded memory growth.
//
// Limitation: does not persist across restarts and is not shared between
// multiple API instances. Use RedisRateLimiter for those scenarios.
type InMemoryRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*windowEntry
	max     int
	window  time.Duration
}

// NewInMemoryRateLimiter creates a limiter that allows at most max requests
// per window duration per key. Starts a background cleanup goroutine.
func NewInMemoryRateLimiter(max int, window time.Duration) *InMemoryRateLimiter {
	rl := &InMemoryRateLimiter{
		entries: make(map[string]*windowEntry),
		max:     max,
		window:  window,
	}
	go rl.cleanup()
	return rl
}

// Allow implements RateLimiter. Thread-safe.
func (rl *InMemoryRateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	e, ok := rl.entries[key]
	if !ok || now.After(e.resetAt) {
		rl.entries[key] = &windowEntry{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	if e.count >= rl.max {
		return false
	}
	e.count++
	return true
}

// cleanup removes expired entries every window to avoid memory leaks.
func (rl *InMemoryRateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for k, e := range rl.entries {
			if now.After(e.resetAt) {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}

// — Redis stub ─────────────────────────────────────────────────────────────────

// RedisRateLimiter is a placeholder for a future Redis-backed implementation.
// To activate: implement Allow(key string) bool using a sliding window with
// Redis ZADD / ZREMRANGEBYSCORE / ZCOUNT and replace the injected limiter in
// main.go with an instance of this struct.
//
// type RedisRateLimiter struct {
//     client *redis.Client
//     max    int
//     window time.Duration
// }
//
// func (r *RedisRateLimiter) Allow(key string) bool { ... }

// — Middleware ─────────────────────────────────────────────────────────────────

// RateLimit returns middleware that enforces the given RateLimiter on each request.
// The rate-limit key is the client's real IP address.
func RateLimit(limiter RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			if !limiter.Allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "too many requests — please slow down",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realIP tries to extract the original client IP honoring common proxy headers.
func realIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
