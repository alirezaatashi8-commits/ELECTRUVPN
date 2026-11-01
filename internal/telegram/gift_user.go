package telegram

import (
	"fmt"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
)

func (b *UserBot) sendGiftShop(cli *Client, chatID, fromID int64) {
	var plans []models.ShopPlan
	b.DB.Where("enabled = ?", true).Order("sort_order asc, id asc").Find(&plans)
	bought, redeemed := shop.ListUserGifts(b.DB, fromID)
	var sb strings.Builder
	sb.WriteString("🎁 <b>کارت هدیه</b>\n\nکارت هدیه بخرید و کد را به دوست‌تان بدهید، یا کد دریافتی را فعال کنید.\n\n")
	show := bought
	if len(show) > 5 {
		show = show[:5]
	}
	if len(show) > 0 {
		sb.WriteString("<b>کارت‌های خریده‌شده:</b>\n")
		for _, g := range show {
			sb.WriteString(fmt.Sprintf("• <code>%s</code> · %s · <b>%s</b>\n", g.Code, g.PlanName, g.Status))
		}
		sb.WriteString("\n")
	}
	if len(redeemed) > 0 {
		sb.WriteString("<b>فعال‌شده:</b>\n")
		for i, g := range redeemed {
			if i >= 3 {
				break
			}
			sb.WriteString(fmt.Sprintf("• %s · %s\n", g.PlanName, g.Code))
		}
		sb.WriteString("\n")
	}
	var rows [][]InlineKeyboardButton
	rows = append(rows, []InlineKeyboardButton{{Text: "♻️ فعال‌سازی کد هدیه", CallbackData: "gift_redeem"}})
	for _, p := range plans {
		rows = append(rows, []InlineKeyboardButton{{
			Text:         fmt.Sprintf("🎁 %s — %s ت", p.Name, formatToman(p.PriceToman)),
			CallbackData: fmt.Sprintf("giftplan:%d", p.ID),
		}})
	}
	rows = append(rows, []InlineKeyboardButton{{Text: "🏠 منو", CallbackData: "menu"}})
	_ = cli.SendMessageMarkup(chatID, sb.String(), &InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (b *UserBot) startGiftRedeem(cli *Client, fromID, chatID int64) {
	SetPendingGiftRedeem(fromID, true)
	_ = cli.SendMessageMarkup(chatID, "کد هدیه را بفرستید (مثلاً <code>EVG-A1B2-C3D4E5</code>)\nبرای لغو: /cancel", b.mainMenu())
}

func (b *UserBot) redeemGiftCode(cli *Client, fromID, chatID int64, code string) {
	SetPendingGiftRedeem(fromID, false)
	cl, gift, err := shop.RedeemGift(b.DB, code, fromID, chatID)
	if err != nil {
		_ = cli.SendMessageMarkup(chatID, "❌ "+err.Error(), b.mainMenu())
		return
	}
	if b.AfterClientChange != nil {
		b.AfterClientChange()
	}
	msg := fmt.Sprintf("✅ <b>کارت هدیه فعال شد</b>\nپلن: %s\nحجم: %d GB · %d روز\nSub: <code>%s</code>\nاز «وضعیت اشتراک» و «کانفیگ‌های من» استفاده کنید.",
		gift.PlanName, gift.TotalGB, gift.DurationDays, cl.SubID)
	_ = cli.SendMessageMarkup(chatID, msg, b.mainMenu())
}

func (b *UserBot) startGiftPurchase(cli *Client, fromID, chatID int64, from *User, planID uint) {
	var plan models.ShopPlan
	if b.DB.First(&plan, planID).Error != nil || !plan.Enabled {
		_ = cli.SendMessage(chatID, "پلن پیدا نشد.")
		return
	}
	_ = b.DB.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status IN ?", fromID, []string{"pending_payment", "pending_review"}).
		Update("status", "cancelled").Error
	name := ""
	if from != nil {
		name = from.FirstName
		if from.Username != "" {
			name += " @" + from.Username
		}
	}
	order := models.ShopOrder{
		TelegramUserID: fromID, TelegramChatID: chatID, TelegramName: name,
		PlanID: plan.ID, PlanName: "هدیه: " + plan.Name, TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: plan.PriceToman,
		InboundID: plan.InboundID, Status: "pending_payment", Kind: "gift",
	}
	if err := b.DB.Create(&order).Error; err != nil {
		_ = cli.SendMessage(chatID, "خطا در ثبت سفارش هدیه.")
		return
	}
	card, holder, instructions, _ := shop.PaymentInfo(b.DB)
	msg := fmt.Sprintf(`🎁 <b>سفارش کارت هدیه #%d</b>
پلن: %s
مبلغ: <b>%s تومان</b>

💳 کارت:
<code>%s</code>
%s

%s

پس از واریز، <b>عکس رسید</b> را بفرستید. بعد از تأیید، کد هدیه برایتان ارسال می‌شود.`,
		order.ID, plan.Name, formatToman(plan.PriceToman), card, holder, instructions)
	_ = cli.SendMessageMarkup(chatID, msg, &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "◀️ بازگشت", CallbackData: "gift_shop"}},
	}})
}
