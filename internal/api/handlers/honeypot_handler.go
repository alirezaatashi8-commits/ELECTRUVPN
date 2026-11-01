package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/honeypot"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

type HoneypotHandler struct {
	S *core.Server
}

func NewHoneypotHandler(s *core.Server) *HoneypotHandler {
	return &HoneypotHandler{S: s}
}

func (h *HoneypotHandler) Stats(c *gin.Context) {
	live := map[string]any{}
	if honeypot.Default != nil {
		live = honeypot.Default.Stats()
	}
	var recent []models.HoneypotProbe
	h.S.DB.Order("id desc").Limit(50).Find(&recent)
	var unique int64
	h.S.DB.Model(&models.HoneypotProbe{}).Distinct("remote_ip").Count(&unique)
	var bans []models.HoneypotBan
	h.S.DB.Order("id desc").Limit(100).Find(&bans)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"live": live, "recent": recent, "uniqueIPs": unique, "bans": bans,
		"settings": h.readSettings(),
	}})
}

func (h *HoneypotHandler) readSettings() gin.H {
	get := func(k, d string) string {
		v := telegram.GetSetting(h.S.DB, k)
		if v == "" {
			return d
		}
		return v
	}
	return gin.H{
		"enabled":       get(models.HoneypotEnabledKey, "1") != "0",
		"listen":        get(models.HoneypotListenKey, "127.0.0.1:19601"),
		"extraPorts":    get(models.HoneypotExtraPortsKey, ""),
		"maxConn":       get(models.HoneypotMaxConnKey, "256"),
		"perIP":         get(models.HoneypotPerIPKey, "6"),
		"minHoldSec":    get(models.HoneypotMinHoldKey, "45"),
		"maxHoldSec":    get(models.HoneypotMaxHoldKey, "180"),
		"aggression":    get(models.HoneypotAggressionKey, "8"),
		"autoInject":    get(models.HoneypotAutoInjectKey, "1") != "0",
		"autoBan":       get(models.HoneypotAutoBanKey, "1") != "0",
		"banThreshold":  get(models.HoneypotBanThresholdKey, "3"),
		"banWindowMin":  get(models.HoneypotBanWindowKey, "30"),
		"banDurationH":  get(models.HoneypotBanDurationKey, "24"),
		"telegramAlert": get(models.HoneypotTelegramAlertKey, "1") != "0",
		"fakeLogin":     get(models.HoneypotFakeLoginKey, "1") != "0",
		"globalListURL": get(models.HoneypotGlobalListKey, ""),
	}
}

