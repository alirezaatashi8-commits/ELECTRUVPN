package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

type ShopHandler struct{ S *core.Server }

func NewShopHandler(s *core.Server) *ShopHandler { return &ShopHandler{S: s} }

func (h *ShopHandler) ListPlans(c *gin.Context) {
	var rows []models.ShopPlan
	h.S.DB.Order("sort_order asc, id asc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

func (h *ShopHandler) SavePlan(c *gin.Context) {
	var p models.ShopPlan
	if c.ShouldBindJSON(&p) != nil || strings.TrimSpace(p.Name) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name required"})
		return
	}
	if p.DurationDays < 1 {
		p.DurationDays = 30
	}
	if p.InboundID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "inboundId is required — pick a valid inbound for this plan"})
		return
	}
	var ib models.Inbound
	if h.S.DB.First(&ib, p.InboundID).Error != nil {
		c.JSON(400, gin.H{"success": false, "msg": fmt.Sprintf("inbound #%d not found", p.InboundID)})
		return
	}
	if p.ID == 0 {
		if err := h.S.DB.Create(&p).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	} else {
		if err := h.S.DB.Save(&p).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	auditAction(h.S, c, "shop.plan.save", strconv.Itoa(int(p.ID)), p.Name)
	c.JSON(200, gin.H{"success": true, "obj": p})
}

func (h *ShopHandler) DeletePlan(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	h.S.DB.Delete(&models.ShopPlan{}, id)
	auditAction(h.S, c, "shop.plan.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

func (h *ShopHandler) ListOrders(c *gin.Context) {
	q := h.S.DB.Model(&models.ShopOrder{}).Order("id desc")
	if st := strings.TrimSpace(c.Query("status")); st != "" && st != "all" {
		q = q.Where("status = ?", st)
	}
	var rows []models.ShopOrder
	q.Limit(100).Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

func (h *ShopHandler) ApproveOrder(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	reviewer := ""
	if u := auth.CurrentUser(c); u != nil {
		reviewer = u.Username
	}
	var order models.ShopOrder
	if h.S.DB.First(&order, id).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "not found"})
		return
	}
	if order.Status == "approved" {
		c.JSON(409, gin.H{"success": false, "msg": "سفارش قبلاً تأیید شده"})
		return
	}
	order.ReviewedBy = reviewer
	if order.Kind == "wallet_topup" {
		if err := shop.FulfillWalletTopup(h.S.DB, &order); err != nil {
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		ut := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
		if ut != "" && order.TelegramChatID != 0 {
			w, _ := shop.GetOrCreateWallet(h.S.DB, order.TelegramUserID)
			_ = telegram.NewClient(ut).SendMessage(order.TelegramChatID, "✅ شارژ کیف پول تأیید شد. موجودی: "+formatInt(w.BalanceToman)+" تومان")
		}
		auditAction(h.S, c, "shop.order.approve", c.Param("id"), "wallet")
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"kind": "wallet_topup"}})
		return
	}
	if order.Kind == "gift" {
		var plan models.ShopPlan
		if h.S.DB.First(&plan, order.PlanID).Error != nil {
			c.JSON(400, gin.H{"success": false, "msg": "plan not found"})
			return
		}
		// Claim the order first (atomic) so concurrent approves cannot issue two gift cards.
		now := time.Now()
		res := h.S.DB.Model(&models.ShopOrder{}).
			Where("id = ? AND (status IN ? OR (status = ? AND reviewed_at < ?))", id,
				[]string{"pending_payment", "pending_review"}, "processing", now.Add(-5*time.Minute)).
			Updates(map[string]any{"status": "processing", "reviewed_by": reviewer, "reviewed_at": now})
		if res.Error != nil {
			c.JSON(500, gin.H{"success": false, "msg": res.Error.Error()})
			return
		}
		if res.RowsAffected == 0 {
			c.JSON(409, gin.H{"success": false, "msg": "سفارش قبلاً تأیید شده یا قابل تأیید نیست"})
			return
		}
		g, err := shop.IssueGiftFromPlan(h.S.DB, plan, order.TelegramUserID, order.PriceToman, false, "web-order", 90)
		if err != nil {
			// Roll status back so admin can retry.
			_ = h.S.DB.Model(&models.ShopOrder{}).Where("id = ? AND status = ?", id, "processing").
				Updates(map[string]any{"status": "pending_review"}).Error
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		h.S.DB.Model(&models.ShopOrder{}).Where("id = ?", id).Updates(map[string]any{
			"status": "approved", "reviewed_at": now, "promo_code": g.Code, "reviewed_by": reviewer,
		})
		ut := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
		if ut != "" && order.TelegramChatID != 0 {
			_ = telegram.NewClient(ut).SendMessage(order.TelegramChatID, "🎁 کارت هدیه:\n<code>"+g.Code+"</code>")
		}
		auditAction(h.S, c, "shop.order.approve", c.Param("id"), g.Code)
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"kind": "gift", "code": g.Code}})
		return
	}

	cl, _, err := shop.FulfillOrder(h.S.DB, &order)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// notify user (only when we actually transitioned; pre-check above blocks re-approve)
	ut := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if ut != "" && order.TelegramChatID != 0 {
		subURL := "/sub/" + cl.SubID
		if h.S.Config.SubscriptionBaseURL != "" {
			subURL = strings.TrimRight(h.S.Config.SubscriptionBaseURL, "/") + "/sub/" + cl.SubID
		}
		_ = telegram.NewClient(ut).SendMessageMarkup(order.TelegramChatID,
			"✅ سفارش #"+strconv.Itoa(id)+" تأیید شد.\nSub: <code>"+cl.SubID+"</code>\n"+subURL,
			&telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{{Text: "📊 وضعیت اشتراک من", CallbackData: "status"}},
			}})
	}
	// Apply client to runtime (live add preferred; full regen fallback)
	xrayOK, xrayMsg := true, ""
	var ib models.Inbound
	if cl.ID > 0 && h.S.DB.First(&ib, cl.InboundID).Error == nil {
		if !h.S.TryLiveAddClient(ib, cl) {
			if err := h.S.RegenerateAndRestartXray(); err != nil {
				xrayOK, xrayMsg = false, err.Error()
			}
		}
	} else if err := h.S.RegenerateAndRestartXray(); err != nil {
		xrayOK, xrayMsg = false, err.Error()
	}
	telegram.NotifyReferrerForOrder(h.S.DB, &order)
	auditAction(h.S, c, "shop.order.approve", c.Param("id"), cl.Email)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"clientId": cl.ID, "subId": cl.SubID,
		"runtimeApplied": xrayOK, "runtimeError": xrayMsg,
	}})
}

