package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

func NotifyReferrerForOrder(db *gorm.DB, order *models.ShopOrder) {
	if order == nil || order.ID == 0 {
		return
	}
	var rw models.ReferralReward
	if db.Where("order_id = ?", order.ID).First(&rw).Error != nil || rw.RewardToman <= 0 {
		return
	}
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	chat := rw.ReferrerTelegramID
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", rw.ReferrerTelegramID).First(&link).Error == nil && link.TelegramChatID != 0 {
		chat = link.TelegramChatID
	}
	w, _ := shop.GetOrCreateWallet(db, rw.ReferrerTelegramID)
	msg := fmt.Sprintf("🎁 <b>پاداش معرف واریز شد</b>\n\n+%s تومان (%d%% از سفارش #%d)\nموجودی کیف پول: <b>%s</b> تومان\n\nاز «💰 کیف پول» برای تمدید استفاده کنید.",
		formatToman(rw.RewardToman), rw.Percent, order.ID, formatToman(w.BalanceToman))
	_ = NewClient(token).SendMessageMarkup(chat, msg, &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "💰 کیف پول", CallbackData: "wallet"}},
		{{Text: "🔗 لینک دعوت من", CallbackData: "referral"}},
	}})
}

func (b *UserBot) handleStartPayload(cli *Client, fromID, chatID int64, parts []string) {
	payload := ""
	if len(parts) >= 2 {
		payload = strings.TrimSpace(strings.Join(parts[1:], " "))
	}
	// Deep links from subscription landing / external pages
	switch strings.ToLower(payload) {
	case "shop", "renew", "buy":
		b.sendShop(cli, chatID, fromID)
		return
	case "wallet", "topup", "charge":
		b.sendWallet(cli, fromID, chatID)
		return
	case "status":
		_ = cli.SendMessageMarkup(chatID, b.statusForTelegram(fromID), b.mainMenu())
		return
	case "config", "configs", "myconfigs":
		b.sendMyConfigs(cli, fromID, chatID)
		return
	case "trial":
		b.giveTrial(cli, fromID, chatID)
		return
	}
	if ref := shop.ParseReferrerFromStart(payload); ref > 0 {
		_ = shop.AttributeReferrer(b.DB, fromID, ref, shop.ReferralCodeForUser(ref))
	}
	cfg := shop.LoadReferralConfig(b.DB)
	msg := "👋 به ربات فروش و وضعیت VPN خوش آمدید.\nاز منوی پایین صفحه یا دکمه‌های شیشه‌ای استفاده کنید:"
	if cfg.Enabled && cfg.Percent > 0 {
		msg += fmt.Sprintf("\n\n🔗 با دعوت دوستان <b>%d%%</b> از خریدشان به کیف پول شما واریز می‌شود.\nاز منو «🔗 دعوت دوستان» لینک اختصاصی بگیرید.", cfg.Percent)
	}
	_ = cli.SendMessageFull(chatID, msg, b.mainMenu(), b.replyMainKeyboard())
}

func (b *UserBot) sendReferral(cli *Client, fromID, chatID int64) {
	cfg := shop.LoadReferralConfig(b.DB)
	if !cfg.Enabled {
		_ = cli.SendMessageMarkup(chatID, "سیستم دعوت فعلاً غیرفعال است.", b.mainMenu())
		return
	}
	// cache bot username if empty
	if cfg.BotUsername == "" {
		if u, err := cli.GetMe(); err == nil && u != "" {
			_ = shop.SaveReferralConfig(b.DB, shop.ReferralConfig{
				Enabled: cfg.Enabled, Percent: cfg.Percent,
				FirstPurchaseOnly: cfg.FirstPurchaseOnly, MinOrderToman: cfg.MinOrderToman,
				BotUsername: u,
			})
			cfg = shop.LoadReferralConfig(b.DB)
		}
	}
	link := shop.BuildReferralLink(b.DB, fromID)
	st := shop.GetReferralStats(b.DB, fromID)
	msg := "🔗 <b>لینک دعوت اختصاصی شما</b>\n\n"
	msg += "<code>" + link + "</code>\n\n"
	if cfg.Percent > 0 {
		msg += fmt.Sprintf("با هر خرید دوست‌تان، <b>%d%%</b> مبلغ به کیف پول شما واریز می‌شود.\n", cfg.Percent)
	} else {
		msg += "درصد پاداش فعلاً <b>۰</b> است (فقط ردیابی دعوت).\n"
	}
	if cfg.FirstPurchaseOnly {
		msg += "پاداش فقط برای <b>اولین خرید</b> هر دعوت‌شده محاسبه می‌شود.\n"
	}
	msg += fmt.Sprintf("\n👥 دعوت‌شده‌ها: <b>%d</b>\n💰 پاداش‌های واریزشده: <b>%d</b>\n💵 مجموع درآمد: <b>%s</b> تومان",
		st.InviteCount, st.RewardCount, formatToman(st.TotalEarned))
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "🎁 تست رایگان", CallbackData: "trial"}, {Text: "🛒 خرید", CallbackData: "shop"}},
		{{Text: "💰 کیف پول", CallbackData: "wallet"}},
		{{Text: "🏠 منو", CallbackData: "menu"}},
	}}
	_ = cli.SendMessageMarkup(chatID, msg, kb)
}

