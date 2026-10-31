package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

type EnterpriseHandler struct{ S *core.Server }

// NewEnterpriseHandler creates or exposes the NewEnterpriseHandler handler.
func NewEnterpriseHandler(s *core.Server) *EnterpriseHandler { return &EnterpriseHandler{S: s} }

// Search handles the Search API operation.
func (h *EnterpriseHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if len(q) < 2 || len(q) > 128 {
		c.JSON(400, gin.H{"success": false, "msg": "query must be 2-128 characters"})
		return
	}
	like := "%" + q + "%"
	out := gin.H{"inbounds": []gin.H{}, "clients": []gin.H{}, "nodes": []gin.H{}, "plans": []gin.H{}}
	var ib []models.Inbound
	h.S.DB.Where("remark LIKE ? OR labels LIKE ?", like, like).Limit(20).Find(&ib)
	for _, x := range ib {
		out["inbounds"] = append(out["inbounds"].([]gin.H), gin.H{"id": x.ID, "title": x.Remark, "port": x.Port, "protocol": x.Protocol})
	}
	var cl []models.Client
	h.S.DB.Where("email LIKE ? OR \"group\" LIKE ?", like, like).Limit(20).Find(&cl)
	for _, x := range cl {
		out["clients"] = append(out["clients"].([]gin.H), gin.H{"id": x.ID, "title": x.Email, "inboundId": x.InboundID, "group": x.Group, "enabled": x.Enable})
	}
	var nodes []models.ServerNode
	h.S.DB.Where("name LIKE ? OR address LIKE ?", like, like).Limit(20).Find(&nodes)
	for _, x := range nodes {
		out["nodes"] = append(out["nodes"].([]gin.H), gin.H{"id": x.ID, "title": x.Name, "address": x.Address, "role": x.Role})
	}
	var plans []models.ServicePlan
	h.S.DB.Where("name LIKE ? OR description LIKE ?", like, like).Limit(20).Find(&plans)
	for _, x := range plans {
		out["plans"] = append(out["plans"].([]gin.H), gin.H{"id": x.ID, "title": x.Name, "trafficGB": x.TotalGB, "durationDays": x.DurationDay})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

// Metrics handles the Metrics API operation.
func (h *EnterpriseHandler) Metrics(c *gin.Context) {
	var inbounds, clients, enabledClients, devices, nodes, healthyNodes, activeJobs, alerts int64
	h.S.DB.Model(&models.Inbound{}).Count(&inbounds)
	h.S.DB.Model(&models.Client{}).Count(&clients)
	h.S.DB.Model(&models.Client{}).Where("enable = ?", true).Count(&enabledClients)
	h.S.DB.Model(&models.ClientDevice{}).Count(&devices)
	h.S.DB.Model(&models.ServerNode{}).Where("enabled = ?", true).Count(&nodes)
	h.S.DB.Model(&models.AgentNode{}).Where("enabled = ? AND health_state = ?", true, "healthy").Count(&healthyNodes)
	h.S.DB.Model(&models.PlatformJob{}).Where("status IN ?", []string{"queued", "running"}).Count(&activeJobs)
	h.S.DB.Model(&models.AlertEvent{}).Where("resolved_at IS NULL").Count(&alerts)
	var traffic struct {
		Up   int64
		Down int64
	}
	h.S.DB.Model(&models.Client{}).Select("COALESCE(SUM(up),0) up, COALESCE(SUM(down),0) down").Scan(&traffic)
	var expired int64
	h.S.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ?", time.Now().UnixMilli()).Count(&expired)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"inbounds": inbounds, "clients": clients, "enabledClients": enabledClients, "devices": devices, "nodes": nodes, "healthyNodes": healthyNodes, "activeJobs": activeJobs, "openAlerts": alerts, "expiredClients": expired, "uploadBytes": traffic.Up, "downloadBytes": traffic.Down, "timestamp": time.Now().UTC()}})
}

// Alerts handles the Alerts API operation.
func (h *EnterpriseHandler) Alerts(c *gin.Context) {
	var rows []models.AlertEvent
	q := h.S.DB.Order("last_seen desc").Limit(200)
	if c.Query("open") == "1" {
		q = q.Where("resolved_at IS NULL")
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// ResolveAlert handles the ResolveAlert API operation.
func (h *EnterpriseHandler) ResolveAlert(c *gin.Context) {
	var row models.AlertEvent
	if h.S.DB.First(&row, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "alert not found"})
		return
	}
	now := time.Now()
	row.ResolvedAt = &now
	if err := h.S.DB.Save(&row).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "alert.resolve", fmt.Sprint(row.ID), row.Message)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// RefreshAlerts computes deterministic operational alerts from current state.
// Fingerprints make repeated refreshes idempotent and avoid alert storms.
func (h *EnterpriseHandler) RefreshAlerts(c *gin.Context) {
	created := 0
	now := time.Now()
	add := func(severity, kind, target, msg string, payload any) {
		sum := sha256.Sum256([]byte(kind + "|" + target + "|" + msg))
		fp := hex.EncodeToString(sum[:])
		var row models.AlertEvent
		if h.S.DB.Where("fingerprint = ?", fp).First(&row).Error == nil {
			row.LastSeen = now
			row.ResolvedAt = nil
			_ = h.S.DB.Save(&row)
			return
		}
		b, _ := json.Marshal(payload)
		row = models.AlertEvent{Fingerprint: fp, Severity: severity, Kind: kind, Target: target, Message: msg, Payload: string(b), FirstSeen: now, LastSeen: now, CreatedAt: now, UpdatedAt: now}
		if h.S.DB.Create(&row).Error == nil {
			created++
		}
	}
	var nodes []models.ServerNode
	h.S.DB.Where("enabled = ?", true).Find(&nodes)
	for _, n := range nodes {
		var a models.AgentNode
		if h.S.DB.Where("node_id = ?", n.ID).First(&a).Error != nil {
			add("warning", "node.agent_missing", fmt.Sprint(n.ID), "Node has no registered agent", gin.H{"nodeId": n.ID})
			continue
		}
		if a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > 90*time.Second {
			add("critical", "node.offline", fmt.Sprint(n.ID), "Node agent heartbeat is stale", gin.H{"nodeId": n.ID, "lastSeenAt": a.LastSeenAt})
		}
		if a.DiskTotalBytes > 0 && float64(a.DiskBytes)/float64(a.DiskTotalBytes) >= 0.90 {
			add("warning", "node.disk", fmt.Sprint(n.ID), "Node disk usage is above 90%", gin.H{"percent": 100 * float64(a.DiskBytes) / float64(a.DiskTotalBytes)})
		}
		if a.CPUPercent >= 90 {
			add("warning", "node.cpu", fmt.Sprint(n.ID), "Node CPU usage is above 90%", gin.H{"percent": a.CPUPercent})
		}
		if a.MemoryTotalBytes > 0 && float64(a.MemoryBytes)/float64(a.MemoryTotalBytes) >= 0.90 {
			add("warning", "node.memory", fmt.Sprint(n.ID), "Node memory usage is above 90%", gin.H{"percent": 100 * float64(a.MemoryBytes) / float64(a.MemoryTotalBytes)})
		}
	}
	var certs []models.CertificateRecord
	h.S.DB.Find(&certs)
	for _, cert := range certs {
		if cert.ExpiresAt != nil && cert.ExpiresAt.Before(now.Add(30*24*time.Hour)) {
			add("warning", "certificate.expiry", fmt.Sprint(cert.ID), "Certificate expires within 30 days", gin.H{"domain": cert.Domain, "expiresAt": cert.ExpiresAt})
		}
	}
	var expired int64
	h.S.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ? AND enable = ?", now.UnixMilli(), true).Count(&expired)
	if expired > 0 {
		add("info", "clients.expired", "global", fmt.Sprintf("%d enabled clients are expired", expired), gin.H{"count": expired})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"created": created, "checkedAt": now.UTC()}})
}
