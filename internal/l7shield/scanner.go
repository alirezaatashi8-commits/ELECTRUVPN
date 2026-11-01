package l7shield

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Known vulnerability-scanner / exploit probe path fragments (lowercase).
// Matching is substring on path only — never touches legitimate /api /sub /tma routes.
var scannerPathHints = []string{
	"/.env", "/.git", "/.svn", "/.aws", "/.docker",
	"/wp-admin", "/wp-login", "/xmlrpc.php", "/wp-content",
	"/phpmyadmin", "/pma/", "/adminer", "/mysql",
	"/actuator", "/cgi-bin", "/shell", "/eval-stdin",
	"/vendor/phpunit", "/debug/default", "/_ignition",
	"/manager/html", "/solr/", "/jenkins", "/console/",
	"/.well-known/security.txt", // often hit by scanners in bulk with other probes
	"/telescope", "/horizon", "/_profiler",
	"/config.json", "/credentials", "/id_rsa",
	"/etc/passwd", "/proc/self",
	"/boaform", "/HNAP1", "/GponForm", // IoT bots
	"/setup.cgi", "/cgi-bin/luci",
}

// Paths that should never be treated as scanner probes even if they share substrings.
var scannerAllowPrefix = []string{
	"/api/", "/sub/", "/sub-group/", "/tma", "/portal", "/assets/",
	"/login", "/health", "/metrics", "/web",
}

type blackholeEntry struct {
	Until    time.Time
	Reason   string
	Hits     int
	LastPath string
}

// Blackhole is a fast path for IPs that probe known scanner URLs.
type Blackhole struct {
	mu      sync.Mutex
	blocked map[string]*blackholeEntry
	// Tunables
	BanDuration    time.Duration
	ProbeThreshold int // consecutive/scanner hits before ban
	probeCount     map[string]int
	probeWindow    map[string]time.Time
	TotalBanned    int64
	TotalHits      int64
}

func NewBlackhole() *Blackhole {
	return &Blackhole{
		blocked:        make(map[string]*blackholeEntry),
		probeCount:     make(map[string]int),
		probeWindow:    make(map[string]time.Time),
		BanDuration:    2 * time.Hour,
		ProbeThreshold: 2, // two scanner-path hits in window → ban
	}
}

// DefaultBlackhole is the process-wide scanner sink.
var DefaultBlackhole = NewBlackhole()

func isAllowedPanelPath(path string) bool {
	p := strings.ToLower(path)
	for _, a := range scannerAllowPrefix {
		if strings.HasPrefix(p, a) || p == strings.TrimSuffix(a, "/") {
			return true
		}
	}
	return false
}

func isScannerPath(path string) bool {
	p := strings.ToLower(path)
	if isAllowedPanelPath(p) {
		return false
	}
	for _, h := range scannerPathHints {
		if strings.Contains(p, strings.ToLower(h)) {
			return true
		}
	}
	// bulk noise: many random .php/.asp at root
	if strings.Count(p, "/") <= 2 {
		if strings.HasSuffix(p, ".php") || strings.HasSuffix(p, ".asp") || strings.HasSuffix(p, ".aspx") {
			return true
		}
	}
	return false
}

// IsBlocked reports whether IP is in the scanner blackhole.
func (b *Blackhole) IsBlocked(ip string) (bool, string) {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return false, ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.blocked[ip]
	if !ok {
		return false, ""
	}
	if time.Now().After(e.Until) {
		delete(b.blocked, ip)
		return false, ""
	}
	return true, e.Reason
}

// Ban puts IP into blackhole until duration elapses.
func (b *Blackhole) Ban(ip, reason, path string, d time.Duration) {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return
	}
	if d <= 0 {
		d = b.BanDuration
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.blocked[ip]; ok && time.Now().Before(e.Until) {
		e.Hits++
		e.LastPath = path
		e.Until = time.Now().Add(d)
		return
	}
	b.blocked[ip] = &blackholeEntry{
		Until: time.Now().Add(d), Reason: reason, Hits: 1, LastPath: path,
	}
	b.TotalBanned++
}

// Unban removes IP from blackhole.
func (b *Blackhole) Unban(ip string) {
	b.mu.Lock()
	delete(b.blocked, ip)
	delete(b.probeCount, ip)
	delete(b.probeWindow, ip)
	b.mu.Unlock()
}

// ObserveScannerPath records a hit on a scanner URL; may ban.
func (b *Blackhole) ObserveScannerPath(ip, path string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return false
	}
	if !isScannerPath(path) {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.TotalHits++
	now := time.Now()
	if e, ok := b.blocked[ip]; ok && now.Before(e.Until) {
		e.Hits++
		e.LastPath = path
		return true
	}
	// sliding 5-minute window for probe counts
	if ws, ok := b.probeWindow[ip]; !ok || now.Sub(ws) > 5*time.Minute {
		b.probeWindow[ip] = now
		b.probeCount[ip] = 0
	}
	b.probeCount[ip]++
	if b.probeCount[ip] >= b.ProbeThreshold {
		b.blocked[ip] = &blackholeEntry{
			Until: now.Add(b.BanDuration), Reason: "scanner_path", Hits: b.probeCount[ip], LastPath: path,
		}
		b.TotalBanned++
		return true
	}
	return false
}

// List returns current bans for admin UI.
func (b *Blackhole) List() []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(b.blocked))
	for ip, e := range b.blocked {
		if now.After(e.Until) {
			delete(b.blocked, ip)
			continue
		}
		out = append(out, map[string]any{
			"ip": ip, "until": e.Until, "reason": e.Reason,
			"hits": e.Hits, "lastPath": e.LastPath,
			"remainingSec": int(e.Until.Sub(now).Seconds()),
		})
	}
	return out
}

// Stats summarizes blackhole counters.
func (b *Blackhole) Stats() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	active := 0
	now := time.Now()
	for ip, e := range b.blocked {
		if now.After(e.Until) {
			delete(b.blocked, ip)
			continue
		}
		active++
	}
	return map[string]any{
		"activeBans":     active,
		"totalBanned":    b.TotalBanned,
		"totalProbeHits": b.TotalHits,
		"banDuration":    b.BanDuration.String(),
		"probeThreshold": b.ProbeThreshold,
	}
}

// Middleware drops traffic from blackholed IPs and auto-bans scanner probes.
func (b *Blackhole) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		path := c.Request.URL.Path

		if blocked, reason := b.IsBlocked(ip); blocked {
			c.Header("X-Electru-Shield", "blackhole")
			c.Header("X-Electru-Ban-Reason", reason)
			c.Header("Retry-After", "3600")
			time.Sleep(500 * time.Millisecond) // cheap tarpit
			c.String(http.StatusForbidden, "")
			c.Abort()
			return
		}

		if isScannerPath(path) {
			banned := b.ObserveScannerPath(ip, path)
			c.Header("X-Electru-Shield", "scanner")
			time.Sleep(300 * time.Millisecond)
			if banned {
				c.Header("X-Electru-Shield", "blackhole")
				c.String(http.StatusForbidden, "")
			} else {
				// first probe: empty 404 without revealing stack
				c.Status(http.StatusNotFound)
			}
			c.Abort()
			return
		}
		c.Next()
	}
}
