package auth

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

// SessionCookieName is the cookie holding the signed JWT.
const SessionCookieName = "electru_session"

// RequireAuth rejects any request without a valid, unexpired session
// cookie. On success it stores the parsed claims in the gin context under
// "claims" so downstream handlers can read the logged-in user.
func RequireAuth(jwtSecret string, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookieName)
		if err != nil || token == "" {
			// Telegram WebApp / mobile clients may send Authorization: Bearer <jwt>
			h := c.GetHeader("Authorization")
			if strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			}
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "not authenticated"})
			return
		}

		claims, err := ParseToken(jwtSecret, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "session expired"})
			return
		}

		if claims.IssuedAt == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "invalid session"})
			return
		}
		var user models.User
		if err := db.Select("password_changed_at", "enabled", "role").First(&user, claims.UserID).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "session validation failed"})
			return
		}
		if !user.Enabled && user.Role != "owner" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "account disabled"})
			return
		}
		if user.PasswordChangedAt != nil && claims.IssuedAt.Time.Before(*user.PasswordChangedAt) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "session revoked; sign in again"})
			return
		}
		// Operator-configurable session timeout and IP policy. These are fail-open
		// when the setting is absent, preserving compatibility with older installs.
		if raw := panelSetting(db, "settings.sessionTimeoutHours"); raw != "" {
			if hours, err := strconv.ParseFloat(raw, 64); err == nil && hours > 0 && time.Since(claims.IssuedAt.Time) > time.Duration(hours*float64(time.Hour)) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "session timeout; sign in again"})
				return
			}
		}
		if !ipAllowed(c.ClientIP(), panelSetting(db, "settings.panelIPAllowlist"), panelSetting(db, "settings.panelIPBlocklist")) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "client IP is not allowed"})
			return
		}
		if strings.Contains(c.Request.URL.Path, "/api/v1") && !ipAllowed(c.ClientIP(), panelSetting(db, "settings.apiIPAllowlist"), panelSetting(db, "settings.apiIPBlocklist")) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "API client IP is not allowed"})
			return
		}
		if claims.ID != "" {
			var session models.PanelSession
			if err := db.Where("token_id = ?", claims.ID).First(&session).Error; err != nil || session.RevokedAt != nil || session.ExpiresAt.Before(time.Now()) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "session revoked or expired"})
				return
			}
			// Throttle last_seen writes (avoid UPDATE on every API call).
			if session.LastSeenAt.IsZero() || time.Since(session.LastSeenAt) > 5*time.Minute {
				db.Model(&session).Update("last_seen_at", time.Now())
			}
		}
		c.Set("claims", claims)
		c.Next()
	}
}

func panelSetting(db *gorm.DB, key string) string {
	var r models.PanelSetting
	if db.Where("key = ?", key).First(&r).Error != nil {
		return ""
	}
	return r.Value
}

func ipAllowed(raw, allow, block string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	match := func(list string) bool {
		for _, item := range strings.Split(list, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if item == raw {
				return true
			}
			if _, n, e := net.ParseCIDR(item); e == nil && n.Contains(ip) {
				return true
			}
		}
		return false
	}
	if strings.TrimSpace(block) != "" && match(block) {
		return false
	}
	if strings.TrimSpace(allow) != "" && !match(allow) {
		return false
	}
	return true
}
