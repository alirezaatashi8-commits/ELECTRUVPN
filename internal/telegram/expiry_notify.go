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

// RunExpiryNotices: smart time + volume alerts with quick-renew CTA.
// Runs every 15 minutes. Re-notifies after the user recovers (renews) and
// enters the danger zone again.
func RunExpiryNotices(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	doExpiryPass(db)
	_ = shop.DisableExpiredTrials(db)
	_ = shop.RecoverStuckOrders(db)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doExpiryPass(db)
			if n := shop.DisableExpiredTrials(db); n > 0 {
				slog.Info("disabled expired trials", "count", n)
			}
			if n := shop.RecoverStuckOrders(db); n > 0 {
				slog.Info("recovered stuck orders", "count", n)
			}
		}
	}
}

func renewKeyboard(db *gorm.DB) *InlineKeyboardMarkup {
	pct := shop.LoyaltyDiscountPct(db)
	renewLabel := "🔄 تمدید سریع"
	if pct > 0 {
		renewLabel = fmt.Sprintf("🎁 تمدید سریع با %d%% تخفیف وفاداری", pct)
	}
	rows := [][]InlineKeyboardButton{
		{{Text: renewLabel, CallbackData: "shop_loyalty"}},
		{{Text: "🛒 همه پلن‌ها", CallbackData: "shop"}},
		{{Text: "📊 وضعیت اشتراک من", CallbackData: "status"}},
	}
	base := strings.TrimRight(GetSetting(db, "subscription_base_url"), "/")
	if base == "" {
		base = strings.TrimRight(GetSetting(db, "public_host"), "/")
		if base != "" && !strings.HasPrefix(base, "http") {
			base = "https://" + base
		}
	}
	if base != "" {
		rows = append([][]InlineKeyboardButton{{{Text: "🖥 تمدید در مینی‌اپ", WebApp: &WebAppInfo{URL: base + "/tma/?tab=shop"}}}}, rows...)
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

func chatForLink(link models.TelegramCustomer) int64 {
	if link.TelegramChatID != 0 {
		return link.TelegramChatID
	}
	return link.TelegramUserID
}

// notifySmart sends at most once per (key, watermark). When the condition clears
// (watermark "ok"), the key is reset so a future dip notifies again.
func notifySmart(db *gorm.DB, key, watermark string) bool {
	var st models.PanelSetting
	err := db.Where("key = ?", key).First(&st).Error
	if err != nil {
		if watermark == "ok" {
			return false
		}
		_ = db.Create(&models.PanelSetting{Key: key, Value: watermark}).Error
		return true
	}
	if watermark == "ok" {
		if st.Value != "ok" {
			_ = db.Model(&st).Update("value", "ok").Error
		}
		return false
	}
	if st.Value == watermark {
		return false
	}
	_ = db.Model(&st).Update("value", watermark).Error
	return true
}

func clientTrafficLimit(cl models.Client) int64 {
	if cl.TrafficLimitBytes > 0 {
		return cl.TrafficLimitBytes
	}
	if cl.TotalGB > 0 {
		return cl.TotalGB * 1024 * 1024 * 1024
	}
	return 0
}

func doExpiryPass(db *gorm.DB) {
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	cli := NewClient(token)
	now := time.Now().UnixMilli()
	kb := renewKeyboard(db)

	timeWindows := []struct {
		label string
		min   int64
		max   int64
		flag  string
		text  string
	}{
		{"24h", now + 18*3600*1000, now + 30*3600*1000, "smart_exp_24h",
			"⏰ <b>۲۴ ساعت تا پایان اشتراک</b>\nاشتراک «%s» حدود یک روز دیگر تمام می‌شود.\n\nبا دکمه تمدید سریع (تخفیف وفاداری) قطعی پیش نمی‌آید."},
		{"2h", now + 1*3600*1000, now + 3*3600*1000, "smart_exp_2h",
			"🚨 <b>۲ ساعت تا پایان اشتراک</b>\nاشتراک «%s» به‌زودی قطع می‌شود.\nهمین الان تمدید کنید تا سرویس قطع نشود."},
	}

	var links []models.TelegramCustomer
	db.Find(&links)
	for _, link := range links {
		chat := chatForLink(link)
		if chat == 0 {
			continue
		}
		var cl models.Client
		if db.First(&cl, link.ClientID).Error != nil {
			continue
		}

		// --- time-based ---
		if cl.ExpiryTime > 0 {
			inDanger := false
			for _, t := range timeWindows {
				if cl.ExpiryTime >= t.min && cl.ExpiryTime <= t.max {
					inDanger = true
					// watermark encodes the expiry timestamp so a renewal (new expiry) can re-alert
					wm := fmt.Sprintf("%s:%d", t.label, cl.ExpiryTime)
					key := fmt.Sprintf("%s_%d", t.flag, cl.ID)
					if !notifySmart(db, key, wm) {
						continue
					}
					msg := fmt.Sprintf(t.text, cl.Email)
					_ = cli.SendMessageMarkup(chat, msg, kb)
					slog.Info("smart expiry notice", "client", cl.ID, "window", t.label)
				}
			}
			if !inDanger && cl.ExpiryTime > now+30*3600*1000 {
				// recovered / renewed far ahead — reset flags
				_ = notifySmart(db, fmt.Sprintf("smart_exp_24h_%d", cl.ID), "ok")
				_ = notifySmart(db, fmt.Sprintf("smart_exp_2h_%d", cl.ID), "ok")
			}
		}

		// --- volume-based ---
		limit := clientTrafficLimit(cl)
		if limit <= 0 {
			continue
		}
		used := cl.Up + cl.Down
		remain := float64(limit-used) / float64(limit)
		if remain < 0 {
			remain = 0
		}
		key10 := fmt.Sprintf("smart_vol_10_%d", cl.ID)
		key2 := fmt.Sprintf("smart_vol_2_%d", cl.ID)
		if remain > 0.15 {
			// healthy again after renew — allow future alerts
			_ = notifySmart(db, key10, "ok")
			_ = notifySmart(db, key2, "ok")
			continue
		}
		if remain <= 0.02 {
			wm := fmt.Sprintf("2pct:limit=%d", limit)
			if notifySmart(db, key2, wm) {
				msg := fmt.Sprintf("🚨 <b>حجم تقریباً تمام شده</b>\nاشتراک «%s» کمتر از ۲٪ حجم دارد (%.0f٪ مانده).\nبرای تمدید سریع دکمه زیر را بزنید.", cl.Email, remain*100)
				_ = cli.SendMessageMarkup(chat, msg, kb)
				slog.Info("smart volume notice", "client", cl.ID, "remain", remain)
			}
		} else if remain <= 0.10 {
			wm := fmt.Sprintf("10pct:limit=%d", limit)
			if notifySmart(db, key10, wm) {
				msg := fmt.Sprintf("⚠️ <b>هشدار حجم</b>\nاشتراک «%s» حدود %.0f٪ حجم باقی مانده دارد.\nبرای تمدید سریع دکمه زیر را بزنید.", cl.Email, remain*100)
				_ = cli.SendMessageMarkup(chat, msg, kb)
				slog.Info("smart volume notice", "client", cl.ID, "remain", remain)
			}
		}
	}
}
