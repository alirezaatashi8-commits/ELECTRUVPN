package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

// FinanceHandler provides revenue KPIs, plan sales, churn analysis and exports.
type FinanceHandler struct{ S *core.Server }

func NewFinanceHandler(s *core.Server) *FinanceHandler { return &FinanceHandler{S: s} }

func parseFinanceRange(c *gin.Context) (from, to time.Time) {
	now := time.Now()
	to = now
	from = now.AddDate(0, -1, 0) // default last 30 days
	if v := strings.TrimSpace(c.Query("from")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := strings.TrimSpace(c.Query("to")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.Add(24*time.Hour - time.Nanosecond)
		}
	}
	if to.Before(from) {
		from, to = to, from
	}
	return from, to
}

func (h *FinanceHandler) revenueBetween(from, to time.Time) (planRev, topupRev, giftRev, total int64, orderCount int64) {
	type row struct {
		Kind  string
		Sum   int64
		Count int64
	}
	var rows []row
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Select("kind, COALESCE(SUM(price_toman),0) as sum, COUNT(*) as count").
		Where("status = ? AND created_at >= ? AND created_at <= ?", "approved", from, to).
		Group("kind").
		Scan(&rows).Error
	for _, r := range rows {
		orderCount += r.Count
		switch r.Kind {
		case "wallet_topup":
			topupRev += r.Sum
		case "gift":
			giftRev += r.Sum
		default:
			planRev += r.Sum
		}
	}
	total = planRev + topupRev + giftRev
	return
}

// Summary returns revenue KPIs for today / week / month / total + growth + ARPU.
func (h *FinanceHandler) Summary(c *gin.Context) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -int(startOfDay.Weekday()))
	if startOfDay.Weekday() == time.Sunday {
		startOfWeek = startOfDay.AddDate(0, 0, -6) // Monday-based week
	} else {
		startOfWeek = startOfDay.AddDate(0, 0, -int(startOfDay.Weekday())+1)
	}
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	prevMonthStart := startOfMonth.AddDate(0, -1, 0)
	prevMonthEnd := startOfMonth.Add(-time.Nanosecond)

	todayPlan, todayTopup, todayGift, todayTotal, todayCnt := h.revenueBetween(startOfDay, now)
	weekPlan, weekTopup, weekGift, weekTotal, weekCnt := h.revenueBetween(startOfWeek, now)
	monthPlan, monthTopup, monthGift, monthTotal, monthCnt := h.revenueBetween(startOfMonth, now)
	_, _, _, prevMonthTotal, _ := h.revenueBetween(prevMonthStart, prevMonthEnd)
	allPlan, allTopup, allGift, allTotal, allCnt := h.revenueBetween(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), now)

	var growth float64
	if prevMonthTotal > 0 {
		growth = float64(monthTotal-prevMonthTotal) / float64(prevMonthTotal) * 100
	} else if monthTotal > 0 {
		growth = 100
	}

	// Paying customers (distinct telegram users with approved order)
	var payingUsers int64
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Where("status = ? AND telegram_user_id > 0", "approved").
		Distinct("telegram_user_id").
		Count(&payingUsers).Error

	var arpu float64
	if payingUsers > 0 {
		arpu = float64(allTotal) / float64(payingUsers)
	}

	// Pending review value (pipeline)
	var pendingSum int64
	var pendingCnt int64
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Select("COALESCE(SUM(price_toman),0), COUNT(*)").
		Where("status IN ?", []string{"pending_review", "pending_payment"}).
		Row().Scan(&pendingSum, &pendingCnt)

	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"today":        gin.H{"total": todayTotal, "plan": todayPlan, "topup": todayTopup, "gift": todayGift, "orders": todayCnt},
		"week":         gin.H{"total": weekTotal, "plan": weekPlan, "topup": weekTopup, "gift": weekGift, "orders": weekCnt},
		"month":        gin.H{"total": monthTotal, "plan": monthPlan, "topup": monthTopup, "gift": monthGift, "orders": monthCnt},
		"prevMonth":    gin.H{"total": prevMonthTotal},
		"allTime":      gin.H{"total": allTotal, "plan": allPlan, "topup": allTopup, "gift": allGift, "orders": allCnt},
		"growthPct":    growth,
		"payingUsers":  payingUsers,
		"arpu":         arpu,
		"pendingSum":   pendingSum,
		"pendingCount": pendingCnt,
		"currency":     "IRT",
		"generatedAt":  now,
	}})
}

