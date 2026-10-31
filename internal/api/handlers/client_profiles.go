package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type clientProfileRequest struct {
	InboundID uint   `json:"inboundId"`
	Name      string `json:"name"`
	Priority  int    `json:"priority"`
}

// clientTrafficSubjects returns the Xray stats keys belonging to one logical
// client. Profile aliases are keyed by profile ID so two profiles with the
// same display name can never collide in Xray's global stats namespace.
func clientTrafficSubjects(db *gorm.DB, client models.Client) []string {
	subjects := []string{client.Email}
	var profiles []models.ClientConfigProfile
	if err := db.Where("client_id = ? AND enabled = ?", client.ID, true).Order("id asc").Find(&profiles).Error; err != nil {
		return subjects
	}
	for _, pr := range profiles {
		subjects = append(subjects, fmt.Sprintf("%s#profile-%d", client.Email, pr.ID))
	}
	return subjects
}

// clientsTrafficSubjects returns clientID -> every Xray stats subject for many
// clients (primary email plus enabled profile aliases) using a single query.
func clientsTrafficSubjects(db *gorm.DB, clients []models.Client) map[uint][]string {
	out := make(map[uint][]string, len(clients))
	if len(clients) == 0 {
		return out
	}
	emailByID := make(map[uint]string, len(clients))
	for _, cl := range clients {
		out[cl.ID] = []string{cl.Email}
		emailByID[cl.ID] = cl.Email
	}
	var profiles []models.ClientConfigProfile
	if err := db.Select("id", "client_id").Where("enabled = ?", true).Find(&profiles).Error; err != nil {
		return out
	}
	for _, pr := range profiles {
		email, ok := emailByID[pr.ClientID]
		if !ok {
			continue
		}
		out[pr.ClientID] = append(out[pr.ClientID], fmt.Sprintf("%s#profile-%d", email, pr.ID))
	}
	return out
}

// Profiles handles the Profiles API operation.
func (h *ClientHandler) Profiles(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var rows []models.ClientConfigProfile
	if err := h.S.DB.Where("client_id = ?", client.ID).Order("priority asc, id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, p := range rows {
		var ib models.Inbound
		if h.S.DB.First(&ib, p.InboundID).Error != nil {
			continue
		}
		out = append(out, gin.H{"id": p.ID, "name": p.Name, "priority": p.Priority, "enabled": p.Enabled, "inboundId": ib.ID, "inbound": ib.Remark, "network": ib.Network, "security": ib.Security})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": out})
}

// CreateProfile handles the CreateProfile API operation.
func (h *ClientHandler) CreateProfile(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var req clientProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if req.InboundID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "inboundId is required"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, req.InboundID).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	if ib.Protocol != models.ProtocolVLESS {
		c.JSON(400, gin.H{"success": false, "msg": "config profiles currently require a VLESS inbound"})
		return
	}
	if ib.ID == client.InboundID {
		c.JSON(409, gin.H{"success": false, "msg": "client already belongs to this inbound"})
		return
	}
	var n int64
	h.S.DB.Model(&models.ClientConfigProfile{}).Where("client_id = ? AND inbound_id = ?", client.ID, ib.ID).Count(&n)
	if n > 0 {
		c.JSON(409, gin.H{"success": false, "msg": "profile already exists"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = string(ib.Network) + "-" + ib.Remark
	}
	if len([]rune(name)) > 128 {
		c.JSON(400, gin.H{"success": false, "msg": "profile name is too long"})
		return
	}
	p := models.ClientConfigProfile{ClientID: client.ID, InboundID: ib.ID, Name: name, Priority: req.Priority, Enabled: true}
	if err := h.S.DB.Create(&p).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Delete(&p).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "profile rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "client.profile.create", fmt.Sprint(p.ID), name)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": p})
}

