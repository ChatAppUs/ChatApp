package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- Per-IP token-bucket rate limiting ----

type tokenBucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	refill  float64 // tokens per second
	burst   float64
}

// newRateLimiter allows perMinute requests sustained with a burst allowance.
func newRateLimiter(perMinute, burst int) *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*tokenBucket{},
		refill:  float64(perMinute) / 60.0,
		burst:   float64(burst),
	}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.refill
	b.last = now
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	// Bound memory: evict all idle buckets when the map grows large.
	if len(l.buckets) > 100_000 {
		for k, v := range l.buckets {
			if now.Sub(v.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	return true
}

// limit wraps a handler with per-client-IP rate limiting.
func (l *rateLimiter) limit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			writeErr(w, http.StatusTooManyRequests, "too many requests; slow down and retry")
			return
		}
		next(w, r)
	}
}

// ---- Hardened CORS ----

// withCORS reflects only explicitly allowed origins (comma-separated in
// ALLOWED_ORIGINS; "*" only when no origins are configured, i.e. local dev).
func withCORS(next http.Handler, allowed string) http.Handler {
	origins := map[string]bool{}
	wildcard := false
	for _, o := range strings.Split(allowed, ",") {
		o = strings.TrimSpace(o)
		if o == "*" {
			wildcard = true
		} else if o != "" {
			origins[o] = true
		}
	}
	if !wildcard && len(origins) == 0 {
		wildcard = true // development default
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if wildcard {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- Security headers ----

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(w, r)
	})
}
