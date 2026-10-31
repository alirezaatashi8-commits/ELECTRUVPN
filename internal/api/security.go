package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/gin-gonic/gin"
)

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
		// 'unsafe-eval' is required by the standard Alpine.js build the panel UI is written
		// against: it compiles x-data / @click / :class expressions with new Function().
		// Without it the browser blocks Alpine, so no UI binding works (the login form never
		// submits). Everything is still served from 'self'; no remote script origins are allowed.
		c.Header("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; manifest-src 'self'")
		if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

func requestBodyLimit(maxBytes int64) gin.HandlerFunc {
	if maxBytes < 1 {
		maxBytes = 1
	}
	return func(c *gin.Context) {
		effectiveLimit := maxBytes
		// The authenticated API defaults to 4 MiB, but upload endpoints have
		// explicit, narrower limits in their handlers. Do not let the parent
		// middleware silently truncate multipart uploads before those handlers
		// can enforce their own limits.
		if maxBytes == 4<<20 {
			path := strings.TrimRight(c.Request.URL.Path, "/")
			switch {
			case strings.HasSuffix(path, "/api/inbounds/import-3x-ui"):
				effectiveLimit = 129 << 20
			case strings.HasSuffix(path, "/api/pro/backups/upload"):
				effectiveLimit = 1537 << 20
			}
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, effectiveLimit)
		}
		c.Next()
	}
}

func requestRateLimit(limit int, window time.Duration) gin.HandlerFunc {
	limiter := auth.NewRequestLimiter(limit, window)
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		key := c.ClientIP() + "|" + c.Request.Method + "|" + path
		if ok, retry := limiter.Allow(key); !ok {
			if retry > 0 {
				seconds := int((retry + time.Second - 1) / time.Second)
				if seconds < 1 {
					seconds = 1
				}
				c.Header("Retry-After", strconv.Itoa(seconds))
			}
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "msg": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

// sameOriginProtection is a defense-in-depth CSRF check for browser session
// requests. API-key and agent routes are outside this middleware. Missing
// Origin/Referer is rejected for state-changing browser calls, preventing
// cross-site form tricks from reaching cookie-authenticated mutations.
func sameOriginProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		// Bearer JWT is not auto-attached by browsers on cross-site form posts,
		// so missing Origin is acceptable when Authorization is present (TMA admin).
		authz := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(authz, "Bearer ") && len(authz) > 20 {
			c.Next()
			return
		}
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			origin = strings.TrimSpace(c.GetHeader("Referer"))
		}
		if origin == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "origin required"})
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Scheme, requestScheme(c)) || !sameHost(u.Host, requestHost(c)) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "cross-site request blocked"})
			return
		}
		c.Next()
	}
}

func requestScheme(c *gin.Context) string {
	if c.Request.TLS != nil {
		return "https"
	}
	if p := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); p != "" {
		return strings.ToLower(strings.Split(p, ",")[0])
	}
	return "http"
}
func requestHost(c *gin.Context) string {
	if h := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); h != "" {
		return strings.TrimSpace(strings.Split(h, ",")[0])
	}
	return c.Request.Host
}
func sameHost(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	// URL Host may include a port. Compare hostnames and ports when present.
	au, ae := url.Parse("http://" + a)
	bu, be := url.Parse("http://" + b)
	return ae == nil && be == nil && strings.EqualFold(au.Hostname(), bu.Hostname()) && au.Port() == bu.Port()
}
