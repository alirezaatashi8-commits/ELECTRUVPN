package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

// TicketHandler provides built-in support tickets for panel admins and
// end-users authenticating via subscription SubID.
type TicketHandler struct{ S *core.Server }

func NewTicketHandler(s *core.Server) *TicketHandler { return &TicketHandler{S: s} }

// ListTickets — admin view (filter by status).
func (h *TicketHandler) ListTickets(c *gin.Context) {
	q := h.S.DB.Model(&models.Ticket{}).Order("updated_at desc")
	if st := strings.TrimSpace(c.Query("status")); st != "" && st != "all" {
		q = q.Where("status = ?", st)
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var rows []models.Ticket
	if err := q.Limit(limit).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// GetTicket returns ticket + messages.
func (h *TicketHandler) GetTicket(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Ticket
	if h.S.DB.First(&t, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "ticket not found"})
		return
	}
	var msgs []models.TicketMessage
	_ = h.S.DB.Where("ticket_id = ?", t.ID).Order("id asc").Find(&msgs).Error
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"ticket": t, "messages": msgs}})
}

type ticketCreateAdminReq struct {
	ClientID uint   `json:"clientId"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Priority string `json:"priority"`
}

// CreateTicketAdmin opens a ticket on behalf of a client.
func (h *TicketHandler) CreateTicketAdmin(c *gin.Context) {
	var req ticketCreateAdminReq
	if c.ShouldBindJSON(&req) != nil || req.ClientID == 0 || strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "clientId, subject, body required"})
		return
	}
	var cl models.Client
	if h.S.DB.First(&cl, req.ClientID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	prio := strings.ToLower(strings.TrimSpace(req.Priority))
	if prio == "" {
		prio = "normal"
	}
	now := time.Now()
	t := models.Ticket{
		ClientID: cl.ID, SubID: cl.SubID, Email: cl.Email,
		Subject: strings.TrimSpace(req.Subject), Status: "open", Priority: prio,
		CreatedBy: "admin", LastReplyAt: &now, MessageCount: 1,
	}
	if err := h.S.DB.Create(&t).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	author := "admin"
	var adminID uint
	if u := auth.CurrentUser(c); u != nil {
		author = u.Username
		adminID = u.ID
	}
	msg := models.TicketMessage{TicketID: t.ID, AuthorType: "admin", AuthorName: author, AdminID: adminID, Body: strings.TrimSpace(req.Body)}
	_ = h.S.DB.Create(&msg).Error
	telegram.NotifyAdminsNewTicket(h.S.DB, t, req.Body)
	auditAction(h.S, c, "ticket.create", strconv.Itoa(int(t.ID)), t.Subject)
	c.JSON(200, gin.H{"success": true, "obj": t})
}

type ticketReplyReq struct {
	Body   string `json:"body"`
	Status string `json:"status"` // optional: open|pending|closed
}

// ReplyTicket admin reply.
func (h *TicketHandler) ReplyTicket(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Ticket
	if h.S.DB.First(&t, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "ticket not found"})
		return
	}
	var req ticketReplyReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Body) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "body required"})
		return
	}
	author := "admin"
	var adminID uint
	if u := auth.CurrentUser(c); u != nil {
		author = u.Username
		adminID = u.ID
	}
	msg := models.TicketMessage{TicketID: t.ID, AuthorType: "admin", AuthorName: author, AdminID: adminID, Body: strings.TrimSpace(req.Body)}
	if err := h.S.DB.Create(&msg).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	now := time.Now()
	updates := map[string]any{"last_reply_at": now, "message_count": t.MessageCount + 1, "status": "pending"}
	if st := strings.ToLower(strings.TrimSpace(req.Status)); st == "open" || st == "pending" || st == "closed" {
		updates["status"] = st
		if st == "closed" {
			updates["closed_at"] = now
		}
	}
	_ = h.S.DB.Model(&t).Updates(updates).Error
	auditAction(h.S, c, "ticket.reply", strconv.Itoa(id), nil)
	c.JSON(200, gin.H{"success": true})
}

// CloseTicket sets status closed.
func (h *TicketHandler) CloseTicket(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := time.Now()
	if err := h.S.DB.Model(&models.Ticket{}).Where("id = ?", id).Updates(map[string]any{"status": "closed", "closed_at": now}).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "ticket.close", strconv.Itoa(id), nil)
	c.JSON(200, gin.H{"success": true})
}

// --- Public (subscription token) ---

type publicTicketCreateReq struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// PublicCreateTicket — POST /sub/:subId/tickets
func (h *TicketHandler) PublicCreateTicket(c *gin.Context) {
	cl, ok := h.clientFromSub(c)
	if !ok {
		return
	}
	var req publicTicketCreateReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "subject and body required"})
		return
	}
	now := time.Now()
	t := models.Ticket{
		ClientID: cl.ID, SubID: cl.SubID, Email: cl.Email,
		Subject: strings.TrimSpace(req.Subject), Status: "open", Priority: "normal",
		CreatedBy: "client", LastReplyAt: &now, MessageCount: 1,
	}
	if err := h.S.DB.Create(&t).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	msg := models.TicketMessage{TicketID: t.ID, AuthorType: "client", AuthorName: cl.Email, Body: strings.TrimSpace(req.Body)}
	_ = h.S.DB.Create(&msg).Error
	telegram.NotifyAdminsNewTicket(h.S.DB, t, req.Body)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": t.ID, "status": t.Status}})
}

// PublicListTickets — GET /sub/:subId/tickets
func (h *TicketHandler) PublicListTickets(c *gin.Context) {
	cl, ok := h.clientFromSub(c)
	if !ok {
		return
	}
	var rows []models.Ticket
	_ = h.S.DB.Where("client_id = ?", cl.ID).Order("updated_at desc").Limit(50).Find(&rows).Error
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// PublicGetTicket — GET /sub/:subId/tickets/:id
func (h *TicketHandler) PublicGetTicket(c *gin.Context) {
	cl, ok := h.clientFromSub(c)
	if !ok {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Ticket
	if h.S.DB.Where("id = ? AND client_id = ?", id, cl.ID).First(&t).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	var msgs []models.TicketMessage
	_ = h.S.DB.Where("ticket_id = ?", t.ID).Order("id asc").Find(&msgs).Error
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"ticket": t, "messages": msgs}})
}

// PublicReplyTicket — POST /sub/:subId/tickets/:id/reply
func (h *TicketHandler) PublicReplyTicket(c *gin.Context) {
	cl, ok := h.clientFromSub(c)
	if !ok {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var t models.Ticket
	if h.S.DB.Where("id = ? AND client_id = ?", id, cl.ID).First(&t).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	if t.Status == "closed" {
		c.JSON(400, gin.H{"success": false, "msg": "ticket closed"})
		return
	}
	var req ticketReplyReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Body) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "body required"})
		return
	}
	msg := models.TicketMessage{TicketID: t.ID, AuthorType: "client", AuthorName: cl.Email, Body: strings.TrimSpace(req.Body)}
	_ = h.S.DB.Create(&msg).Error
	now := time.Now()
	_ = h.S.DB.Model(&t).Updates(map[string]any{"last_reply_at": now, "message_count": t.MessageCount + 1, "status": "open"}).Error
	telegram.NotifyAdminsNewTicket(h.S.DB, t, req.Body)
	c.JSON(200, gin.H{"success": true})
}

// PublicUsage — GET /sub/:subId/usage  (traffic + days left for portal UI)
func (h *TicketHandler) PublicUsage(c *gin.Context) {
	cl, ok := h.clientFromSub(c)
	if !ok {
		return
	}
	used := cl.Up + cl.Down
	limit := cl.TotalGB * 1024 * 1024 * 1024
	pct := 0.0
	if limit > 0 {
		pct = float64(used) / float64(limit) * 100
		if pct > 100 {
			pct = 100
		}
	}
	daysLeft := -1 // unlimited
	if cl.ExpiryTime > 0 {
		ms := cl.ExpiryTime - time.Now().UnixMilli()
		if ms <= 0 {
			daysLeft = 0
		} else {
			daysLeft = int(ms / (24 * 60 * 60 * 1000))
			if daysLeft == 0 && ms > 0 {
				daysLeft = 1
			}
		}
	}
	totalDays := 0
	if cl.ExpiryTime > 0 && !cl.PurchasedAt.IsZero() {
		totalDays = int((cl.ExpiryTime - cl.PurchasedAt.UnixMilli()) / (24 * 60 * 60 * 1000))
		if totalDays < 1 {
			totalDays = 1
		}
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"email": cl.Email, "enable": cl.Enable, "status": cl.SubscriptionStatus,
		"up": cl.Up, "down": cl.Down, "usedBytes": used, "totalGB": cl.TotalGB,
		"limitBytes": limit, "usagePercent": pct, "unlimitedTraffic": cl.TotalGB == 0,
		"expiryTime": cl.ExpiryTime, "daysLeft": daysLeft, "totalDays": totalDays,
		"unlimitedTime": cl.ExpiryTime == 0, "expired": cl.IsExpired(), "overQuota": cl.IsOverQuota(),
	}})
}

func (h *TicketHandler) clientFromSub(c *gin.Context) (models.Client, bool) {
	subID := strings.TrimSpace(c.Param("subId"))
	var cl models.Client
	if h.S.DB.Where("sub_id = ?", subID).First(&cl).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return cl, false
	}
	if !cl.Enable || !cl.SubscriptionEnabled || strings.EqualFold(cl.SubscriptionStatus, "revoked") {
		c.JSON(403, gin.H{"success": false, "msg": "subscription unavailable"})
		return cl, false
	}
	return cl, true
}