func (h *ShopHandler) RejectOrder(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	now := time.Now()
	res := h.S.DB.Model(&models.ShopOrder{}).Where("id = ? AND status IN ?", id, []string{"pending_payment", "pending_review"}).
		Updates(map[string]any{"status": "rejected", "reviewed_at": now})
	if res.RowsAffected == 0 {
		c.JSON(409, gin.H{"success": false, "msg": "سفارش قابل رد نیست"})
		return
	}
	var order models.ShopOrder
	if h.S.DB.First(&order, id).Error == nil {
		ut := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
		if ut != "" && order.TelegramChatID != 0 {
			_ = telegram.NewClient(ut).SendMessage(order.TelegramChatID, "❌ سفارش #"+strconv.Itoa(id)+" رد شد.")
		}
	}
	auditAction(h.S, c, "shop.order.reject", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

type shopSettingsReq struct {
	CardNumber   string `json:"cardNumber"`
	CardHolder   string `json:"cardHolder"`
	Instructions string `json:"instructions"`
	Enabled      *bool  `json:"enabled"`
}

func (h *ShopHandler) GetSettings(c *gin.Context) {
	card, holder, inst, en := shop.PaymentInfo(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"cardNumber": card, "cardHolder": holder, "instructions": inst, "enabled": en,
	}})
}

func (h *ShopHandler) SaveSettings(c *gin.Context) {
	var req shopSettingsReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	_ = telegram.SetSetting(h.S.DB, models.ShopCardNumberKey, strings.TrimSpace(req.CardNumber))
	_ = telegram.SetSetting(h.S.DB, models.ShopCardHolderKey, strings.TrimSpace(req.CardHolder))
	_ = telegram.SetSetting(h.S.DB, models.ShopInstructionsKey, strings.TrimSpace(req.Instructions))
	if req.Enabled != nil {
		v := "1"
		if !*req.Enabled {
			v = "0"
		}
		_ = telegram.SetSetting(h.S.DB, models.ShopEnabledKey, v)
	}
	auditAction(h.S, c, "shop.settings.save", "shop", nil)
	c.JSON(200, gin.H{"success": true})
}

type broadcastReq struct {
	Message   string  `json:"message"`
	Target    string  `json:"target"` // all | expiring_today | expiring_3d | high_usage | inactive | trial
	MinUsedGB float64 `json:"minUsedGB"`
}