// AdminReferralStatus formats current affiliate settings for admin bot.
func AdminReferralStatus(db *gorm.DB) string {
	cfg := shop.LoadReferralConfig(db)
	en := "خاموش"
	if cfg.Enabled {
		en = "روشن"
	}
	fpo := "خیر"
	if cfg.FirstPurchaseOnly {
		fpo = "بله"
	}
	var rewards, earned int64
	db.Model(&models.ReferralReward{}).Count(&rewards)
	db.Model(&models.ReferralReward{}).Select("COALESCE(SUM(reward_toman),0)").Scan(&earned)
	var attrs int64
	db.Model(&models.ReferralAttribution{}).Count(&attrs)
	return fmt.Sprintf(`🎁 <b>سیستم معرف (Referral)</b>

وضعیت: <b>%s</b>
درصد پاداش: <b>%d%%</b>
فقط خرید اول: <b>%s</b>
حداقل مبلغ سفارش: <b>%s</b> تومان
یوزرنیم ربات: @%s

آمار کل: %d دعوت · %d پاداش · %s تومان

تنظیم:
<code>/setreferral 10</code> — فعال با ۱۰٪
<code>/setreferral 0</code> — فعال بدون پاداش نقدی
<code>/setreferral off</code> — خاموش
<code>/setreferral first on|off</code> — فقط خرید اول
<code>/setreferral min 50000</code> — حداقل مبلغ
<code>/setreferral bot MyBotUser</code> — یوزرنیم ربات کاربر
`, en, cfg.Percent, fpo, formatToman(cfg.MinOrderToman), cfg.BotUsername,
		attrs, rewards, formatToman(earned))
}

func AdminSetReferral(db *gorm.DB, parts []string) string {
	if len(parts) < 2 {
		return AdminReferralStatus(db)
	}
	cfg := shop.LoadReferralConfig(db)
	arg := strings.ToLower(parts[1])
	switch arg {
	case "off", "disable", "0off":
		cfg.Enabled = false
		shop.SaveReferralConfig(db, cfg)
		return "✅ سیستم معرف خاموش شد.\n\n" + AdminReferralStatus(db)
	case "on", "enable":
		cfg.Enabled = true
		if cfg.Percent == 0 {
			cfg.Percent = 10
		}
		shop.SaveReferralConfig(db, cfg)
		return "✅ سیستم معرف روشن شد.\n\n" + AdminReferralStatus(db)
	case "first":
		if len(parts) < 3 {
			return "مثال: /setreferral first on"
		}
		cfg.FirstPurchaseOnly = parts[2] == "on" || parts[2] == "1" || parts[2] == "true"
		shop.SaveReferralConfig(db, cfg)
		return "✅ ذخیره شد.\n\n" + AdminReferralStatus(db)
	case "min":
		if len(parts) < 3 {
			return "مثال: /setreferral min 50000"
		}
		v, _ := strconv.ParseInt(parts[2], 10, 64)
		cfg.MinOrderToman = v
		shop.SaveReferralConfig(db, cfg)
		return "✅ حداقل مبلغ ذخیره شد.\n\n" + AdminReferralStatus(db)
	case "bot":
		if len(parts) < 3 {
			return "مثال: /setreferral bot MyBotUsername"
		}
		cfg.BotUsername = strings.TrimPrefix(parts[2], "@")
		shop.SaveReferralConfig(db, cfg)
		return "✅ یوزرنیم ذخیره شد.\n\n" + AdminReferralStatus(db)
	default:
		pct, err := strconv.Atoi(parts[1])
		if err != nil || pct < 0 || pct > 50 {
			return "درصد باید عدد ۰ تا ۵۰ باشد.\nمثال: /setreferral 15"
		}
		cfg.Enabled = true
		cfg.Percent = pct
		shop.SaveReferralConfig(db, cfg)
		if pct == 0 {
			return "✅ سیستم روشن است اما پاداش نقدی ۰٪ (فقط ردیابی).\n\n" + AdminReferralStatus(db)
		}
		return fmt.Sprintf("✅ پاداش معرف روی <b>%d%%</b> تنظیم شد.\n\n%s", pct, AdminReferralStatus(db))
	}
}
