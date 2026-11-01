package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/devices"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TMAHandler struct{ S *core.Server }

func NewTMAHandler(s *core.Server) *TMAHandler { return &TMAHandler{S: s} }

func (h *TMAHandler) authUser(c *gin.Context) (int64, bool) {
	initData := strings.TrimSpace(c.GetHeader("X-Telegram-Init-Data"))
	if initData == "" {
		initData = strings.TrimSpace(c.Query("initData"))
	}
	if initData == "" || len(initData) > 4096 {
		c.JSON(401, gin.H{"success": false, "msg": "unauthorized"})
		return 0, false
	}
	token := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if token == "" {
		c.JSON(401, gin.H{"success": false, "msg": "unauthorized"})
		return 0, false
	}
	uid, _, err := telegram.ValidateWebAppInitData(initData, token, 2*time.Hour)
	if err != nil || uid == 0 {
		c.JSON(401, gin.H{"success": false, "msg": "unauthorized"})
		return 0, false
	}
	return uid, true
}

func (h *TMAHandler) clientForTG(tgID int64) (models.Client, models.TelegramCustomer, bool) {
	var link models.TelegramCustomer
	if h.S.DB.Where("telegram_user_id = ?", tgID).First(&link).Error != nil {
		return models.Client{}, link, false
	}
	var cl models.Client
	if h.S.DB.First(&cl, link.ClientID).Error != nil {
		return models.Client{}, link, false
	}
	return cl, link, true
}

// Dashboard returns status, usage, plans, devices for Mini App.
func (h *TMAHandler) Dashboard(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg, "maintenance": telegram.MaintenanceOn(h.S.DB)})
		return
	}
	cl, link, has := h.clientForTG(tgID)
	w, _ := shop.GetOrCreateWallet(h.S.DB, tgID)
	var plans []models.ShopPlan
	h.S.DB.Where("enabled = ?", true).Order("sort_order asc").Find(&plans)
	var deviceViews []devices.PublicView
	activeN, limitN := int64(0), 0
	if has {
		rows, _ := devices.ListForClient(h.S.DB, cl.ID, 30)
		for _, d := range rows {
			deviceViews = append(deviceViews, devices.ToPublic(d))
		}
		activeN, _ = devices.CountActive(h.S.DB, cl.ID, false)
		limitN = devices.EffectiveLimit(cl)
	}
	host := strings.TrimSpace(telegram.GetSetting(h.S.DB, "public_host"))
	if host == "" && h.S.Config != nil {
		host = strings.TrimSpace(h.S.Config.SubscriptionBaseURL)
	}
	share := ""
	if has {
		var ib models.Inbound
		if h.S.DB.First(&ib, cl.InboundID).Error == nil {
			share, _ = xray.BuildShareLink(ib, cl, host)
		}
	}
	used := int64(0)
	if has {
		used = cl.Up + cl.Down
	}
	var ibEnable *bool
	if has {
		var ib models.Inbound
		if h.S.DB.First(&ib, cl.InboundID).Error == nil {
			v := ib.Enable
			ibEnable = &v
		}
	}
	health := shop.ComputeHealthScore(cl, shop.HealthInput{
		ActiveDevices: int(activeN),
		DeviceLimit:   limitN,
		InboundEnable: ibEnable,
	})
	if !has {
		health = shop.ComputeHealthScore(models.Client{}, shop.HealthInput{})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"hasClient":         has,
		"email":             cl.Email,
		"subscriptionName":  cl.SubscriptionName,
		"subId":             cl.SubID,
		"enable":            cl.Enable,
		"totalGB":           cl.TotalGB,
		"trafficLimitBytes": cl.TrafficLimitBytes,
		"quotaLabel":        shop.FormatQuotaLabel(cl),
		"usedBytes":         used,
		"up":                cl.Up,
		"down":              cl.Down,
		"expiryTime":        cl.ExpiryTime,
		"walletBalance":     w.BalanceToman,
		"autoRenew":         link.AutoRenewEnabled,
		"autoRenewPlanId":   link.AutoRenewPlanID,
		"shareLink":         share,
		"plans":             plans,
		"devices":           deviceViews,
		"maxActiveSessions": cl.MaxActiveSessions,
		"limitIp":           cl.LimitIP,
		"deviceLimit":       limitN,
		"activeDevices":     activeN,
		"deviceBinding":     cl.DeviceBindingEnabled,
		"health":            health,
	}})
}

