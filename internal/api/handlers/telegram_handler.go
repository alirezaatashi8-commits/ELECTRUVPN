package handlers

import (
	"strings"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/notify"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

type TelegramHandler struct{ S *core.Server }

func NewTelegramHandler(s *core.Server) *TelegramHandler { return &TelegramHandler{S: s} }

// GetConfig returns current Telegram bot configuration (tokens masked).
func (h *TelegramHandler) GetConfig(c *gin.Context) {
	adminTok := telegram.GetSetting(h.S.DB, telegram.KeyAdminToken)
	userTok := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	ids := telegram.GetSetting(h.S.DB, telegram.KeyAdminIDs)
	roles := telegram.GetSetting(h.S.DB, telegram.KeyAdminRoles)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"adminBotTokenSet":  adminTok != "",
		"adminBotTokenHint": maskToken(adminTok),
		"adminIds":          ids,
		"adminRoles":        roles,
		"userBotTokenSet":   userTok != "",
		"userBotTokenHint":  maskToken(userTok),
	}})
}

type telegramConfigReq struct {
	AdminBotToken *string `json:"adminBotToken"`
	AdminIds      *string `json:"adminIds"`
	AdminRoles    *string `json:"adminRoles"`
	UserBotToken  *string `json:"userBotToken"`
}

// SaveConfig stores tokens and admin numeric IDs.
// Omit a token field to keep the previous value; send empty string to clear.
func (h *TelegramHandler) SaveConfig(c *gin.Context) {
	var req telegramConfigReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	// Resolve effective tokens after this save (validate before writing secrets).
	adminTok := telegram.GetSetting(h.S.DB, telegram.KeyAdminToken)
	userTok := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if req.AdminBotToken != nil {
		adminTok = strings.TrimSpace(*req.AdminBotToken)
	}
	if req.UserBotToken != nil {
		userTok = strings.TrimSpace(*req.UserBotToken)
	}
	if adminTok != "" && userTok != "" && adminTok == userTok {
		c.JSON(400, gin.H{"success": false, "msg": "توکن ربات ادمین و کاربر نباید یکسان باشد (Conflict در getUpdates)"})
		return
	}
	if req.AdminBotToken != nil && adminTok != "" {
		if _, err := telegram.NewClient(adminTok).GetMe(); err != nil {
			c.JSON(400, gin.H{"success": false, "msg": "توکن ادمین نامعتبر: " + err.Error()})
			return
		}
	}
	if req.UserBotToken != nil && userTok != "" {
		if _, err := telegram.NewClient(userTok).GetMe(); err != nil {
			c.JSON(400, gin.H{"success": false, "msg": "توکن کاربر نامعتبر: " + err.Error()})
			return
		}
	}
	if req.AdminBotToken != nil {
		if err := telegram.SetSetting(h.S.DB, telegram.KeyAdminToken, adminTok); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	if req.AdminIds != nil {
		if err := telegram.SetSetting(h.S.DB, telegram.KeyAdminIDs, strings.TrimSpace(*req.AdminIds)); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	if req.AdminRoles != nil {
		if err := telegram.SetSetting(h.S.DB, telegram.KeyAdminRoles, strings.TrimSpace(*req.AdminRoles)); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	if req.UserBotToken != nil {
		if err := telegram.SetSetting(h.S.DB, telegram.KeyUserToken, userTok); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	// Keep legacy notify.Telegram path in sync with admin bot credentials.
	notify.SetTelegramOverride(
		telegram.GetSetting(h.S.DB, telegram.KeyAdminToken),
		telegram.GetSetting(h.S.DB, telegram.KeyAdminIDs),
	)
	auditAction(h.S, c, "telegram.config.save", "telegram", nil)
	c.JSON(200, gin.H{"success": true})
}

// TestAdminBot sends a test message to configured admin IDs.
func (h *TelegramHandler) TestAdminBot(c *gin.Context) {
	token := telegram.GetSetting(h.S.DB, telegram.KeyAdminToken)
	ids := telegram.ParseAdminIDs(telegram.GetSetting(h.S.DB, telegram.KeyAdminIDs))
	if token == "" || len(ids) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "set admin bot token and at least one admin numeric ID"})
		return
	}
	cli := telegram.NewClient(token)
	uname, err := cli.GetMe()
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "token invalid: " + err.Error()})
		return
	}
	var lastErr error
	ok := 0
	for id := range ids {
		if err := cli.SendMessage(id, "✅ ElectruVPN Admin Bot connected as @"+uname); err != nil {
			lastErr = err
		} else {
			ok++
		}
	}
	if ok == 0 && lastErr != nil {
		c.JSON(400, gin.H{"success": false, "msg": "send failed: " + lastErr.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"username": uname, "delivered": ok}})
}

// TestUserBot validates the user bot token.
func (h *TelegramHandler) TestUserBot(c *gin.Context) {
	token := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if token == "" {
		c.JSON(400, gin.H{"success": false, "msg": "user bot token not set"})
		return
	}
	cli := telegram.NewClient(token)
	uname, err := cli.GetMe()
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "token invalid: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"username": uname}})
}

func maskToken(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if len(t) < 12 {
		return "***"
	}
	return t[:6] + "…" + t[len(t)-4:]
}
