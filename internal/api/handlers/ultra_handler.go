package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/crypto"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/privacy"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UltraHandler struct{ S *core.Server }

// NewUltraHandler creates or exposes the NewUltraHandler handler.
func NewUltraHandler(s *core.Server) *UltraHandler { return &UltraHandler{S: s} }

// RevokeSubscription handles the RevokeSubscription API operation.
func (h *UltraHandler) RevokeSubscription(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	now := time.Now()
	newToken := strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := h.S.DB.Model(&cl).Updates(map[string]any{"sub_id": newToken, "subscription_enabled": false, "subscription_status": "revoked", "subscription_revoked_at": &now, "subscription_token_version": gorm.Expr("subscription_token_version + 1")}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription.revoked", fmt.Sprint(cl.ID), gin.H{"clientId": cl.ID})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "status": "revoked"}})
}

// EnableSubscription handles the EnableSubscription API operation.
func (h *UltraHandler) EnableSubscription(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if cl.IsExpired() || cl.IsOverQuota() {
		c.JSON(409, gin.H{"success": false, "msg": "client is expired or over quota"})
		return
	}
	if err := h.S.DB.Model(&cl).Updates(map[string]any{"subscription_enabled": true, "subscription_status": "active", "subscription_revoked_at": nil}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription.enabled", fmt.Sprint(cl.ID), gin.H{"clientId": cl.ID})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "status": "active"}})
}

// RotateSubscriptionGroup handles the RotateSubscriptionGroup API operation.
func (h *UltraHandler) RotateSubscriptionGroup(c *gin.Context) {
	var g models.SubscriptionGroup
	if err := h.S.DB.First(&g, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "group not found"})
		return
	}
	g.Token = strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := h.S.DB.Model(&g).Update("token", g.Token).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription_group.rotate", fmt.Sprint(g.ID), nil)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": g.ID, "token": g.Token, "url": subscriptionGroupURLForAddress(subscriptionBaseForFamily(h.S.Config, g.AddressType), c, h.S.Config.PanelBasePath, g.Token, g.AddressType)}})
}

// ToggleSubscriptionGroup handles the ToggleSubscriptionGroup API operation.
func (h *UltraHandler) ToggleSubscriptionGroup(c *gin.Context) {
	var g models.SubscriptionGroup
	if err := h.S.DB.First(&g, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "group not found"})
		return
	}
	g.Enabled = !g.Enabled
	if err := h.S.DB.Model(&g).Update("enabled", g.Enabled).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription_group.toggle", fmt.Sprint(g.ID), gin.H{"enabled": g.Enabled})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": g.ID, "enabled": g.Enabled}})
}