// clientsOwnedByTG returns configs linked to this Telegram user (purchase / trial / link).
func (h *TMAHandler) clientsOwnedByTG(tgID int64) []models.Client {
	seen := map[uint]bool{}
	var out []models.Client
	var link models.TelegramCustomer
	if h.S.DB.Where("telegram_user_id = ?", tgID).First(&link).Error == nil && link.ClientID > 0 {
		var cl models.Client
		if h.S.DB.First(&cl, link.ClientID).Error == nil {
			out = append(out, cl)
			seen[cl.ID] = true
		}
	}
	var byEmail []models.Client
	h.S.DB.Where("email LIKE ? OR email LIKE ?", fmt.Sprintf("tg%d_%%", tgID), fmt.Sprintf("trial_tg%d_%%", tgID)).
		Order("id desc").Limit(30).Find(&byEmail)
	for _, cl := range byEmail {
		if !seen[cl.ID] {
			out = append(out, cl)
			seen[cl.ID] = true
		}
	}
	var orders []models.ShopOrder
	h.S.DB.Where("telegram_user_id = ? AND status = ? AND client_id > 0", tgID, "approved").
		Order("id desc").Limit(30).Find(&orders)
	for _, o := range orders {
		if seen[o.ClientID] {
			continue
		}
		var cl models.Client
		if h.S.DB.First(&cl, o.ClientID).Error == nil {
			out = append(out, cl)
			seen[cl.ID] = true
		}
	}
	return out
}

// ListConfigs returns the user's configs with personal labels (SubscriptionName).
func (h *TMAHandler) ListConfigs(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	rows := h.clientsOwnedByTG(tgID)
	out := make([]gin.H, 0, len(rows))
	for _, cl := range rows {
		label := strings.TrimSpace(cl.SubscriptionName)
		if label == "" {
			label = strings.TrimSpace(cl.Email)
		}
		st := "active"
		if !cl.Enable {
			st = "disabled"
		} else if cl.IsExpired() {
			st = "expired"
		} else if cl.IsOverQuota() {
			st = "quota"
		}
		out = append(out, gin.H{
			"id": cl.ID, "email": cl.Email, "subId": cl.SubID,
			"label": label, "subscriptionName": cl.SubscriptionName,
			"enable": cl.Enable, "status": st,
			"expiryTime": cl.ExpiryTime, "totalGB": cl.TotalGB,
			"usedBytes": cl.Up + cl.Down,
		})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"configs": out, "count": len(out)}})
}

type tmaConfigLabelReq struct {
	Label string `json:"label"`
	Name  string `json:"name"` // alias
}

// RenameConfig sets a personal display name on a config the user owns.
func (h *TMAHandler) RenameConfig(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var req tmaConfigLabelReq
	_ = c.ShouldBindJSON(&req)
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = strings.TrimSpace(req.Name)
	}
	if label == "" {
		c.JSON(400, gin.H{"success": false, "msg": "نام خالی است"})
		return
	}
	// normalize length & strip control chars
	label = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, label)
	if len([]rune(label)) > 48 {
		rs := []rune(label)
		label = string(rs[:48])
	}
	owned := false
	for _, cl := range h.clientsOwnedByTG(tgID) {
		if cl.ID == uint(id) {
			owned = true
			break
		}
	}
	if !owned {
		c.JSON(403, gin.H{"success": false, "msg": "این کانفیگ متعلق به شما نیست"})
		return
	}
	if err := h.S.DB.Model(&models.Client{}).Where("id = ?", uint(id)).
		Updates(map[string]any{"subscription_name": label, "updated_at": time.Now()}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// keep TelegramCustomer email display in sync when primary link
	_ = h.S.DB.Model(&models.TelegramCustomer{}).
		Where("telegram_user_id = ? AND client_id = ?", tgID, uint(id)).
		Update("email", label).Error
	AuditTMA(h.S.DB, tgID, "tma.config.rename", strconv.FormatUint(id, 10), gin.H{"label": label})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": id, "label": label}})
}

