package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/devices"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/reseller"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ClientHandler struct{ S *core.Server }

// NewClientHandler creates or exposes the NewClientHandler handler.
func NewClientHandler(s *core.Server) *ClientHandler { return &ClientHandler{S: s} }

// actorFromCtx loads the panel user for the current request (RequireRoles or claims).
func (h *ClientHandler) actorFromCtx(c *gin.Context) *models.User {
	if u := auth.CurrentUser(c); u != nil {
		return u
	}
	cl, ok := c.Get("claims")
	if !ok {
		return nil
	}
	claims, ok := cl.(*auth.Claims)
	if !ok {
		return nil
	}
	var u models.User
	if h.S.DB.First(&u, claims.UserID).Error != nil {
		return nil
	}
	return &u
}

// canAccessClient reports whether actor may view/modify this client.
// Staff (owner/admin/support) may access all; sales/operator/reseller only own (or reseller subtree).
func (h *ClientHandler) canAccessClient(actor *models.User, client *models.Client) bool {
	if actor == nil || client == nil {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(actor.Role))
	switch role {
	case auth.RoleOwner, auth.RoleAdmin, auth.RoleSupport:
		return true
	case auth.RoleSales, auth.RoleOperator:
		return client.CreatedByUserID == 0 || client.CreatedByUserID == actor.ID
	case auth.RoleReseller:
		if client.CreatedByUserID == actor.ID {
			return true
		}
		if client.CreatedByUserID == 0 {
			return false
		}
		ids, err := reseller.DescendantIDs(h.S.DB, actor.ID)
		if err != nil {
			return false
		}
		for _, id := range ids {
			if id == client.CreatedByUserID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

type createClientRequest struct {
	InboundID               uint   `json:"inboundId" binding:"required"`
	Email                   string `json:"email" binding:"required,max=128"`
	Flow                    string `json:"flow"`
	LimitIP                 int    `json:"limitIp"`
	TotalGB                 int64  `json:"totalGB"`
	ExpiryTime              int64  `json:"expiryTime"`
	Days                    int    `json:"days"` // duration counted from now; 0 = use expiryTime / never
	SubscriptionEnabled     *bool  `json:"subscriptionEnabled"`
	SubscriptionName        string `json:"subscriptionName"`
	SubscriptionGroup       string `json:"subscriptionGroup"`
	SubscriptionRefreshMin  int    `json:"subscriptionRefreshMin"`
	SubscriptionAddressType string `json:"subscriptionAddressType"`
	SubscriptionAutoUpdate  *bool  `json:"subscriptionAutoUpdate"`
	SubscriptionSmart       *bool  `json:"subscriptionSmart"`
}

func wireguardKeys() (string, string, error) {
	cmd := exec.Command("wg", "genkey")
	priv, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	priv = bytes.TrimSpace(priv)
	cmd2 := exec.Command("wg", "pubkey")
	cmd2.Stdin = bytes.NewReader(priv)
	pub, err := cmd2.Output()
	if err != nil {
		return "", "", err
	}
	return string(priv), string(bytes.TrimSpace(pub)), nil
}

func allocateWGAddress(db *gorm.DB, inboundID uint) string {
	var addresses []string
	_ = db.Model(&models.Client{}).Where("inbound_id = ? AND address <> ''", inboundID).Pluck("address", &addresses).Error
	used := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		used[address] = struct{}{}
	}
	subnet := inboundID%250 + 1
	for host := 2; host <= 254; host++ {
		candidate := fmt.Sprintf("10.77.%d.%d/32", subnet, host)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
	return ""
}

func clientPassword(ib models.Inbound) string {
	if ib.Protocol != models.ProtocolShadowsocks {
		return uuid.NewString()
	}
	var ss models.ShadowsocksSettings
	_ = json.Unmarshal([]byte(ib.Settings), &ss)
	switch ss.Method {
	case "2022-blake3-aes-128-gcm":
		b := make([]byte, 16)
		if _, err := rand.Read(b); err == nil {
			return base64.StdEncoding.EncodeToString(b)
		}
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		b := make([]byte, 32)
		if _, err := rand.Read(b); err == nil {
			return base64.StdEncoding.EncodeToString(b)
		}
	}
	return uuid.NewString()
}

// Create adds a new client to an inbound. UUID/password are generated
// server-side (never trust the browser to pick a secret), then xray is
// List returns clients with optional inbound and status filters.
func (h *ClientHandler) List(c *gin.Context) {
	limit, offset := 100, 0
	if n, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && n > 0 {
		if n > 500 {
			n = 500
		}
		limit = n
	}
	if n, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && n >= 0 {
		offset = n
	}
	q := h.S.DB.Model(&models.Client{}).Order("id asc")
	// Scope non-staff roles to their own clients (reseller: self + descendants)
	if actor := h.actorFromCtx(c); actor != nil {
		role := strings.ToLower(strings.TrimSpace(actor.Role))
		switch role {
		case auth.RoleReseller:
			var err error
			q, err = reseller.ApplyClientScope(h.S.DB, q, actor)
			if err != nil {
				c.JSON(500, gin.H{"success": false, "msg": "scope failed"})
				return
			}
		case auth.RoleSales, auth.RoleOperator:
			q = q.Where("created_by_user_id = 0 OR created_by_user_id = ?", actor.ID)
		}
	}
	if v := strings.TrimSpace(c.Query("inboundId")); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id < 1 {
			c.JSON(400, gin.H{"success": false, "msg": "invalid inboundId"})
			return
		}
		q = q.Where("inbound_id = ?", id)
	}
	if term := strings.TrimSpace(c.Query("q")); term != "" {
		if len([]rune(term)) > 128 {
			c.JSON(400, gin.H{"success": false, "msg": "q too long"})
			return
		}
		like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(term)) + "%"
		groupExpr := "LOWER(`group`)"
		if h.S.DB.Dialector.Name() == "postgres" {
			groupExpr = `LOWER("group")`
		}
		q = q.Where("(LOWER(email) LIKE ? ESCAPE '\\' OR "+groupExpr+" LIKE ? ESCAPE '\\')", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "client count failed"})
		return
	}
	var rows []models.Client
	if total > 0 {
		if err := q.Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "client list failed"})
			return
		}
	}
	if cl, ok := c.Get("claims"); ok {
		if claims, ok := cl.(*auth.Claims); ok {
			var user models.User
			if h.S.DB.First(&user, claims.UserID).Error == nil && user.Role == "viewer" {
				for i := range rows {
					rows[i].UUID = ""
					rows[i].Password = ""
					rows[i].PrivateKey = ""
					rows[i].PublicKey = ""
					rows[i].SubID = ""
				}
			}
		}
	}
	healthByID := map[uint]shop.HealthScore{}
	for _, cl := range rows {
		healthByID[cl.ID] = shop.ComputeHealthScore(cl, shop.HealthInput{})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "data": rows,
		"healthById": healthByID,
		"pagination": gin.H{"limit": limit, "offset": offset, "total": total, "hasMore": int64(offset+limit) < total},
	})
}

