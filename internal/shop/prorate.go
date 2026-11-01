package shop

import (
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// ProrateQuote describes credit applied when switching plans mid-cycle.
type ProrateQuote struct {
	Enabled        bool   `json:"enabled"`
	HasActive      bool   `json:"hasActive"`
	RemainDays     int    `json:"remainDays"`
	CreditToman    int64  `json:"creditToman"`
	NewPlanPrice   int64  `json:"newPlanPrice"`
	FinalToman     int64  `json:"finalToman"`  // max(0, new - credit) before promo
	ExcessToman    int64  `json:"excessToman"` // credit above new plan → wallet top-up on fulfill
	DailyRateToman int64  `json:"dailyRateToman"`
	SourcePlan     string `json:"sourcePlan,omitempty"`
	Message        string `json:"message"`
}

const ProrateEnabledKey = "prorate_enabled" // "0" to disable; default on

func ProrateEnabled(db *gorm.DB) bool {
	v := panelSetting(db, ProrateEnabledKey)
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

// lastPaidPlanOrder finds the most recent approved plan (or gift-fulfilled) order for pricing basis.
func lastPaidPlanOrder(db *gorm.DB, tgUserID int64) (models.ShopOrder, bool) {
	var o models.ShopOrder
	err := db.Where("telegram_user_id = ? AND status = ? AND kind IN ? AND price_toman > 0 AND duration_days > 0",
		tgUserID, "approved", []string{"plan", "gift"}).
		Order("id desc").First(&o).Error
	return o, err == nil
}

func activeClientForTG(db *gorm.DB, tgUserID int64) (models.Client, bool) {
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", tgUserID).First(&link).Error != nil || link.ClientID == 0 {
		return models.Client{}, false
	}
	var cl models.Client
	if db.First(&cl, link.ClientID).Error != nil {
		return models.Client{}, false
	}
	return cl, true
}

func remainWholeDays(cl models.Client, now time.Time) int {
	if cl.ExpiryTime <= 0 {
		return 0
	}
	ms := cl.ExpiryTime - now.UnixMilli()
	if ms <= 0 {
		return 0
	}
	days := int(ms / (24 * 60 * 60 * 1000))
	if days < 1 && ms > 0 {
		days = 1 // less than a day still counts as 1 for fairness
	}
	return days
}

// QuoteProrate computes mid-cycle credit when moving to newPlan.
// Credit = min(remainDays, lastDuration) * (lastPaid / lastDuration).
// Final before promo = max(0, newPlan.Price - credit). Excess credit returned as ExcessToman (wallet on fulfill).
func QuoteProrate(db *gorm.DB, tgUserID int64, newPlan models.ShopPlan) ProrateQuote {
	q := ProrateQuote{
		Enabled: ProrateEnabled(db), NewPlanPrice: newPlan.PriceToman,
		FinalToman: newPlan.PriceToman,
	}
	if !q.Enabled || tgUserID == 0 || newPlan.ID == 0 {
		q.Message = "بدون اعتبار باقی‌مانده"
		return q
	}
	cl, ok := activeClientForTG(db, tgUserID)
	if !ok || !cl.Enable {
		q.Message = "اشتراک فعال برای محاسبه باقی‌مانده نیست"
		return q
	}
	now := time.Now()
	if cl.IsExpired() {
		q.Message = "اشتراک منقضی — بدون اعتبار Prorate"
		return q
	}
	remain := remainWholeDays(cl, now)
	if remain <= 0 {
		q.Message = "روز باقی‌مانده‌ای نیست"
		return q
	}
	q.HasActive = true
	q.RemainDays = remain

	daily := int64(0)
	src := cl.SubscriptionName
	if o, ok := lastPaidPlanOrder(db, tgUserID); ok {
		daily = o.PriceToman / int64(o.DurationDays)
		if daily < 1 && o.PriceToman > 0 {
			daily = 1
		}
		src = o.PlanName
		// Cap remain used for credit at original duration (can't get more than paid)
		if remain > o.DurationDays {
			remain = o.DurationDays
			q.RemainDays = remain
		}
	} else {
		// Fallback: match shop plan by GB + rough duration from remaining window
		var plans []models.ShopPlan
		db.Where("enabled = ?", true).Find(&plans)
		for _, p := range plans {
			if p.PriceToman <= 0 || p.DurationDays <= 0 {
				continue
			}
			if cl.TotalGB > 0 && p.TotalGB == cl.TotalGB {
				daily = p.PriceToman / int64(p.DurationDays)
				src = p.Name
				break
			}
		}
		if daily <= 0 && len(plans) > 0 {
			// cheapest daily rate among enabled plans as conservative credit
			for _, p := range plans {
				if p.DurationDays <= 0 || p.PriceToman <= 0 {
					continue
				}
				d := p.PriceToman / int64(p.DurationDays)
				if d > 0 && (daily == 0 || d < daily) {
					daily = d
					src = p.Name
				}
			}
		}
	}
	q.DailyRateToman = daily
	q.SourcePlan = src
	if daily <= 0 {
		q.Message = "مبنایی برای ارزش روز باقی‌مانده پیدا نشد"
		q.FinalToman = newPlan.PriceToman
		return q
	}

	credit := daily * int64(remain)
	if credit > newPlan.PriceToman*3 {
		// safety cap: never credit more than 3× new plan face value
		credit = newPlan.PriceToman * 3
	}
	q.CreditToman = credit
	if credit >= newPlan.PriceToman {
		q.FinalToman = 0
		q.ExcessToman = credit - newPlan.PriceToman
		q.Message = fmt.Sprintf("ارتقاء/تعویض: %d روز باقی ≈ %s تومان اعتبار → پلن جدید رایگان", remain, formatInt(newPlan.PriceToman))
		if q.ExcessToman > 0 {
			q.Message += fmt.Sprintf(" · مازاد %s تومان به کیف پول", formatInt(q.ExcessToman))
		}
	} else {
		q.FinalToman = newPlan.PriceToman - credit
		q.Message = fmt.Sprintf("اعتبار %d روز باقی: %s تومان از %s کم شد → قابل پرداخت %s تومان",
			remain, formatInt(credit), formatInt(newPlan.PriceToman), formatInt(q.FinalToman))
	}
	return q
}

func formatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	// thin spacing for readability
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ApplyProrateToOrder mutates price fields; call before Create.
// Promo should be applied AFTER prorate on the reduced base (or on original — we apply on post-prorate base).
func ApplyProrateToOrder(db *gorm.DB, tgUserID int64, plan models.ShopPlan, order *models.ShopOrder) ProrateQuote {
	q := QuoteProrate(db, tgUserID, plan)
	if !q.HasActive || q.CreditToman <= 0 {
		return q
	}
	order.OriginalPriceToman = plan.PriceToman
	order.PriceToman = q.FinalToman
	// fold credit into DiscountToman so admin sees total reduction
	order.DiscountToman += q.CreditToman
	if q.CreditToman > 0 {
		tag := fmt.Sprintf(" · Prorate -%s", formatInt(q.CreditToman))
		if !strings.Contains(order.PlanName, "Prorate") {
			order.PlanName = order.PlanName + tag
		}
	}
	// Store excess in note via PromoCode suffix is wrong; use AdminNote-style — ShopOrder has no note field.
	// Excess applied on fulfill via PanelSetting one-shot key.
	if q.ExcessToman > 0 {
		key := fmt.Sprintf("prorate_excess_%d", tgUserID)
		_ = setPanel(db, key, fmt.Sprintf("%d", q.ExcessToman))
	}
	return q
}

// ConsumeProrateExcess credits wallet after successful fulfill if excess was stored.
func ConsumeProrateExcess(db *gorm.DB, tgUserID int64) int64 {
	key := fmt.Sprintf("prorate_excess_%d", tgUserID)
	v := panelSetting(db, key)
	if v == "" {
		return 0
	}
	_ = setPanel(db, key, "")
	var n int64
	fmt.Sscanf(v, "%d", &n)
	if n <= 0 {
		return 0
	}
	if _, err := CreditWallet(db, tgUserID, n, "prorate_excess", "upgrade", "مازاد اعتبار ارتقاء پلن"); err != nil {
		return 0
	}
	return n
}

func StoreProrateExcess(db *gorm.DB, tgUserID int64, excess int64) {
	key := fmt.Sprintf("prorate_excess_%d", tgUserID)
	if excess <= 0 {
		_ = setPanel(db, key, "")
		return
	}
	_ = setPanel(db, key, fmt.Sprintf("%d", excess))
}

func SaveProrateEnabled(db *gorm.DB, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return setPanel(db, ProrateEnabledKey, v)
}
