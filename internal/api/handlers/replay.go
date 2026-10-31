package handlers

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recordClientReplay writes a Replay Studio event (best-effort, never fails caller).
func recordClientReplay(db *gorm.DB, cl *models.Client, eventType, summary, actor, source string, ctx any) {
	if db == nil || cl == nil || cl.ID == 0 {
		return
	}
	snap := map[string]any{
		"id": cl.ID, "email": cl.Email, "inboundId": cl.InboundID, "enable": cl.Enable,
		"limitIp": cl.LimitIP, "totalGB": cl.TotalGB, "up": cl.Up, "down": cl.Down,
		"expiryTime": cl.ExpiryTime, "subId": cl.SubID, "group": cl.Group,
		"uuid": trimSecret(cl.UUID), "flow": cl.Flow,
	}
	// attach inbound brief
	var ib models.Inbound
	if cl.InboundID > 0 && db.Select("id", "remark", "protocol", "port", "enable").First(&ib, cl.InboundID).Error == nil {
		snap["inboundRemark"] = ib.Remark
		snap["protocol"] = ib.Protocol
		snap["port"] = ib.Port
		snap["inboundEnable"] = ib.Enable
	}
	sb, _ := json.Marshal(snap)
	var cb []byte
	if ctx != nil {
		cb, _ = json.Marshal(ctx)
	}
	if source == "" {
		source = "panel"
	}
	if len(summary) > 250 {
		summary = summary[:250]
	}
	_ = db.Create(&models.ClientReplayEvent{
		ClientID: cl.ID, Email: cl.Email, InboundID: cl.InboundID,
		EventType: eventType, Summary: summary, SnapshotJSON: string(sb),
		ContextJSON: string(cb), Actor: actor, Source: source, CreatedAt: time.Now(),
	}).Error
}

func trimSecret(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return s
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// ReplayHandler serves Replay Studio APIs.
type ReplayHandler struct {
	S *core.Server
}

func NewReplayHandler(s *core.Server) *ReplayHandler { return &ReplayHandler{S: s} }

// Timeline returns chronological events for a client (by id).
func (h *ReplayHandler) Timeline(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid client id"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	var events []models.ClientReplayEvent
	h.S.DB.Where("client_id = ?", id).Order("id desc").Limit(limit).Find(&events)
	// also merge related audit logs mentioning this client
	var audits []models.AuditLog
	h.S.DB.Where("target = ? OR target LIKE ? OR details LIKE ?", strconv.Itoa(id), "%"+cl.Email+"%", "%"+cl.Email+"%").
		Order("id desc").Limit(50).Find(&audits)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"client": cl, "events": events, "audits": audits,
	}})
}

// Search finds clients and returns short replay preview by email/subId.
func (h *ReplayHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(400, gin.H{"success": false, "msg": "q required"})
		return
	}
	var clients []models.Client
	like := "%" + q + "%"
	tx := h.S.DB.Where("email LIKE ? OR sub_id LIKE ?", like, like)
	if idNum, err := strconv.Atoi(q); err == nil && idNum > 0 {
		tx = h.S.DB.Where("email LIKE ? OR sub_id LIKE ? OR id = ?", like, like, idNum)
	}
	tx.Order("id desc").Limit(30).Find(&clients)
	out := make([]gin.H, 0, len(clients))
	for _, cl := range clients {
		var last models.ClientReplayEvent
		h.S.DB.Where("client_id = ?", cl.ID).Order("id desc").First(&last)
		out = append(out, gin.H{
			"id": cl.ID, "email": cl.Email, "inboundId": cl.InboundID,
			"enable": cl.Enable, "lastEvent": last,
		})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

// AddNote attaches a support note into the replay timeline.
func (h *ReplayHandler) AddNote(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Summary string `json:"summary"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Summary) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "summary required"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	actor, _ := c.Get("username")
	actorStr, _ := actor.(string)
	recordClientReplay(h.S.DB, &cl, "note", req.Summary, actorStr, "panel", gin.H{"kind": "support_note"})
	auditAction(h.S, c, "replay.note", strconv.Itoa(id), req.Summary)
	c.JSON(200, gin.H{"success": true})
}
