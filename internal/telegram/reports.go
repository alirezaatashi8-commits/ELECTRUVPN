package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// RunPeriodicReports sends daily (08:00 local) and weekly (Monday) digests to admins.
func RunPeriodicReports(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(20 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maybeSendReports(db)
		}
	}
}

func maybeSendReports(db *gorm.DB) {
	now := time.Now()
	// Daily around 08:00–08:25
	if now.Hour() == 8 && now.Minute() < 25 {
		key := "report_daily_" + now.Format("2006-01-02")
		if !markOnce(db, key) {
			sendAdminBroadcast(db, buildReport(db, "روزانه", 24*time.Hour))
		}
	}
	// Weekly Monday 09:00–09:25
	if now.Weekday() == time.Monday && now.Hour() == 9 && now.Minute() < 25 {
		key := "report_weekly_" + now.Format("2006-W02")
		// use ISO-like week
		y, w := now.ISOWeek()
		key = fmt.Sprintf("report_weekly_%d_%d", y, w)
		if !markOnce(db, key) {
			sendAdminBroadcast(db, buildReport(db, "هفتگی", 7*24*time.Hour))
		}
	}
}

func markOnce(db *gorm.DB, key string) bool {
	var st models.PanelSetting
	if db.Where("key = ?", key).First(&st).Error == nil {
		return false
	}
	_ = db.Create(&models.PanelSetting{Key: key, Value: time.Now().Format(time.RFC3339)}).Error
	return true
}

func buildReport(db *gorm.DB, title string, window time.Duration) string {
	since := time.Now().Add(-window)
	var clients, active, newClients int64
	db.Model(&models.Client{}).Count(&clients)
	db.Model(&models.Client{}).Where("enable = ?", true).Count(&active)
	db.Model(&models.Client{}).Where("created_at >= ?", since).Count(&newClients)

	var salesCount int64
	var salesSum int64
	db.Model(&models.ShopOrder{}).Where("status = ? AND reviewed_at >= ?", "approved", since).Count(&salesCount)
	db.Model(&models.ShopOrder{}).Where("status = ? AND reviewed_at >= ?", "approved", since).
		Select("COALESCE(SUM(price_toman),0)").Scan(&salesSum)

	var traffic struct{ Up, Down int64 }
	db.Model(&models.Client{}).Select("COALESCE(SUM(up),0) as up, COALESCE(SUM(down),0) as down").Scan(&traffic)

	var pendingOrders int64
	db.Model(&models.ShopOrder{}).Where("status = ?", "pending_review").Count(&pendingOrders)

	return fmt.Sprintf(`📈 <b>گزارش %s</b>
بازه: %s

👥 کلاینت کل: %d · فعال: %d
🆕 جدید در بازه: %d
💰 فروش تأییدشده: %d سفارش · %s تومان
🛒 در انتظار تأیید: %d
📶 ترافیک کل سرور: ↑ %s  ↓ %s
%s`,
		title, window.String(),
		clients, active, newClients,
		salesCount, formatToman(salesSum), pendingOrders,
		humanBytes(traffic.Up), humanBytes(traffic.Down),
		time.Now().Format("2006-01-02 15:04"))
}

func sendAdminBroadcast(db *gorm.DB, text string) {
	token := GetSetting(db, KeyAdminToken)
	ids := ParseAdminIDs(GetSetting(db, KeyAdminIDs))
	if token == "" || len(ids) == 0 {
		return
	}
	cli := NewClient(token)
	for id := range ids {
		if err := cli.SendMessage(id, text); err != nil {
			slog.Warn("admin report send", "error", err)
		}
	}
}

// ReportNow builds an on-demand report (admin /report).
func ReportNow(db *gorm.DB) string {
	return buildReport(db, "آنی", 24*time.Hour)
}