// PlanSales returns sales breakdown by plan name for a date range.
func (h *FinanceHandler) PlanSales(c *gin.Context) {
	from, to := parseFinanceRange(c)
	type row struct {
		PlanID   uint   `json:"planId"`
		PlanName string `json:"planName"`
		Orders   int64  `json:"orders"`
		Revenue  int64  `json:"revenue"`
		TotalGB  int64  `json:"totalGB"`
		AvgPrice int64  `json:"avgPrice"`
	}
	var rows []row
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Select("plan_id, COALESCE(NULLIF(plan_name,''), '(unnamed)') as plan_name, COUNT(*) as orders, COALESCE(SUM(price_toman),0) as revenue, COALESCE(SUM(total_gb),0) as total_gb, COALESCE(AVG(price_toman),0) as avg_price").
		Where("status = ? AND kind IN ? AND created_at >= ? AND created_at <= ?", "approved", []string{"plan", "gift", ""}, from, to).
		Group("plan_id, plan_name").
		Order("revenue DESC").
		Scan(&rows).Error
	c.JSON(200, gin.H{"success": true, "obj": rows, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02")})
}

// MonthlyTrend returns last 12 months of approved revenue.
func (h *FinanceHandler) MonthlyTrend(c *gin.Context) {
	now := time.Now()
	type point struct {
		Month   string `json:"month"`
		Revenue int64  `json:"revenue"`
		Orders  int64  `json:"orders"`
	}
	out := make([]point, 0, 12)
	for i := 11; i >= 0; i-- {
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -i, 0)
		end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
		_, _, _, total, cnt := h.revenueBetween(start, end)
		out = append(out, point{Month: start.Format("2006-01"), Revenue: total, Orders: cnt})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

// Churn analyzes expired clients that did not renew and at-risk users.
func (h *FinanceHandler) Churn(c *gin.Context) {
	now := time.Now()
	nowMs := now.UnixMilli()
	days := 30
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 7 && n <= 365 {
			days = n
		}
	}
	windowStartMs := now.AddDate(0, 0, -days).UnixMilli()

	// Clients that expired within the window
	var expiredIDs []uint
	_ = h.S.DB.Model(&models.Client{}).
		Where("expiry_time > 0 AND expiry_time >= ? AND expiry_time < ?", windowStartMs, nowMs).
		Pluck("id", &expiredIDs).Error

	expiredCount := int64(len(expiredIDs))
	var renewedCount int64
	if len(expiredIDs) > 0 {
		// Renewed = has an approved plan order after their expiry, linked via telegram customer
		type link struct {
			ClientID       uint
			TelegramUserID int64
			ExpiryTime     int64
		}
		var links []link
		_ = h.S.DB.Raw(`
			SELECT c.id as client_id, COALESCE(t.telegram_user_id,0) as telegram_user_id, c.expiry_time
			FROM clients c
			LEFT JOIN telegram_customers t ON t.client_id = c.id
			WHERE c.id IN ?
		`, expiredIDs).Scan(&links).Error

		for _, l := range links {
			if l.TelegramUserID == 0 {
				continue
			}
			var cnt int64
			expAt := time.UnixMilli(l.ExpiryTime)
			_ = h.S.DB.Model(&models.ShopOrder{}).
				Where("status = ? AND kind = ? AND telegram_user_id = ? AND created_at >= ?",
					"approved", "plan", l.TelegramUserID, expAt).
				Count(&cnt).Error
			if cnt > 0 {
				renewedCount++
			}
		}
	}

	churned := expiredCount - renewedCount
	var churnRate float64
	if expiredCount > 0 {
		churnRate = float64(churned) / float64(expiredCount) * 100
	}

	// At-risk: expire within next 7 days, not auto-renew
	riskUntilMs := now.AddDate(0, 0, 7).UnixMilli()
	type riskRow struct {
		ID         uint   `json:"id"`
		Email      string `json:"email"`
		ExpiryTime int64  `json:"expiryTime"`
		TotalGB    int64  `json:"totalGB"`
		InboundID  uint   `json:"inboundId"`
	}
	var atRisk []riskRow
	_ = h.S.DB.Model(&models.Client{}).
		Select("id, email, expiry_time, total_gb, inbound_id").
		Where("enable = ? AND expiry_time > ? AND expiry_time <= ?", true, nowMs, riskUntilMs).
		Order("expiry_time ASC").
		Limit(100).
		Scan(&atRisk).Error

	// Active vs expired snapshot
	var activeClients, expiredClients, neverExpire int64
	_ = h.S.DB.Model(&models.Client{}).Where("enable = ? AND (expiry_time = 0 OR expiry_time > ?)", true, nowMs).Count(&activeClients).Error
	_ = h.S.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ?", nowMs).Count(&expiredClients).Error
	_ = h.S.DB.Model(&models.Client{}).Where("expiry_time = 0").Count(&neverExpire).Error

	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"windowDays":      days,
		"expiredInWindow": expiredCount,
		"renewed":         renewedCount,
		"churned":         churned,
		"churnRatePct":    churnRate,
		"atRisk":          atRisk,
		"atRiskCount":     len(atRisk),
		"activeClients":   activeClients,
		"expiredClients":  expiredClients,
		"neverExpire":     neverExpire,
	}})
}