func (h *HoneypotHandler) SaveSettings(c *gin.Context) {
	var req struct {
		Enabled       *bool  `json:"enabled"`
		Listen        string `json:"listen"`
		ExtraPorts    string `json:"extraPorts"`
		MaxConn       *int   `json:"maxConn"`
		PerIP         *int   `json:"perIP"`
		MinHoldSec    *int   `json:"minHoldSec"`
		MaxHoldSec    *int   `json:"maxHoldSec"`
		Aggression    *int   `json:"aggression"`
		AutoInject    *bool  `json:"autoInject"`
		AutoBan       *bool  `json:"autoBan"`
		BanThreshold  *int   `json:"banThreshold"`
		BanWindowMin  *int   `json:"banWindowMin"`
		BanDurationH  *int   `json:"banDurationH"`
		TelegramAlert *bool  `json:"telegramAlert"`
		FakeLogin     *bool  `json:"fakeLogin"`
		GlobalListURL string `json:"globalListURL"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	set := func(k, v string) { _ = telegram.SetSetting(h.S.DB, k, v) }
	if req.Enabled != nil {
		if *req.Enabled {
			set(models.HoneypotEnabledKey, "1")
		} else {
			set(models.HoneypotEnabledKey, "0")
		}
	}
	if req.Listen != "" {
		set(models.HoneypotListenKey, req.Listen)
	}
	if req.ExtraPorts != "" || req.ExtraPorts == "" {
		set(models.HoneypotExtraPortsKey, req.ExtraPorts)
	}
	if req.MaxConn != nil {
		set(models.HoneypotMaxConnKey, strconv.Itoa(*req.MaxConn))
	}
	if req.PerIP != nil {
		set(models.HoneypotPerIPKey, strconv.Itoa(*req.PerIP))
	}
	if req.MinHoldSec != nil {
		set(models.HoneypotMinHoldKey, strconv.Itoa(*req.MinHoldSec))
	}
	if req.MaxHoldSec != nil {
		set(models.HoneypotMaxHoldKey, strconv.Itoa(*req.MaxHoldSec))
	}
	if req.Aggression != nil {
		a := *req.Aggression
		if a < 1 {
			a = 1
		}
		if a > 10 {
			a = 10
		}
		set(models.HoneypotAggressionKey, strconv.Itoa(a))
	}
	if req.AutoInject != nil {
		if *req.AutoInject {
			set(models.HoneypotAutoInjectKey, "1")
		} else {
			set(models.HoneypotAutoInjectKey, "0")
		}
	}
	if req.AutoBan != nil {
		if *req.AutoBan {
			set(models.HoneypotAutoBanKey, "1")
		} else {
			set(models.HoneypotAutoBanKey, "0")
		}
	}
	if req.BanThreshold != nil {
		set(models.HoneypotBanThresholdKey, strconv.Itoa(*req.BanThreshold))
	}
	if req.BanWindowMin != nil {
		set(models.HoneypotBanWindowKey, strconv.Itoa(*req.BanWindowMin))
	}
	if req.BanDurationH != nil {
		set(models.HoneypotBanDurationKey, strconv.Itoa(*req.BanDurationH))
	}
	if req.TelegramAlert != nil {
		if *req.TelegramAlert {
			set(models.HoneypotTelegramAlertKey, "1")
		} else {
			set(models.HoneypotTelegramAlertKey, "0")
		}
	}
	if req.FakeLogin != nil {
		if *req.FakeLogin {
			set(models.HoneypotFakeLoginKey, "1")
		} else {
			set(models.HoneypotFakeLoginKey, "0")
		}
	}
	set(models.HoneypotGlobalListKey, req.GlobalListURL)

	auditAction(h.S, c, "honeypot.settings", "save", h.readSettings())
	_ = h.S.RegenerateAndRestartXray()
	c.JSON(200, gin.H{"success": true, "obj": h.readSettings()})
}

// Unban removes an IP from the honeypot ban list and multi-layer system bans.
func (h *HoneypotHandler) Unban(c *gin.Context) {
	ip := strings.TrimSpace(c.Param("ip"))
	if ip == "" {
		c.JSON(400, gin.H{"success": false, "msg": "ip required"})
		return
	}
	h.S.DB.Where("ip = ?", ip).Delete(&models.HoneypotBan{})
	if honeypot.Default != nil {
		honeypot.Default.UnbanIP(ip)
	}
	auditAction(h.S, c, "honeypot.unban", ip, nil)
	c.JSON(200, gin.H{"success": true, "ip": ip})
}

// DailyReport returns (and optionally telegrams) the 24h honeypot summary.
func (h *HoneypotHandler) DailyReport(c *gin.Context) {
	send := c.Query("telegram") == "1" || c.Query("telegram") == "true"
	var rep map[string]any
	if honeypot.Default != nil {
		rep = honeypot.Default.DailyReport(send)
	} else {
		rep = map[string]any{"probes24h": 0, "uniqueIPs24h": 0, "bans24h": 0, "activeBans": 0, "topIPs": []any{}}
	}
	c.JSON(200, gin.H{"success": true, "obj": rep, "telegram": send})
}

// ManualBan adds an IP to the ban list and applies multi-layer system ban.
func (h *HoneypotHandler) ManualBan(c *gin.Context) {
	var req struct {
		IP     string `json:"ip"`
		Hours  int    `json:"hours"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.IP) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "ip required"})
		return
	}
	ip := strings.TrimSpace(req.IP)
	if req.Hours <= 0 {
		req.Hours = 24
	}
	if req.Reason == "" {
		req.Reason = "manual"
	}
	var exp *time.Time
	if req.Hours > 0 {
		t := time.Now().Add(time.Duration(req.Hours) * time.Hour)
		exp = &t
	}
	ban := models.HoneypotBan{IP: ip, Reason: req.Reason, Source: "manual", ExpiresAt: exp, CreatedAt: time.Now(), ProbeCount: 0}
	_ = h.S.DB.Where("ip = ?", ip).Assign(ban).FirstOrCreate(&ban).Error
	if honeypot.Default != nil {
		honeypot.Default.BanIP(ip, req.Hours)
	}
	auditAction(h.S, c, "honeypot.manual_ban", ip, gin.H{"hours": req.Hours, "reason": req.Reason})
	c.JSON(200, gin.H{"success": true, "ip": ip})
}