// ListByInbound handles the ListByInbound API operation.
func (h *ClientHandler) ListByInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	page, pageSize := 1, 50
	if n, e := strconv.Atoi(c.DefaultQuery("page", "1")); e == nil && n > 0 {
		page = n
	}
	if n, e := strconv.Atoi(c.DefaultQuery("pageSize", "50")); e == nil && n >= 10 && n <= 100 {
		pageSize = n
	}
	q := h.S.DB.Model(&models.Client{}).Where("inbound_id = ?", uint(id))
	if actor := h.actorFromCtx(c); actor != nil {
		role := strings.ToLower(strings.TrimSpace(actor.Role))
		switch role {
		case auth.RoleReseller:
			var scopeErr error
			q, scopeErr = reseller.ApplyClientScope(h.S.DB, q, actor)
			if scopeErr != nil {
				c.JSON(500, gin.H{"success": false, "msg": "scope failed"})
				return
			}
		case auth.RoleSales, auth.RoleOperator:
			q = q.Where("created_by_user_id = 0 OR created_by_user_id = ?", actor.ID)
		}
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "client count failed"})
		return
	}
	var rows []models.Client
	if total > 0 {
		if err := q.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "client list failed"})
			return
		}
	}
	if cl, ok := c.Get("claims"); ok {
		if claims, ok := cl.(*auth.Claims); ok {
			var user models.User
			if h.S.DB.First(&user, claims.UserID).Error == nil && user.Role == "viewer" {
				for i := range rows {
					rows[i].UUID = ""
					rows[i].Password = ""
					rows[i].PrivateKey = ""
					rows[i].PublicKey = ""
					rows[i].SubID = ""
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": rows, "page": page, "pageSize": pageSize, "total": total, "hasMore": int64(page*pageSize) < total})
}

// Create adds a new client to an inbound. UUID/password are generated
// server-side (never trust the browser to pick a secret), then xray is
// restarted so the new client can connect immediately.
func (h *ClientHandler) Create(c *gin.Context) {
	var req createClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || len([]rune(req.Email)) > 128 || req.TotalGB < 0 || req.LimitIP < 0 || req.ExpiryTime < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client limits or email"})
		return
	}
	if req.Days < 0 || req.Days > maxInboundDays {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client days"})
		return
	}
	if req.Days > 0 {
		req.ExpiryTime = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).UnixMilli()
	}
	actor := auth.CurrentUser(c)
	if actor != nil {
		if msg := auth.CheckClientQuota(actor, req.TotalGB, true); msg != "" {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": msg})
			return
		}
		// Mandatory wholesale: reserve/debit credit before create when price configured.
		if reseller.IsResellerRole(actor.Role) {
			if cost := reseller.CostForGB(h.S.DB, actor, req.TotalGB); cost > 0 {
				if actor.CreditBalance < cost {
					c.JSON(http.StatusPaymentRequired, gin.H{"success": false, "msg": "insufficient reseller credit for wholesale GB cost", "required": cost, "balance": actor.CreditBalance})
					return
				}
			}
		}
	}
	// chargedRef filled after successful debit (refund if create fails)
	var wholesaleCharged int64

	var ib models.Inbound
	if err := h.S.DB.First(&ib, req.InboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	addressType := normalizeSubscriptionAddress(req.SubscriptionAddressType)
	if strings.TrimSpace(req.SubscriptionAddressType) == "" {
		addressType = normalizeSubscriptionAddress(ib.SubscriptionDefaultAddress)
	}
	if addressType == "" {
		addressType = "ipv4"
	}
	// Auto-downgrade dual/all on single-stack inbounds so create never fails
	// for the common "IPv4 only" deployment.
	mode := normalizeIPMode(ib.IPMode)
	if (addressType == "dual" || addressType == "all") && mode != "dual" {
		if mode == "ipv6" {
			addressType = "ipv6"
		} else {
			addressType = "ipv4"
		}
	}
	if err := validateSubscriptionAddressForMode(ib.IPMode, addressType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}

	client := models.Client{
		InboundID:           req.InboundID,
		Email:               req.Email,
		UUID:                uuid.NewString(),
		Password:            clientPassword(ib),
		Flow:                req.Flow,
		Enable:              true,
		LimitIP:             req.LimitIP,
		TotalGB:             req.TotalGB,
		ExpiryTime:          req.ExpiryTime,
		SubID:               strings.ReplaceAll(uuid.NewString(), "-", ""),
		SubscriptionEnabled: req.SubscriptionEnabled == nil || *req.SubscriptionEnabled,
		SubscriptionName:    strings.TrimSpace(req.SubscriptionName),
		SubscriptionGroup:   strings.TrimSpace(req.SubscriptionGroup),
		SubscriptionAddressType: addressType,
		SubscriptionAutoUpdate:   req.SubscriptionAutoUpdate == nil || *req.SubscriptionAutoUpdate,
		SubscriptionSmart:        req.SubscriptionSmart != nil && *req.SubscriptionSmart,
		SubscriptionStatus:       "active",
		SubscriptionTokenVersion: 1,
		PurchasedAt:              time.Now(),
		SubscriptionRefreshMin: func() int {
			if req.SubscriptionRefreshMin < 1 {
				return 15
			}
			if req.SubscriptionRefreshMin > 1440 {
				return 1440
			}
			return req.SubscriptionRefreshMin
		}(),
	}

	if ib.Protocol == models.ProtocolWireGuard {
		priv, pub, err := wireguardKeys()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "WireGuard requires wg-tools (wg) on the server"})
			return
		}
		client.PrivateKey = priv
		client.PublicKey = pub
		client.Address = allocateWGAddress(h.S.DB, ib.ID)
		if client.Address == "" {
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "WireGuard address pool exhausted"})
			return
		}
	}

	if actor != nil {
		client.CreatedByUserID = actor.ID
		if reseller.IsResellerRole(actor.Role) {
			charged, err := reseller.ChargeForClientGB(h.S.DB, actor, client.TotalGB, "client:pending")
			if err != nil {
				c.JSON(http.StatusPaymentRequired, gin.H{"success": false, "msg": err.Error()})
				return
			}
			wholesaleCharged = charged
		}
	}
	if err := h.S.DB.Create(&client).Error; err != nil {
		if wholesaleCharged > 0 && actor != nil {
			// Refund reserved wholesale credit on create failure
			_ = reseller.Credit(h.S.DB, nil, actor.ID, wholesaleCharged, "adjust", "client:rollback", "refund after failed client create")
		}
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "could not create client (email must be unique): " + err.Error()})
		return
	}
	if actor != nil {
		_ = h.S.DB.Model(actor).Updates(map[string]any{
			"allocated_gb": gorm.Expr("allocated_gb + ?", client.TotalGB),
			"client_count": gorm.Expr("client_count + 1"),
		}).Error
	}

	// Prefer live HandlerService user add when enabled; fall back to full restart.
	if !h.S.TryLiveAddClient(ib, client) {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			_ = h.S.DB.Delete(&models.Client{}, client.ID).Error
			if actor != nil {
				// The reseller was already charged and counted; give both back,
				// otherwise every failed apply permanently burns credit/quota.
				_ = h.S.DB.Model(actor).Updates(map[string]any{
					"allocated_gb": gorm.Expr("CASE WHEN allocated_gb >= ? THEN allocated_gb - ? ELSE 0 END", client.TotalGB, client.TotalGB),
					"client_count": gorm.Expr("CASE WHEN client_count > 0 THEN client_count - 1 ELSE 0 END"),
				}).Error
				if wholesaleCharged > 0 {
					_ = reseller.Credit(h.S.DB, nil, actor.ID, wholesaleCharged, "adjust", "client:rollback", "refund after failed runtime apply")
				}
			}
			rollbackErr := h.S.RegenerateAndRestartXray()
			msg := "client creation was rolled back because runtime apply failed: " + err.Error()
			if rollbackErr != nil {
				msg += "; previous runtime restore also failed: " + rollbackErr.Error()
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
			return
		}
	}

	auditAction(h.S, c, "client.create", fmt.Sprint(client.ID), gin.H{"email": client.Email, "totalGB": client.TotalGB, "createdBy": client.CreatedByUserID})
	recordClientReplay(h.S.DB, &client, "created", "client created", replayActor(c), "panel", gin.H{"totalGB": client.TotalGB})
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": client})
}

