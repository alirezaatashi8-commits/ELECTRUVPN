package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

const maxInitDataLen = 4096

// AdminAuth exchanges verified Telegram WebApp initData for a panel session
// (JWT cookie + bearer token). Only Telegram IDs with owner/admin role may enter.
func (h *TMAHandler) AdminAuth(c *gin.Context) {
	initData := strings.TrimSpace(c.GetHeader("X-Telegram-Init-Data"))
	if initData == "" {
		var body struct {
			InitData string `json:"initData"`
		}
		_ = c.ShouldBindJSON(&body)
		initData = strings.TrimSpace(body.InitData)
	}
	// Intentionally NO query-string fallback: initData must not land in access logs / referrers.

	if initData == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "initData required"})
		return
	}
	if len(initData) > maxInitDataLen {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "initData too large"})
		return
	}

	token := telegram.GetSetting(h.S.DB, telegram.KeyAdminToken)
	if token == "" {
		token = telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	}
	if token == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "bot token not configured"})
		return
	}

	uid, username, err := telegram.ValidateWebAppInitData(initData, token, time.Hour)
	if err != nil || uid == 0 {
		alt := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
		if token != alt && alt != "" {
			uid, username, err = telegram.ValidateWebAppInitData(initData, alt, time.Hour)
		}
	}
	if err != nil || uid == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "احراز هویت تلگرام ناموفق"})
		return
	}

	role := telegram.AdminRole(h.S.DB, uid)
	if role != "owner" && role != "admin" {
		// Do not echo tgId to non-admins (enumeration / social-engineering aid).
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "دسترسی فقط برای ادمین کل"})
		return
	}

	user, err := resolvePanelOwner(h.S)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}

	ttl := 8 * time.Hour
	jwt, err := auth.IssueToken(h.S.Config.JWTSecret, user.ID, user.Username, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not issue session"})
		return
	}
	now := time.Now()
	if claims, parseErr := auth.ParseToken(h.S.Config.JWTSecret, jwt); parseErr == nil && claims.ID != "" {
		_ = h.S.DB.Create(&models.PanelSession{
			UserID: user.ID, TokenID: claims.ID, IP: c.ClientIP(),
			UserAgent: "tma-admin/" + sanitizeUA(username),
			CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(ttl),
		}).Error
	}

	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	path := cookiePath(h.S.Config.PanelBasePath)
	c.SetCookie(auth.SessionCookieName, jwt, int(ttl.Seconds()), path, "", secure, true)

	AuditTMA(h.S.DB, uid, "tma.admin.auth", user.Username, gin.H{"role": role, "panelUser": user.ID})

	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"token":      jwt,
		"expiresIn":  int(ttl.Seconds()),
		"username":   user.Username,
		"panelRole":  user.Role,
		"tgId":       uid,
		"tgUsername": username,
		"tgRole":     role,
		"panelPath":  path,
	}})
}

// AdminMe is a lightweight probe that the TMA admin SPA uses after auth.
func (h *TMAHandler) AdminMe(c *gin.Context) {
	initData := strings.TrimSpace(c.GetHeader("X-Telegram-Init-Data"))
	if initData == "" || len(initData) > maxInitDataLen {
		c.JSON(401, gin.H{"success": false, "msg": "unauthorized"})
		return
	}
	token := telegram.GetSetting(h.S.DB, telegram.KeyAdminToken)
	if token == "" {
		token = telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	}
	uid, username, err := telegram.ValidateWebAppInitData(initData, token, time.Hour)
	if err != nil {
		alt := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
		if alt != "" && alt != token {
			uid, username, err = telegram.ValidateWebAppInitData(initData, alt, time.Hour)
		}
	}
	if err != nil || uid == 0 {
		c.JSON(401, gin.H{"success": false, "msg": "unauthorized"})
		return
	}
	role := telegram.AdminRole(h.S.DB, uid)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"tgId": uid, "username": username, "role": role,
		"allowed": role == "owner" || role == "admin",
	}})
}

func resolvePanelOwner(s *core.Server) (models.User, error) {
	var user models.User
	if err := s.DB.Where("role = ? AND enabled = ?", "owner", true).Order("id asc").First(&user).Error; err == nil {
		return user, nil
	}
	if err := s.DB.Where("enabled = ?", true).Order("id asc").First(&user).Error; err == nil {
		return user, nil
	}
	return user, fmt.Errorf("هیچ کاربر پنل فعالی یافت نشد — ابتدا از لاگین وب ادمین بسازید")
}

func sanitizeUA(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
