package auth

import (
	"sync"
	"time"
)

// LoginGuard tracks failed login attempts per key (IP+username) and locks
// out further attempts for a cooldown period once a threshold is hit. It's
// a small in-memory brute-force brake, not a replacement for putting the
// panel behind fail2ban/a WAF -- but it costs nothing to include and stops
// the most naive password-guessing scripts cold.
type LoginGuard struct {
	mu          sync.Mutex
	attempts    map[string]int
	lockedUntil map[string]time.Time
	lastSeen    map[string]time.Time
	firstFail   map[string]time.Time
	maxAttempts int
	lockFor     time.Duration
	// OnLockout is invoked once when a key transitions into lockout (optional).
	OnLockout func(key string, attempts int)
}

func NewLoginGuard(maxAttempts int, lockFor time.Duration) *LoginGuard {
	return &LoginGuard{
		attempts:    make(map[string]int),
		lockedUntil: make(map[string]time.Time),
		lastSeen:    make(map[string]time.Time),
		firstFail:   make(map[string]time.Time),
		maxAttempts: maxAttempts,
		lockFor:     lockFor,
	}
}

// Allowed reports whether key may attempt a login right now, and if not,
// how much longer the lockout lasts.
func (g *LoginGuard) Allowed(key string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.lastSeen[key] = now
	until, locked := g.lockedUntil[key]
	if !locked {
		return true, 0
	}
	if time.Now().After(until) {
		delete(g.lockedUntil, key)
		delete(g.attempts, key)
		delete(g.lastSeen, key)
		return true, 0
	}
	return false, time.Until(until)
}

// RecordFailure increments the failure count for key and locks it out once
// the threshold is reached.
func (g *LoginGuard) RecordFailure(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.lastSeen[key] = now
	// Bound memory even if an attacker cycles through unique usernames.
	if len(g.attempts) >= 10000 && g.attempts[key] == 0 {
		for k, until := range g.lockedUntil {
			if now.After(until) {
				delete(g.lockedUntil, k)
				delete(g.attempts, k)
				delete(g.lastSeen, k)
				delete(g.firstFail, k)
			}
		}
	}
	if len(g.attempts) >= 10000 && g.attempts[key] == 0 {
		var oldest string
		var oldestAt time.Time
		for k, seen := range g.lastSeen {
			if oldest == "" || seen.Before(oldestAt) {
				oldest, oldestAt = k, seen
			}
		}
		if oldest != "" {
			delete(g.lastSeen, oldest)
			delete(g.attempts, oldest)
			delete(g.lockedUntil, oldest)
			delete(g.firstFail, oldest)
		}
	}
	// Failures decay: old failures spread over a long time must not add up
	// to a lockout for a legitimate user who mistyped a password weeks ago.
	if _, locked := g.lockedUntil[key]; !locked {
		if t0, ok := g.firstFail[key]; !ok || now.Sub(t0) > 15*time.Minute {
			g.attempts[key] = 0
			g.firstFail[key] = now
		}
	}
	g.attempts[key]++
	if g.attempts[key] >= g.maxAttempts {
		_, already := g.lockedUntil[key]
		g.lockedUntil[key] = time.Now().Add(g.lockFor)
		if !already && g.OnLockout != nil {
			cb := g.OnLockout
			n := g.attempts[key]
			go cb(key, n)
		}
	}
}

// RecordSuccess clears any tracked failures for key after a successful login.
func (g *LoginGuard) RecordSuccess(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.attempts, key)
	delete(g.lockedUntil, key)
	delete(g.lastSeen, key)
	delete(g.firstFail, key)
}

// RequestLimiter is a bounded, in-memory fixed-window limiter for application
// layer abuse. It is intentionally simple: infrastructure-level DDoS must be
// handled upstream by a provider/WAF/firewall, while this stops a single host
// from consuming the panel's goroutines/SQLite connection pool with bursts.
type RequestLimiter struct {
	mu      sync.Mutex
	buckets map[string]requestBucket
	limit   int
	window  time.Duration
}

type requestBucket struct {
	count int
	start time.Time
}

func NewRequestLimiter(limit int, window time.Duration) *RequestLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RequestLimiter{buckets: make(map[string]requestBucket), limit: limit, window: window}
}

func (r *RequestLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.buckets[key]; ok {
		if now.Sub(b.start) < r.window {
			if b.count >= r.limit {
				return false, r.window - now.Sub(b.start)
			}
			b.count++
			r.buckets[key] = b
			return true, 0
		}
	}
	r.buckets[key] = requestBucket{count: 1, start: now}
	if len(r.buckets) >= 10000 {
		for k, b := range r.buckets {
			if now.Sub(b.start) >= r.window {
				delete(r.buckets, k)
			}
		}
	}
	if len(r.buckets) >= 10000 {
		var oldest string
		var oldestAt time.Time
		for k, b := range r.buckets {
			if oldest == "" || b.start.Before(oldestAt) {
				oldest, oldestAt = k, b.start
			}
		}
		if oldest != "" {
			delete(r.buckets, oldest)
		}
	}
	return true, 0
}