// PinStatus reports whether Mini App PIN lock is enabled for this user.
func (h *TMAHandler) PinStatus(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"enabled": shop.TMAPinEnabled(h.S.DB, tgID)}})
}

type tmaPinReq struct {
	PIN        string `json:"pin"`
	CurrentPIN string `json:"currentPin"`
	NewPIN     string `json:"newPin"`
}

// PinSet enables or changes the Mini App PIN (4–6 digits).
func (h *TMAHandler) PinSet(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req tmaPinReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	newPin := strings.TrimSpace(req.NewPIN)
	if newPin == "" {
		newPin = strings.TrimSpace(req.PIN)
	}
	if shop.TMAPinEnabled(h.S.DB, tgID) {
		if err := shop.VerifyTMAPIN(h.S.DB, tgID, req.CurrentPIN); err != nil {
			c.JSON(400, gin.H{"success": false, "msg": "رمز فعلی نادرست است"})
			return
		}
	}
	if err := shop.SetTMAPIN(h.S.DB, tgID, newPin); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.pin.set", "", nil)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"enabled": true, "message": "قفل رمز فعال شد"}})
}

// PinVerify unlocks the Mini App for this session after correct PIN.
func (h *TMAHandler) PinVerify(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req tmaPinReq
	_ = c.ShouldBindJSON(&req)
	if err := shop.VerifyTMAPIN(h.S.DB, tgID, req.PIN); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"unlocked": true}})
}

// PinDisable removes the Mini App PIN after verifying the current one.
func (h *TMAHandler) PinDisable(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req tmaPinReq
	_ = c.ShouldBindJSON(&req)
	pin := strings.TrimSpace(req.CurrentPIN)
	if pin == "" {
		pin = strings.TrimSpace(req.PIN)
	}
	if err := shop.VerifyTMAPIN(h.S.DB, tgID, pin); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = shop.ClearTMAPIN(h.S.DB, tgID)
	AuditTMA(h.S.DB, tgID, "tma.pin.disable", "", nil)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"enabled": false, "message": "قفل رمز برداشته شد"}})
}

func (h *TMAHandler) ListDevices(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	rows, err := devices.ListForClient(h.S.DB, cl.ID, 50)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	out := make([]devices.PublicView, 0, len(rows))
	for _, d := range rows {
		out = append(out, devices.ToPublic(d))
	}
	activeN, _ := devices.CountActive(h.S.DB, cl.ID, false)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"devices": out,
		"active":  activeN,
		"limit":   devices.EffectiveLimit(cl),
		"binding": cl.DeviceBindingEnabled,
	}})
}

func (h *TMAHandler) RevokeDevice(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	changed, err := devices.Revoke(h.S.DB, cl.ID, uint(id))
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if !changed {
		c.JSON(404, gin.H{"success": false, "msg": "device not found"})
		return
	}
	if err := h.S.DB.Model(&models.Client{}).Where("id = ?", cl.ID).
		Update("subscription_token_version", gorm.Expr("subscription_token_version + 1")).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "device revoked, but subscription-token invalidation failed"})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.device.revoke", fmt.Sprint(id), nil)
	c.JSON(200, gin.H{"success": true})
}

func (h *TMAHandler) RevokeAllDevices(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, link, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	chatID := link.TelegramChatID
	if chatID == 0 {
		chatID = tgID
	}
	// Sensitive: require confirmation in Telegram chat (T9).
	pending, err := shop.CreateSensitivePending(h.S.DB, tgID, chatID, shop.SensKindRevokeAll, cl.ID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.notifySensitiveConfirm(chatID, pending); err != nil {
		shop.ConsumeSensitivePending(h.S.DB, pending.Token)
		c.JSON(500, gin.H{"success": false, "msg": "نتوانستیم پیام تأیید را به تلگرام بفرستیم. ربات را استارت کنید و دوباره تلاش کنید."})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.device.revoke_all.pending", cl.SubID, gin.H{"token": pending.Token})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"pending": true,
		"message": "برای تکمیل، دکمه تأیید را در چت ربات بزنید (مهلت ۱۰ دقیقه).",
		"kind":    shop.SensKindRevokeAll,
	}})
}

