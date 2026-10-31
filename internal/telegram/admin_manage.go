package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/google/uuid"
)

// --- Quick user management for admin bot ---

func (b *AdminBot) searchUser(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "مثال: /find ali  یا  /find 123456789"
	}
	var clients []models.Client
	var tgID int64
	// telegram id
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		tgID = id
		var links []models.TelegramCustomer
		b.DB.Where("telegram_user_id = ?", id).Find(&links)
		for _, link := range links {
			var cl models.Client
			if b.DB.First(&cl, link.ClientID).Error == nil {
				clients = append(clients, cl)
			}
		}
	}
	if len(clients) == 0 {
		b.DB.Where("email LIKE ? OR sub_id = ? OR subscription_name LIKE ?", "%"+query+"%", query, "%"+query+"%").
			Limit(8).Find(&clients)
		if len(clients) > 0 && tgID == 0 {
			var link models.TelegramCustomer
			if b.DB.Where("client_id = ?", clients[0].ID).First(&link).Error == nil {
				tgID = link.TelegramUserID
			}
		}
	}
	if len(clients) == 0 && tgID == 0 {
		// still try backup-only users
		resolved, _ := ResolveTelegramUserID(b.DB, query)
		if resolved == 0 {
			return "کاربری پیدا نشد: " + query
		}
		tgID = resolved
	}
	var sb strings.Builder
	sb.WriteString("🔎 نتایج:\n")
	for _, cl := range clients {
		sb.WriteString(formatClientBrief(cl))
		sb.WriteString("\n")
	}
	if tgID == 0 && len(clients) > 0 {
		var link models.TelegramCustomer
		if b.DB.Where("client_id = ?", clients[0].ID).First(&link).Error == nil {
			tgID = link.TelegramUserID
		}
	}
	if tgID > 0 {
		rows := AdminListCloudBackups(b.DB, tgID, 3)
		sb.WriteString(fmt.Sprintf("\n☁️ پشتیبان ابری (TG <code>%d</code>):\n", tgID))
		if len(rows) == 0 {
			sb.WriteString("— کدی ثبت نشده. /issuebackup " + fmt.Sprint(tgID) + "\n")
		} else {
			for _, r := range rows {
				st := "باطل"
				if r.Enabled {
					st = "فعال"
				}
				sb.WriteString(fmt.Sprintf("• [%s] <code>%s</code> · %s\n", st, r.BackupCode, r.CreatedAt.Local().Format("01-02 15:04")))
			}
			sb.WriteString("جزئیات: /backupcode " + fmt.Sprint(tgID) + "\n")
		}
	}
	return sb.String()
}

func formatClientBrief(cl models.Client) string {
	used := humanBytes(cl.Up + cl.Down)
	exp := "∞"
	if cl.ExpiryTime > 0 {
		exp = time.UnixMilli(cl.ExpiryTime).Local().Format("2006-01-02")
	}
	gb := "∞"
	if cl.TotalGB > 0 {
		gb = fmt.Sprintf("%dGB", cl.TotalGB)
	}
	st := "on"
	if !cl.Enable {
		st = "off"
	}
	return fmt.Sprintf("• #%d <code>%s</code> [%s]\n  حجم %s · مصرف %s · تا %s\n  sub <code>%s</code>",
		cl.ID, cl.Email, st, gb, used, exp, cl.SubID)
}

// extendClient adds GB and/or days. id can be client id.
func (b *AdminBot) extendClient(id uint, addGB int64, addDays int) string {
	var cl models.Client
	if b.DB.First(&cl, id).Error != nil {
		return "کلاینت پیدا نشد"
	}
	if addGB > 0 {
		if cl.TotalGB == 0 {
			cl.TotalGB = addGB
		} else {
			cl.TotalGB += addGB
		}
	}
	if addDays > 0 {
		base := time.Now().UnixMilli()
		if cl.ExpiryTime > base {
			base = cl.ExpiryTime
		}
		cl.ExpiryTime = base + int64(addDays)*24*60*60*1000
	}
	cl.Enable = true
	cl.SubscriptionStatus = "active"
	if err := b.DB.Save(&cl).Error; err != nil {
		return "خطا: " + err.Error()
	}
	// notify linked telegram user
	var link models.TelegramCustomer
	if b.DB.Where("client_id = ?", cl.ID).First(&link).Error == nil && link.TelegramChatID != 0 {
		ut := GetSetting(b.DB, KeyUserToken)
		if ut != "" {
			_ = NewClient(ut).SendMessage(link.TelegramChatID, fmt.Sprintf("✨ اشتراک شما تمدید/ارتقاء شد.\n+%d GB · +%d روز", addGB, addDays))
		}
	}
	return "✅ بروزرسانی شد\n" + formatClientBrief(cl)
}

// createQuickClient: /new inboundId email [gb] [days]
func (b *AdminBot) createQuickClient(parts []string) string {
	if len(parts) < 3 {
		return "مثال: /new 1 user@tg 50 30\n(inboundId email [GB] [days])"
	}
	inbID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return "inboundId نامعتبر"
	}
	var ib models.Inbound
	if b.DB.First(&ib, uint(inbID)).Error != nil {
		return "اینباند پیدا نشد"
	}
	email := strings.TrimSpace(parts[2])
	gb := int64(0)
	days := 30
	if len(parts) >= 4 {
		gb, _ = strconv.ParseInt(parts[3], 10, 64)
	}
	if len(parts) >= 5 {
		days, _ = strconv.Atoi(parts[4])
	}
	now := time.Now()
	cl := models.Client{
		InboundID: uint(inbID), Email: email,
		UUID: uuid.NewString(), Password: uuid.NewString(),
		Enable: true, TotalGB: gb,
		ExpiryTime:          now.UnixMilli() + int64(days)*24*60*60*1000,
		SubID:               strings.ReplaceAll(uuid.NewString(), "-", ""),
		SubscriptionEnabled: true, SubscriptionName: email,
		SubscriptionStatus: "active", SubscriptionTokenVersion: 1,
		SubscriptionAutoUpdate: true, PurchasedAt: now,
		SubscriptionRefreshMin: 15, SubscriptionAddressType: "all",
	}
	if err := b.DB.Create(&cl).Error; err != nil {
		return "خطا: " + err.Error()
	}
	return "✅ ساخته شد\n" + formatClientBrief(cl)
}

func (b *AdminBot) manageHelp() string {
	return `👤 <b>مدیریت سریع کاربران</b>

/find نام_یا_آیدی — جستجو (+ کد پشتیبان)
/new inboundId email [GB] [days] — ساخت اکانت
/addgb ID مقدار — افزایش حجم
/adddays ID مقدار — افزایش روز
/enable ID — فعال
/disable ID — غیرفعال

☁️ <b>پشتیبان ابری کاربر</b>
/backupcode آیدی_یا_ایمیل — نمایش کدهای کامل
/issuebackup آیدی — صدور کد جدید برای کاربر
/sendbackup آیدی — ارسال کد فعال به چت کاربر
/reenablebackup کد — فعال‌سازی مجدد کد منقضی/قفل

دکمه‌های شیشه‌ای منو هم برای گزارش و سفارش‌ها در دسترس است.`
}
