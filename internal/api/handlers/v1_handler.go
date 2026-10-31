package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

type V1Handler struct{ S *core.Server }

// NewV1Handler creates or exposes the NewV1Handler handler.
func NewV1Handler(s *core.Server) *V1Handler { return &V1Handler{S: s} }

type v1Inbound struct {
	ID       uint   `json:"id"`
	Remark   string `json:"remark"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Network  string `json:"network"`
	Security string `json:"security"`
	Enabled  bool   `json:"enabled"`
	Labels   string `json:"labels,omitempty"`
}
type v1Client struct {
	ID         uint   `json:"id"`
	InboundID  uint   `json:"inboundId"`
	Email      string `json:"email"`
	Enable     bool   `json:"enable"`
	LimitIP    int    `json:"limitIp"`
	TotalGB    int64  `json:"totalGB"`
	ExpiryTime int64  `json:"expiryTime"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Group      string `json:"group,omitempty"`
}

// Inbounds handles the Inbounds API operation.
func (h *V1Handler) Inbounds(c *gin.Context) {
	limit, offset := pageParams(c)
	var rows []models.Inbound
	q := h.S.DB.Order("id asc").Limit(limit).Offset(offset)
	if e := q.Find(&rows).Error; e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	out := make([]v1Inbound, 0, len(rows))
	for _, x := range rows {
		out = append(out, v1Inbound{ID: x.ID, Remark: x.Remark, Port: x.Port, Protocol: string(x.Protocol), Network: string(x.Network), Security: string(x.Security), Enabled: x.Enable, Labels: x.Labels})
	}
	c.JSON(200, gin.H{"success": true, "data": out, "pagination": gin.H{"limit": limit, "offset": offset}})
}

// Clients handles the Clients API operation.
func (h *V1Handler) Clients(c *gin.Context) {
	limit, offset := pageParams(c)
	q := h.S.DB.Order("id asc").Limit(limit).Offset(offset)
	if v := c.Query("inboundId"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inboundId"})
			return
		}
		q = q.Where("inbound_id = ?", id)
	}
	if qv := strings.TrimSpace(c.Query("q")); qv != "" {
		if len(qv) > 128 {
			c.JSON(400, gin.H{"success": false, "msg": "q too long"})
			return
		}
		like := "%" + qv + "%"
		q = q.Where("email LIKE ? OR \"group\" LIKE ?", like, like)
	}
	var rows []models.Client
	if e := q.Find(&rows).Error; e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	out := make([]v1Client, 0, len(rows))
	for _, x := range rows {
		out = append(out, v1Client{ID: x.ID, InboundID: x.InboundID, Email: x.Email, Enable: x.Enable, LimitIP: x.LimitIP, TotalGB: x.TotalGB, ExpiryTime: x.ExpiryTime, Up: x.Up, Down: x.Down, Group: x.Group})
	}
	c.JSON(200, gin.H{"success": true, "data": out, "pagination": gin.H{"limit": limit, "offset": offset}})
}

// Health handles the Health API operation.
func (h *V1Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"xray": h.S.Xray.Status(), "singbox": h.S.SingBox.Status()}})
}

// Restart handles the Restart API operation.
func (h *V1Handler) Restart(c *gin.Context) {
	if e := h.S.RegenerateAndRestartXray(); e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
}
func pageParams(c *gin.Context) (int, int) {
	limit := 100
	offset := 0
	if v, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && v > 0 {
		if v > 500 {
			v = 500
		}
		limit = v
	}
	if v, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && v >= 0 {
		offset = v
	}
	return limit, offset
}

// Subscriptions handles the Subscriptions API operation.
func (h *V1Handler) Subscriptions(c *gin.Context) {
	limit, offset := pageParams(c)
	q := h.S.DB.Model(&models.Client{}).Where("subscription_enabled = ?", true).Order("id asc").Limit(limit).Offset(offset)
	if v := c.Query("clientId"); v != "" {
		q = q.Where("id = ?", v)
	}
	if v := strings.TrimSpace(c.Query("status")); v != "" {
		q = q.Where("subscription_status = ?", v)
	}
	var rows []models.Client
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, x := range rows {
		out = append(out, gin.H{"clientId": x.ID, "inboundId": x.InboundID, "name": x.SubscriptionName, "status": x.SubscriptionStatus, "addressType": x.SubscriptionAddressType, "autoUpdate": x.SubscriptionAutoUpdate, "smart": x.SubscriptionSmart, "refreshMin": x.SubscriptionRefreshMin, "hits": x.SubscriptionHits, "lastSeenAt": x.SubscriptionLastSeenAt, "expiresAt": x.ExpiryTime})
	}
	c.JSON(200, gin.H{"success": true, "data": out, "pagination": gin.H{"limit": limit, "offset": offset}})
}

// Subscription handles the Subscription API operation.
func (h *V1Handler) Subscription(c *gin.Context) {
	var x models.Client
	if err := h.S.DB.First(&x, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "subscription not found"})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"clientId": x.ID, "inboundId": x.InboundID, "name": x.SubscriptionName, "status": x.SubscriptionStatus, "addressType": x.SubscriptionAddressType, "autoUpdate": x.SubscriptionAutoUpdate, "smart": x.SubscriptionSmart, "refreshMin": x.SubscriptionRefreshMin, "hits": x.SubscriptionHits, "lastSeenAt": x.SubscriptionLastSeenAt, "expiresAt": x.ExpiryTime}})
}

// SubscriptionDevices returns privacy-safe device metadata observed for a client subscription.
// It never exposes the stored hashed source IP or raw User-Agent.
func (h *V1Handler) SubscriptionDevices(c *gin.Context) {
	id := c.Param("id")
	var cl models.Client
	if err := h.S.DB.Where("id = ?", id).First(&cl).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "subscription not found"})
		return
	}
	var rows []models.ClientDevice
	if err := h.S.DB.Where("client_id = ?", cl.ID).Order("last_seen_at desc").Limit(500).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}
