package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// SubStatus is the subscription state used for targeted broadcast.
type SubStatus string

const (
	SubStatusActive    SubStatus = "active"      // enabled, not expired, not over-quota
	SubStatusExpired   SubStatus = "expired"     // past expiry or disabled
	SubStatusNoBuy     SubStatus = "no_purchase" // no approved shop order
	SubStatusUnknown   SubStatus = "unknown"     // linked TG but no client row
	SubStatusTrial     SubStatus = "trial"
	SubStatusExpiring  SubStatus = "expiring_3d"
	SubStatusOldBuy    SubStatus = "purchased_over_30d"
	SubStatusHighUsage SubStatus = "high_usage"
	SubStatusAll       SubStatus = "all"
)

// ClassifySubscription determines the primary status of a telegram user for broadcast routing.
//
// Algorithm (priority order for exclusive buckets active / expired / no_purchase):
//  1. Load TelegramCustomer → Client (if any).
//  2. If no approved ShopOrder for this TG id AND no non-trial client → no_purchase.
//  3. If client exists and (expired OR disabled OR over-quota with past expiry) → expired.
//  4. If client enabled, not expired, not over-quota → active.
//  5. Else → unknown.
//
// Additional non-exclusive flags (expiring_3d, trial, high_usage, purchased_over_30d)
// are evaluated separately when that target is selected.
func ClassifySubscription(db *gorm.DB, tgUserID int64, clientID uint) SubStatus {
	var approved int64
	db.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status = ?", tgUserID, "approved").
		Count(&approved)

	var cl *models.Client
	if clientID > 0 {
		var row models.Client
		if db.First(&row, clientID).Error == nil {
			cl = &row
		}
	}
	if cl == nil {
		var row models.Client
		if db.Where("email LIKE ? OR email LIKE ?", fmt.Sprintf("tg%d_%%", tgUserID), fmt.Sprintf("trial_tg%d_%%", tgUserID)).
			Order("id desc").First(&row).Error == nil {
			cl = &row
		}
	}

	if cl == nil {
		if approved == 0 {
			return SubStatusNoBuy
		}
		return SubStatusUnknown
	}

	// Explicit trial detection
	if strings.HasPrefix(cl.Email, "trial_") {
		if cl.IsExpired() || !cl.Enable {
			return SubStatusExpired
		}
		// still active trial counts as active for "active" filter; trial filter is separate
	}

	if !cl.Enable || cl.IsExpired() {
		return SubStatusExpired
	}
	if cl.IsOverQuota() {
		// volume exhausted but time remains — treat as expired for marketing winback
		return SubStatusExpired
	}
	if approved == 0 && strings.HasPrefix(cl.Email, "trial_") {
		// trial only, never bought — still "active" if trial runs, but no_purchase matches trial-only users
		return SubStatusActive
	}
	return SubStatusActive
}

// CustomerFilter selects which telegram users receive a targeted broadcast.
type CustomerFilter struct {
	// all | active | expired | no_purchase | expiring_3d | expiring_today |
	// purchased_over_30d | trial | high_usage
	Target    string
	MinUsedGB float64
}

// BroadcastPreview counts how many unique chats match each core segment.
// BroadcastPreview counts recipients for each segment (for admin UI).
func BroadcastPreview(db *gorm.DB) map[string]int {
	targets := []string{
		"all", "active", "expired", "no_purchase", "trial",
		"expiring_3d", "expiring_7d", "expiring_today",
		"purchased_over_30d", "high_usage",
		"under_1gb", "low_remain", "offline_7d", "offline_10d",
	}
	out := map[string]int{}
	for _, tg := range targets {
		out[tg] = len(CollectCustomerChatIDs(db, &CustomerFilter{Target: tg, MinUsedGB: 10}))
	}
	return out
}