type updateClientRequest struct {
	Enable                  *bool   `json:"enable"`
	LimitIP                 *int    `json:"limitIp"`
	TotalGB                 *int64  `json:"totalGB"`
	ExpiryTime              *int64  `json:"expiryTime"`
	Days                    *int    `json:"days"` // duration counted from now, 0 = never expires (wins over expiryTime)
	Group                   *string `json:"group"`
	AutoExtendGB            *int64  `json:"autoExtendGB"`
	AutoExtendDays          *int    `json:"autoExtendDays"`
	SubscriptionEnabled     *bool   `json:"subscriptionEnabled"`
	SubscriptionName        *string `json:"subscriptionName"`
	SubscriptionGroup       *string `json:"subscriptionGroup"`
	SubscriptionRefreshMin  *int    `json:"subscriptionRefreshMin"`
	SubscriptionAddressType *string `json:"subscriptionAddressType"`
	SubscriptionAutoUpdate  *bool   `json:"subscriptionAutoUpdate"`
	SubscriptionSmart       *bool   `json:"subscriptionSmart"`
}

// Update handles the Update API operation.
func (h *ClientHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if actor := h.actorFromCtx(c); actor != nil && !h.canAccessClient(actor, &client) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot modify this client"})
		return
	}

	original := client

	var inbound models.Inbound
	if err := h.S.DB.First(&inbound, client.InboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client inbound not found"})
		return
	}

	var req updateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if (req.LimitIP != nil && *req.LimitIP < 0) || (req.TotalGB != nil && *req.TotalGB < 0) || (req.ExpiryTime != nil && *req.ExpiryTime < 0) || (req.AutoExtendGB != nil && *req.AutoExtendGB < 0) || (req.AutoExtendDays != nil && *req.AutoExtendDays < 0) || (req.Days != nil && (*req.Days < 0 || *req.Days > maxInboundDays)) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client limits"})
		return
	}
	if req.Days != nil {
		exp := int64(0)
		if *req.Days > 0 {
			exp = time.Now().Add(time.Duration(*req.Days) * 24 * time.Hour).UnixMilli()
		}
		req.ExpiryTime = &exp
	}

	updates := map[string]any{}
	if req.Enable != nil {
		updates["enable"] = *req.Enable
	}
	if req.LimitIP != nil {
		updates["limit_ip"] = *req.LimitIP
	}
	if req.TotalGB != nil {
		actor := auth.CurrentUser(c)
		if actor != nil && client.CreatedByUserID == actor.ID {
			delta := *req.TotalGB - client.TotalGB
			if delta > 0 {
				if msg := auth.CheckClientQuota(actor, delta, false); msg != "" {
					c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": msg})
					return
				}
				if reseller.IsResellerRole(actor.Role) {
					if _, err := reseller.ChargeForClientGB(h.S.DB, actor, delta, fmt.Sprintf("client:%d:gb+", client.ID)); err != nil {
						c.JSON(http.StatusPaymentRequired, gin.H{"success": false, "msg": err.Error()})
						return
					}
				}
			}
		} else if actor != nil && (actor.Role == auth.RoleSales || actor.Role == auth.RoleOperator || actor.Role == auth.RoleReseller) {
			// Sales/reseller may only expand clients they own within quota
			if client.CreatedByUserID != 0 && client.CreatedByUserID != actor.ID {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot modify another admin's client"})
				return
			}
			delta := *req.TotalGB - client.TotalGB
			if delta > 0 {
				if msg := auth.CheckClientQuota(actor, delta, false); msg != "" {
					c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": msg})
					return
				}
				// Reseller wholesale: charge for additional GB
				if reseller.IsResellerRole(actor.Role) {
					if _, err := reseller.ChargeForClientGB(h.S.DB, actor, delta, fmt.Sprintf("client:%d:gb+", client.ID)); err != nil {
						c.JSON(http.StatusPaymentRequired, gin.H{"success": false, "msg": err.Error()})
						return
					}
				}
			}
		}
		updates["total_gb"] = *req.TotalGB
	}
	if req.ExpiryTime != nil {
		updates["expiry_time"] = *req.ExpiryTime
	}
	if req.Group != nil {
		updates["group"] = strings.TrimSpace(*req.Group)
	}
	if req.AutoExtendGB != nil {
		updates["auto_extend_gb"] = *req.AutoExtendGB
	}
	if req.AutoExtendDays != nil {
		updates["auto_extend_days"] = *req.AutoExtendDays
	}
	if req.SubscriptionEnabled != nil {
		updates["subscription_enabled"] = *req.SubscriptionEnabled
	}
	if req.SubscriptionName != nil {
		updates["subscription_name"] = strings.TrimSpace(*req.SubscriptionName)
	}
	if req.SubscriptionGroup != nil {
		updates["subscription_group"] = strings.TrimSpace(*req.SubscriptionGroup)
	}
	if req.SubscriptionAddressType != nil {
		v := normalizeSubscriptionAddress(*req.SubscriptionAddressType)
		// Auto-downgrade dual/all when inbound is not dual-stack (common IPv4-only configs).
		mode := normalizeIPMode(inbound.IPMode)
		if (v == "dual" || v == "all") && mode != "dual" {
			if mode == "ipv6" {
				v = "ipv6"
			} else {
				v = "ipv4"
			}
		}
		if err := validateSubscriptionAddressForMode(inbound.IPMode, v); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
			return
		}
		updates["subscription_address_type"] = v
	}
	if req.SubscriptionAutoUpdate != nil {
		updates["subscription_auto_update"] = *req.SubscriptionAutoUpdate
	}
	if req.SubscriptionSmart != nil {
		updates["subscription_smart"] = *req.SubscriptionSmart
	}
	if req.SubscriptionEnabled != nil && !*req.SubscriptionEnabled {
		updates["subscription_status"] = "disabled"
	}
	if req.SubscriptionEnabled != nil && *req.SubscriptionEnabled && client.SubscriptionStatus == "revoked" {
		updates["subscription_status"] = "active"
		updates["subscription_revoked_at"] = nil
	}
	if req.SubscriptionRefreshMin != nil {
		if *req.SubscriptionRefreshMin < 1 || *req.SubscriptionRefreshMin > 1440 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "subscriptionRefreshMin must be between 1 and 1440"})
			return
		}
		updates["subscription_refresh_min"] = *req.SubscriptionRefreshMin
	}
	// A client the traffic worker disabled because of expiry/quota comes back
	// automatically once its volume or time is extended.
	if (req.ExpiryTime != nil || req.TotalGB != nil) && !client.Enable && strings.EqualFold(client.SubscriptionStatus, "expired") && (req.Enable == nil || !*req.Enable) {
		if _, statusSet := updates["subscription_status"]; !statusSet {
			probe := client
			if req.ExpiryTime != nil {
				probe.ExpiryTime = *req.ExpiryTime
			}
			if req.TotalGB != nil {
				probe.TotalGB = *req.TotalGB
			}
			if !probe.IsExpired() && !probe.IsOverQuota() {
				updates["enable"] = true
				updates["subscription_status"] = "active"
			}
		}
	}

	// Only the enable flag changed? (decided before the status column is added)
	liveCandidate := req.Enable != nil && len(updates) == 1
	// Keep the status column in step with the enable switch. The traffic worker
	// writes status "expired" when it disables a client; without this an admin
	// who re-enables the client still saw it as "expired" (and anything that
	// requires status "active" kept excluding it).
	if req.Enable != nil && !strings.EqualFold(client.SubscriptionStatus, "revoked") {
		if _, statusSet := updates["subscription_status"]; !statusSet {
			if *req.Enable {
				updates["subscription_status"] = "active"
			} else {
				updates["subscription_status"] = "disabled"
			}
		}
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": client})
		return
	}
	if err := h.S.DB.Model(&client).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}

	// Prefer live add/remove when only enable flipped; otherwise full restart.
	liveOK := false
	if liveCandidate {
		if *req.Enable {
			var fresh models.Client
			_ = h.S.DB.First(&fresh, client.ID)
			liveOK = h.S.TryLiveAddClient(inbound, fresh)
		} else {
			liveOK = h.S.TryLiveRemoveClient(client.Email)
		}
	}
	if !liveOK {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			_ = h.S.DB.Save(&original).Error
			rollbackErr := h.S.RegenerateAndRestartXray()
			msg := "client update was rolled back because runtime apply failed: " + err.Error()
			if rollbackErr != nil {
				msg += "; previous runtime restore also failed: " + rollbackErr.Error()
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
			return
		}
	}

	if req.TotalGB != nil && client.CreatedByUserID > 0 {
		delta := *req.TotalGB - original.TotalGB
		if delta != 0 {
			_ = h.S.DB.Model(&models.User{}).Where("id = ?", client.CreatedByUserID).Update("allocated_gb", gorm.Expr("allocated_gb + ?", delta)).Error
		}
	}
	auditAction(h.S, c, "client.mutate", strconv.Itoa(id), gin.H{"email": client.Email, "updates": updates})
	recordClientReplay(h.S.DB, &client, "updated", "client settings changed", replayActor(c), "panel", updates)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Delete handles the Delete API operation.
