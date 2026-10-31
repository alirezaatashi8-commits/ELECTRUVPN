package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// AdminHandler manages panel operators (RBAC + sales quotas).
type AdminHandler struct{ S *core.Server }

func NewAdminHandler(s *core.Server) *AdminHandler { return &AdminHandler{S: s} }

// ListAdmins returns all panel users (owner only).
func (h *AdminHandler) ListAdmins(c *gin.Context) {
	var rows []models.User
	if err := h.S.DB.Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, u := range rows {
		out = append(out, gin.H{
			"id": u.ID, "username": u.Username, "role": u.Role, "enabled": u.Enabled,
			"maxTotalGB": u.MaxTotalGB, "maxGBPerClient": u.MaxGBPerClient, "maxClients": u.MaxClients,
			"allocatedGB": u.AllocatedGB, "clientCount": u.ClientCount, "remainingGB": u.RemainingGB(),
			"notes": u.Notes, "lastLoginAt": u.LastLoginAt, "lastLoginIp": u.LastLoginIP,
			"createdAt": u.CreatedAt, "mustChangePassword": u.MustChangePassword,
		})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

type createAdminReq struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	Role           string `json:"role"`
	MaxTotalGB     int64  `json:"maxTotalGB"`
	MaxGBPerClient int64  `json:"maxGBPerClient"`
	MaxClients     int    `json:"maxClients"`
	Notes          string `json:"notes"`
}

// CreateAdmin creates a panel operator with optional sales quotas.
func (h *AdminHandler) CreateAdmin(c *gin.Context) {
	var req createAdminReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Username == "" || len(req.Password) < 8 {
		c.JSON(400, gin.H{"success": false, "msg": "username and password (min 8) required"})
		return
	}
	switch req.Role {
	case auth.RoleAdmin, auth.RoleSales, auth.RoleSupport, auth.RoleOperator:
	default:
		c.JSON(400, gin.H{"success": false, "msg": "role must be admin|sales|support|operator"})
		return
	}
	if req.MaxTotalGB < 0 || req.MaxGBPerClient < 0 || req.MaxClients < 0 {
		c.JSON(400, gin.H{"success": false, "msg": "quotas must be >= 0"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "hash failed"})
		return
	}
	u := models.User{
		Username: req.Username, PasswordHash: string(hash), Role: req.Role, Enabled: true,
		MustChangePassword: true, MaxTotalGB: req.MaxTotalGB, MaxGBPerClient: req.MaxGBPerClient,
		MaxClients: req.MaxClients, Notes: strings.TrimSpace(req.Notes),
	}
	if err := h.S.DB.Create(&u).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "admin.create", fmt.Sprint(u.ID), gin.H{"username": u.Username, "role": u.Role, "maxTotalGB": u.MaxTotalGB})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": u.ID, "username": u.Username, "role": u.Role}})
}

type updateAdminReq struct {
	Role               *string `json:"role"`
	Enabled            *bool   `json:"enabled"`
	MaxTotalGB         *int64  `json:"maxTotalGB"`
	MaxGBPerClient     *int64  `json:"maxGBPerClient"`
	MaxClients         *int    `json:"maxClients"`
	Notes              *string `json:"notes"`
	Password           *string `json:"password"`
	MustChangePassword *bool   `json:"mustChangePassword"`
}