func CollectCustomerChatIDs(db *gorm.DB, f *CustomerFilter) []int64 {
	type row struct {
		TelegramChatID int64
		TelegramUserID int64
		ClientID       uint
	}
	var list []row
	db.Table("telegram_customers").Select("telegram_chat_id, telegram_user_id, client_id").Find(&list)

	// Also include TG users who only appear on shop orders (never linked customer)
	if f != nil && (f.Target == "no_purchase" || f.Target == "all") {
		var orderTGs []int64
		db.Model(&models.ShopOrder{}).Distinct("telegram_user_id").Pluck("telegram_user_id", &orderTGs)
		for _, tg := range orderTGs {
			if tg == 0 {
				continue
			}
			found := false
			for _, r := range list {
				if r.TelegramUserID == tg {
					found = true
					break
				}
			}
			if !found {
				list = append(list, row{TelegramUserID: tg, TelegramChatID: tg})
			}
		}
	}

	now := time.Now().UnixMilli()
	seen := map[int64]bool{}
	var ids []int64
	target := "all"
	if f != nil && f.Target != "" {
		target = strings.ToLower(strings.TrimSpace(f.Target))
	}
	for _, r := range list {
		id := r.TelegramChatID
		if id == 0 {
			id = r.TelegramUserID
		}
		if id == 0 || seen[id] {
			continue
		}
		if !matchBroadcastTarget(db, r.ClientID, r.TelegramUserID, target, f, now) {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func matchBroadcastTarget(db *gorm.DB, clientID uint, tgUID int64, target string, f *CustomerFilter, nowMs int64) bool {
	switch target {
	case "", "all":
		return true

	case "active":
		return ClassifySubscription(db, tgUID, clientID) == SubStatusActive

	case "expired":
		return ClassifySubscription(db, tgUID, clientID) == SubStatusExpired

	case "no_purchase", "no_buy":
		// Never had an approved paid order (trial-only or only window-shoppers)
		var n int64
		db.Model(&models.ShopOrder{}).
			Where("telegram_user_id = ? AND status = ? AND kind <> ?", tgUID, "approved", "wallet_topup").
			Count(&n)
		// also exclude wallet-only if they never bought a plan
		var plans int64
		db.Model(&models.ShopOrder{}).
			Where("telegram_user_id = ? AND status = ? AND (kind = ? OR kind = ? OR kind = '')", tgUID, "approved", "plan", "").
			Count(&plans)
		return plans == 0

	case "expiring_3d", "expiring_today":
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		if cl.ExpiryTime <= 0 || !cl.Enable {
			return false
		}
		days := 3
		if target == "expiring_today" {
			days = 1
		}
		until := nowMs + int64(days)*24*60*60*1000
		return cl.ExpiryTime > nowMs && cl.ExpiryTime <= until

	case "purchased_over_30d":
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		if !cl.PurchasedAt.IsZero() {
			return cl.PurchasedAt.Before(cutoff)
		}
		return cl.CreatedAt.Before(cutoff)

	case "trial":
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		return strings.HasPrefix(cl.Email, "trial_")

	case "high_usage":
		minGB := 10.0
		if f != nil && f.MinUsedGB > 0 {
			minGB = f.MinUsedGB
		}
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		used := float64(cl.Up+cl.Down) / (1024 * 1024 * 1024)
		return used >= minGB

	case "low_remain", "under_1gb", "low_quota":
		// Remaining traffic under 1 GiB (or under 10% of limit)
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		if !cl.Enable || cl.IsExpired() {
			return false
		}
		lim := cl.TrafficLimitBytes
		if lim <= 0 && cl.TotalGB > 0 {
			lim = cl.TotalGB * 1024 * 1024 * 1024
		}
		if lim <= 0 {
			return false // unlimited
		}
		used := cl.Up + cl.Down
		remain := lim - used
		if remain < 0 {
			remain = 0
		}
		if target == "low_remain" {
			return float64(remain)/float64(lim) <= 0.10 && remain > 0
		}
		return remain > 0 && remain <= 1024*1024*1024 // ≤ 1 GiB left

	case "offline_7d", "offline_10d", "inactive":
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		days := 10
		if target == "offline_7d" {
			days = 7
		}
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		// Prefer last device activity; fall back to client updated_at
		var lastDev time.Time
		var d models.ClientDevice
		if db.Where("client_id = ?", cl.ID).Order("last_seen_at desc").First(&d).Error == nil {
			lastDev = d.LastSeenAt
		}
		if lastDev.IsZero() {
			lastDev = cl.UpdatedAt
		}
		if lastDev.IsZero() {
			lastDev = cl.CreatedAt
		}
		return lastDev.Before(cutoff)

	case "expiring_7d":
		var cl models.Client
		if clientID == 0 || db.First(&cl, clientID).Error != nil {
			return false
		}
		if cl.ExpiryTime <= 0 || !cl.Enable {
			return false
		}
		until := nowMs + 7*24*60*60*1000
		return cl.ExpiryTime > nowMs && cl.ExpiryTime <= until

	default:
		return false
	}
}

// TargetedBroadcast runs the algorithm and sends the message.
func TargetedBroadcast(db *gorm.DB, target, message string) (matched, ok, fail int) {
	token := GetSetting(db, KeyUserToken)
	if token == "" || strings.TrimSpace(message) == "" {
		return 0, 0, 0
	}
	ids := CollectCustomerChatIDs(db, &CustomerFilter{Target: target})
	o, f := NewClient(token).Broadcast(ids, message)
	return len(ids), o, f
}