func (h *ClientHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var original models.Client
	if err := h.S.DB.First(&original, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if actor := h.actorFromCtx(c); actor != nil && !h.canAccessClient(actor, &original) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot delete this client"})
		return
	}

	var profiles []models.ClientConfigProfile
	_ = h.S.DB.Where("client_id = ?", id).Find(&profiles).Error
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("client_id = ?", id).Delete(&models.ClientConfigProfile{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Client{}, id).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if original.CreatedByUserID > 0 {
		_ = h.S.DB.Model(&models.User{}).Where("id = ?", original.CreatedByUserID).Updates(map[string]any{
			"allocated_gb": gorm.Expr("CASE WHEN allocated_gb >= ? THEN allocated_gb - ? ELSE 0 END", original.TotalGB, original.TotalGB),
			"client_count": gorm.Expr("CASE WHEN client_count > 0 THEN client_count - 1 ELSE 0 END"),
		}).Error
	}

	if !h.S.TryLiveRemoveClient(original.Email) {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			_ = h.S.DB.Create(&original).Error
			for _, pr := range profiles {
				_ = h.S.DB.Create(&pr).Error
			}
			// restore quota counters since client was rolled back
			if original.CreatedByUserID > 0 {
				_ = h.S.DB.Model(&models.User{}).Where("id = ?", original.CreatedByUserID).Updates(map[string]any{
					"allocated_gb": gorm.Expr("allocated_gb + ?", original.TotalGB),
					"client_count": gorm.Expr("client_count + 1"),
				}).Error
			}
			rollbackErr := h.S.RegenerateAndRestartXray()
			msg := "client deletion was rolled back because runtime apply failed: " + err.Error()
			if rollbackErr != nil {
				msg += "; previous runtime restore also failed: " + rollbackErr.Error()
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
			return
		}
	}

	// Refund wholesale credit to reseller who created the client (best-effort)
	if original.CreatedByUserID > 0 && original.TotalGB > 0 {
		if _, err := reseller.RefundClientGB(h.S.DB, original.CreatedByUserID, original.TotalGB, fmt.Sprintf("client:%d:delete", original.ID)); err != nil {
			_ = err
		}
	}

	auditAction(h.S, c, "client.delete", strconv.Itoa(id), gin.H{"email": original.Email, "totalGB": original.TotalGB, "subId": original.SubID})
	recordClientReplay(h.S.DB, &original, "delete", "client deleted", replayActor(c), "panel", nil)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ShareLink returns the vless://, vmess://, trojan://, or ss:// URI for one
// client, using the Host header (or ?host=) as the public address so it
// works whether the panel is reached by IP or domain.
// Live returns the current per-client traffic state plus the generated
// subscription/config link. Online means recent observed traffic; an idle
// persistent connection cannot be proven from Xray's StatsService alone.
func publicHostForFamily(s *config.Config, family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	switch family {
	case "ipv4":
		if strings.TrimSpace(s.PublicHostV4) != "" {
			return strings.TrimSpace(s.PublicHostV4)
		}
	case "ipv6":
		if strings.TrimSpace(s.PublicHostV6) != "" {
			return strings.TrimSpace(s.PublicHostV6)
		}
	case "domain":
		if strings.TrimSpace(s.PublicHostDomain) != "" {
			return strings.TrimSpace(s.PublicHostDomain)
		}
	}
	return strings.TrimSpace(s.PublicHost)
}

func validateInboundFamily(ib models.Inbound, family string) error {
	family = strings.ToLower(strings.TrimSpace(family))
	mode := strings.ToLower(strings.TrimSpace(ib.IPMode))
	if mode == "" {
		mode = "ipv4"
	}
	switch family {
	case "domain":
		if mode != "ipv4" && mode != "ipv6" && mode != "dual" {
			return fmt.Errorf("invalid inbound IP mode")
		}
		return nil
	case "dual", "all":
		if mode != "dual" {
			return fmt.Errorf("this inbound is not dual-stack")
		}
		return nil
	case "ipv4":
		if mode != "ipv4" && mode != "dual" {
			return fmt.Errorf("this inbound does not support IPv4")
		}
	case "ipv6":
		if mode != "ipv6" && mode != "dual" {
			return fmt.Errorf("this inbound does not support IPv6")
		}
	default:
		return fmt.Errorf("address type must be ipv4, ipv6, domain, dual, or all")
	}
	return nil
}

func validateHostFamily(host, family string) error {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	ip := net.ParseIP(host)
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "domain":
		if ip != nil {
			return fmt.Errorf("configured public host is not a domain")
		}
	case "ipv4":
		if ip != nil && ip.To4() == nil {
			return fmt.Errorf("configured public host is not IPv4")
		}
	case "ipv6":
		if ip != nil && ip.To4() != nil {
			return fmt.Errorf("configured public host is not IPv6")
		}
	}
	return nil
}

func requestedAddressTypes(mode string) []string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "dual":
		return []string{"ipv4", "ipv6"}
	case "all":
		return []string{"ipv4", "ipv6", "domain"}
	case "domain", "ipv6":
		return []string{mode}
	default:
		return []string{"ipv4"}
	}
}