// ResetUUID requests Telegram confirmation before rotating UUID/password (T9).
func (h *TMAHandler) ResetUUID(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, link, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	chatID := link.TelegramChatID
	if chatID == 0 {
		chatID = tgID
	}
	pending, err := shop.CreateSensitivePending(h.S.DB, tgID, chatID, shop.SensKindResetUUID, cl.ID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.notifySensitiveConfirm(chatID, pending); err != nil {
		shop.ConsumeSensitivePending(h.S.DB, pending.Token)
		c.JSON(500, gin.H{"success": false, "msg": "نتوانستیم پیام تأیید را به تلگرام بفرستیم. ربات را استارت کنید و دوباره تلاش کنید."})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.reset_uuid.pending", cl.SubID, gin.H{"token": pending.Token})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"pending": true,
		"message": "برای تکمیل، دکمه تأیید را در چت ربات بزنید (مهلت ۱۰ دقیقه).",
		"kind":    shop.SensKindResetUUID,
	}})
}

func (h *TMAHandler) notifySensitiveConfirm(chatID int64, pending shop.SensitivePending) error {
	ut := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	if strings.TrimSpace(ut) == "" {
		return fmt.Errorf("user bot token missing")
	}
	label := shop.SensitiveKindLabel(pending.Kind)
	msg := fmt.Sprintf(`⚠️ <b>تأیید عملیات حساس</b>

عملیات: <b>%s</b>
مهلت: ۱۰ دقیقه

اگر این درخواست از طرف شما نبود، «انصراف» را بزنید.`, label)
	kb := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "✅ تأیید می‌کنم", CallbackData: "sens_ok:" + pending.Token}},
		{{Text: "❌ انصراف", CallbackData: "sens_no:" + pending.Token}},
	}}
	return telegram.NewClient(ut).SendMessageMarkup(chatID, msg, kb)
}

func (h *TMAHandler) Plans(c *gin.Context) {
	if _, ok := h.authUser(c); !ok {
		return
	}
	var plans []models.ShopPlan
	h.S.DB.Where("enabled = ?", true).Order("sort_order asc").Find(&plans)
	c.JSON(200, gin.H{"success": true, "obj": plans})
}

// Wallet returns balance and recent ledger.
func (h *TMAHandler) Wallet(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	w, _ := shop.GetOrCreateWallet(h.S.DB, tgID)
	var ledger []models.WalletLedger
	h.S.DB.Where("telegram_user_id = ?", tgID).Order("id desc").Limit(20).Find(&ledger)
	var link models.TelegramCustomer
	_ = h.S.DB.Where("telegram_user_id = ?", tgID).First(&link)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"balance": w.BalanceToman, "ledger": ledger,
		"autoRenew": link.AutoRenewEnabled, "autoRenewPlanId": link.AutoRenewPlanID,
	}})
}

// Orders returns the user's shop purchase history (plans, top-ups, status).
func (h *TMAHandler) Orders(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	limit := 30
	rows := shop.ListUserOrders(h.S.DB, tgID, limit)
	out := make([]gin.H, 0, len(rows))
	for _, o := range rows {
		out = append(out, gin.H{
			"id": o.ID, "planName": o.PlanName, "kind": o.Kind,
			"totalGB": o.TotalGB, "durationDays": o.DurationDays,
			"priceToman": o.PriceToman, "originalPriceToman": o.OriginalPriceToman,
			"discountToman": o.DiscountToman, "status": o.Status,
			"promoCode": o.PromoCode, "createdAt": o.CreatedAt,
			"reviewedAt": o.ReviewedAt,
		})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"orders": out, "count": len(out)}})
}

type tmaTopupReq struct {
	AmountToman int64 `json:"amountToman"`
}