// SubscriptionPresets handles the SubscriptionPresets API operation.
func (h *UltraHandler) SubscriptionPresets(c *gin.Context) {
	var rows []models.SubscriptionPreset
	if err := h.S.DB.Where("enabled = ?", true).Order("name asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateSubscriptionPreset handles the CreateSubscriptionPreset API operation.
func (h *UltraHandler) CreateSubscriptionPreset(c *gin.Context) {
	var r struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		AddressType  string `json:"addressType"`
		RefreshMin   int    `json:"refreshMin"`
		AutoUpdate   *bool  `json:"autoUpdate"`
		SmartEnabled *bool  `json:"smartEnabled"`
		MobileGuide  *bool  `json:"mobileGuide"`
		QR           *bool  `json:"qr"`
		Enabled      *bool  `json:"enabled"`
		ConfigJSON   string `json:"configJson"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid request"})
		return
	}
	r.Name = strings.TrimSpace(r.Name)
	r.AddressType = normalizeSubscriptionAddress(r.AddressType)
	if r.Name == "" || len(r.Name) > 64 {
		c.JSON(400, gin.H{"success": false, "msg": "name required"})
		return
	}
	if r.RefreshMin < 1 {
		r.RefreshMin = 15
	}
	if r.RefreshMin > 1440 {
		r.RefreshMin = 1440
	}
	v := func(p *bool, d bool) bool {
		if p == nil {
			return d
		}
		return *p
	}
	row := models.SubscriptionPreset{Name: r.Name, Description: strings.TrimSpace(r.Description), AddressType: r.AddressType, RefreshMin: r.RefreshMin, AutoUpdate: v(r.AutoUpdate, true), SmartEnabled: v(r.SmartEnabled, false), MobileGuide: v(r.MobileGuide, true), QR: v(r.QR, true), Enabled: v(r.Enabled, true), ConfigJSON: r.ConfigJSON}
	if len(row.ConfigJSON) > 65536 {
		c.JSON(400, gin.H{"success": false, "msg": "configJson too large"})
		return
	}
	if err := h.S.DB.Create(&row).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription_preset.create", fmt.Sprint(row.ID), row.Name)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// DeleteSubscriptionPreset handles the DeleteSubscriptionPreset API operation.
func (h *UltraHandler) DeleteSubscriptionPreset(c *gin.Context) {
	if err := h.S.DB.Delete(&models.SubscriptionPreset{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription_preset.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// ApplySubscriptionPreset handles the ApplySubscriptionPreset API operation.
func (h *UltraHandler) ApplySubscriptionPreset(c *gin.Context) {
	var r struct {
		PresetID uint `json:"presetId"`
	}
	if c.ShouldBindJSON(&r) != nil || r.PresetID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "presetId required"})
		return
	}
	var p models.SubscriptionPreset
	var cl models.Client
	if h.S.DB.First(&p, r.PresetID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "preset not found"})
		return
	}
	if h.S.DB.First(&cl, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	updates := map[string]any{"subscription_address_type": p.AddressType, "subscription_refresh_min": p.RefreshMin, "subscription_auto_update": p.AutoUpdate, "subscription_smart": p.SmartEnabled}
	if err := h.S.DB.Model(&cl).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "subscription_preset.apply", fmt.Sprint(cl.ID), p.Name)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "preset": p}})
}

// SubscriptionAccessLog handles the SubscriptionAccessLog API operation.
func (h *UltraHandler) SubscriptionAccessLog(c *gin.Context) {
	limit := 100
	if v, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && v > 0 {
		if v > 500 {
			v = 500
		}
		limit = v
	}
	q := h.S.DB.Order("created_at desc").Limit(limit)
	if id := strings.TrimSpace(c.Query("clientId")); id != "" {
		q = q.Where("client_id = ?", id)
	}
	if typ := strings.TrimSpace(c.Query("addressType")); typ != "" {
		q = q.Where("address_type = ?", typ)
	}
	var rows []models.SubscriptionAccessLog
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// SubscriptionAnalyticsOverview handles the SubscriptionAnalyticsOverview API operation.
func (h *UltraHandler) SubscriptionAnalyticsOverview(c *gin.Context) {
	var active, revoked, expired, total int64
	h.S.DB.Model(&models.Client{}).Where("subscription_enabled = ? AND subscription_status = ?", true, "active").Count(&active)
	h.S.DB.Model(&models.Client{}).Where("subscription_status = ?", "revoked").Count(&revoked)
	h.S.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ? AND subscription_enabled = ?", time.Now().UnixMilli(), true).Count(&expired)
	h.S.DB.Model(&models.Client{}).Where("subscription_enabled = ?", true).Count(&total)
	var hits int64
	h.S.DB.Model(&models.SubscriptionAccessLog{}).Where("created_at >= ?", time.Now().Add(-24*time.Hour)).Count(&hits)
	var byType []struct {
		AddressType string
		Count       int64
	}
	h.S.DB.Model(&models.SubscriptionAccessLog{}).Select("address_type, count(*) as count").Where("created_at >= ?", time.Now().Add(-24*time.Hour)).Group("address_type").Scan(&byType)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"active": active, "revoked": revoked, "expired": expired, "enabled": total, "updates24h": hits, "byAddressType24h": byType}})
}

func hashUserAgent(v, secret string) string {
	v = strings.TrimSpace(v)
	if v == "" || secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil))
}

func subscriptionAccessLog(s *core.Server, c *gin.Context, clientID, groupID uint, addressType string, status, links int) {
	now := time.Now()
	_ = s.DB.Create(&models.SubscriptionAccessLog{ClientID: clientID, GroupID: groupID, AddressType: addressType, UserAgentHash: hashUserAgent(c.GetHeader("User-Agent"), s.Config.JWTSecret), IPHash: privacy.IdentifierHash(privacy.NormalizeIP(c.ClientIP()), s.Config.JWTSecret), Status: status, Links: links, CreatedAt: now}).Error
	// Keep the history bounded without running a cleanup job for every request.
	if now.Unix()%3600 == 0 {
		_ = s.DB.Where("created_at < ?", now.Add(-90*24*time.Hour)).Delete(&models.SubscriptionAccessLog{}).Error
	}
}

// Devices handles the Devices API operation.
func (h *UltraHandler) Devices(c *gin.Context) {
	var rows []models.ClientDevice
	q := h.S.DB.Order("last_seen_at desc")
	if id := c.Query("clientId"); id != "" {
		q = q.Where("client_id = ?", id)
	}
	if err := q.Limit(500).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// DeleteDevice handles the DeleteDevice API operation.
func (h *UltraHandler) DeleteDevice(c *gin.Context) {
	now := time.Now()
	if err := h.S.DB.Model(&models.ClientDevice{}).Where("id = ?", c.Param("id")).Updates(map[string]any{"active": false, "revoked_at": now}).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "device.revoke", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// ResetDevices handles the ResetDevices API operation.
func (h *UltraHandler) ResetDevices(c *gin.Context) {
	id := c.Param("id")
	now := time.Now()
	if err := h.S.DB.Model(&models.ClientDevice{}).Where("client_id = ? AND active = ?", id, true).Updates(map[string]any{"active": false, "revoked_at": now}).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "device.reset", id, nil)
	c.JSON(200, gin.H{"success": true})
}

// Plans handles the Plans API operation.
func (h *UltraHandler) Plans(c *gin.Context) {
	var rows []models.ServicePlan
	if err := h.S.DB.Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

type planReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TotalGB     int64  `json:"totalGB"`
	LimitIP     int    `json:"limitIp"`
	DurationDay int    `json:"durationDay"`
	Enabled     *bool  `json:"enabled"`
}

// CreatePlan handles the CreatePlan API operation.
func (h *UltraHandler) CreatePlan(c *gin.Context) {
	var r planReq
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || r.TotalGB < 0 || r.LimitIP < 0 || r.DurationDay < 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid plan"})
		return
	}
	en := true
	if r.Enabled != nil {
		en = *r.Enabled
	}
	p := models.ServicePlan{Name: strings.TrimSpace(r.Name), Description: r.Description, TotalGB: r.TotalGB, LimitIP: r.LimitIP, DurationDay: r.DurationDay, Enabled: en}
	if err := h.S.DB.Create(&p).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "plan.create", fmt.Sprint(p.ID), p.Name)
	c.JSON(200, gin.H{"success": true, "obj": p})
}

// DeletePlan handles the DeletePlan API operation.
func (h *UltraHandler) DeletePlan(c *gin.Context) {
	if err := h.S.DB.Delete(&models.ServicePlan{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "plan.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// ApplyPlan handles the ApplyPlan API operation.
func (h *UltraHandler) ApplyPlan(c *gin.Context) {
	var req struct {
		PlanID uint `json:"planId"`
	}
	if c.ShouldBindJSON(&req) != nil || req.PlanID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "planId required"})
		return
	}
	var p models.ServicePlan
	if err := h.S.DB.First(&p, req.PlanID).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	original := cl
	updates := map[string]any{"plan_id": p.ID, "total_gb": p.TotalGB, "limit_ip": p.LimitIP}
	if p.DurationDay > 0 {
		updates["expiry_time"] = time.Now().Add(time.Duration(p.DurationDay) * 24 * time.Hour).UnixMilli()
	}
	if err := h.S.DB.Model(&cl).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Save(&original).Error
		rollbackErr := h.S.RegenerateAndRestartXray()
		msg := "plan application was rolled back because runtime apply failed: " + err.Error()
		if rollbackErr != nil {
			msg += "; previous runtime restore also failed: " + rollbackErr.Error()
		}
		c.JSON(500, gin.H{"success": false, "msg": msg})
		return
	}
	auditAction(h.S, c, "client.plan", fmt.Sprint(cl.ID), p.Name)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"plan": p, "clientId": cl.ID}})
}

// Snapshots handles the Snapshots API operation.
func (h *UltraHandler) Snapshots(c *gin.Context) {
	var rows []models.UsageSnapshot
	// Newest 2000 rows (asc+limit used to return the OLDEST 2000 and the
	// chart froze once the table grew), then flip back to oldest->newest.
	q := h.S.DB.Order("created_at desc").Limit(2000)
	if id := c.Query("clientId"); id != "" {
		q = q.Where("client_id = ?", id)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// UpdateSubscriptionPolicy handles the UpdateSubscriptionPolicy API operation.
func (h *UltraHandler) UpdateSubscriptionPolicy(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var r struct {
		GracePeriodHours     *int    `json:"gracePeriodHours"`
		MaxActiveSessions    *int    `json:"maxActiveSessions"`
		DeviceBindingEnabled *bool   `json:"deviceBindingEnabled"`
		NewDeviceApproval    *bool   `json:"newDeviceApproval"`
		LinkLeakThreshold    *int    `json:"linkLeakThreshold"`
		PrimaryAddressType   *string `json:"primaryAddressType"`
		RenewalAuto          *bool   `json:"renewalAuto"`
		RenewalDays          *int    `json:"renewalDays"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid request"})
		return
	}
	// Fields that are not sent keep their stored value (they used to be reset to
	// 0 / "ipv4", silently wiping grace period, session cap and leak threshold
	// whenever the UI saved only one of them).
	bad := func(p *int, max int) bool { return p != nil && (*p < 0 || *p > max) }
	if bad(r.GracePeriodHours, 720) || bad(r.MaxActiveSessions, 10000) || bad(r.LinkLeakThreshold, 1000000) || bad(r.RenewalDays, 3650) {
		c.JSON(400, gin.H{"success": false, "msg": "policy value out of range"})
		return
	}
	updates := map[string]any{}
	if r.GracePeriodHours != nil {
		updates["grace_period_hours"] = *r.GracePeriodHours
	}
	if r.MaxActiveSessions != nil {
		updates["max_active_sessions"] = *r.MaxActiveSessions
	}
	if r.LinkLeakThreshold != nil {
		updates["link_leak_threshold"] = *r.LinkLeakThreshold
	}
	if r.RenewalDays != nil {
		updates["renewal_days"] = *r.RenewalDays
	}
	addr := cl.PrimaryAddressType
	if r.PrimaryAddressType != nil {
		addr = strings.ToLower(strings.TrimSpace(*r.PrimaryAddressType))
		if addr != "ipv4" && addr != "ipv6" && addr != "domain" && addr != "dual" && addr != "all" {
			c.JSON(400, gin.H{"success": false, "msg": "primaryAddressType must be ipv4, ipv6, domain, dual or all"})
			return
		}
		var ib models.Inbound
		if err := h.S.DB.First(&ib, cl.InboundID).Error; err != nil {
			c.JSON(404, gin.H{"success": false, "msg": "client inbound not found"})
			return
		}
		if err := validateInboundFamily(ib, addr); err != nil {
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		updates["primary_address_type"] = addr
	}
	if r.DeviceBindingEnabled != nil {
		updates["device_binding_enabled"] = *r.DeviceBindingEnabled
	}
	if r.NewDeviceApproval != nil {
		updates["new_device_approval"] = *r.NewDeviceApproval
	}
	if r.RenewalAuto != nil {
		updates["renewal_auto"] = *r.RenewalAuto
	}
	if len(updates) == 0 {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "unchanged": true}})
		return
	}
	if err := h.S.DB.Model(&cl).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "subscription.policy.update", fmt.Sprint(cl.ID), updates)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "gracePeriodHours": cl.GracePeriodHours, "maxActiveSessions": cl.MaxActiveSessions, "deviceBindingEnabled": cl.DeviceBindingEnabled, "newDeviceApproval": cl.NewDeviceApproval, "linkLeakThreshold": cl.LinkLeakThreshold, "primaryAddressType": cl.PrimaryAddressType, "renewalAuto": cl.RenewalAuto, "renewalDays": cl.RenewalDays}})
}