// effectiveLinkFamily downgrades dual/all requests to the inbound's own IP mode
// when the inbound is not dual-stack, so clients whose subscription address type
// defaults to "all" still get links/QR codes on single-stack inbounds.
func effectiveLinkFamily(ib models.Inbound, family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	if family != "dual" && family != "all" {
		return family
	}
	mode := strings.ToLower(strings.TrimSpace(ib.IPMode))
	if mode == "" {
		mode = "ipv4"
	}
	if mode == "ipv4" || mode == "ipv6" {
		return mode
	}
	return family
}

func (h *ClientHandler) buildClientLinks(c *gin.Context, client models.Client, ib models.Inbound, addressType string) (map[string]string, error) {
	addressType = strings.ToLower(strings.TrimSpace(addressType))
	if addressType == "" {
		addressType = "ipv4"
	}
	if err := validateInboundFamily(ib, addressType); err != nil {
		return nil, err
	}
	links := map[string]string{}
	for _, family := range requestedAddressTypes(addressType) {
		host := publicHostForFamily(h.S.Config, family)
		if host == "" && !strings.EqualFold(h.S.Config.SecurityMode, "strict") {
			host = c.Query("host")
			if host == "" {
				host = c.Request.Host
			}
			if parsedHost, _, err := net.SplitHostPort(host); err == nil {
				host = parsedHost
			}
		}
		if host == "" {
			return nil, fmt.Errorf("public %s host is required in strict security mode", family)
		}
		if err := validateHostFamily(host, family); err != nil {
			return nil, err
		}
		link, err := xray.BuildShareLink(ib, client, host)
		if err != nil {
			return nil, err
		}
		links[family] = link
	}
	return links, nil
}

// Live handles the Live API operation.
func (h *ClientHandler) Live(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if actor := h.actorFromCtx(c); actor != nil && !h.canAccessClient(actor, &client) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot access this client"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, client.InboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	// Refresh only when the shared live sample is stale. This keeps a user
	// detail modal responsive without spawning an xray statsquery for every
	// browser poll.
	subjects := clientTrafficSubjects(h.S.DB, client)
	live, ok := xray.LiveSampleForSubjects(subjects...)
	if !ok || time.Since(live.SampledAt) > 5*time.Second {
		h.S.RefreshLive()
		live, _ = xray.LiveSampleForSubjects(subjects...)
	}
	addressType := strings.ToLower(strings.TrimSpace(c.Query("family")))
	if addressType == "" {
		addressType = "ipv4"
	}
	addressType = effectiveLinkFamily(ib, addressType)
	links, err := h.buildClientLinks(c, client, ib, addressType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	link := links[addressType]
	if addressType == "dual" || addressType == "all" {
		link = links["ipv4"]
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"client":      gin.H{"id": client.ID, "email": client.Email, "enable": client.Enable, "totalGB": client.TotalGB, "up": client.Up, "down": client.Down, "expiryTime": client.ExpiryTime, "limitIp": client.LimitIP},
		"inbound":     gin.H{"id": ib.ID, "remark": ib.Remark, "protocol": ib.Protocol, "network": ib.Network, "security": ib.Security},
		"live":        live,
		"link":        link,
		"links":       links,
		"addressType": addressType,
	}})
}

// ShareLink handles the ShareLink API operation.
func (h *ClientHandler) ShareLink(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if actor := h.actorFromCtx(c); actor != nil && !h.canAccessClient(actor, &client) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot access this client"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, client.InboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	// Refresh detected public IP before building links so filtered→new IP
	// migrations show up on the next QR/sub copy without restart.
	if h.S != nil {
		h.S.AdaptPublicIPIfNeeded()
	}
	addressType := strings.ToLower(strings.TrimSpace(c.Query("family")))
	if addressType == "" {
		addressType = strings.ToLower(strings.TrimSpace(client.SubscriptionAddressType))
	}
	if addressType == "" {
		addressType = strings.ToLower(strings.TrimSpace(ib.SubscriptionDefaultAddress))
	}
	if addressType == "" {
		addressType = "ipv4"
	}
	addressType = effectiveLinkFamily(ib, addressType)
	// Domain without a configured domain host → fall back to ipv4 so admin can still copy a working link.
	if addressType == "domain" {
		if strings.TrimSpace(publicHostForFamily(h.S.Config, "domain")) == "" || net.ParseIP(strings.Trim(strings.TrimSpace(publicHostForFamily(h.S.Config, "domain")), "[]")) != nil {
			addressType = "ipv4"
		}
	}
	links, err := h.buildClientLinks(c, client, ib, addressType)
	if err != nil {
		slog.Warn("client link build failed", "client_id", client.ID, "inbound_id", ib.ID, "protocol", ib.Protocol, "family", addressType, "error", err.Error())
		c.JSON(http.StatusPreconditionFailed, gin.H{"success": false, "msg": err.Error()})
		return
	}
	primary := links[addressType]
	if addressType == "dual" || addressType == "all" {
		primary = links["ipv4"]
		if primary == "" {
			primary = links["ipv6"]
		}
	}
	if primary == "" {
		for _, v := range links {
			if v != "" {
				primary = v
				break
			}
		}
	}
	subscriptionLinks := map[string]string{}
	for _, f := range []string{"ipv4", "ipv6", "domain", "dual", "all"} {
		base := subscriptionBaseForFamily(h.S.Config, f)
		sub := subscriptionURLForFamily(base, c, h.S.Config.PanelBasePath, client.SubID, f)
		if f == "ipv4" || f == "ipv6" || f == "domain" {
			if links[f] != "" {
				subscriptionLinks[f] = sub
			}
		} else if f == "dual" && links["ipv4"] != "" && links["ipv6"] != "" {
			subscriptionLinks[f] = sub
		} else if f == "all" {
			// Always expose an "all" sub URL when at least one family link exists;
			// the /sub handler downgrades dual/all for single-stack inbounds.
			if len(links) > 0 {
				subscriptionLinks[f] = sub
			}
		}
	}
	// Preferred subscription for this client (matches their address type).
	preferredSub := subscriptionLinks[addressType]
	if preferredSub == "" {
		preferredSub = subscriptionLinks["ipv4"]
	}
	if preferredSub == "" {
		for _, v := range subscriptionLinks {
			if v != "" {
				preferredSub = v
				break
			}
		}
	}
	if !client.SubscriptionEnabled || !ib.SubscriptionEnabled {
		subscriptionLinks = map[string]string{}
		preferredSub = ""
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"link": primary, "links": links, "family": addressType,
		"subscription": preferredSub, "subscriptionLinks": subscriptionLinks,
		"subscriptionAddressType": client.SubscriptionAddressType,
		"autoUpdate": client.SubscriptionAutoUpdate,
	}})
}

type bulkCreateClientsRequest struct {
	InboundID  uint   `json:"inboundId" binding:"required"`
	Count      int    `json:"count" binding:"required,min=1,max=500"`
	Prefix     string `json:"prefix"`
	TotalGB    int64  `json:"totalGB"`
	LimitIP    int    `json:"limitIp"`
	ExpiryTime int64  `json:"expiryTime"`
	Days       int    `json:"days"` // duration counted from now; 0 = use expiryTime / never
}