// CreateTopup starts a wallet top-up order (card-to-card + receipt in bot).
func (h *TMAHandler) CreateTopup(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	var req tmaTopupReq
	if c.ShouldBindJSON(&req) != nil || req.AmountToman < 10000 {
		c.JSON(400, gin.H{"success": false, "msg": "min 10000 toman"})
		return
	}
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status IN ? AND kind = ?", tgID, []string{"pending_payment", "pending_review"}, "wallet_topup").
		Update("status", "cancelled").Error
	order := models.ShopOrder{
		TelegramUserID: tgID, TelegramChatID: tgID,
		PlanName: "شارژ کیف پول (TMA)", PriceToman: req.AmountToman,
		Status: "pending_payment", Kind: "wallet_topup",
	}
	if err := h.S.DB.Create(&order).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	card, holder, inst, _ := shop.PaymentInfo(h.S.DB)
	AuditTMA(h.S.DB, tgID, "tma.wallet.topup", strconv.FormatUint(uint64(order.ID), 10), gin.H{"amount": req.AmountToman})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"orderId": order.ID, "amountToman": req.AmountToman,
		"cardNumber": card, "cardHolder": holder, "instructions": inst,
		"hint": "عکس رسید را در چت ربات ارسال کنید",
	}})
}

type tmaBuyReq struct {
	PlanID    uint   `json:"planId"`
	PromoCode string `json:"promoCode"`
}

