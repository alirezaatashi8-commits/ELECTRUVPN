package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// RunSatisfactionSurveys: post-purchase, post-expiry, and ticket-close surveys.
func RunSatisfactionSurveys(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(2 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendDueSurveys(db)
			sendExpirySurveys(db)
		}
	}
}

func surveyKeyboard(kind string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{
			{Text: "😍 عالی", CallbackData: "survey:5:" + kind},
			{Text: "🙂 خوب", CallbackData: "survey:4:" + kind},
		},
		{
			{Text: "😐 متوسط", CallbackData: "survey:3:" + kind},
			{Text: "🙁 ضعیف", CallbackData: "survey:2:" + kind},
		},
	}}
}

func sendDueSurveys(db *gorm.DB) {
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	from := time.Now().Add(-4 * 24 * time.Hour)
	to := time.Now().Add(-3 * 24 * time.Hour)
	var clients []models.Client
	db.Where("purchased_at >= ? AND purchased_at < ? AND enable = ?", from, to, true).Limit(100).Find(&clients)
	cli := NewClient(token)
	for _, cl := range clients {
		key := fmt.Sprintf("survey_purchase_%d", cl.ID)
		if !markOnce(db, key) {
			continue
		}
		var link models.TelegramCustomer
		if db.Where("client_id = ?", cl.ID).First(&link).Error != nil {
			continue
		}
		chat := link.TelegramChatID
		if chat == 0 {
			chat = link.TelegramUserID
		}
		if chat == 0 {
			continue
		}
		msg := fmt.Sprintf(`⭐ <b>نظرسنجی کیفیت اتصال</b>

چند روز از فعال‌سازی «%s» گذشته.
کیفیت اتصال را چطور ارزیابی می‌کنید؟`, cl.Email)
		if err := cli.SendMessageMarkup(chat, msg, surveyKeyboard("purchase")); err != nil {
			slog.Warn("survey send", "error", err)
		}
	}
}

// 2 days after subscription expiry
func sendExpirySurveys(db *gorm.DB) {
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	now := time.Now().UnixMilli()
	// expired between 2 and 3 days ago
	fromExp := now - 3*24*60*60*1000
	toExp := now - 2*24*60*60*1000
	var clients []models.Client
	db.Where("expiry_time > 0 AND expiry_time >= ? AND expiry_time < ?", fromExp, toExp).Limit(100).Find(&clients)
	cli := NewClient(token)
	for _, cl := range clients {
		key := fmt.Sprintf("survey_expiry_%d", cl.ID)
		if !markOnce(db, key) {
			continue
		}
		var link models.TelegramCustomer
		if db.Where("client_id = ?", cl.ID).First(&link).Error != nil {
			continue
		}
		chat := link.TelegramChatID
		if chat == 0 {
			chat = link.TelegramUserID
		}
		if chat == 0 {
			continue
		}
		msg := fmt.Sprintf(`⭐ <b>نظرسنجی پس از پایان اشتراک</b>

اشتراک «%s» حدود ۲ روز پیش تمام شد.
از کیفیت سرویس راضی بودید؟`, cl.Email)
		_ = cli.SendMessageMarkup(chat, msg, surveyKeyboard("expiry"))
	}
}

// SendTicketClosedSurvey is called when admin closes a ticket.
func SendTicketClosedSurvey(db *gorm.DB, t models.Ticket) {
	token := GetSetting(db, KeyUserToken)
	if token == "" {
		return
	}
	key := fmt.Sprintf("survey_ticket_%d", t.ID)
	if !markOnce(db, key) {
		return
	}
	chat := t.TelegramChatID
	if chat == 0 {
		chat = t.TelegramUserID
	}
	if chat == 0 {
		return
	}
	msg := fmt.Sprintf(`⭐ <b>نظرسنجی پشتیبانی</b>

تیکت #%d بسته شد.
از پاسخگویی پشتیبانی راضی بودید؟`, t.ID)
	_ = NewClient(token).SendMessageMarkup(chat, msg, surveyKeyboard("ticket"))
}