// BulkCreate generates many clients in a single request -- handy for
// resellers handing out dozens of configs at once. Every client is
// inserted before xray is regenerated/restarted exactly once at the end,
// instead of once per client: creating 100 clients this way costs one
// xray restart instead of a hundred.
// BulkCreate validates and creates multiple clients in one administrative operation.
func (h *ClientHandler) BulkCreate(c *gin.Context) {
	var req bulkCreateClientsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if req.TotalGB < 0 || req.LimitIP < 0 || req.ExpiryTime < 0 || req.Days < 0 || req.Days > maxInboundDays {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client limits"})
		return
	}
	if req.Days > 0 {
		req.ExpiryTime = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).UnixMilli()
	}

	var ib models.Inbound
	if err := h.S.DB.First(&ib, req.InboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}

	prefix := strings.TrimSpace(req.Prefix)
	if prefix == "" {
		prefix = "client"
	}
	prefix = truncateRunes(prefix, 80)

	created := make([]models.Client, 0, req.Count)
	base := uuid.NewString()[:6]
	tx := h.S.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": tx.Error.Error()})
		return
	}
	rollback := func(err error) {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
	}
	for i := 1; i <= req.Count; i++ {
		addrType := normalizeSubscriptionAddress(ib.SubscriptionDefaultAddress)
		if addrType == "" || addrType == "all" || addrType == "dual" {
			mode := normalizeIPMode(ib.IPMode)
			if mode == "ipv6" {
				addrType = "ipv6"
			} else {
				addrType = "ipv4"
			}
		}
		client := models.Client{
			InboundID:                req.InboundID,
			Email:                    fmt.Sprintf("%s-%s-%02d", prefix, base, i),
			UUID:                     uuid.NewString(),
			Password:                 clientPassword(ib),
			Enable:                   true,
			LimitIP:                  req.LimitIP,
			TotalGB:                  req.TotalGB,
			ExpiryTime:               req.ExpiryTime,
			SubID:                    strings.ReplaceAll(uuid.NewString(), "-", ""),
			SubscriptionEnabled:      true,
			SubscriptionAutoUpdate:   true,
			SubscriptionStatus:       "active",
			SubscriptionTokenVersion: 1,
			SubscriptionAddressType:  addrType,
			PurchasedAt:              time.Now(),
		}
		if ib.Protocol == models.ProtocolWireGuard {
			priv, pub, e := wireguardKeys()
			if e != nil {
				rollback(fmt.Errorf("WireGuard key generation failed: %w", e))
				return
			}
			client.PrivateKey, client.PublicKey = priv, pub
			client.Address = allocateWGAddress(tx, ib.ID)
			if client.Address == "" {
				rollback(fmt.Errorf("WireGuard address pool exhausted"))
				return
			}
		}
		if err := tx.Create(&client).Error; err != nil {
			rollback(fmt.Errorf("could not create client %q: %w", client.Email, err))
			return
		}
		created = append(created, client)
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}

	if err := h.S.RegenerateAndRestartXray(); err != nil {
		ids := make([]uint, 0, len(created))
		for _, cl := range created {
			ids = append(ids, cl.ID)
		}
		_ = h.S.DB.Where("id IN ?", ids).Delete(&models.Client{}).Error
		rollbackErr := h.S.RegenerateAndRestartXray()
		msg := "bulk client creation was rolled back because runtime apply failed: " + err.Error()
		if rollbackErr != nil {
			msg += "; previous runtime restore also failed: " + rollbackErr.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "obj": created})
}

// ResetTraffic resets one client's accounting window without touching any other
// client. Xray counters are monotonic, so the current Xray value becomes the
// new baseline and the panel reports the delta from that point onward.
func (h *ClientHandler) ResetTraffic(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client id"})
		return
	}
	var client models.Client
	if err := h.S.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "client not found"})
		return
	}
	if actor := h.actorFromCtx(c); actor != nil && !h.canAccessClient(actor, &client) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "cannot reset this client"})
		return
	}
	stats, err := xray.PollStats(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, false)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "could not read Xray counters: " + err.Error()})
		return
	}
	aggregate := xray.SumTraffic(stats, clientTrafficSubjects(h.S.DB, client)...)
	baseUp, baseDown := aggregate.Uplink, aggregate.Downlink
	resetUpdates := map[string]any{"up": 0, "down": 0, "traffic_base_up": baseUp, "traffic_base_down": baseDown}
	// A client the traffic worker switched off for exceeding its quota must come
	// back after its counter is reset (unless it is also past its expiry date).
	revived := !client.Enable && strings.EqualFold(client.SubscriptionStatus, "expired") && !client.IsExpired()
	if revived {
		resetUpdates["enable"] = true
		resetUpdates["subscription_status"] = "active"
	}
	if err := h.S.DB.Model(&client).Updates(resetUpdates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	runtimeWarning := ""
	if revived {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			runtimeWarning = "traffic was reset and the client re-enabled, but applying it to xray failed: " + err.Error()
		}
	}
	for _, subject := range clientTrafficSubjects(h.S.DB, client) {
		xray.ResetLiveSample(subject)
	}
	auditAction(h.S, c, "client.traffic_reset", strconv.Itoa(id), client.Email)
	recordClientReplay(h.S.DB, &client, "traffic_reset", "traffic counters reset", replayActor(c), "panel", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"clientId": client.ID, "resetAt": time.Now(), "baselineUp": baseUp, "baselineDown": baseDown, "reenabled": revived, "warning": runtimeWarning}})
}

// RotateSecret generates a fresh UUID/password for a client and reapplies the
// runtime config. Existing credentials stop working immediately after reload.
func (h *ClientHandler) RotateSecret(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid client id"})
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
	original := cl
	updates := map[string]any{"uuid": uuid.NewString(), "password": clientPassword(ib)}
	if ib.Protocol == models.ProtocolWireGuard {
		priv, pub, e := wireguardKeys()
		if e != nil {
			c.JSON(500, gin.H{"success": false, "msg": e.Error()})
			return
		}
		updates["private_key"], updates["public_key"] = priv, pub
	}
	if err := h.S.DB.Model(&cl).Updates(updates).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Save(&original).Error
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "secret rotation was rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "client.secret_rotate", strconv.Itoa(id), cl.Email)
	recordClientReplay(h.S.DB, &cl, "secret_rotate", "uuid/password/sub token rotated", replayActor(c), "panel", nil)
	c.JSON(200, gin.H{"success": true})
}

type bulkClientActionRequest struct {
	IDs    []uint `json:"ids" binding:"required,min=1"`
	Action string `json:"action" binding:"required"`
	Group  string `json:"group"` // used by set_group action (admin organizational label)
}