// ApplyPromo quotes a promo code against a plan (or raw amount) without creating an order.
func (h *TMAHandler) ApplyPromo(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req struct {
		Code        string `json:"code"`
		PlanID      uint   `json:"planId"`
		AmountToman int64  `json:"amountToman"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "code required"})
		return
	}
	base := req.AmountToman
	if req.PlanID > 0 {
		var plan models.ShopPlan
		if h.S.DB.First(&plan, req.PlanID).Error != nil || !plan.Enabled {
			c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
			return
		}
		base = plan.PriceToman
		if active, pct, wcfg := shop.HasWinbackOffer(h.S.DB, tgID); active && shop.PlanEligibleForWinback(wcfg, plan.ID) {
			base = shop.DiscountedPrice(plan.PriceToman, pct)
		}
	}
	q, err := shop.QuotePromo(h.S.DB, req.Code, tgID, base)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": q})
}

// CreatePurchase starts a plan purchase order.
func (h *TMAHandler) CreatePurchase(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	var req tmaBuyReq
	if c.ShouldBindJSON(&req) != nil || req.PlanID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "planId required"})
		return
	}
	var plan models.ShopPlan
	if h.S.DB.First(&plan, req.PlanID).Error != nil || !plan.Enabled {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status IN ? AND kind = ?", tgID, []string{"pending_payment", "pending_review"}, "plan").
		Update("status", "cancelled").Error
	price := plan.PriceToman
	name := plan.Name
	kind := "plan"
	var disc int64
	prorateMsg := ""
	if active, pct, wcfg := shop.HasWinbackOffer(h.S.DB, tgID); active && shop.PlanEligibleForWinback(wcfg, plan.ID) {
		price = shop.DiscountedPrice(plan.PriceToman, pct)
		name = plan.Name + " (تخفیف)"
	} else {
		pq := shop.QuoteProrate(h.S.DB, tgID, plan)
		if pq.HasActive && pq.CreditToman > 0 {
			price = pq.FinalToman
			disc = pq.CreditToman
			kind = "upgrade"
			name = plan.Name + " (ارتقاء/تعویض)"
			prorateMsg = pq.Message
			shop.StoreProrateExcess(h.S.DB, tgID, pq.ExcessToman)
		}
	}
	orig := plan.PriceToman
	promo := strings.TrimSpace(req.PromoCode)
	if promo != "" {
		q, err := shop.QuotePromo(h.S.DB, promo, tgID, price)
		if err != nil {
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		price = q.FinalToman
		disc += q.DiscountToman
		promo = q.Code
		name = name + " · " + promo
	}
	order := models.ShopOrder{
		TelegramUserID: tgID, TelegramChatID: tgID,
		PlanID: plan.ID, PlanName: name, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: price,
		InboundID: plan.InboundID, Status: "pending_payment", Kind: kind,
		PromoCode: promo, OriginalPriceToman: orig, DiscountToman: disc,
	}
	if err := h.S.DB.Create(&order).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	card, holder, inst, _ := shop.PaymentInfo(h.S.DB)
	AuditTMA(h.S.DB, tgID, "tma.shop.buy", strconv.FormatUint(uint64(order.ID), 10), gin.H{"planId": plan.ID, "price": price, "promo": promo})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"orderId": order.ID, "planName": plan.Name, "priceToman": price, "originalPriceToman": orig,
		"discountToman": disc, "promoCode": promo, "kind": kind, "prorateMessage": prorateMsg,
		"cardNumber": card, "cardHolder": holder, "instructions": inst,
		"hint": "مبلغ را واریز و عکس رسید را در ربات بفرستید",
	}})
}

// BuyWithWallet purchases (or upgrades) a plan by debiting the Telegram user's wallet immediately.
func (h *TMAHandler) BuyWithWallet(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	var req tmaBuyReq
	if c.ShouldBindJSON(&req) != nil || req.PlanID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "planId required"})
		return
	}
	var plan models.ShopPlan
	if h.S.DB.First(&plan, req.PlanID).Error != nil || !plan.Enabled {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	price := plan.PriceToman
	name := plan.Name
	kind := "plan"
	var disc int64
	prorateMsg := ""
	if active, pct, wcfg := shop.HasWinbackOffer(h.S.DB, tgID); active && shop.PlanEligibleForWinback(wcfg, plan.ID) {
		price = shop.DiscountedPrice(plan.PriceToman, pct)
		name = plan.Name + " (تخفیف)"
	} else {
		pq := shop.QuoteProrate(h.S.DB, tgID, plan)
		if pq.HasActive && pq.CreditToman > 0 {
			price = pq.FinalToman
			disc = pq.CreditToman
			kind = "upgrade"
			name = plan.Name + " (ارتقاء/تعویض)"
			prorateMsg = pq.Message
			shop.StoreProrateExcess(h.S.DB, tgID, pq.ExcessToman)
		}
	}
	orig := plan.PriceToman
	promo := strings.TrimSpace(req.PromoCode)
	if promo != "" {
		q, err := shop.QuotePromo(h.S.DB, promo, tgID, price)
		if err != nil {
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		price = q.FinalToman
		disc += q.DiscountToman
		promo = q.Code
		name = name + " · " + promo
	}
	// Pre-check balance for a clear insufficient-funds response.
	w, _ := shop.GetOrCreateWallet(h.S.DB, tgID)
	if price > 0 && w.BalanceToman < price {
		c.JSON(400, gin.H{"success": false, "code": "insufficient_balance", "msg": "موجودی کافی نیست", "obj": gin.H{
			"balance": w.BalanceToman, "required": price, "shortage": price - w.BalanceToman,
		}})
		return
	}
	order := models.ShopOrder{
		TelegramUserID: tgID, TelegramChatID: tgID,
		PlanID: plan.ID, PlanName: name, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: price,
		InboundID: plan.InboundID, Status: "pending_review", Kind: kind,
		PromoCode: promo, OriginalPriceToman: orig, DiscountToman: disc,
	}
	cl, err := shop.DebitAndFulfill(h.S.DB, &order, "purchase", "tma", name)
	if err != nil {
		msg := err.Error()
		code := ""
		if strings.Contains(msg, "موجودی کافی نیست") {
			code = "insufficient_balance"
		}
		c.JSON(400, gin.H{"success": false, "code": code, "msg": msg})
		return
	}
	_ = h.S.RegenerateAndRestartXray()
	AuditTMA(h.S.DB, tgID, "tma.shop.buy_wallet", cl.SubID, gin.H{"planId": plan.ID, "price": price, "promo": promo})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"subId": cl.SubID, "email": cl.Email, "clientId": cl.ID,
		"planName": plan.Name, "priceToman": price, "discountToman": disc,
		"promoCode": promo, "kind": kind, "prorateMessage": prorateMsg,
		"shareHint": "کانفیگ جدید فعال شد",
	}})
}

// RenewWithWallet extends current subscription from wallet balance using selected plan.
func (h *TMAHandler) RenewWithWallet(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	var link models.TelegramCustomer
	if h.S.DB.Where("telegram_user_id = ?", tgID).First(&link).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "no linked client"})
		return
	}
	var req tmaBuyReq
	_ = c.ShouldBindJSON(&req)
	planID := link.AutoRenewPlanID
	if req.PlanID > 0 {
		planID = req.PlanID
	}
	if planID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "select plan"})
		return
	}
	var plan models.ShopPlan
	if h.S.DB.First(&plan, planID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	price := plan.PriceToman
	kind := "plan"
	name := plan.Name
	pq := shop.QuoteProrate(h.S.DB, tgID, plan)
	if pq.HasActive && pq.CreditToman > 0 {
		price = pq.FinalToman
		kind = "upgrade"
		name = plan.Name + " (ارتقاء/تعویض)"
		shop.StoreProrateExcess(h.S.DB, tgID, pq.ExcessToman)
	}
	order := models.ShopOrder{
		TelegramUserID: tgID, TelegramChatID: link.TelegramChatID,
		PlanID: plan.ID, PlanName: name, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: price,
		InboundID: plan.InboundID, Status: "pending_review", Kind: kind,
		OriginalPriceToman: plan.PriceToman, DiscountToman: pq.CreditToman,
	}
	cl, err := shop.DebitAndFulfill(h.S.DB, &order, "renew", "tma", name)
	if err != nil {
		msg := err.Error()
		code := ""
		if strings.Contains(msg, "موجودی کافی نیست") {
			code = "insufficient_balance"
		}
		c.JSON(400, gin.H{"success": false, "code": code, "msg": msg})
		return
	}
	_ = h.S.RegenerateAndRestartXray()
	AuditTMA(h.S.DB, tgID, "tma.renew.wallet", cl.SubID, gin.H{"planId": plan.ID, "price": price, "prorate": pq.CreditToman})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"subId": cl.SubID, "email": cl.Email, "priceToman": price, "prorateMessage": pq.Message}})
}

type tmaAutoRenewReq struct {
	Enabled bool `json:"enabled"`
	PlanID  uint `json:"planId"`
}

func (h *TMAHandler) SetAutoRenew(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	var req tmaAutoRenewReq
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid"})
		return
	}
	var link models.TelegramCustomer
	if h.S.DB.Where("telegram_user_id = ?", tgID).First(&link).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	link.AutoRenewEnabled = req.Enabled
	if req.PlanID > 0 {
		link.AutoRenewPlanID = req.PlanID
	}
	_ = h.S.DB.Save(&link).Error
	AuditTMA(h.S.DB, tgID, "tma.autorenew", strconv.FormatUint(uint64(link.ClientID), 10), req)
	c.JSON(200, gin.H{"success": true})
}

func tmaUserBlocked(h *TMAHandler, tgID int64) (bool, string) {
	token := telegram.GetSetting(h.S.DB, telegram.KeyUserToken)
	var cli *telegram.Client
	if token != "" {
		cli = telegram.NewClient(token)
	}
	blocked, msg, _ := telegram.UserGate(h.S.DB, cli, tgID)
	return blocked, msg
}

// --- Cloud Backup & Account Migration (TMA) ---

// BackupStatus returns the user's active cloud backup info (no secret code).
func (h *TMAHandler) BackupStatus(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	info := telegram.GetActiveCloudBackup(h.S.DB, tgID)
	c.JSON(200, gin.H{"success": true, "obj": info})
}

// CreateBackup generates/rotates a cloud backup code.
func (h *TMAHandler) CreateBackup(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	code, summary, expiresAt, err := telegram.CreateCloudBackupForAPI(h.S.DB, tgID, tgID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	codePrefix := code
	if len(codePrefix) > 8 {
		codePrefix = codePrefix[:8]
	}
	AuditTMA(h.S.DB, tgID, "tma.cloud_backup.create", codePrefix, gin.H{
		"clients": summary.ClientCount, "wallet": summary.Wallet,
	})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"code":      code,
		"expiresAt": expiresAt,
		"ttlDays":   models.CloudBackupDefaultTTLDays,
		"summary":   summary,
		"warning":   "این کد را در جای امن نگه دارید. با آن می‌توانید بعد از تعویض گوشی یا ریست تلگرام، اکانت را بازیابی کنید. هر بار ساخت کد جدید، کد قبلی باطل می‌شود.",
	}})
}

type tmaRestoreReq struct {
	Code string `json:"code"`
}

// RestoreBackup restores account from a backup code onto the current Telegram user.
func (h *TMAHandler) RestoreBackup(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	if blocked, msg := tmaUserBlocked(h, tgID); blocked {
		c.JSON(503, gin.H{"success": false, "msg": msg})
		return
	}
	var req tmaRestoreReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Code) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "کد پشتیبان الزامی است"})
		return
	}
	summary, err := telegram.RestoreCloudBackupForAPI(h.S.DB, req.Code, tgID, tgID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.cloud_backup.restore", "", gin.H{
		"clients": summary.ClientCount, "wallet": summary.Wallet,
	})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"summary": summary,
		"message": "بازیابی موفق — اکانت‌ها به این تلگرام لینک شدند. از تب خانه یا کانفیگ‌های من لینک را بردارید.",
	}})
}

// RevokeBackup disables the active cloud backup code for this user.
func (h *TMAHandler) RevokeBackup(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	n, err := telegram.RevokeCloudBackup(h.S.DB, tgID)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	AuditTMA(h.S.DB, tgID, "tma.cloud_backup.revoke", "", gin.H{"count": n})
	if n == 0 {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"revoked": 0, "message": "کد فعالی وجود نداشت"}})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"revoked": n, "message": "کد پشتیبان باطل شد"}})
}

// QuoteUpgrade returns prorated price for switching to a plan.
func (h *TMAHandler) QuoteUpgrade(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	planID, _ := strconv.ParseUint(c.Query("planId"), 10, 64)
	if planID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "planId required"})
		return
	}
	var plan models.ShopPlan
	if h.S.DB.First(&plan, planID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "plan not found"})
		return
	}
	q := shop.QuoteProrate(h.S.DB, tgID, plan)
	c.JSON(200, gin.H{"success": true, "obj": q})
}

// ListProfiles returns device profile categories for the linked client.
func (h *TMAHandler) ListProfiles(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	rows, err := devices.ListProfiles(h.S.DB, cl.ID)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	presets := make([]gin.H, 0, len(models.ProfilePresets))
	for _, p := range models.ProfilePresets {
		presets = append(presets, gin.H{"key": p.Key, "label": p.Label, "icon": p.Icon})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"profiles": rows, "presets": presets, "subId": cl.SubID}})
}

type tmaProfileReq struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func (h *TMAHandler) UpsertProfile(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	var req tmaProfileReq
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Key) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "key required"})
		return
	}
	row, err := devices.UpsertProfile(h.S.DB, cl.ID, req.Key, req.Label)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": row})
}

type tmaAssignDeviceReq struct {
	ProfileKey string `json:"profileKey"`
	Name       string `json:"name"`
}

// AssignDevice sets profile category + optional display name on a device row.
func (h *TMAHandler) AssignDevice(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "id required"})
		return
	}
	var req tmaAssignDeviceReq
	_ = c.ShouldBindJSON(&req)
	if err := devices.AssignDeviceProfile(h.S.DB, cl.ID, uint(id), req.ProfileKey, req.Name); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// ProfilesBundle returns profiles + devices grouped for the UI.
func (h *TMAHandler) ProfilesBundle(c *gin.Context) {
	tgID, ok := h.authUser(c)
	if !ok {
		return
	}
	cl, _, has := h.clientForTG(tgID)
	if !has {
		c.JSON(404, gin.H{"success": false, "msg": "no client"})
		return
	}
	profiles, _ := devices.ListProfiles(h.S.DB, cl.ID)
	devs, _ := devices.ListForClient(h.S.DB, cl.ID, 50)
	outDevs := make([]devices.PublicView, 0, len(devs))
	for _, d := range devs {
		if d.ProfileKey == "" && d.Active {
			// soft-guess for display only (not persisted unless user assigns)
			g := devices.GuessProfileKey(d)
			pv := devices.ToPublic(d)
			if g != "" {
				pv.ProfileKey = g
			}
			outDevs = append(outDevs, pv)
		} else {
			outDevs = append(outDevs, devices.ToPublic(d))
		}
	}
	base := strings.TrimRight(telegram.GetSetting(h.S.DB, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(telegram.GetSetting(h.S.DB, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	subURL := ""
	if base != "" && cl.SubID != "" {
		subURL = strings.TrimRight(base, "/") + "/sub/" + cl.SubID
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"profiles": profiles, "devices": outDevs, "subId": cl.SubID, "subUrl": subURL,
	}})
}