// UpdateAdmin updates role, quotas, enabled flag (owner only).
func (h *AdminHandler) UpdateAdmin(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var u models.User
	if h.S.DB.First(&u, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "user not found"})
		return
	}
	var req updateAdminReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	// Protect last owner
	if u.Role == auth.RoleOwner {
		if req.Role != nil && *req.Role != auth.RoleOwner {
			c.JSON(400, gin.H{"success": false, "msg": "cannot demote owner"})
			return
		}
		if req.Enabled != nil && !*req.Enabled {
			c.JSON(400, gin.H{"success": false, "msg": "cannot disable owner"})
			return
		}
	}
	updates := map[string]any{}
	if req.Role != nil {
		r := strings.ToLower(strings.TrimSpace(*req.Role))
		switch r {
		case auth.RoleAdmin, auth.RoleSales, auth.RoleSupport, auth.RoleOperator, auth.RoleOwner:
			updates["role"] = r
		default:
			c.JSON(400, gin.H{"success": false, "msg": "invalid role"})
			return
		}
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.MaxTotalGB != nil {
		if *req.MaxTotalGB < 0 {
			c.JSON(400, gin.H{"success": false, "msg": "maxTotalGB invalid"})
			return
		}
		updates["max_total_gb"] = *req.MaxTotalGB
	}
	if req.MaxGBPerClient != nil {
		if *req.MaxGBPerClient < 0 {
			c.JSON(400, gin.H{"success": false, "msg": "maxGBPerClient invalid"})
			return
		}
		updates["max_gb_per_client"] = *req.MaxGBPerClient
	}
	if req.MaxClients != nil {
		if *req.MaxClients < 0 {
			c.JSON(400, gin.H{"success": false, "msg": "maxClients invalid"})
			return
		}
		updates["max_clients"] = *req.MaxClients
	}
	if req.Notes != nil {
		updates["notes"] = strings.TrimSpace(*req.Notes)
	}
	if req.MustChangePassword != nil {
		updates["must_change_password"] = *req.MustChangePassword
	}
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		if len(*req.Password) < 8 {
			c.JSON(400, gin.H{"success": false, "msg": "password min 8"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "hash failed"})
			return
		}
		updates["password_hash"] = string(hash)
		now := time.Now()
		updates["password_changed_at"] = &now
		updates["must_change_password"] = false
	}
	if len(updates) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "no changes"})
		return
	}
	if err := h.S.DB.Model(&u).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "admin.update", strconv.Itoa(id), updates)
	c.JSON(200, gin.H{"success": true})
}

// MeQuota returns the current operator's remaining sales quota.
func (h *AdminHandler) MeQuota(c *gin.Context) {
	u := auth.CurrentUser(c)
	if u == nil {
		// load from claims
		if cl, ok := c.Get("claims"); ok {
			if x, ok := cl.(*auth.Claims); ok {
				var row models.User
				if h.S.DB.First(&row, x.UserID).Error == nil {
					u = &row
				}
			}
		}
	}
	if u == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"id": u.ID, "username": u.Username, "role": u.Role,
		"maxTotalGB": u.MaxTotalGB, "maxGBPerClient": u.MaxGBPerClient, "maxClients": u.MaxClients,
		"allocatedGB": u.AllocatedGB, "clientCount": u.ClientCount, "remainingGB": u.RemainingGB(),
	}})
}

// ListAudit returns recent audit log entries with RBAC-aware filters.
// Query: limit, username, role, action, source, target, from, to (RFC3339 or YYYY-MM-DD).
func (h *AdminHandler) ListAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	q := h.S.DB.Model(&models.AuditLog{}).Order("id desc").Limit(limit)
	u := auth.CurrentUser(c)
	if u != nil && (u.Role == auth.RoleSales || u.Role == auth.RoleSupport || u.Role == auth.RoleOperator) {
		q = q.Where("user_id = ?", u.ID)
	}
	if user := strings.TrimSpace(c.Query("username")); user != "" {
		q = q.Where("username = ?", user)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		q = q.Where("role = ?", role)
	}
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		q = q.Where("action LIKE ?", "%"+action+"%")
	}
	if source := strings.TrimSpace(c.Query("source")); source != "" {
		q = q.Where("source = ?", source)
	}
	if target := strings.TrimSpace(c.Query("target")); target != "" {
		q = q.Where("target LIKE ?", "%"+target+"%")
	}
	if from := strings.TrimSpace(c.Query("from")); from != "" {
		q = q.Where("created_at >= ?", from)
	}
	if to := strings.TrimSpace(c.Query("to")); to != "" {
		q = q.Where("created_at <= ?", to)
	}
	var rows []models.AuditLog
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}
