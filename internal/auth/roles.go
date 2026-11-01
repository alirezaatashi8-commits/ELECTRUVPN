package auth

import (
	"net/http"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Canonical roles (2026 RBAC).
const (
	RoleOwner    = "owner"
	RoleAdmin    = "admin"
	RoleSales    = "sales"
	RoleSupport  = "support"
	RoleOperator = "operator" // legacy mid-level; treated like sales+ops write
	RoleReseller = "reseller" // multi-tier commercial reseller (scoped clients + credit wallet)
)

// RequireRoles allows any of the listed roles. Also rejects disabled accounts.
func RequireRoles(db *gorm.DB, roles ...string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[strings.ToLower(strings.TrimSpace(r))] = true
	}
	return func(c *gin.Context) {
		cl, ok := c.Get("claims")
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "not authenticated"})
			return
		}
		claims := cl.(*Claims)
		var u models.User
		if db.First(&u, claims.UserID).Error != nil {
			c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "user not found"})
			return
		}
		if !u.Enabled && u.Role != RoleOwner {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "account disabled"})
			return
		}
		role := strings.ToLower(strings.TrimSpace(u.Role))
		if allowed[role] {
			c.Set("user", &u)
			c.Set("userRole", role)
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "msg": "insufficient role"})
	}
}

// CanManageUsers is owner-only.
func CanManageUsers(role string) bool {
	return strings.EqualFold(role, RoleOwner)
}

// CanWriteClients — create/update/delete VPN clients.
func CanWriteClients(role string) bool {
	switch strings.ToLower(role) {
	case RoleOwner, RoleAdmin, RoleSales, RoleOperator, RoleReseller:
		return true
	default:
		return false
	}
}

// CanWriteSystem — settings, nodes, backups, dangerous ops.
func CanWriteSystem(role string) bool {
	switch strings.ToLower(role) {
	case RoleOwner, RoleAdmin:
		return true
	default:
		return false
	}
}

// CanReadAll — support may read but not write.
func CanReadAll(role string) bool {
	switch strings.ToLower(role) {
	case RoleOwner, RoleAdmin, RoleSales, RoleSupport, RoleOperator, RoleReseller:
		return true
	default:
		return false
	}
}

// CheckClientQuota validates that user may allocate wantGB for a new/updated client.
// Returns nil if OK, or an error message string.
func CheckClientQuota(u *models.User, wantGB int64, isNew bool) string {
	if u == nil {
		return "no user"
	}
	role := strings.ToLower(u.Role)
	if role == RoleOwner || role == RoleAdmin {
		return "" // unlimited unless explicitly capped
	}
	// resellers use the same numeric quota fields as sales
	if u.MaxGBPerClient > 0 && wantGB > u.MaxGBPerClient {
		return "exceeds max GB per client for this admin"
	}
	if isNew && u.MaxClients > 0 && u.ClientCount >= u.MaxClients {
		return "client count quota exceeded for this admin"
	}
	if u.MaxTotalGB > 0 {
		// For updates, caller should pass delta; for create, wantGB is full amount.
		left := u.MaxTotalGB - u.AllocatedGB
		if wantGB > left {
			return "sales GB quota exceeded for this admin"
		}
	}
	return ""
}

// CurrentUser returns the models.User set by RequireRoles, if any.
func CurrentUser(c *gin.Context) *models.User {
	if v, ok := c.Get("user"); ok {
		if u, ok := v.(*models.User); ok {
			return u
		}
	}
	return nil
}