// Transactions lists approved/pending orders for the finance ledger view.
func (h *FinanceHandler) Transactions(c *gin.Context) {
	from, to := parseFinanceRange(c)
	status := strings.TrimSpace(c.Query("status")) // empty = all, or approved|pending_review|...
	kind := strings.TrimSpace(c.Query("kind"))
	limit := 200
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	q := h.S.DB.Model(&models.ShopOrder{}).
		Where("created_at >= ? AND created_at <= ?", from, to).
		Order("created_at DESC").
		Limit(limit)
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	}
	if kind != "" && kind != "all" {
		q = q.Where("kind = ?", kind)
	}
	var rows []models.ShopOrder
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "count": len(rows)})
}

// ExportCSV streams a UTF-8 BOM CSV of transactions (Excel-friendly).
func (h *FinanceHandler) ExportCSV(c *gin.Context) {
	from, to := parseFinanceRange(c)
	status := strings.TrimSpace(c.Query("status"))
	q := h.S.DB.Model(&models.ShopOrder{}).
		Where("created_at >= ? AND created_at <= ?", from, to).
		Order("created_at ASC")
	if status != "" && status != "all" {
		q = q.Where("status = ?", status)
	} else {
		// default export = approved only for accounting
		q = q.Where("status = ?", "approved")
	}
	var rows []models.ShopOrder
	if err := q.Limit(10000).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}

	filename := fmt.Sprintf("electruvpn-finance-%s-%s.csv", from.Format("20060102"), to.Format("20060102"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Status(http.StatusOK)

	// UTF-8 BOM so Excel opens Persian correctly
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{
		"ID", "Date", "Kind", "Status", "TelegramUserID", "TelegramName",
		"PlanID", "PlanName", "TotalGB", "DurationDays", "PriceToman",
		"OriginalPrice", "Discount", "PromoCode", "ReviewedBy", "ClientID",
	})
	for _, o := range rows {
		_ = w.Write([]string{
			strconv.FormatUint(uint64(o.ID), 10),
			o.CreatedAt.Format("2006-01-02 15:04:05"),
			o.Kind,
			o.Status,
			strconv.FormatInt(o.TelegramUserID, 10),
			o.TelegramName,
			strconv.FormatUint(uint64(o.PlanID), 10),
			o.PlanName,
			strconv.FormatInt(o.TotalGB, 10),
			strconv.Itoa(o.DurationDays),
			strconv.FormatInt(o.PriceToman, 10),
			strconv.FormatInt(o.OriginalPriceToman, 10),
			strconv.FormatInt(o.DiscountToman, 10),
			o.PromoCode,
			o.ReviewedBy,
			strconv.FormatUint(uint64(o.ClientID), 10),
		})
	}
	w.Flush()
}