// BulkAction applies safe lifecycle operations to selected clients with one runtime reload.
func (h *ClientHandler) BulkAction(c *gin.Context) {
	var req bulkClientActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	allowed := map[string]bool{"enable": true, "disable": true, "delete": true, "reset": true, "rotate": true, "set_group": true}
	if !allowed[req.Action] {
		c.JSON(400, gin.H{"success": false, "msg": "unsupported bulk action"})
		return
	}
	if len(req.IDs) > 500 {
		c.JSON(400, gin.H{"success": false, "msg": "too many clients in one bulk action (max 500)"})
		return
	}
	var rows []models.Client
	if err := h.S.DB.Where("id IN ?", req.IDs).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// set_group is pure metadata — no Xray reload required.
	if req.Action == "set_group" {
		groupName := strings.TrimSpace(req.Group)
		if len([]rune(groupName)) > 64 {
			c.JSON(400, gin.H{"success": false, "msg": "group name too long (max 64 characters)"})
			return
		}
		if e := h.S.DB.Model(&models.Client{}).Where("id IN ?", req.IDs).Update("group", groupName).Error; e != nil {
			c.JSON(500, gin.H{"success": false, "msg": e.Error()})
			return
		}
		auditAction(h.S, c, "clients.bulk_set_group", fmt.Sprintf("%d→%s", len(rows), groupName), nil)
		c.JSON(200, gin.H{"success": true, "changed": len(rows), "group": groupName})
		return
	}
	if req.Action == "reset" {
		stats, err := xray.PollStats(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, false)
		if err != nil {
			c.JSON(503, gin.H{"success": false, "msg": err.Error()})
			return
		}
		tx := h.S.DB.Begin()
		if tx.Error != nil {
			c.JSON(500, gin.H{"success": false, "msg": tx.Error.Error()})
			return
		}
		revivedAny := false
		for _, cl := range rows {
			aggregate := xray.SumTraffic(stats, clientTrafficSubjects(h.S.DB, cl)...)
			up, down := aggregate.Uplink, aggregate.Downlink
			upd := map[string]any{"up": 0, "down": 0, "traffic_base_up": up, "traffic_base_down": down}
			if !cl.Enable && strings.EqualFold(cl.SubscriptionStatus, "expired") && !cl.IsExpired() {
				upd["enable"], upd["subscription_status"] = true, "active"
				revivedAny = true
			}
			if e := tx.Model(&models.Client{}).Where("id=?", cl.ID).Updates(upd).Error; e != nil {
				tx.Rollback()
				c.JSON(500, gin.H{"success": false, "msg": e.Error()})
				return
			}
		}
		if e := tx.Commit().Error; e != nil {
			c.JSON(500, gin.H{"success": false, "msg": e.Error()})
			return
		}
		for _, cl := range rows {
			for _, subject := range clientTrafficSubjects(h.S.DB, cl) {
				xray.ResetLiveSample(subject)
			}
		}
		warning := ""
		if revivedAny {
			if e := h.S.RegenerateAndRestartXray(); e != nil {
				warning = "counters reset, but applying re-enabled clients to xray failed: " + e.Error()
			}
		}
		auditAction(h.S, c, "clients.bulk_reset", fmt.Sprint(len(rows)), nil)
		c.JSON(200, gin.H{"success": true, "changed": len(rows), "warning": warning})
		return
	}
	if req.Action == "rotate" {
		original := make([]models.Client, len(rows))
		copy(original, rows)
		for i := range rows {
			var ib models.Inbound
			if e := h.S.DB.First(&ib, rows[i].InboundID).Error; e != nil {
				c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
				return
			}
			updates := map[string]any{"uuid": uuid.NewString(), "password": clientPassword(ib)}
			if ib.Protocol == models.ProtocolWireGuard {
				priv, pub, e := wireguardKeys()
				if e != nil {
					c.JSON(500, gin.H{"success": false, "msg": e.Error()})
					return
				}
				updates["private_key"], updates["public_key"] = priv, pub
			}
			if e := h.S.DB.Model(&rows[i]).Updates(updates).Error; e != nil {
				c.JSON(500, gin.H{"success": false, "msg": e.Error()})
				return
			}
		}
		if e := h.S.RegenerateAndRestartXray(); e != nil {
			for i := range original {
				_ = h.S.DB.Save(&original[i]).Error
			}
			_ = h.S.RegenerateAndRestartXray()
			c.JSON(500, gin.H{"success": false, "msg": e.Error()})
			return
		}
		auditAction(h.S, c, "clients.bulk_rotate", fmt.Sprint(len(rows)), nil)
		c.JSON(200, gin.H{"success": true, "changed": len(rows)})
		return
	}
	original := make([]models.Client, len(rows))
	copy(original, rows)
	updates := map[string]any{}
	switch req.Action {
	case "enable":
		updates["enable"] = true
		updates["subscription_status"] = "active"
	case "disable":
		updates["enable"] = false
		updates["subscription_status"] = "disabled"
	}
	var deletedProfiles []models.ClientConfigProfile
	if req.Action == "delete" {
		_ = h.S.DB.Where("client_id IN ?", req.IDs).Find(&deletedProfiles).Error
		if e := h.S.DB.Transaction(func(tx *gorm.DB) error {
			if e := tx.Where("client_id IN ?", req.IDs).Delete(&models.ClientConfigProfile{}).Error; e != nil {
				return e
			}
			return tx.Where("id IN ?", req.IDs).Delete(&models.Client{}).Error
		}); e != nil {
			c.JSON(500, gin.H{"success": false, "msg": e.Error()})
			return
		}
	} else if e := h.S.DB.Model(&models.Client{}).Where("id IN ? AND (subscription_status IS NULL OR subscription_status <> 'revoked')", req.IDs).Updates(updates).Error; e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	if e := h.S.RegenerateAndRestartXray(); e != nil {
		if req.Action == "delete" {
			for i := range original {
				_ = h.S.DB.Create(&original[i]).Error
			}
			for i := range deletedProfiles {
				_ = h.S.DB.Create(&deletedProfiles[i]).Error
			}
		} else {
			for i := range original {
				_ = h.S.DB.Save(&original[i]).Error
			}
		}
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "bulk client change was rolled back: " + e.Error()})
		return
	}
	if req.Action == "delete" {
		// Same bookkeeping as the single delete: release reseller quota/count and
		// refund wholesale credit, otherwise bulk deletes leak both forever.
		type ownerTotals struct{ gb, n int64 }
		perOwner := map[uint]*ownerTotals{}
		for _, cl := range original {
			if cl.CreatedByUserID == 0 {
				continue
			}
			t := perOwner[cl.CreatedByUserID]
			if t == nil {
				t = &ownerTotals{}
				perOwner[cl.CreatedByUserID] = t
			}
			t.gb += cl.TotalGB
			t.n++
		}
		for owner, t := range perOwner {
			_ = h.S.DB.Model(&models.User{}).Where("id = ?", owner).Updates(map[string]any{
				"allocated_gb": gorm.Expr("CASE WHEN allocated_gb >= ? THEN allocated_gb - ? ELSE 0 END", t.gb, t.gb),
				"client_count": gorm.Expr("CASE WHEN client_count >= ? THEN client_count - ? ELSE 0 END", t.n, t.n),
			}).Error
			if t.gb > 0 {
				_, _ = reseller.RefundClientGB(h.S.DB, owner, t.gb, fmt.Sprintf("clients:bulk-delete:%d", len(rows)))
			}
		}
	}
	auditAction(h.S, c, "clients.bulk_"+req.Action, fmt.Sprint(len(rows)), nil)
	c.JSON(200, gin.H{"success": true, "changed": len(rows)})
}