// RenewSubscription handles the RenewSubscription API operation.
func (h *UltraHandler) RenewSubscription(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var r struct {
		Days int `json:"days"`
	}
	_ = c.ShouldBindJSON(&r)
	if r.Days <= 0 {
		r.Days = cl.RenewalDays
		if r.Days <= 0 {
			r.Days = 30
		}
	}
	if r.Days > 3650 {
		c.JSON(400, gin.H{"success": false, "msg": "days too large"})
		return
	}
	now := time.Now()
	base := now
	if cl.ExpiryTime > now.UnixMilli() {
		base = time.UnixMilli(cl.ExpiryTime)
	}
	newExpiry := base.Add(time.Duration(r.Days) * 24 * time.Hour)
	original := cl
	renewUpdates := map[string]any{"expiry_time": newExpiry.UnixMilli(), "renewal_grace_used": false, "subscription_enabled": true, "subscription_status": "active", "subscription_revoked_at": nil}
	// A client that was switched off because its time ran out has to be switched
	// back on and re-added to xray, otherwise "renew" only changed a date and the
	// customer stayed disconnected.
	if !cl.Enable && strings.EqualFold(cl.SubscriptionStatus, "expired") {
		probe := cl
		probe.ExpiryTime = newExpiry.UnixMilli()
		if !probe.IsOverQuota() {
			renewUpdates["enable"] = true
		}
	}
	if err := h.S.DB.Model(&cl).Updates(renewUpdates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Save(&original).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "renewal was rolled back because runtime apply failed: " + err.Error()})
		return
	}
	auditAction(h.S, c, "subscription.renew", fmt.Sprint(cl.ID), gin.H{"days": r.Days, "expiry": newExpiry})
	dispatchWebhookEvent(h.S, "subscription.renewed", gin.H{"clientId": cl.ID, "days": r.Days, "expiry": newExpiry.UTC()})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "expiryTime": newExpiry.UnixMilli(), "days": r.Days}})
}

