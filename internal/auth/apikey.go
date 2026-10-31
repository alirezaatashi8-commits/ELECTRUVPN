package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

func RequireAPIKey(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "API key required"})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "API key required"})
			return
		}
		sum := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(sum[:])
		var k models.APIKey
		if db.Where("secret_hash = ? AND enabled = ?", hash, true).First(&k).Error != nil {
			c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "invalid API key"})
			return
		}
		now := time.Now()
		db.Model(&k).Update("last_used_at", now)
		c.Set("apiKey", &k)
		c.Next()
	}
}

// RequireAPIScope limits an API-key route to the requested scope. Existing
// wildcard (*) keys remain compatible; keys with an empty scope list are
// deliberately denied so permissions fail closed.
func RequireAPIScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get("apiKey")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "API key required"})
			return
		}
		k, ok := v.(*models.APIKey)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "invalid API key context"})
			return
		}
		var scopes []string
		if json.Unmarshal([]byte(k.Scopes), &scopes) != nil {
			scopes = nil
		}
		allowed := false
		for _, s := range scopes {
			if s == "*" || s == scope {
				allowed = true
				break
			}
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "API scope denied", "scope": scope})
			return
		}
		c.Next()
	}
}
