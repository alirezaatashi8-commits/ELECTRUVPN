package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/reseller"
	"github.com/gin-gonic/gin"
)

// ResellerHandler serves multi-tier reseller APIs under /api/pro/resellers.
type ResellerHandler struct{ S *core.Server }

func NewResellerHandler(s *core.Server) *ResellerHandler { return &ResellerHandler{S: s} }

func (h *ResellerHandler) actor(c *gin.Context) *models.User {
	if u := auth.CurrentUser(c); u != nil {
		return u
	}
	// Fallback: load from claims if RequireRoles did not set user
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

func (h *ResellerHandler) audit(c *gin.Context, action, target string, details any) {
	// reuse ProHandler-style audit without coupling
	b := ""
	if details != nil {
		b = fmt.Sprint(details)
	}
	username := "unknown"
	if cl, ok := c.Get("claims"); ok {
		if x, ok := cl.(*auth.Claims); ok {
			username = x.Username
		}
	}
	_ = h.S.DB.Create(&models.AuditLog{
		Username: username, Action: action, Target: target, IP: c.ClientIP(), Details: b,
	}).Error
}

// List returns the reseller tree visible to the caller.
func (h *ResellerHandler) List(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	rows, err := reseller.ListTree(h.S.DB, actor)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// strip password hashes (json:"-" already) — return safe view
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Me returns the caller's reseller profile + wallet (for reseller dashboard).
func (h *ResellerHandler) Me(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	var ledger []models.ResellerLedger
	_ = h.S.DB.Where("user_id = ?", actor.ID).Order("id desc").Limit(30).Find(&ledger).Error
	subs, _ := reseller.DescendantIDs(h.S.DB, actor.ID)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"user":            actor,
		"ledger":          ledger,
		"descendantCount": len(subs),
		"remainingGB":     actor.RemainingGB(),
	}})
}

type createResellerReq struct {
	Username            string `json:"username"`
	Password            string `json:"password"`
	ParentUserID        uint   `json:"parentUserId"`
	MaxTotalGB          int64  `json:"maxTotalGB"`
	MaxGBPerClient      int64  `json:"maxGBPerClient"`
	MaxClients          int    `json:"maxClients"`
	AllowSubResellers   bool   `json:"allowSubResellers"`
	MaxSubResellers     int    `json:"maxSubResellers"`
	CommissionPercent   int    `json:"commissionPercent"`
	WholesalePricePerGB int64  `json:"wholesalePricePerGB"`
	InitialCredit       int64  `json:"initialCredit"`
	Notes               string `json:"notes"`
	MustChangePassword  bool   `json:"mustChangePassword"`
}

