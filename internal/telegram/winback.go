package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

// RunWinbackCampaigns sends return discounts to churned users.
func RunWinbackCampaigns(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	runWinbackPass(db)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runWinbackPass(db)
		}
	}
}

func runWinbackPass(db *gorm.DB) {
	cfg := shop.LoadWinbackConfig(db)
	if !cfg.Enabled || cfg.DiscountPct <= 0 {
		return
	}
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	cli := NewClient(token)
	eligible := shop.EligibleWinbackClients(db, cfg)
	for _, item := range eligible {
		chatID := item.Link.TelegramChatID
		if chatID == 0 {
			chatID = item.Link.TelegramUserID
		}
		if chatID == 0 {
			continue
		}
		// mark sent first to avoid double-send
		key := shop.WinbackOfferKey(item.Cl.ID)
		if err := db.Create(&models.PanelSetting{Key: key, Value: time.Now().Format(time.RFC3339)}).Error; err != nil {
			continue
		}
		msg := cfg.Message
		if strings.TrimSpace(msg) == "" {
			msg = fmt.Sprintf(`👋 دلمان برایتان تنگ شده!

اشتراک «%s» چند روز است تمام شده.
به‌عنوان پیشنهاد بازگشت، <b>%d%% تخفیف</b> روی پلن‌های ویژه برای شما فعال شد.

از دکمه زیر خرید کنید و تخفیف خودکار اعمال می‌شود.`, item.Cl.Email, cfg.DiscountPct)
		} else {
			msg = strings.ReplaceAll(msg, "{pct}", fmt.Sprint(cfg.DiscountPct))
			msg = strings.ReplaceAll(msg, "{email}", item.Cl.Email)
		}
		// list eligible plans with discounted price
		var plans []models.ShopPlan
		q := db.Where("enabled = ?", true).Order("sort_order asc").Limit(8)
		if len(cfg.PlanIDs) > 0 {
			q = q.Where("id IN ?", cfg.PlanIDs)
		}
		q.Find(&plans)
		var sb strings.Builder
		sb.WriteString(msg)
		sb.WriteString("\n\n")
		rows := [][]InlineKeyboardButton{}
		for _, p := range plans {
			if !shop.PlanEligibleForWinback(cfg, p.ID) {
				continue
			}
			newPrice := shop.DiscountedPrice(p.PriceToman, cfg.DiscountPct)
			sb.WriteString(fmt.Sprintf("• %s: <s>%s</s> → <b>%s</b> ت\n", p.Name, formatToman(p.PriceToman), formatToman(newPrice)))
			rows = append(rows, []InlineKeyboardButton{{
				Text:         fmt.Sprintf("🎁 %s — %s ت", p.Name, formatToman(newPrice)),
				CallbackData: fmt.Sprintf("plan:%d", p.ID),
			}})
		}
		rows = append(rows, []InlineKeyboardButton{{Text: "🛒 همه پلن‌ها", CallbackData: "shop"}})
		_ = cli.SendMessageMarkup(chatID, sb.String(), &InlineKeyboardMarkup{InlineKeyboard: rows})
		slog.Info("winback sent", "client", item.Cl.ID, "tg", item.Link.TelegramUserID, "pct", cfg.DiscountPct)
	}
}
