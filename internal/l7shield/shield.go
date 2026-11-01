// Package l7shield provides behavioral rate limiting and progressive defenses
// for public HTTP surfaces (subscription, login) without breaking legitimate clients.
package l7shield

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type action int

const (
	actionAllow action = iota
	actionDelay
	actionEmpty  // 204 / empty body — wastes bots that expect configs
	actionBlock  // 429
	actionTarpit // 503 with long Retry-After; optional honeypot signal
)

type ipState struct {
	mu           sync.Mutex
	windowStart  time.Time
	hits         int
	score        int
	blockedUntil time.Time
	lastPath     string
	badUA        int
	notFound     int
}

// Shield is an in-memory L7 defense (single-node). Suitable for panel hosts.
type Shield struct {
	mu  sync.Mutex
	ips map[string]*ipState
	// Tunables
	Window        time.Duration
	SoftLimit     int // start delay
	HardLimit     int // empty / 429
	BlockLimit    int // temporary block
	BlockDuration time.Duration
	MaxScore      int
	CleanupEvery  time.Duration
	lastCleanup   time.Time
	TotalBlocked  int64
	TotalDelayed  int64
	TotalEmpty    int64
}

func New() *Shield {
	return &Shield{
		ips:           make(map[string]*ipState),
		Window:        time.Minute,
		SoftLimit:     40, // normal clients rarely exceed this per minute per IP
		HardLimit:     80,
		BlockLimit:    120,
		BlockDuration: 30 * time.Minute,
		MaxScore:      100,
		CleanupEvery:  5 * time.Minute,
	}
}

var Default = New()

func (s *Shield) state(ip string) *ipState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastCleanup) > s.CleanupEvery {
		s.cleanupLocked()
		s.lastCleanup = time.Now()
	}
	st, ok := s.ips[ip]
	if !ok {
		st = &ipState{windowStart: time.Now()}
		s.ips[ip] = st
	}
	return st
}

func (s *Shield) cleanupLocked() {
	now := time.Now()
	for ip, st := range s.ips {
		st.mu.Lock()
		expired := now.Sub(st.windowStart) > 2*s.Window && now.After(st.blockedUntil)
		st.mu.Unlock()
		if expired {
			delete(s.ips, ip)
		}
	}
}

func (s *Shield) observe(ip, path, ua string, statusHint int) action {
	st := s.state(ip)
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	if now.Before(st.blockedUntil) {
		return actionTarpit
	}
	if now.Sub(st.windowStart) > s.Window {
		st.windowStart = now
		st.hits = 0
		// decay score slowly
		st.score = st.score * 3 / 4
	}
	st.hits++
	st.lastPath = path

	// Behavioral signals
	ua = strings.TrimSpace(ua)
	if ua == "" || len(ua) < 8 {
		st.badUA++
		st.score += 3
	}
	// Scripted probes often use curl/python/scrapy without typical VPN client UAs
	low := strings.ToLower(ua)
	if strings.Contains(low, "python-requests") || strings.Contains(low, "scrapy") ||
		strings.Contains(low, "httpclient") || strings.Contains(low, "go-http-client") && strings.Contains(path, "/sub/") {
		st.score += 2
	}
	if statusHint == 404 || statusHint == 403 {
		st.notFound++
		st.score += 4
	}
	if st.hits > s.SoftLimit {
		st.score += 1
	}
	if st.hits > s.HardLimit {
		st.score += 3
	}
	if st.score > s.MaxScore {
		st.score = s.MaxScore
	}

	if st.hits >= s.BlockLimit || st.score >= 85 {
		st.blockedUntil = now.Add(s.BlockDuration)
		s.TotalBlocked++
		return actionTarpit
	}
	if st.hits >= s.HardLimit || st.score >= 55 {
		s.TotalEmpty++
		return actionEmpty
	}
	if st.hits >= s.SoftLimit || st.score >= 30 {
		s.TotalDelayed++
		return actionDelay
	}
	return actionAllow
}

// Middleware for subscription and other public sensitive GETs.
func (s *Shield) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		path := c.Request.URL.Path
		ua := c.GetHeader("User-Agent")
		act := s.observe(ip, path, ua, 0)
		switch act {
		case actionTarpit:
			c.Header("Retry-After", "1800")
			c.Header("X-Electru-Shield", "tarpit")
			// Long delay without holding huge buffers
			time.Sleep(800 * time.Millisecond)
			c.String(http.StatusServiceUnavailable, "")
			c.Abort()
			return
		case actionEmpty:
			c.Header("X-Electru-Shield", "empty")
			time.Sleep(200 * time.Millisecond)
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		case actionDelay:
			c.Header("X-Electru-Shield", "delay")
			// progressive delay based on path noise
			time.Sleep(120 * time.Millisecond)
		}
		c.Next()
		// Post-response: penalize 404 on sub
		if c.Writer.Status() == 404 || c.Writer.Status() == 403 {
			_ = s.observe(ip, path, ua, c.Writer.Status())
		}
	}
}

// LoginMiddleware stricter for credential stuffing.
func (s *Shield) LoginMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		st := s.state(ip)
		st.mu.Lock()
		now := time.Now()
		if now.Before(st.blockedUntil) {
			st.mu.Unlock()
			c.Header("Retry-After", "900")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "msg": "temporarily locked"})
			return
		}
		st.mu.Unlock()
		c.Next()
		if c.Writer.Status() == 401 || c.Writer.Status() == 403 {
			st.mu.Lock()
			st.score += 15
			st.hits += 5
			if st.score >= 60 || st.hits >= 30 {
				st.blockedUntil = time.Now().Add(15 * time.Minute)
				s.TotalBlocked++
			}
			st.mu.Unlock()
		}
	}
}

func (s *Shield) Stats() map[string]any {
	s.mu.Lock()
	n := len(s.ips)
	s.mu.Unlock()
	return map[string]any{
		"trackedIPs":    n,
		"totalBlocked":  s.TotalBlocked,
		"totalDelayed":  s.TotalDelayed,
		"totalEmpty":    s.TotalEmpty,
		"softLimit":     s.SoftLimit,
		"hardLimit":     s.HardLimit,
		"blockLimit":    s.BlockLimit,
		"windowSec":     int(s.Window.Seconds()),
		"blockDuration": s.BlockDuration.String(),
	}
}

// Unblock clears state for an IP (admin).
func (s *Shield) Unblock(ip string) {
	s.mu.Lock()
	delete(s.ips, ip)
	s.mu.Unlock()
}

// Score returns current score for debugging.
func (s *Shield) Score(ip string) int {
	st := s.state(ip)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.score
}

var _ = strconv.Itoa