// ExportReportHTML returns a print-friendly HTML report (use browser Print → PDF).
func (h *FinanceHandler) ExportReportHTML(c *gin.Context) {
	from, to := parseFinanceRange(c)
	_, _, _, total, orderCnt := h.revenueBetween(from, to)

	type planRow struct {
		PlanName string
		Orders   int64
		Revenue  int64
	}
	var plans []planRow
	_ = h.S.DB.Model(&models.ShopOrder{}).
		Select("COALESCE(NULLIF(plan_name,''),'(unnamed)') as plan_name, COUNT(*) as orders, COALESCE(SUM(price_toman),0) as revenue").
		Where("status = ? AND created_at >= ? AND created_at <= ?", "approved", from, to).
		Group("plan_name").
		Order("revenue DESC").
		Scan(&plans).Error

	var orders []models.ShopOrder
	_ = h.S.DB.Where("status = ? AND created_at >= ? AND created_at <= ?", "approved", from, to).
		Order("created_at DESC").Limit(500).Find(&orders).Error

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="fa" dir="rtl"><head><meta charset="utf-8">`)
	b.WriteString(`<title>گزارش مالی ELECTRUVPN</title>`)
	b.WriteString(`<style>
body{font-family:Tahoma,Arial,sans-serif;margin:24px;color:#111;background:#fff}
h1{font-size:20px;margin:0 0 8px} h2{font-size:15px;margin:24px 0 8px;border-bottom:1px solid #ddd;padding-bottom:4px}
.meta{color:#555;font-size:12px;margin-bottom:16px}
.kpi{display:flex;gap:16px;flex-wrap:wrap;margin:12px 0}
.kpi div{border:1px solid #ddd;border-radius:8px;padding:12px 16px;min-width:120px}
.kpi b{display:block;font-size:18px;margin-top:4px}
table{width:100%;border-collapse:collapse;font-size:12px;margin-top:8px}
th,td{border:1px solid #ddd;padding:6px 8px;text-align:right}
th{background:#f5f5f5}
@media print{button{display:none} body{margin:12px}}
</style></head><body>`)
	b.WriteString(`<button onclick="window.print()" style="padding:8px 16px;margin-bottom:12px;cursor:pointer">🖨 چاپ / ذخیره PDF</button>`)
	b.WriteString(`<h1>گزارش مالی ELECTRUVPN BETA1</h1>`)
	b.WriteString(fmt.Sprintf(`<div class="meta">بازه: %s تا %s · تولید: %s</div>`,
		from.Format("2006-01-02"), to.Format("2006-01-02"), time.Now().Format("2006-01-02 15:04")))
	b.WriteString(`<div class="kpi">`)
	b.WriteString(fmt.Sprintf(`<div>درآمد کل<b>%s تومان</b></div>`, formatInt(total)))
	b.WriteString(fmt.Sprintf(`<div>تعداد سفارش<b>%d</b></div>`, orderCnt))
	b.WriteString(`</div>`)
	b.WriteString(`<h2>فروش پلن‌ها</h2><table><tr><th>پلن</th><th>تعداد</th><th>درآمد (تومان)</th></tr>`)
	for _, p := range plans {
		b.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%d</td><td>%s</td></tr>`,
			htmlEsc(p.PlanName), p.Orders, formatInt(p.Revenue)))
	}
	b.WriteString(`</table>`)
	b.WriteString(`<h2>تراکنش‌های تأییدشده</h2><table><tr><th>شناسه</th><th>تاریخ</th><th>نوع</th><th>کاربر</th><th>پلن</th><th>مبلغ</th></tr>`)
	for _, o := range orders {
		b.WriteString(fmt.Sprintf(`<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			o.ID, o.CreatedAt.Format("2006-01-02 15:04"), htmlEsc(o.Kind),
			htmlEsc(o.TelegramName), htmlEsc(o.PlanName), formatInt(o.PriceToman)))
	}
	b.WriteString(`</table></body></html>`)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(200, b.String())
}

func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