// ListGroups returns distinct client group names with member counts.
// Groups are free-text labels on clients used for admin organization
// (e.g. "Ahmad - VIP package", "Shop XYZ").
func (h *ClientHandler) ListGroups(c *gin.Context) {
	type groupRow struct {
		Name  string `json:"name"`
		Count int64  `json:"count"`
	}
	var rows []groupRow
	// Include the empty/ungrouped bucket as well for clarity.
	err := h.S.DB.Model(&models.Client{}).
		Select("COALESCE(NULLIF(TRIM(\"group\"), ''), '') AS name, COUNT(*) AS count").
		Group("COALESCE(NULLIF(TRIM(\"group\"), ''), '')").
		Order("count DESC, name ASC").
		Scan(&rows).Error
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// Normalize empty name for the UI.
	for i := range rows {
		if rows[i].Name == "" {
			rows[i].Name = "(ungrouped)"
		}
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// RenameGroup renames a client group across all members (admin convenience).
func (h *ClientHandler) RenameGroup(c *gin.Context) {
	var req struct {
		From string `json:"from" binding:"required"`
		To   string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	from := strings.TrimSpace(req.From)
	to := strings.TrimSpace(req.To)
	if from == "(ungrouped)" {
		from = ""
	}
	if len([]rune(to)) > 64 {
		c.JSON(400, gin.H{"success": false, "msg": "group name too long (max 64 characters)"})
		return
	}
	res := h.S.DB.Model(&models.Client{}).Where("\"group\" = ?", from).Update("group", to)
	if res.Error != nil {
		c.JSON(500, gin.H{"success": false, "msg": res.Error.Error()})
		return
	}
	auditAction(h.S, c, "clients.group_rename", fmt.Sprintf("%s→%s (%d)", from, to, res.RowsAffected), nil)
	c.JSON(200, gin.H{"success": true, "changed": res.RowsAffected, "from": from, "to": to})
}

// OnlineUsers returns clients that currently look online from Xray live samples.
// Online is inferred from recent uplink/downlink counters (StatsService); idle
// long-lived sessions may not appear until they transfer data.
func (h *ClientHandler) OnlineUsers(c *gin.Context) {
	// Speeds need two samples. On a cold tracker (first request since the panel
	// started) take a baseline sample and wait past the stats cache window so
	// the page does not open empty.
	if !xray.HasLiveSamples() {
		h.S.RefreshLive()
		time.Sleep(1600 * time.Millisecond)
	}
	h.S.RefreshLive()
	var clients []models.Client
	if err := h.S.DB.Find(&clients).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	subjectsByClient := clientsTrafficSubjects(h.S.DB, clients)
	inboundIDs := make([]uint, 0)
	for _, cl := range clients {
		inboundIDs = append(inboundIDs, cl.InboundID)
	}
	inboundMap := map[uint]models.Inbound{}
	if len(inboundIDs) > 0 {
		var inbounds []models.Inbound
		_ = h.S.DB.Where("id IN ?", inboundIDs).Find(&inbounds).Error
		for _, ib := range inbounds {
			inboundMap[ib.ID] = ib
		}
	}
	actor := h.actorFromCtx(c)
	rows := make([]gin.H, 0)
	for _, cl := range clients {
		if actor != nil && !h.canAccessClient(actor, &cl) {
			continue
		}
		live, ok := xray.LiveSampleForSubjects(subjectsByClient[cl.ID]...)
		if !ok || !live.Online {
			continue
		}
		remark := ""
		port := 0
		if ib, ok := inboundMap[cl.InboundID]; ok {
			remark = ib.Remark
			port = ib.Port
		}
		rows = append(rows, gin.H{
			"id":            cl.ID,
			"email":         cl.Email,
			"enable":        cl.Enable,
			"inboundId":     cl.InboundID,
			"inboundRemark": remark,
			"port":          port,
			"limitIp":       cl.LimitIP,
			"uploadSpeed":   live.UplinkSpeed,
			"downloadSpeed": live.DownlinkSpeed,
			"uplinkBytes":   live.UplinkBytes,
			"downlinkBytes": live.DownlinkBytes,
			"sampledAt":     live.SampledAt,
			"devices":       live.Devices,
			"connections":   live.Connections,
			"protocol":      string(inboundMap[cl.InboundID].Protocol),
		})
	}
	totalDevices := 0
	for _, r := range rows {
		if d, ok := r["devices"].(int); ok {
			totalDevices += d
		}
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"users": rows, "count": len(rows), "devices": totalDevices}})
}

// Kick drops a client's live Xray sessions via HandlerService (rmu).
// With disable=1 the client is also set Enable=false so they cannot reconnect
// until an admin re-enables them. Default is session-only kick (reconnect allowed).
func (h *ClientHandler) Kick(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	actor := h.actorFromCtx(c)
	if actor != nil && !h.canAccessClient(actor, &cl) {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	disable := c.Query("disable") == "1" || c.Query("disable") == "true"
	liveErr := xray.LiveRemoveUser(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, cl.Email)
	// Fallback: full regenerate removes the user if live rmu is unavailable
	if liveErr != nil && disable {
		_ = h.S.DB.Model(&cl).Updates(map[string]any{"enable": false, "subscription_status": "disabled"}).Error
		_ = h.S.RegenerateAndRestartXray()
	} else if liveErr != nil {
		// Session kick without disable: still try live only; report soft failure
		c.JSON(200, gin.H{"success": true, "obj": gin.H{
			"kicked": false, "liveError": liveErr.Error(),
			"msg": "live remove failed; client may still be connected until next traffic sample or restart",
		}})
		auditAction(h.S, c, "client.kick_attempt", strconv.Itoa(id), gin.H{"email": cl.Email, "error": liveErr.Error()})
		return
	}
	if disable {
		_ = h.S.DB.Model(&cl).Updates(map[string]any{"enable": false, "subscription_status": "disabled"}).Error
		// Keep runtime consistent if live remove worked but enable flipped
		_ = h.S.TryLiveRemoveClient(cl.Email)
	}
	auditAction(h.S, c, "client.kick", strconv.Itoa(id), gin.H{"email": cl.Email, "disable": disable})
	ev := "kick"
	sum := "session kicked"
	if disable {
		ev = "disabled"
		sum = "kicked and disabled"
	}
	recordClientReplay(h.S.DB, &cl, ev, sum, replayActor(c), "panel", gin.H{"disable": disable})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"kicked": true, "disabled": disable, "email": cl.Email}})
}

// ListDevices returns registered devices for one client (admin panel).
func (h *ClientHandler) ListDevices(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	actor := h.actorFromCtx(c)
	if actor != nil && !h.canAccessClient(actor, &cl) {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	rows, err := devices.ListForClient(h.S.DB, cl.ID, 100)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	out := make([]devices.PublicView, 0, len(rows))
	for _, d := range rows {
		out = append(out, devices.ToPublic(d))
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"devices":  out,
		"limitIp":  cl.LimitIP,
		"clientId": cl.ID,
		"email":    cl.Email,
	}})
}

// RevokeClientDevice deactivates one device row for a client.
func (h *ClientHandler) RevokeClientDevice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	devID, err2 := strconv.Atoi(c.Param("deviceId"))
	if err != nil || err2 != nil || id <= 0 || devID <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	actor := h.actorFromCtx(c)
	if actor != nil && !h.canAccessClient(actor, &cl) {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	ok, err := devices.Revoke(h.S.DB, cl.ID, uint(devID))
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"success": false, "msg": "device not found"})
		return
	}
	auditAction(h.S, c, "client.device_revoke", strconv.Itoa(id), gin.H{"deviceId": devID})
	var clDev models.Client
	if h.S.DB.First(&clDev, id).Error == nil {
		recordClientReplay(h.S.DB, &clDev, "device_limit", "device revoked", replayActor(c), "panel", gin.H{"deviceId": devID})
	}
	c.JSON(200, gin.H{"success": true})
}

// RevokeAllClientDevices deactivates every active device for a client.
func (h *ClientHandler) RevokeAllClientDevices(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var cl models.Client
	if err := h.S.DB.First(&cl, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	actor := h.actorFromCtx(c)
	if actor != nil && !h.canAccessClient(actor, &cl) {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	n, err := devices.RevokeAll(h.S.DB, cl.ID)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "client.device_revoke_all", strconv.Itoa(id), gin.H{"count": n})
	c.JSON(200, gin.H{"success": true, "revoked": n})
}

func replayActor(c *gin.Context) string {
	if v, ok := c.Get("username"); ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			return s
		}
	}
	return "system"
}