// ApproveDevice handles the ApproveDevice API operation.
func (h *UltraHandler) ApproveDevice(c *gin.Context) {
	var d models.ClientDevice
	if err := h.S.DB.First(&d, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "device not found"})
		return
	}
	if err := h.S.DB.Model(&d).Updates(map[string]any{"active": true, "revoked_at": nil}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	d.Active, d.RevokedAt = true, nil
	auditAction(h.S, c, "subscription.device.approve", fmt.Sprint(d.ID), gin.H{"clientId": d.ClientID})
	c.JSON(200, gin.H{"success": true, "obj": d})
}

// RevokeDevice handles the RevokeDevice API operation.
func (h *UltraHandler) RevokeDevice(c *gin.Context) {
	var d models.ClientDevice
	if err := h.S.DB.First(&d, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "device not found"})
		return
	}
	now := time.Now()
	if err := h.S.DB.Model(&d).Updates(map[string]any{"active": false, "revoked_at": &now}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	d.Active, d.RevokedAt = false, &now
	auditAction(h.S, c, "subscription.device.revoke", fmt.Sprint(d.ID), gin.H{"clientId": d.ClientID})
	c.JSON(200, gin.H{"success": true, "obj": d})
}

// RotateSubscription handles the RotateSubscription API operation.
func (h *UltraHandler) RotateSubscription(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	cl.SubID = strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := h.S.DB.Model(&cl).Updates(map[string]any{"sub_id": cl.SubID, "subscription_status": "active", "subscription_enabled": true, "subscription_revoked_at": nil, "subscription_token_version": gorm.Expr("subscription_token_version + 1")}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "subscription.rotate", fmt.Sprint(cl.ID), nil)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"clientId": cl.ID, "subId": cl.SubID}})
}

