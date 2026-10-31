package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

// AdminGiftCommand: /giftissue TGID PLANID | /giftrevoke CODE | /gifts | /setloyalty 10
func AdminGiftCommand(db *gorm.DB, cmd string, parts []string) string {
	switch cmd {
	case "gifts", "listgifts":
		rows := shop.ListGiftCards(db, 25)
		if len(rows) == 0 {
			return "کارتی ثبت نشده.\n" + adminGiftHelp()
		}
		var b strings.Builder
		b.WriteString("🎁 <b>کارت‌های هدیه</b>\n\n")
		for _, g := range rows {
			b.WriteString(fmt.Sprintf("• <code>%s</code> · %s · <b>%s</b> · خریدار %d\n", g.Code, g.PlanName, g.Status, g.BuyerTGID))
		}
		return b.String()
	case "giftrevoke", "revokegift":
		if len(parts) < 2 {
			return "مثال: /giftrevoke EVG-XXXX-XXXXXX"
		}
		if err := shop.RevokeGift(db, parts[1]); err != nil {
			return "❌ " + err.Error()
		}
		return "✅ کد باطل شد."
	case "giftissue", "issuegift":
		if len(parts) < 3 {
			return adminGiftHelp()
		}
		tgID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || tgID <= 0 {
			return "آیدی تلگرام نامعتبر"
		}
		planID, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil || planID == 0 {
			return "شناسه پلن نامعتبر"
		}
		var plan models.ShopPlan
		if db.First(&plan, planID).Error != nil {
			return "پلن پیدا نشد — /plans در پنل"
		}
		g, err := shop.IssueGiftFromPlan(db, plan, tgID, 0, true, "admin-issue", 90)
		if err != nil {
			return "❌ " + err.Error()
		}
		// notify user
		ut := GetSetting(db, KeyUserToken)
		if ut != "" {
			_ = NewClient(ut).SendMessage(tgID, fmt.Sprintf("🎁 ادمین یک کارت هدیه برایتان صادر کرد:\n<code>%s</code>\nپلن: %s\nبا /redeem یا منوی کارت هدیه فعال کنید.", g.Code, g.PlanName))
		}
		return fmt.Sprintf("✅ صادر شد\nکد: <code>%s</code>\nبرای TG %d · %s", g.Code, tgID, g.PlanName)
	case "setprorate":
		if len(parts) < 2 {
			en := shop.ProrateEnabled(db)
			return fmt.Sprintf("Prorate الان: %v\n/setprorate on|off", en)
		}
		on := parts[1] == "on" || parts[1] == "1"
		val := "0"
		if on {
			val = "1"
		}
		_ = shop.SaveProrateEnabled(db, on)
		return fmt.Sprintf("✅ Prorate: %s", val)
	case "setloyalty":
		if len(parts) < 2 {
			return fmt.Sprintf("تخفیف وفاداری فعلی: %d%%\nمثال: /setloyalty 15", shop.LoyaltyDiscountPct(db))
		}
		if parts[1] == "off" {
			_ = shop.SaveLoyalty(db, false, 0)
			return "✅ تخفیف وفاداری خاموش شد"
		}
		pct, err := strconv.Atoi(parts[1])
		if err != nil || pct < 0 || pct > 50 {
			return "درصد ۰ تا ۵۰"
		}
		_ = shop.SaveLoyalty(db, true, pct)
		return fmt.Sprintf("✅ تخفیف وفاداری %d%%", pct)
	default:
		return adminGiftHelp()
	}
}

func adminGiftHelp() string {
	return `🎁 <b>کارت هدیه / وفاداری</b>
<code>/giftissue 123456789 1</code> — صدور برای کاربر (آیدی · پلن)
<code>/giftrevoke EVG-...</code>
<code>/gifts</code>
<code>/setloyalty 10</code> — درصد تخفیف تمدید هوشمند`
}