func (h *ShopHandler) Broadcast(c *gin.Context) {
	var req broadcastReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Message) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "message required"})
		return
	}
	token := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if token == "" {
		c.JSON(400, gin.H{"success": false, "msg": "user bot token not set"})
		return
	}
	ids := h.resolveBroadcastTargets(req)
	ok, fail := telegram.NewClient(token).Broadcast(ids, strings.TrimSpace(req.Message))
	actorID, actorName := uint(0), "admin"
	if u := auth.CurrentUser(c); u != nil {
		actorID, actorName = u.ID, u.Username
	}
	_ = h.S.DB.Create(&models.BroadcastJob{
		Target: req.Target, Message: strings.TrimSpace(req.Message),
		Matched: len(ids), SentOK: ok, SentFail: fail,
		ActorUserID: actorID, ActorName: actorName, Source: "panel",
	}).Error
	auditAction(h.S, c, "shop.broadcast", req.Target, gin.H{"ok": ok, "fail": fail, "matched": len(ids), "preview": len(req.Message)})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"ok": ok, "fail": fail, "targets": len(ids), "filter": req.Target}})
}

func (h *ShopHandler) BroadcastHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	var rows []models.BroadcastJob
	h.S.DB.Order("id desc").Limit(limit).Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

func (h *ShopHandler) resolveBroadcastTargets(req broadcastReq) []int64 {
	return telegram.CollectCustomerChatIDs(h.S.DB, &telegram.CustomerFilter{
		Target: req.Target, MinUsedGB: req.MinUsedGB,
	})
}

func (h *ShopHandler) GetTrialSettings(c *gin.Context) {
	cfg := shop.LoadTrialConfig(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": cfg})
}

func (h *ShopHandler) SaveTrialSettings(c *gin.Context) {
	var req trialSettingsReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	if req.Enabled != nil {
		v := "0"
		if *req.Enabled {
			v = "1"
		}
		_ = telegram.SetSetting(h.S.DB, models.TrialEnabledKey, v)
	}
	if req.MB != nil {
		_ = telegram.SetSetting(h.S.DB, models.TrialMBKey, strconv.FormatInt(*req.MB, 10))
	}
	if req.Hours != nil {
		_ = telegram.SetSetting(h.S.DB, models.TrialHoursKey, strconv.Itoa(*req.Hours))
	}
	if req.InboundID != nil {
		_ = telegram.SetSetting(h.S.DB, models.TrialInboundKey, strconv.FormatUint(uint64(*req.InboundID), 10))
	}
	if req.MaxPerIPDay != nil {
		v := *req.MaxPerIPDay
		if v < 1 {
			v = 1
		}
		_ = telegram.SetSetting(h.S.DB, models.TrialMaxPerIPDayKey, strconv.Itoa(v))
	}
	if req.MaxPerDevice != nil {
		v := *req.MaxPerDevice
		if v < 1 {
			v = 1
		}
		_ = telegram.SetSetting(h.S.DB, models.TrialMaxPerDeviceKey, strconv.Itoa(v))
	}
	auditAction(h.S, c, "shop.trial.save", "trial", nil)
	c.JSON(200, gin.H{"success": true, "obj": shop.LoadTrialConfig(h.S.DB)})
}

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }

type winbackSettingsReq struct {
	Enabled     *bool  `json:"enabled"`
	DaysAfter   *int   `json:"daysAfter"`
	DiscountPct *int   `json:"discountPct"`
	PlanIDs     string `json:"planIds"` // comma-separated
	Message     string `json:"message"`
}

func (h *ShopHandler) GetWinbackSettings(c *gin.Context) {
	cfg := shop.LoadWinbackConfig(h.S.DB)
	ids := ""
	for i, id := range cfg.PlanIDs {
		if i > 0 {
			ids += ","
		}
		ids += strconv.FormatUint(uint64(id), 10)
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"enabled": cfg.Enabled, "daysAfter": cfg.DaysAfter, "discountPct": cfg.DiscountPct,
		"planIds": ids, "message": cfg.Message,
	}})
}

func (h *ShopHandler) SaveWinbackSettings(c *gin.Context) {
	var req winbackSettingsReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	if req.Enabled != nil {
		v := "0"
		if *req.Enabled {
			v = "1"
		}
		_ = telegram.SetSetting(h.S.DB, models.WinbackEnabledKey, v)
	}
	if req.DaysAfter != nil {
		_ = telegram.SetSetting(h.S.DB, models.WinbackDaysAfterKey, strconv.Itoa(*req.DaysAfter))
	}
	if req.DiscountPct != nil {
		_ = telegram.SetSetting(h.S.DB, models.WinbackDiscountPctKey, strconv.Itoa(*req.DiscountPct))
	}
	_ = telegram.SetSetting(h.S.DB, models.WinbackPlanIDsKey, strings.TrimSpace(req.PlanIDs))
	_ = telegram.SetSetting(h.S.DB, models.WinbackMessageKey, strings.TrimSpace(req.Message))
	auditAction(h.S, c, "shop.winback.save", "winback", req)
	cfg := shop.LoadWinbackConfig(h.S.DB)
	ids := ""
	for i, id := range cfg.PlanIDs {
		if i > 0 {
			ids += ","
		}
		ids += strconv.FormatUint(uint64(id), 10)
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"enabled": cfg.Enabled, "daysAfter": cfg.DaysAfter, "discountPct": cfg.DiscountPct,
		"planIds": ids, "message": cfg.Message,
	}})
}

