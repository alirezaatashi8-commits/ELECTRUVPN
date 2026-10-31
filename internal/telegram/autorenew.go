package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"gorm.io/gorm"
)

// RunAutoRenew tries wallet-based renewal for customers who enabled it.
// onSuccess is optional; when non-nil it is called once after at least one
// successful renew in a pass (so the panel can regenerate Xray/sing-box).
func RunAutoRenew(ctx context.Context, db *gorm.DB, onSuccess ...func()) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	var cb func()
	if len(onSuccess) > 0 {
		cb = onSuccess[0]
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if runAutoRenewPass(db) && cb != nil {
				cb()
			}
		}
	}
}

func runAutoRenewPass(db *gorm.DB) bool {
	var links []models.TelegramCustomer
	db.Where("auto_renew_enabled = ?", true).Find(&links)
	ut := GetSetting(db, KeyUserToken)
	cli := NewClient(ut)
	anyOK := false
	for _, link := range links {
		ok, msg := shop.TryAutoRenew(db, link)
		if !ok {
			// only notify on real balance problems, not "not due" / cooldown
			if msg == "موجودی کافی نیست" && link.TelegramChatID != 0 && ut != "" {
				// cooldown already set inside TryAutoRenew — message once per cooldown window
				key := fmt.Sprintf("autorenew_msg_%d", link.TelegramUserID)
				var st models.PanelSetting
				if db.Where("key = ?", key).First(&st).Error == nil {
					if ts, err := time.Parse(time.RFC3339, st.Value); err == nil && time.Since(ts) < 6*time.Hour {
						continue
					}
				}
				_ = db.Where("key = ?", key).Delete(&models.PanelSetting{})
				_ = db.Create(&models.PanelSetting{Key: key, Value: time.Now().UTC().Format(time.RFC3339)}).Error
				_ = cli.SendMessage(link.TelegramChatID, "⚠️ تمدید خودکار ناموفق: موجودی کیف پول کافی نیست. لطفاً شارژ کنید.")
			}
			continue
		}
		anyOK = true
		slog.Info("auto-renew ok", "tg", link.TelegramUserID, "plan", msg)
		if link.TelegramChatID != 0 && ut != "" {
			_ = cli.SendMessage(link.TelegramChatID, fmt.Sprintf("✅ تمدید خودکار انجام شد.\nپلن: %s\nاز موجودی کیف پول کسر شد.", msg))
		}
	}
	return anyOK
}
