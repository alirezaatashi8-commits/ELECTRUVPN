package shop

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProgressBar returns a text bar like [██████░░░░] 60%
func ProgressBar(pct float64, width int) string {
	if width < 5 {
		width = 10
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func FormatStatusCard(cl models.Client, inboundRemark string) string {
	used := cl.Up + cl.Down
	var trafficLine, bar string
	limB := EffectiveTrafficLimitBytes(cl)
	if limB == 0 {
		trafficLine = fmt.Sprintf("نامحدود · مصرف‌شده %s", human(used))
		bar = ProgressBar(0, 10)
	} else {
		limit := float64(limB)
		pct := float64(used) / limit * 100
		if pct > 100 {
			pct = 100
		}
		remain := limit - float64(used)
		if remain < 0 {
			remain = 0
		}
		trafficLine = fmt.Sprintf("%s / %s · باقی‌مانده ~%s", human(used), FormatQuotaLabel(cl), human(int64(remain)))
		bar = ProgressBar(pct, 10) + fmt.Sprintf(" %.0f%%", pct)
	}
	daysLine := "بدون انقضا"
	if cl.ExpiryTime > 0 {
		ms := cl.ExpiryTime - time.Now().UnixMilli()
		if ms <= 0 {
			daysLine = "منقضی شده"
		} else {
			days := int(ms / (24 * 60 * 60 * 1000))
			if days < 1 {
				days = 1
			}
			exp := time.UnixMilli(cl.ExpiryTime).Local().Format("2006-01-02 15:04")
			daysLine = fmt.Sprintf("%d روز · تا %s", days, exp)
		}
	}
	st := "فعال"
	if !cl.Enable {
		st = "غیرفعال"
	} else if cl.IsExpired() {
		st = "منقضی"
	} else if cl.IsOverQuota() {
		st = "اتمام حجم"
	}
	node := inboundRemark
	if node == "" {
		node = "—"
	}
	return fmt.Sprintf(`🎟 <b>وضعیت اشتراک من</b>

👤 %s
📡 وضعیت: <b>%s</b>
📊 ترافیک:
%s
%s
⏳ زمان: %s
🖥 سرور/نود: %s
🔑 Sub ID: <code>%s</code>`,
		esc(cl.Email), st, bar, trafficLine, daysLine, esc(node), esc(cl.SubID))
}

func human(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	u := []string{"KB", "MB", "GB", "TB"}
	i := -1
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.2f %s", f, u[i])
}

func EscText(s string) string { return esc(s) }

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func panelSetting(db *gorm.DB, key string) string {
	var r models.PanelSetting
	if db.Where("key = ?", key).First(&r).Error != nil {
		return ""
	}
	return strings.TrimSpace(r.Value)
}

// EffectiveTrafficLimitBytes returns the enforceable byte cap (trial bytes or TotalGB).
func EffectiveTrafficLimitBytes(cl models.Client) int64 {
	if cl.TrafficLimitBytes > 0 {
		return cl.TrafficLimitBytes
	}
	if cl.TotalGB > 0 {
		return cl.TotalGB * 1024 * 1024 * 1024
	}
	return 0
}

// FormatQuotaLabel human label for remaining/total traffic.
func FormatQuotaLabel(cl models.Client) string {
	lim := EffectiveTrafficLimitBytes(cl)
	if lim <= 0 {
		return "نامحدود"
	}
	if lim < 1024*1024*1024 {
		mb := float64(lim) / (1024 * 1024)
		return fmt.Sprintf("%.0f MB", mb)
	}
	return fmt.Sprintf("%d GB", lim/(1024*1024*1024))
}

// FulfillOrder creates or renews a client after payment approval (idempotent, transactional).
func FulfillOrder(db *gorm.DB, order *models.ShopOrder) (models.Client, string, error) {
	var out models.Client
	err := db.Transaction(func(tx *gorm.DB) error {
		cl, err := fulfillOrderTx(tx, order)
		if err != nil {
			return err
		}
		out = cl
		return nil
	})
	if err == nil && order != nil {
		if _, rerr := ProcessReferralReward(db, order); rerr != nil {
			// order already fulfilled — log only
		}
		_ = CommitPromoUsage(db, order)
		if order.TelegramUserID > 0 {
			_ = ConsumeProrateExcess(db, order.TelegramUserID)
		}
	}
	return out, "", err
}

// fulfillOrderTx must run inside an existing transaction. Locks the order row.
func fulfillOrderTx(tx *gorm.DB, order *models.ShopOrder) (models.Client, error) {
	if order == nil || order.ID == 0 {
		return models.Client{}, fmt.Errorf("invalid order")
	}
	var cur models.ShopOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&cur, order.ID).Error; err != nil {
		return models.Client{}, err
	}
	if cur.Status == "approved" {
		*order = cur
		if cur.ClientID == 0 {
			return models.Client{}, nil
		}
		var cl models.Client
		if err := tx.First(&cl, cur.ClientID).Error; err != nil {
			return models.Client{}, err
		}
		return cl, nil
	}
	if cur.Status != "pending_review" && cur.Status != "pending_payment" && cur.Status != "processing" {
		return models.Client{}, fmt.Errorf("وضعیت سفارش قابل تأیید نیست: %s", cur.Status)
	}
	if cur.InboundID == 0 && cur.PlanID > 0 {
		var plan models.ShopPlan
		if err := tx.First(&plan, cur.PlanID).Error; err == nil && plan.InboundID > 0 {
			cur.InboundID = plan.InboundID
			_ = tx.Model(&models.ShopOrder{}).Where("id = ?", cur.ID).Update("inbound_id", plan.InboundID).Error
		}
	}
	if cur.InboundID == 0 {
		return models.Client{}, fmt.Errorf("inbound not set on order (set plan inbound id)")
	}
	var ib models.Inbound
	if err := tx.First(&ib, cur.InboundID).Error; err != nil {
		return models.Client{}, fmt.Errorf("inbound #%d not found — update shop plan inbound", cur.InboundID)
	}
	if !ib.Enable {
		return models.Client{}, fmt.Errorf("inbound #%d is disabled", cur.InboundID)
	}
	if ib.IsLimited() {
		return models.Client{}, fmt.Errorf("inbound #%d reached its volume or time limit", cur.InboundID)
	}

	if ib.Protocol == models.ProtocolWireGuard {
		return models.Client{}, fmt.Errorf("inbound #%d is WireGuard: shop plans need a per-client key, choose a VLESS/VMess/Trojan/Shadowsocks inbound", cur.InboundID)
	}
	var link models.TelegramCustomer
	var cl models.Client
	renew := false
	if tx.Where("telegram_user_id = ?", cur.TelegramUserID).First(&link).Error == nil && link.ClientID > 0 {
		if tx.First(&cl, link.ClientID).Error == nil {
			renew = true
		}
	}
	now := time.Now()
	addDays := cur.DurationDays
	if addDays < 1 {
		addDays = 30
	}

	if renew {
		// Monthly-style renew: set quota to plan size (not accumulate).
		// Xray counters are cumulative: effective = aggregate - TrafficBase.
		// Snapshot current absolute into base so effective usage becomes ~0 without calling xray.
		cl.TotalGB = cur.TotalGB
		cl.TrafficLimitBytes = 0
		cl.TrafficBaseUp = cl.Up + cl.TrafficBaseUp
		cl.TrafficBaseDown = cl.Down + cl.TrafficBaseDown
		cl.Up = 0
		cl.Down = 0
		base := now.UnixMilli()
		// upgrade/prorate: remaining days already credited — start new period from now
		if cur.Kind != "upgrade" && cl.ExpiryTime > base {
			base = cl.ExpiryTime
		}
		cl.ExpiryTime = base + int64(addDays)*24*60*60*1000
		cl.Enable = true
		cl.SubscriptionEnabled = true
		cl.SubscriptionStatus = "active"
		cl.SubscriptionRevokedAt = nil
		if cur.PlanName != "" {
			cl.SubscriptionName = cur.PlanName
		}
		if cur.InboundID > 0 {
			cl.InboundID = cur.InboundID
		}
		if err := tx.Save(&cl).Error; err != nil {
			return models.Client{}, err
		}
	} else {
		sub := strings.ReplaceAll(uuid.NewString(), "-", "")
		if len(sub) > 32 {
			sub = sub[:32]
		}
		email := fmt.Sprintf("tg%d_%d", cur.TelegramUserID, now.Unix())
		cl = models.Client{
			InboundID: cur.InboundID, Email: email,
			UUID: uuid.NewString(), Password: shopClientPassword(ib),
			Enable: true, TotalGB: cur.TotalGB,
			ExpiryTime:               now.UnixMilli() + int64(addDays)*24*60*60*1000,
			SubID:                    sub,
			SubscriptionEnabled:      true,
			SubscriptionName:         cur.PlanName,
			SubscriptionStatus:       "active",
			SubscriptionTokenVersion: 1,
			SubscriptionAutoUpdate:   true,
			PurchasedAt:              now,
			SubscriptionRefreshMin:   15,
			SubscriptionAddressType:  "all",
		}
		if err := tx.Create(&cl).Error; err != nil {
			return models.Client{}, err
		}
		_ = tx.Where("telegram_user_id = ?", cur.TelegramUserID).Delete(&models.TelegramCustomer{}).Error
		if err := tx.Create(&models.TelegramCustomer{
			TelegramUserID: cur.TelegramUserID, TelegramChatID: cur.TelegramChatID,
			ClientID: cl.ID, SubID: cl.SubID, Email: cl.Email,
		}).Error; err != nil {
			return models.Client{}, err
		}
	}

	cur.ClientID = cl.ID
	cur.Status = "approved"
	tnow := now
	cur.ReviewedAt = &tnow
	if order.ReviewedBy != "" {
		cur.ReviewedBy = order.ReviewedBy
	}
	if err := tx.Save(&cur).Error; err != nil {
		return models.Client{}, err
	}
	*order = cur
	return cl, nil
}

// DebitAndFulfill debits wallet and fulfills plan order in one transaction.
// Pass order with ID=0 so the row is created only after debit succeeds (no orphan pending_review).
func DebitAndFulfill(db *gorm.DB, order *models.ShopOrder, debitKind, ref, note string) (models.Client, error) {
	if order == nil {
		return models.Client{}, fmt.Errorf("nil order")
	}
	var out models.Client
	err := db.Transaction(func(tx *gorm.DB) error {
		if order.ID != 0 {
			// Re-check the order under a row lock BEFORE taking money. Without it a
			// second payment (double tap, or an admin approving the receipt at the
			// same moment) was debited again and then "succeeded" on the already
			// approved order, charging the customer twice for one plan.
			var chk models.ShopOrder
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&chk, order.ID).Error; err != nil {
				return err
			}
			if chk.Status == "approved" {
				return fmt.Errorf("این سفارش قبلاً پرداخت و تأیید شده است")
			}
			if chk.Status != "pending_review" && chk.Status != "pending_payment" && chk.Status != "processing" {
				return fmt.Errorf("وضعیت سفارش قابل پرداخت نیست: %s", chk.Status)
			}
		}
		if order.PriceToman > 0 {
			if _, err := debitWalletTx(tx, order.TelegramUserID, order.PriceToman, debitKind, ref, note); err != nil {
				return err
			}
		}
		if order.ID == 0 {
			order.Status = "pending_review"
			if err := tx.Create(order).Error; err != nil {
				return err
			}
		}
		cl, err := fulfillOrderTx(tx, order)
		if err != nil {
			return err
		}
		out = cl
		return nil
	})
	if err == nil && order != nil {
		_, _ = ProcessReferralReward(db, order)
		_ = CommitPromoUsage(db, order)
	}
	return out, err
}