// DeleteProfile handles the DeleteProfile API operation.
func (h *ClientHandler) DeleteProfile(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	pid, _ := strconv.Atoi(c.Param("profileId"))
	var p models.ClientConfigProfile
	if err := h.S.DB.Where("id = ? AND client_id = ?", pid, id).First(&p).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "profile not found"})
		return
	}
	if err := h.S.DB.Delete(&p).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Create(&p).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "profile deletion rolled back: " + err.Error()})
		return
	}
	// Preserve the logical client's accumulated quota after removing a profile.
	// Xray may reset its per-user counters on restart, so the baseline must be
	// recalculated against the remaining profile aliases instead of allowing the
	// dashboard to suddenly subtract the deleted profile's historical traffic.
	stats, err := xray.PollStats(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, false)
	if err != nil {
		_ = h.S.DB.Create(&p).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "profile deletion rolled back because traffic counters could not be reconciled: " + err.Error()})
		return
	}
	var current models.Client
	if err := h.S.DB.First(&current, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "profile deleted but client accounting could not be reconciled"})
		return
	}
	aggregate := xray.SumTraffic(stats, clientTrafficSubjects(h.S.DB, current)...)
	if err := h.S.DB.Model(&current).Updates(map[string]any{"traffic_base_up": aggregate.Uplink - current.Up, "traffic_base_down": aggregate.Downlink - current.Down}).Error; err != nil {
		_ = h.S.DB.Create(&p).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "profile deletion rolled back because traffic baseline could not be saved"})
		return
	}
	for _, subject := range clientTrafficSubjects(h.S.DB, current) {
		xray.ResetLiveSample(subject)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Doctor handles the Doctor API operation.
func (h *ClientHandler) Doctor(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
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
	add := func(n string, ok bool, msg string) {
		checks = append(checks, gin.H{"name": n, "ok": ok, "message": msg})
	}
	add("enabled", cl.Enable, "client is enabled")
	add("quota", !cl.IsOverQuota(), "traffic quota is available")
	add("expiry", !cl.IsExpired(), "subscription is not expired")
	add("inbound", ib.Enable && !ib.IsLimited(), "inbound is enabled and has not reached its volume/time limit")
	add("port", ib.Port > 0 && ib.Port <= 65535, "port is in range")
	var st models.StreamSettings
	_ = json.Unmarshal([]byte(ib.StreamSettings), &st)
	if ib.Protocol == models.ProtocolVLESS && ib.Security == models.SecurityReality {
		add("reality", st.Reality != nil && st.Reality.PrivateKey != "" && st.Reality.PublicKey != "", "REALITY keys present")
	}
	host := ""
	for _, v := range []string{h.S.Config.PublicHostDomain, h.S.Config.PublicHostV4, h.S.Config.PublicHost} {
		if strings.TrimSpace(v) != "" {
			host = v
			break
		}
	}
	if host != "" {
		_, err := xray.BuildShareLink(ib, cl, host)
		add("link", err == nil, fmt.Sprintf("link generation: %v", err))
	} else {
		add("link", false, "public host is not configured")
	}
	ok := true
	for _, x := range checks {
		if !x["ok"].(bool) {
			ok = false
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"healthy": ok, "checks": checks, "inbound": ib.Remark, "network": ib.Network, "security": ib.Security}})
}

var _ = gorm.ErrRecordNotFound

// BulkAttachProfiles links one client to many VLESS inbounds as config profiles.
// All share the same UUID, quota, and expiry — traffic on any path counts once.
func (h *ClientHandler) BulkAttachProfiles(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var req struct {
		InboundIDs []uint `json:"inboundIds"`
		NamePrefix string `json:"namePrefix"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.InboundIDs) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "inboundIds required"})
		return
	}
	if len(req.InboundIDs) > 20 {
		c.JSON(400, gin.H{"success": false, "msg": "max 20 profiles per request"})
		return
	}
	prefix := strings.TrimSpace(req.NamePrefix)
	created := make([]models.ClientConfigProfile, 0)
	for i, ibID := range req.InboundIDs {
		if ibID == 0 || ibID == client.InboundID {
			continue
		}
		var ib models.Inbound
		if h.S.DB.First(&ib, ibID).Error != nil || ib.Protocol != models.ProtocolVLESS {
			continue
		}
		var n int64
		h.S.DB.Model(&models.ClientConfigProfile{}).Where("client_id = ? AND inbound_id = ?", client.ID, ib.ID).Count(&n)
		if n > 0 {
			continue
		}
		name := prefix
		if name == "" {
			name = string(ib.Network) + "-" + ib.Remark
		} else {
			name = fmt.Sprintf("%s-%s", prefix, ib.Remark)
		}
		if len([]rune(name)) > 128 {
			name = string([]rune(name)[:128])
		}
		p := models.ClientConfigProfile{
			ClientID: client.ID, InboundID: ib.ID, Name: name,
			Priority: (i + 1) * 10, Enabled: true,
		}
		if err := h.S.DB.Create(&p).Error; err != nil {
			continue
		}
		created = append(created, p)
	}
	if len(created) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "no new profiles created (need distinct VLESS inbounds)"})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		for _, p := range created {
			_ = h.S.DB.Delete(&p).Error
		}
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "client.profiles.bulk", fmt.Sprint(client.ID), gin.H{"count": len(created), "sharedQuota": true})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"profiles":    created,
		"sharedQuota": true,
		"message":     "چند کانفیگ با یک سهمیه مرکزی — مصرف همه مسیرها از یک TotalGB کم می‌شود",
	}})
}
