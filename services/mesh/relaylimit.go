package mesh

// relaylimit.go — per-source relay quotas (abuse prevention).
//
// A relay forwards other devices' traffic. Without a per-source cap, one
// flooded device can monopolise the relay's radio and queue. This is a
// token-bucket limiter keyed by source device id, applied only to traffic a
// node forwards (never to packets addressed to itself).

import (
	"sync"
	"time"
)

// RelayQuota configures the per-source token bucket.
type RelayQuota struct {
	// Every is the interval between refilled tokens (1/PerSecond rate).
	Every time.Duration
	// Burst is the bucket capacity (short bursts allowed above the rate).
	Burst int
}

// DefaultRelayQuota allows a burst of 200 packets then refills 50/s —
// comfortably above chat/voice-note traffic, far below flood rates.
func DefaultRelayQuota() RelayQuota {
	return RelayQuota{Every: 20 * time.Millisecond, Burst: 200}
}

type relayBucket struct {
	tokens float64
	last   time.Time
}

// RelayLimiter is a per-source token bucket for forwarded traffic.
type RelayLimiter struct {
	mu      sync.Mutex
	quota   RelayQuota
	buckets map[string]*relayBucket
}

// NewRelayLimiter creates a limiter with the given quota.
func NewRelayLimiter(q RelayQuota) *RelayLimiter {
	if q.Every <= 0 || q.Burst <= 0 {
		q = DefaultRelayQuota()
	}
	return &RelayLimiter{quota: q, buckets: make(map[string]*relayBucket)}
}

// Allow reports whether src may forward one packet now.
func (r *RelayLimiter) Allow(src string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[src]
	if !ok {
		b = &relayBucket{tokens: float64(r.quota.Burst), last: now}
		r.buckets[src] = b
	}
	elapsed := now.Sub(b.last)
	b.tokens += elapsed.Seconds() / r.quota.Every.Seconds()
	if b.tokens > float64(r.quota.Burst) {
		b.tokens = float64(r.quota.Burst)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Len returns the number of tracked sources (bounded via Prune).
func (r *RelayLimiter) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buckets)
}

// Prune drops buckets idle longer than maxIdle so long-lived relays do not
// grow the map without bound.
func (r *RelayLimiter) Prune(now time.Time, maxIdle time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for src, b := range r.buckets {
		if now.Sub(b.last) > maxIdle {
			delete(r.buckets, src)
		}
	}
}
