// Package relay — per-IP rate limiting middleware (stdlib only, no external deps).
package relay

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// tokenBucket is a simple token-bucket rate limiter for a single IP.
// It allows up to maxTokens events per window duration.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     int
	maxTokens  int
	window     time.Duration
	windowStart time.Time
	lastSeen   time.Time
}

// allow returns true if the request is permitted, false if it should be rate-limited.
func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.lastSeen = now

	// Refill: start a new window if the current one has expired.
	if now.Sub(b.windowStart) >= b.window {
		b.tokens = b.maxTokens
		b.windowStart = now
	}

	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// IPRateLimiter limits WebSocket upgrade attempts per remote IP address.
// It uses a sliding-window (actually fixed-window per IP) approach with
// a background goroutine to periodically evict stale entries.
type IPRateLimiter struct {
	buckets sync.Map // IP string → *tokenBucket

	maxTokens int
	window    time.Duration
}

// NewIPRateLimiter creates a limiter that allows up to maxTokens requests per window per IP.
// Call Close() (or rely on GC) when done; the cleanup goroutine runs on a timer.
func NewIPRateLimiter(maxTokens int, window time.Duration) *IPRateLimiter {
	rl := &IPRateLimiter{
		maxTokens: maxTokens,
		window:    window,
	}
	// Start stale-entry cleanup goroutine.
	go rl.cleanupLoop()
	return rl
}

// Allow returns true when the given IP is within its rate limit.
func (rl *IPRateLimiter) Allow(ip string) bool {
	v, _ := rl.buckets.LoadOrStore(ip, &tokenBucket{
		tokens:      rl.maxTokens,
		maxTokens:   rl.maxTokens,
		window:      rl.window,
		windowStart: time.Now(),
		lastSeen:    time.Now(),
	})
	return v.(*tokenBucket).allow()
}

// cleanupLoop removes buckets that have not been seen for 2× the window duration.
func (rl *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.window * 2)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-rl.window * 2)
		rl.buckets.Range(func(k, v any) bool {
			b := v.(*tokenBucket)
			b.mu.Lock()
			stale := b.lastSeen.Before(cutoff)
			b.mu.Unlock()
			if stale {
				rl.buckets.Delete(k)
			}
			return true
		})
	}
}

// defaultLimiter is the package-level rate limiter: 10 upgrade attempts per minute per IP.
var defaultLimiter = NewIPRateLimiter(10, time.Minute)

// RateLimitMiddleware wraps next and rejects requests from IPs that exceed
// 10 WebSocket upgrade attempts per minute with HTTP 429.
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			// Fall back to the raw address if parsing fails.
			ip = r.RemoteAddr
		}
		if !defaultLimiter.Allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
