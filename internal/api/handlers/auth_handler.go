package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	S     *core.Server
	guard *auth.LoginGuard
}

func cookiePath(basePath string) string {
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "//" {
		return "/"
	}
	return strings.TrimRight(basePath, "/")
}

// NewAuthHandler creates or exposes the NewAuthHandler handler.
func NewAuthHandler(s *core.Server) *AuthHandler {
	g := auth.NewLoginGuard(5, 5*time.Minute)
	g.OnLockout = func(key string, attempts int) {
		token := telegram.GetSetting(s.DB, telegram.KeyAdminToken)
		ids := telegram.ParseAdminIDs(telegram.GetSetting(s.DB, telegram.KeyAdminIDs))
		if token == "" || len(ids) == 0 {
			return
		}
		cli := telegram.NewClient(token)
		msg := fmt.Sprintf("🚨 <b>Brute-force alert</b>\nLogin lockout after %d failures\nKey: <code>%s</code>", attempts, key)
		for id := range ids {
			_ = cli.SendMessage(id, msg)
		}
	}
	return &AuthHandler{S: s, guard: g}
}

type loginRequest struct {
	Username string `json:"username" binding:"required,min=1,max=128"`
	Password string `json:"password" binding:"required,max=256"`
}

// Login validates credentials and, on success, sets an httpOnly session
// cookie containing a signed JWT. There's no separate "get a token back in
// the JSON body" flow on purpose -- keeping the token cookie-only makes it
// invisible to any XSS in the SPA's own JS.
// Login authenticates an administrator and creates a secure browser session.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid request"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid username"})
		return
	}
	guardKey := fmt.Sprintf("%s:%s", c.ClientIP(), req.Username)
	if settingBool(h.S, "settings.emergencyNewLogins", false) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "new logins are temporarily disabled by emergency mode"})
		return
	}
	if ok, remaining := h.guard.Allowed(guardKey); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"msg":     fmt.Sprintf("too many failed attempts, try again in %d minute(s)", int(remaining.Minutes())+1),
		})
		return
	}

	var user models.User
	if err := h.S.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		h.guard.RecordFailure(guardKey)
		slog.Warn("authentication failure", "ip", c.ClientIP(), "request_id", c.GetString("request_id"))
		if ok, _ := h.guard.Allowed(guardKey); !ok {
			auditAction(h.S, c, "auth.lockout", req.Username, "unknown-user")
		}
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "invalid username or password"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		h.guard.RecordFailure(guardKey)
		slog.Warn("authentication failure", "ip", c.ClientIP(), "request_id", c.GetString("request_id"))
		if ok, _ := h.guard.Allowed(guardKey); !ok {
			auditAction(h.S, c, "auth.lockout", user.Username, "bad-password")
		}
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "invalid username or password"})
		return
	}

	h.guard.RecordSuccess(guardKey)
	auditAction(h.S, c, "auth.login", user.Username, "success")

	token, err := auth.IssueToken(h.S.Config.JWTSecret, user.ID, user.Username, 7*24*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not issue session"})
		return
	}

	now := time.Now()
	h.S.DB.Model(&user).Updates(map[string]any{"last_login_at": now, "last_login_ip": c.ClientIP()})
	if tokenClaims, parseErr := auth.ParseToken(h.S.Config.JWTSecret, token); parseErr == nil && tokenClaims.ID != "" {
		h.S.DB.Create(&models.PanelSession{UserID: user.ID, TokenID: tokenClaims.ID, IP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent"), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
	}

	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(auth.SessionCookieName, token, 7*24*3600, cookiePath(h.S.Config.PanelBasePath), "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "logged in"})
}

// Logout clears the session cookie.

// ChangeCredentials lets the signed-in account change its username and/or password.
// The current password is always required, and the session is revoked after a successful change.
type changeCredentialsRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	Username        string `json:"username" binding:"required,min=3,max=64"`
	NewPassword     string `json:"newPassword" binding:"required,min=12"`
}

// ChangeCredentials handles the ChangeCredentials API operation.
func (h *AuthHandler) ChangeCredentials(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)
	var req changeCredentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "username must be 3-64 characters and password must be at least 12 characters"})
		return
	}

	username := strings.TrimSpace(req.Username)
	if len(username) < 3 || len(username) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "username must be 3-64 characters"})
		return
	}
	for _, r := range username {
		if !(r == '_' || r == '-' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "username may contain only letters, numbers, dot, dash and underscore"})
			return
		}
	}

	var user models.User
	if err := h.S.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "user not found"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "current password is incorrect"})
		return
	}

	var existing models.User
	if err := h.S.DB.Where("username = ? AND id <> ?", username, user.ID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "username is already in use"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not hash password"})
		return
	}

	now := time.Now()
	if err := h.S.DB.Model(&user).Updates(map[string]any{
		"username":             username,
		"password_hash":        string(hash),
		"must_change_password": false,
		"password_changed_at":  now,
	}).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "could not update credentials; username may already be in use"})
		return
	}

	auditAction(h.S, c, "auth.credentials.change", username, "username/password updated")
	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(auth.SessionCookieName, "", -1, cookiePath(h.S.Config.PanelBasePath), "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "credentials updated; sign in again"})
}

// RevokeAllSessions handles the RevokeAllSessions API operation.
func (h *AuthHandler) RevokeAllSessions(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)
	var user models.User
	if err := h.S.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "user not found"})
		return
	}
	now := time.Now()
	if err := h.S.DB.Model(&user).Update("password_changed_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not revoke sessions"})
		return
	}
	h.S.DB.Model(&models.PanelSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", now)
	auditAction(h.S, c, "auth.sessions.revoke_all", user.Username, "all sessions revoked")
	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(auth.SessionCookieName, "", -1, cookiePath(h.S.Config.PanelBasePath), "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "all sessions revoked; sign in again"})
}

// Logout handles the Logout API operation.
func (h *AuthHandler) Logout(c *gin.Context) {
	if claims, ok := c.Get("claims"); ok {
		if cl, ok := claims.(*auth.Claims); ok && cl.ID != "" {
			now := time.Now()
			h.S.DB.Model(&models.PanelSession{}).Where("token_id = ?", cl.ID).Update("revoked_at", now)
		}
	}
	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(auth.SessionCookieName, "", -1, cookiePath(h.S.Config.PanelBasePath), "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Me returns the currently logged-in account and role, used by the SPA on boot.
func (h *AuthHandler) Me(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)

	var user models.User
	if err := h.S.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"username":           claims.Username,
		"role":               user.Role,
		"mustChangePassword": user.MustChangePassword,
	}})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required,min=12"`
}

// ChangePassword lets the logged-in admin rotate their own password -- the
// first thing anyone should do after the generated first-run password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid request"})
		return
	}

	var user models.User
	if err := h.S.DB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "user not found"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "msg": "current password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not hash password"})
		return
	}

	now := time.Now()
	if err := h.S.DB.Model(&user).Updates(map[string]any{"password_hash": string(hash), "must_change_password": false, "password_changed_at": now}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not update password"})
		return
	}
	// Revoke the current cookie immediately; the user must sign in again.
	c.SetSameSite(http.SameSiteLaxMode)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetCookie(auth.SessionCookieName, "", -1, cookiePath(h.S.Config.PanelBasePath), "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "password updated"})
}