// SubscriptionGroups handles the SubscriptionGroups API operation.
func (h *UltraHandler) SubscriptionGroups(c *gin.Context) {
	var rows []models.SubscriptionGroup
	if err := h.S.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, g := range rows {
		items = append(items, gin.H{"id": g.ID, "name": g.Name, "token": g.Token, "enabled": g.Enabled, "addressType": g.AddressType, "refreshMin": g.RefreshMin, "preset": g.Preset, "smartEnabled": g.SmartEnabled, "healthFilter": g.HealthFilter, "nodeGroupId": g.NodeGroupID, "maxLinks": g.MaxLinks, "createdAt": g.CreatedAt, "updatedAt": g.UpdatedAt, "url": subscriptionGroupURLForAddress(subscriptionBaseForFamily(h.S.Config, g.AddressType), c, h.S.Config.PanelBasePath, g.Token, g.AddressType)})
	}
	c.JSON(200, gin.H{"success": true, "obj": items})
}

// CreateSubscriptionGroup handles the CreateSubscriptionGroup API operation.
func (h *UltraHandler) CreateSubscriptionGroup(c *gin.Context) {
	var req struct {
		Name         string `json:"name"`
		AddressType  string `json:"addressType"`
		RefreshMin   int    `json:"refreshMin"`
		Preset       string `json:"preset"`
		SmartEnabled *bool  `json:"smartEnabled"`
		HealthFilter string `json:"healthFilter"`
		NodeGroupID  uint   `json:"nodeGroupId"`
		MaxLinks     int    `json:"maxLinks"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid request"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		c.JSON(400, gin.H{"success": false, "msg": "group name required"})
		return
	}
	req.AddressType = normalizeSubscriptionAddress(req.AddressType)
	if req.RefreshMin < 1 {
		req.RefreshMin = 15
	}
	if req.RefreshMin > 1440 {
		req.RefreshMin = 1440
	}
	if strings.TrimSpace(req.Preset) == "" {
		req.Preset = "standard"
	}
	if req.HealthFilter == "" {
		req.HealthFilter = "healthy"
	}
	if req.MaxLinks < 0 {
		req.MaxLinks = 0
	}
	smart := false
	if req.SmartEnabled != nil {
		smart = *req.SmartEnabled
	}
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	g := models.SubscriptionGroup{Name: req.Name, Token: strings.ReplaceAll(uuid.NewString(), "-", ""), Enabled: en, AddressType: req.AddressType, RefreshMin: req.RefreshMin, Preset: req.Preset, SmartEnabled: smart, HealthFilter: req.HealthFilter, NodeGroupID: req.NodeGroupID, MaxLinks: req.MaxLinks}
	if err := h.S.DB.Create(&g).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "subscription_group.create", fmt.Sprint(g.ID), g.Name)
	c.JSON(200, gin.H{"success": true, "obj": g})
}

// DeleteSubscriptionGroup handles the DeleteSubscriptionGroup API operation.
func (h *UltraHandler) DeleteSubscriptionGroup(c *gin.Context) {
	if err := h.S.DB.Delete(&models.SubscriptionGroup{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "subscription_group.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// ServeSubscriptionGroup exposes only the explicitly grouped client links.
// It never discovers arbitrary clients from a user supplied identifier.
func subscriptionGroupURLForAddress(base string, c *gin.Context, basePath, token, addressType string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = requestPanelBase(c, basePath)
	} else {
		base = strings.TrimRight(base, "/")
	}
	u := base + "/sub-group/" + url.PathEscape(token)
	if addressType != "" && addressType != "ipv4" {
		u += "?family=" + url.QueryEscape(addressType)
	}
	return u
}

// ServeSubscriptionGroup handles the ServeSubscriptionGroup API operation.
func (h *UltraHandler) ServeSubscriptionGroup(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	var g models.SubscriptionGroup
	if err := h.S.DB.Where("token = ? AND enabled = ?", token, true).First(&g).Error; err != nil {
		c.String(404, "")
		return
	}
	var clients []models.Client
	if err := h.S.DB.Where("subscription_group = ? AND enable = ? AND subscription_enabled = ?", g.Name, true, true).Find(&clients).Error; err != nil {
		c.String(500, "")
		return
	}

	// Intelligent multi-node selection: healthy-first + strategy (latency/load/weighted).
	strategy := "health"
	failover := true
	if g.NodeGroupID > 0 {
		var ng models.NodeGroup
		if h.S.DB.First(&ng, g.NodeGroupID).Error == nil {
			if ng.Strategy != "" {
				strategy = ng.Strategy
			}
			failover = ng.Failover
		}
	}
	candidates, _ := routing.SelectNodes(h.S.DB, routing.SelectOptions{
		GroupID: g.NodeGroupID, Strategy: strategy, Failover: failover,
		HealthFilter: g.HealthFilter, MaxNodes: 16,
	})

	links := []string{}
	for _, cl := range clients {
		if g.MaxLinks > 0 && len(links) >= g.MaxLinks {
			break
		}
		if subscriptionExpiredWithGrace(cl) || cl.IsOverQuota() || !cl.Enable || !cl.SubscriptionEnabled || !strings.EqualFold(cl.SubscriptionStatus, "active") {
			continue
		}
		if !subscriptionAccessAllowed((&SubscriptionHandler{S: h.S}), c, cl) {
			continue
		}
		var ib models.Inbound
		if h.S.DB.First(&ib, cl.InboundID).Error != nil || !ib.SubscriptionEnabled {
			continue
		}
		if g.SmartEnabled && (ib.Maintenance || !ib.Enable || ib.IsLimited()) {
			continue
		}
		if err := validateInboundFamily(ib, g.AddressType); err != nil {
			continue
		}

		// Hosts: prefer ranked node addresses (failover/LB), then panel public hosts.
		hostList := make([]string, 0, 8)
		seenHost := map[string]bool{}
		addHost := func(host string) {
			host = strings.TrimSpace(host)
			if host == "" || seenHost[host] {
				return
			}
			seenHost[host] = true
			hostList = append(hostList, host)
		}
		for _, cand := range candidates {
			if h := routing.HostForCandidate(cand); h != "" {
				addHost(h)
			}
		}
		for _, family := range requestedAddressTypes(g.AddressType) {
			addHost(publicHostForFamily(h.S.Config, family))
		}
		if len(hostList) == 0 {
			continue
		}

		for _, host := range hostList {
			if g.MaxLinks > 0 && len(links) >= g.MaxLinks {
				break
			}
			// Family validation is soft when host comes from a node address.
			link, err := xray.BuildShareLink(ib, cl, host)
			if err == nil && link != "" {
				links = append(links, link)
			}
		}
	}
	_ = h.S.DB.Model(&models.Client{}).Where("subscription_group = ? AND subscription_enabled = ?", g.Name, true).Updates(map[string]any{"subscription_hits": gorm.Expr("subscription_hits + 1"), "subscription_last_seen_at": time.Now()}).Error
	subscriptionAccessLog(h.S, c, 0, g.ID, g.AddressType, 200, len(links))
	dispatchWebhookEvent(h.S, "subscription.group.refresh", gin.H{"groupId": g.ID, "group": g.Name, "addressType": g.AddressType, "links": len(links), "strategy": strategy, "candidates": len(candidates), "time": time.Now().UTC()})

	format := strings.ToLower(c.DefaultQuery("format", "base64"))
	body := xray.BuildSubscriptionBody(links)
	if format == "encrypted" || format == "enc" {
		password := strings.TrimSpace(c.Query("password"))
		if len(password) < 8 {
			c.String(400, "encrypted subscription requires ?password= (min 8 chars)")
			return
		}
		env, err := crypto.EncryptWithPassword(password, []byte(body), g.Name)
		if err != nil {
			c.String(500, err.Error())
			return
		}
		c.Header("X-Electru-Config-Alg", "argon2id-aes-256-gcm")
		c.Data(200, "application/json; charset=utf-8", env)
		return
	}
	if format == "json" {
		c.JSON(200, gin.H{"success": true, "group": g.Name, "addressType": g.AddressType, "count": len(links), "strategy": strategy, "healthyNodes": countHealthy(candidates), "subscription": subscriptionURLForFamily(h.S.Config.SubscriptionBaseURL, c, h.S.Config.PanelBasePath, g.Token, g.AddressType)})
		return
	}
	// Same headers apps get from a single-client subscription: refresh interval
	// (hours), a title, and no caching.
	if g.RefreshMin > 0 {
		hours := (g.RefreshMin + 59) / 60
		c.Header("profile-update-interval", strconv.Itoa(hours))
	}
	if t := strings.TrimSpace(g.Name); t != "" {
		c.Header("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte(t)))
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(200, body)
}

func countHealthy(cands []routing.Candidate) int {
	n := 0
	for _, c := range cands {
		if c.Healthy {
			n++
		}
	}
	return n
}

// SubscriptionAnalytics handles the SubscriptionAnalytics API operation.
func (h *UltraHandler) SubscriptionAnalytics(c *gin.Context) {
	id := strings.TrimSpace(c.Query("clientId"))
	if id == "" {
		c.JSON(400, gin.H{"success": false, "msg": "clientId required"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var deviceCount int64
	_ = h.S.DB.Model(&models.ClientDevice{}).Where("client_id = ?", cl.ID).Count(&deviceCount).Error
	var ib models.Inbound
	_ = h.S.DB.First(&ib, cl.InboundID).Error
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"clientId": cl.ID, "email": cl.Email, "enabled": cl.SubscriptionEnabled && cl.Enable,
		"hits": cl.SubscriptionHits, "lastSeenAt": cl.SubscriptionLastSeenAt,
		"refreshMin": cl.SubscriptionRefreshMin, "name": cl.SubscriptionName, "group": cl.SubscriptionGroup,
		"expired": cl.IsExpired(), "overQuota": cl.IsOverQuota(), "devices": deviceCount,
		"inbound": gin.H{"id": ib.ID, "remark": ib.Remark, "ipMode": ib.IPMode, "subscriptionEnabled": ib.SubscriptionEnabled},
	}})
}

// SubscriptionClinic handles the SubscriptionClinic API operation.
func (h *UltraHandler) SubscriptionClinic(c *gin.Context) {
	id := strings.TrimSpace(c.Query("clientId"))
	if id == "" {
		c.JSON(400, gin.H{"success": false, "msg": "clientId required"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, cl.InboundID).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	checks := []gin.H{}
	addHostCheck := func(kind string) {
		host := publicHostForFamily(h.S.Config, kind)
		ok := host != ""
		if ok {
			if err := validateHostFamily(host, kind); err != nil {
				ok = false
			}
		}
		item := gin.H{"name": kind + " public address", "ok": ok, "value": host}
		if !ok {
			item["reason"] = "public address is missing or has the wrong address type"
		}
		if kind == "domain" && ok {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			defer cancel()
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				item["ok"] = false
				item["reason"] = "domain does not currently resolve"
			} else {
				item["resolvedIPs"] = ips
			}
		}
		checks = append(checks, item)
	}
	addHostCheck("ipv4")
	addHostCheck("ipv6")
	addHostCheck("domain")
	if ib.SubscriptionEnabled && cl.SubscriptionEnabled {
		checks = append(checks, gin.H{"name": "subscription enabled", "ok": true})
	} else {
		checks = append(checks, gin.H{"name": "subscription enabled", "ok": false, "reason": "subscription is disabled on the inbound or client"})
	}
	for _, addressType := range []string{"ipv4", "ipv6", "domain", "dual", "all"} {
		if err := validateInboundFamily(ib, addressType); err != nil {
			continue
		}
		links, err := (&ClientHandler{S: h.S}).buildClientLinks(c, cl, ib, addressType)
		ok := err == nil && len(links) > 0
		item := gin.H{"name": "generated " + addressType + " config", "ok": ok, "addressType": addressType}
		if err != nil {
			item["reason"] = err.Error()
		}
		checks = append(checks, item)
	}
	subBase := h.S.Config.SubscriptionBaseURL
	if subBase == "" {
		subBase = requestBaseURL(c, h.S.Config.PanelBasePath)
	}
	if u, err := url.Parse(subBase); err != nil || u.Scheme == "" || u.Host == "" {
		checks = append(checks, gin.H{"name": "subscription endpoint", "ok": false, "reason": "invalid subscription base URL"})
	} else {
		checks = append(checks, gin.H{"name": "subscription endpoint", "ok": true, "value": u.Scheme + "://" + u.Host})
	}
	okCount := 0
	for _, x := range checks {
		if x["ok"] == true {
			okCount++
		}
	}
	status := "healthy"
	if okCount != len(checks) {
		status = "warning"
	}
	if okCount == 0 {
		status = "failed"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"status": status, "checks": checks, "passed": okCount, "total": len(checks)}})
}

func requestBaseURL(c *gin.Context, basePath string) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	return scheme + "://" + host + strings.TrimRight("/"+strings.Trim(basePath, "/"), "/")
}

// Export returns a normalized, client-friendly configuration bundle. The
// panel keeps the canonical share link and also emits a small JSON envelope
// useful for automation and QR/client portals.
func (h *UltraHandler) Export(c *gin.Context) {
	var cl models.Client
	if err := h.S.DB.First(&cl, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, cl.InboundID).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	host := c.Query("host")
	if host == "" {
		host = h.S.Config.PublicHost
	}
	if host == "" {
		host = c.Request.Host
		if i := strings.LastIndex(host, ":"); i > 0 {
			host = host[:i]
		}
	}
	link, err := xray.BuildShareLink(ib, cl, host)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	format := strings.ToLower(c.DefaultQuery("format", "json"))
	payload := map[string]any{
		"version": 2, "clientId": cl.ID, "email": cl.Email,
		"protocol": ib.Protocol, "network": ib.Network, "security": ib.Security,
		"host": host, "port": ib.Port, "link": link,
		"subscription": fmt.Sprintf("%s/sub/%s", strings.TrimRight(requestBaseURL(c, h.S.Config.PanelBasePath), "/"), cl.SubID),
	}
	raw, _ := json.Marshal(payload)

	// Password-encrypted envelope (Argon2id + AES-256-GCM). Password via query or JSON body.
	if format == "encrypted" || format == "enc" {
		password := strings.TrimSpace(c.Query("password"))
		if password == "" {
			var body struct {
				Password string `json:"password"`
			}
			_ = c.ShouldBindJSON(&body)
			password = strings.TrimSpace(body.Password)
		}
		if len(password) < 8 {
			c.JSON(400, gin.H{"success": false, "msg": "encrypted export requires password (min 8 chars)"})
			return
		}
		env, err := crypto.EncryptWithPassword(password, raw, cl.Email)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
		// Optional integrity signature when master key is configured.
		master := crypto.MasterKeyFromEnv(h.S.Config.ConfigEncryptionKey)
		sig := crypto.SignPayload(master, env)
		c.Header("X-Electru-Config-Alg", "argon2id-aes-256-gcm")
		if sig != "" {
			c.Header("X-Electru-Config-Sig", sig)
		}
		c.Data(200, "application/json; charset=utf-8", env)
		return
	}

	switch format {
	case "link":
		c.Data(200, "text/plain; charset=utf-8", []byte(link))
		return
	case "base64":
		c.Data(200, "text/plain; charset=utf-8", []byte(base64.StdEncoding.EncodeToString([]byte(link+"\n"))))
		return
	case "qr":
		c.JSON(200, gin.H{"success": true, "format": "qr", "link": link})
	default:
		master := crypto.MasterKeyFromEnv(h.S.Config.ConfigEncryptionKey)
		if sig := crypto.SignPayload(master, raw); sig != "" {
			c.Header("X-Electru-Config-Sig", sig)
		}
		c.Data(200, "application/json; charset=utf-8", raw)
	}
}