// RecoverStuckOrders moves processing orders older than 10 minutes back to pending_review.
func RecoverStuckOrders(db *gorm.DB) int {
	cut := time.Now().Add(-10 * time.Minute)
	res := db.Model(&models.ShopOrder{}).
		Where("status = ? AND updated_at < ?", "processing", cut).
		Update("status", "pending_review")
	if res.Error != nil {
		return 0
	}
	return int(res.RowsAffected)
}

// PaymentInfo reads card details from panel settings.
func PaymentInfo(db *gorm.DB) (card, holder, instructions string, enabled bool) {
	card = panelSetting(db, models.ShopCardNumberKey)
	holder = panelSetting(db, models.ShopCardHolderKey)
	instructions = panelSetting(db, models.ShopInstructionsKey)
	enabled = panelSetting(db, models.ShopEnabledKey) != "0"
	if instructions == "" {
		instructions = "مبلغ را کارت‌به‌کارت واریز کنید، سپس عکس رسید را در همین چت ارسال کنید."
	}
	return
}

// shopClientPassword returns a secret valid for the inbound's protocol. A plain
// UUID is wrong for Shadowsocks-2022 (needs a base64 key of the exact length);
// xray then rejects the whole generated config and every client goes down.
func shopClientPassword(ib models.Inbound) string {
	if ib.Protocol != models.ProtocolShadowsocks {
		return uuid.NewString()
	}
	var ss models.ShadowsocksSettings
	_ = json.Unmarshal([]byte(ib.Settings), &ss)
	n := 0
	switch ss.Method {
	case "2022-blake3-aes-128-gcm":
		n = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		n = 32
	}
	if n > 0 {
		b := make([]byte, n)
		if _, err := rand.Read(b); err == nil {
			return base64.StdEncoding.EncodeToString(b)
		}
	}
	return uuid.NewString()
}
