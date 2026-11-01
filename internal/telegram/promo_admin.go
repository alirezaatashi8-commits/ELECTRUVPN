package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

// AdminPromoCommand handles /addpromo /disablepromo /promos
// /addpromo CODE percent|fixed VALUE [maxUses] [YYYY-MM-DD] [minOrder]
// /disablepromo CODE
// /promos
func AdminPromoCommand(db *gorm.DB, cmd string, parts []string) string {
	switch cmd {
	case "promos", "listpromo", "promo":
		if len(parts) >= 2 && cmd == "promo" {
			// fallthrough to help if only /promo
			if parts[1] != "" && !strings.EqualFold(parts[1], "list") {
				return adminPromoHelp()
			}
		}
		rows := shop.ListPromos(db, 30)
		if len(rows) == 0 {
			return "هیچ کد تخفیفی ثبت نشده.\n\n" + adminPromoHelp()
		}
		var b strings.Builder
		b.WriteString("🏷 <b>کدهای تخفیف</b>\n\n")
		now := time.Now().UTC()
		for _, p := range rows {
			st := "🟢"
			if !p.IsActive {
				st = "🔴"
			} else if p.ExpiresAt != nil && now.After(*p.ExpiresAt) {
				st = "⏰"
			}
			exp := "∞"
			if p.ExpiresAt != nil {
				exp = p.ExpiresAt.Format("2006-01-02")
			}
			uses := fmt.Sprintf("%d", p.UsedCount)
			if p.MaxUses > 0 {
				uses = fmt.Sprintf("%d/%d", p.UsedCount, p.MaxUses)
			} else {
				uses += "/∞"
			}
			b.WriteString(fmt.Sprintf("%s <code>%s</code> · %s %d · %s · تا %s\n",
				st, p.Code, p.DiscountType, p.DiscountValue, uses, exp))
		}
		b.WriteString("\n" + adminPromoHelp())
		return b.String()
	case "disablepromo":
		if len(parts) < 2 {
			return "مثال: /disablepromo YALDA20"
		}
		if err := shop.AdminDisablePromo(db, parts[1]); err != nil {
			return "❌ " + err.Error()
		}
		return "✅ کد <b>" + shop.NormalizePromoCode(parts[1]) + "</b> غیرفعال شد."
	case "addpromo":
		if len(parts) < 4 {
			return adminPromoHelp()
		}
		code := parts[1]
		dtype := parts[2]
		val, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return "مقدار تخفیف نامعتبر"
		}
		maxUses := 0
		if len(parts) >= 5 {
			maxUses, _ = strconv.Atoi(parts[4])
		}
		var exp *time.Time
		if len(parts) >= 6 && parts[5] != "-" && parts[5] != "0" {
			t, err := time.Parse("2006-01-02", parts[5])
			if err != nil {
				return "تاریخ انقضا باید YYYY-MM-DD باشد"
			}
			t = t.UTC().Add(23*time.Hour + 59*time.Minute)
			exp = &t
		}
		minOrder := int64(0)
		if len(parts) >= 7 {
			minOrder, _ = strconv.ParseInt(parts[6], 10, 64)
		}
		p, err := shop.AdminCreatePromo(db, code, dtype, val, maxUses, exp, minOrder, 1)
		if err != nil {
			return "❌ " + err.Error()
		}
		return fmt.Sprintf("✅ کد <code>%s</code> ذخیره شد.\nنوع: %s · مقدار: %d\nظرفیت: %d · حداقل سفارش: %d\nهر کاربر: حداکثر %d بار",
			p.Code, p.DiscountType, p.DiscountValue, p.MaxUses, p.MinOrderToman, p.PerUserLimit)
	default:
		return adminPromoHelp()
	}
}

func adminPromoHelp() string {
	return `🏷 <b>کد تخفیف</b>
<code>/addpromo YALDA20 percent 20 100 2026-12-30</code>
<code>/addpromo OFF50 fixed 50000 50 2026-11-01 100000</code>
(کد، نوع percent|fixed، مقدار، ظرفیت، انقضا، حداقل‌مبلغ)
<code>/disablepromo YALDA20</code>
<code>/promos</code>`
}