// Create creates a reseller (owner/admin) or sub-reseller (reseller with permission).
func (h *ResellerHandler) Create(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	var req createResellerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		c.JSON(400, gin.H{"success": false, "msg": "username and an 8+ character password are required"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	u, err := reseller.CreateReseller(h.S.DB, actor, req.Username, string(hash), reseller.CreateOpts{
		ParentUserID:        req.ParentUserID,
		MaxTotalGB:          req.MaxTotalGB,
		MaxGBPerClient:      req.MaxGBPerClient,
		MaxClients:          req.MaxClients,
		AllowSubResellers:   req.AllowSubResellers,
		MaxSubResellers:     req.MaxSubResellers,
		CommissionPercent:   req.CommissionPercent,
		WholesalePricePerGB: req.WholesalePricePerGB,
		InitialCredit:       req.InitialCredit,
		Notes:               req.Notes,
		MustChangePassword:  req.MustChangePassword,
	})
	if err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "reseller.create", req.Username, gin.H{"id": u.ID, "parent": u.ParentUserID, "level": u.ResellerLevel})
	c.JSON(200, gin.H{"success": true, "obj": u})
}

// Credit tops up or transfers credit to a reseller.
func (h *ResellerHandler) Credit(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var body struct {
		Amount int64  `json:"amount"`
		Note   string `json:"note"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Amount <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "amount must be positive"})
		return
	}
	var target models.User
	if h.S.DB.First(&target, uint(id64)).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "reseller not found"})
		return
	}
	if !reseller.IsResellerRole(target.Role) {
		c.JSON(400, gin.H{"success": false, "msg": "target is not a reseller"})
		return
	}
	// Staff may credit anyone; reseller only descendants (or enforced inside Credit)
	if reseller.IsStaff(actor.Role) && !(strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		c.JSON(403, gin.H{"success": false, "msg": "only owner/admin may mint credit"})
		return
	}
	if err := reseller.Credit(h.S.DB, actor, target.ID, body.Amount, "topup", "", body.Note); err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		if err == reseller.ErrInsufficient {
			code = 402
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = h.S.DB.First(&target, target.ID)
	h.audit(c, "reseller.credit", fmt.Sprint(target.ID), gin.H{"amount": body.Amount})
	c.JSON(200, gin.H{"success": true, "obj": target})
}

// UpdateQuotas patches quota/policy for a reseller.
func (h *ResellerHandler) UpdateQuotas(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var body struct {
		MaxTotalGB          int64  `json:"maxTotalGB"`
		MaxGBPerClient      int64  `json:"maxGBPerClient"`
		MaxClients          int    `json:"maxClients"`
		AllowSubResellers   bool   `json:"allowSubResellers"`
		MaxSubResellers     int    `json:"maxSubResellers"`
		CommissionPercent   int    `json:"commissionPercent"`
		WholesalePricePerGB int64  `json:"wholesalePricePerGB"`
		Notes               string `json:"notes"`
		Enabled             *bool  `json:"enabled"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	var target models.User
	if h.S.DB.First(&target, uint(id64)).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	if err := reseller.UpdateQuotas(h.S.DB, actor, &target,
		body.MaxTotalGB, body.MaxGBPerClient, body.MaxClients,
		body.AllowSubResellers, body.MaxSubResellers, body.CommissionPercent, body.WholesalePricePerGB, body.Notes); err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if body.Enabled != nil && (strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		_ = h.S.DB.Model(&target).Update("enabled", *body.Enabled).Error
	}
	_ = h.S.DB.First(&target, target.ID)
	h.audit(c, "reseller.update", fmt.Sprint(target.ID), body)
	c.JSON(200, gin.H{"success": true, "obj": target})
}

// Ledger lists credit movements for a reseller.
func (h *ResellerHandler) Ledger(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var target models.User
	if h.S.DB.First(&target, uint(id64)).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	if !reseller.CanManageTarget(h.S.DB, actor, &target) && actor.ID != target.ID {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	var rows []models.ResellerLedger
	_ = h.S.DB.Where("user_id = ?", target.ID).Order("id desc").Limit(100).Find(&rows).Error
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Summary returns aggregate stats for admin reseller board.
func (h *ResellerHandler) Summary(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	if !(strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin") || reseller.IsResellerRole(actor.Role)) {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	q := h.S.DB.Model(&models.User{}).Where("role = ?", models.ResellerRoleName)
	if reseller.IsResellerRole(actor.Role) {
		ids, _, err := reseller.ScopeUserIDs(h.S.DB, actor)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
		q = q.Where("id IN ?", ids)
	}
	var total int64
	var creditSum int64
	_ = q.Count(&total).Error
	_ = q.Select("COALESCE(SUM(credit_balance),0)").Scan(&creditSum).Error
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"resellerCount": total,
		"creditSum":     creditSum,
	}})
}

// Stats returns sales metrics per reseller level/node.
func (h *ResellerHandler) Stats(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	rows, err := reseller.Stats(h.S.DB, actor)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// WholesaleSettings GET/POST global wholesale price (owner/admin).
func (h *ResellerHandler) GetWholesaleSettings(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	price := int64(0)
	var row models.PanelSetting
	if h.S.DB.Where("key = ?", models.ResellerWholesalePricePerGBKey).First(&row).Error == nil {
		_, _ = fmt.Sscan(row.Value, &price)
	}
	minTop := int64(10000)
	var row2 models.PanelSetting
	if h.S.DB.Where("key = ?", models.ResellerTopupMinKey).First(&row2).Error == nil {
		_, _ = fmt.Sscan(row2.Value, &minTop)
	}
	card, holder, inst := reseller.PaymentCardInfo(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"wholesalePricePerGB": price,
		"topupMinToman":       minTop,
		"cardNumber":          card,
		"cardHolder":          holder,
		"instructions":        inst,
	}})
}

func (h *ResellerHandler) SaveWholesaleSettings(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil || !(actor.Role == "owner" || actor.Role == "admin") {
		c.JSON(403, gin.H{"success": false, "msg": "forbidden"})
		return
	}
	var body struct {
		WholesalePricePerGB *int64 `json:"wholesalePricePerGB"`
		TopupMinToman       *int64 `json:"topupMinToman"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	upsert := func(key string, val int64) {
		var row models.PanelSetting
		if h.S.DB.Where("key = ?", key).First(&row).Error != nil {
			_ = h.S.DB.Create(&models.PanelSetting{Key: key, Value: fmt.Sprint(val)}).Error
		} else {
			_ = h.S.DB.Model(&row).Update("value", fmt.Sprint(val)).Error
		}
	}
	if body.WholesalePricePerGB != nil {
		upsert(models.ResellerWholesalePricePerGBKey, *body.WholesalePricePerGB)
	}
	if body.TopupMinToman != nil {
		upsert(models.ResellerTopupMinKey, *body.TopupMinToman)
	}
	h.audit(c, "reseller.settings", "wholesale", body)
	c.JSON(200, gin.H{"success": true})
}

// CreateTopup — reseller submits card-to-card credit request.
func (h *ResellerHandler) CreateTopup(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	var body struct {
		Amount      int64  `json:"amount"`
		ReceiptNote string `json:"receiptNote"`
	}
	if c.ShouldBindJSON(&body) != nil || body.Amount <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "amount required"})
		return
	}
	row, err := reseller.CreateCreditOrder(h.S.DB, actor, body.Amount, body.ReceiptNote)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "reseller.topup.create", fmt.Sprint(row.ID), body)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

func (h *ResellerHandler) ListTopups(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	rows, err := reseller.ListCreditOrders(h.S.DB, actor, c.Query("status"))
	if err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

func (h *ResellerHandler) ApproveTopup(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var body struct {
		AdminNote string `json:"adminNote"`
	}
	_ = c.ShouldBindJSON(&body)
	if err := reseller.ApproveCreditOrder(h.S.DB, actor, uint(id), body.AdminNote); err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "reseller.topup.approve", c.Param("id"), body)
	c.JSON(200, gin.H{"success": true})
}

func (h *ResellerHandler) RejectTopup(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var body struct {
		AdminNote string `json:"adminNote"`
	}
	_ = c.ShouldBindJSON(&body)
	if err := reseller.RejectCreditOrder(h.S.DB, actor, uint(id), body.AdminNote); err != nil {
		code := 400
		if err == reseller.ErrForbidden {
			code = 403
		}
		c.JSON(code, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "reseller.topup.reject", c.Param("id"), body)
	c.JSON(200, gin.H{"success": true})
}

// Quote returns wholesale cost for GB amount for the current reseller (or target).
func (h *ResellerHandler) Quote(c *gin.Context) {
	actor := h.actor(c)
	if actor == nil {
		c.JSON(401, gin.H{"success": false, "msg": "not authenticated"})
		return
	}
	var body struct {
		TotalGB int64 `json:"totalGB"`
		UserID  uint  `json:"userId"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.TotalGB < 0 {
		body.TotalGB = 0
	}
	u := actor
	if body.UserID > 0 && (actor.Role == "owner" || actor.Role == "admin") {
		var target models.User
		if h.S.DB.First(&target, body.UserID).Error == nil {
			u = &target
		}
	}
	price := reseller.EffectiveWholesalePerGB(h.S.DB, u)
	cost := reseller.CostForGB(h.S.DB, u, body.TotalGB)
	bal := int64(0)
	if u != nil {
		bal = u.CreditBalance
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"totalGB":       body.TotalGB,
		"pricePerGB":    price,
		"cost":          cost,
		"creditBalance": bal,
		"sufficient":    cost <= bal || price <= 0,
	}})
}