func (h *ShopHandler) BroadcastPreview(c *gin.Context) {
	c.JSON(200, gin.H{"success": true, "obj": telegram.BroadcastPreview(h.S.DB)})
}

func (h *ShopHandler) GetCustomGBSettings(c *gin.Context) {
	cfg := shop.LoadCustomGBConfig(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"enabled": cfg.Enabled, "pricePerGB": cfg.PricePerGB,
		"minGB": cfg.MinGB, "maxGB": cfg.MaxGB, "days": cfg.Days, "inboundId": cfg.InboundID,
	}})
}

type customGBReq struct {
	Enabled    *bool  `json:"enabled"`
	PricePerGB *int64 `json:"pricePerGB"`
	MinGB      *int   `json:"minGB"`
	MaxGB      *int   `json:"maxGB"`
	Days       *int   `json:"days"`
	InboundID  *uint  `json:"inboundId"`
}

func (h *ShopHandler) SaveCustomGBSettings(c *gin.Context) {
	var req customGBReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	if req.Enabled != nil {
		v := "0"
		if *req.Enabled {
			v = "1"
		}
		_ = telegram.SetSetting(h.S.DB, models.CustomGBEnabledKey, v)
	}
	if req.PricePerGB != nil {
		_ = telegram.SetSetting(h.S.DB, models.CustomGBPriceKey, strconv.FormatInt(*req.PricePerGB, 10))
	}
	if req.MinGB != nil {
		_ = telegram.SetSetting(h.S.DB, models.CustomGBMinKey, strconv.Itoa(*req.MinGB))
	}
	if req.MaxGB != nil {
		_ = telegram.SetSetting(h.S.DB, models.CustomGBMaxKey, strconv.Itoa(*req.MaxGB))
	}
	if req.Days != nil {
		_ = telegram.SetSetting(h.S.DB, models.CustomGBDaysKey, strconv.Itoa(*req.Days))
	}
	if req.InboundID != nil {
		_ = telegram.SetSetting(h.S.DB, models.CustomGBInboundKey, strconv.FormatUint(uint64(*req.InboundID), 10))
	}
	auditAction(h.S, c, "shop.custom_gb.save", "custom_gb", req)
	cfg := shop.LoadCustomGBConfig(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"enabled": cfg.Enabled, "pricePerGB": cfg.PricePerGB,
		"minGB": cfg.MinGB, "maxGB": cfg.MaxGB, "days": cfg.Days, "inboundId": cfg.InboundID,
	}})
}

type trialSettingsReq struct {
	Enabled      *bool  `json:"enabled"`
	MB           *int64 `json:"mb"`
	Hours        *int   `json:"hours"`
	InboundID    *uint  `json:"inboundId"`
	MaxPerIPDay  *int   `json:"maxPerIpDay"`
	MaxPerDevice *int   `json:"maxPerDevice"`
}

type referralSettingsReq struct {
	Enabled           *bool  `json:"enabled"`
	Percent           *int   `json:"percent"`
	FirstPurchaseOnly *bool  `json:"firstPurchaseOnly"`
	MinOrderToman     *int64 `json:"minOrderToman"`
	BotUsername       string `json:"botUsername"`
}

func (h *ShopHandler) GetReferralSettings(c *gin.Context) {
	cfg := shop.LoadReferralConfig(h.S.DB)
	c.JSON(200, gin.H{"success": true, "obj": cfg})
}

func (h *ShopHandler) SaveReferralSettings(c *gin.Context) {
	var req referralSettingsReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	cfg := shop.LoadReferralConfig(h.S.DB)
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Percent != nil {
		cfg.Percent = *req.Percent
	}
	if req.FirstPurchaseOnly != nil {
		cfg.FirstPurchaseOnly = *req.FirstPurchaseOnly
	}
	if req.MinOrderToman != nil {
		cfg.MinOrderToman = *req.MinOrderToman
	}
	if strings.TrimSpace(req.BotUsername) != "" {
		cfg.BotUsername = strings.TrimPrefix(strings.TrimSpace(req.BotUsername), "@")
	}
	out := shop.SaveReferralConfig(h.S.DB, cfg)
	auditAction(h.S, c, "shop.referral.save", "referral", out)
	c.JSON(200, gin.H{"success": true, "obj": out})
}
