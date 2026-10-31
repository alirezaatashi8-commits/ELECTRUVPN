package shop

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

type WinbackConfig struct {
	Enabled     bool   `json:"enabled"`
	DaysAfter   int    `json:"daysAfter"`
	DiscountPct int    `json:"discountPct"`
	PlanIDs     []uint `json:"planIds"` // empty = all enabled
	Message     string `json:"message"`
}

func LoadWinbackConfig(db *gorm.DB) WinbackConfig {
	en := panelSetting(db, models.WinbackEnabledKey)
	days, _ := strconv.Atoi(panelSetting(db, models.WinbackDaysAfterKey))
	pct, _ := strconv.Atoi(panelSetting(db, models.WinbackDiscountPctKey))
	if days <= 0 {
		days = 3
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 90 {
		pct = 90
	}
	var planIDs []uint
	raw := panelSetting(db, models.WinbackPlanIDsKey)
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err == nil && n > 0 {
			planIDs = append(planIDs, uint(n))
		}
	}
	msg := panelSetting(db, models.WinbackMessageKey)
	return WinbackConfig{
		Enabled:   en == "1" || strings.EqualFold(en, "true"),
		DaysAfter: days, DiscountPct: pct, PlanIDs: planIDs, Message: msg,
	}
}

func PlanEligibleForWinback(cfg WinbackConfig, planID uint) bool {
	if len(cfg.PlanIDs) == 0 {
		return true
	}
	for _, id := range cfg.PlanIDs {
		if id == planID {
			return true
		}
	}
	return false
}

// DiscountedPrice applies percent off (integer toman, rounded down).
func DiscountedPrice(price int64, pct int) int64 {
	if pct <= 0 || price <= 0 {
		return price
	}
	if pct > 90 {
		pct = 90
	}
	return price * int64(100-pct) / 100
}

// HasActiveWinbackOffer is true if we already sent winback and user hasn't bought since.
func WinbackOfferKey(clientID uint) string {
	return fmt.Sprintf("winback_sent_%d", clientID)
}

func HasWinbackOffer(db *gorm.DB, tgUserID int64) (active bool, pct int, cfg WinbackConfig) {
	cfg = LoadWinbackConfig(db)
	if !cfg.Enabled || cfg.DiscountPct <= 0 {
		return false, 0, cfg
	}
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", tgUserID).First(&link).Error != nil || link.ClientID == 0 {
		// also check trial-style clients
		var cl models.Client
		if db.Where("email LIKE ?", fmt.Sprintf("tg%d_%%", tgUserID)).Order("id desc").First(&cl).Error != nil {
			return false, 0, cfg
		}
		link.ClientID = cl.ID
	}
	var cl models.Client
	if db.First(&cl, link.ClientID).Error != nil {
		return false, 0, cfg
	}
	if !cl.IsExpired() && cl.Enable && !cl.IsOverQuota() {
		return false, 0, cfg
	}
	// must have been sent winback (or we allow discount for any expired with recent winback flag)
	var st models.PanelSetting
	if db.Where("key = ?", WinbackOfferKey(cl.ID)).First(&st).Error != nil {
		return false, 0, cfg
	}
	// if they purchased after winback sent, offer ends
	var n int64
	db.Model(&models.ShopOrder{}).
		Where("telegram_user_id = ? AND status = ? AND created_at > ?", tgUserID, "approved", st.Value).
		Count(&n)
	// st.Value is RFC3339 when sent
	if sentAt, err := time.Parse(time.RFC3339, st.Value); err == nil {
		db.Model(&models.ShopOrder{}).
			Where("telegram_user_id = ? AND status = ? AND reviewed_at >= ?", tgUserID, "approved", sentAt).
			Count(&n)
	}
	if n > 0 {
		return false, 0, cfg
	}
	return true, cfg.DiscountPct, cfg
}

// EligibleWinbackClients finds expired linked customers due for a winback message.
func EligibleWinbackClients(db *gorm.DB, cfg WinbackConfig) []struct {
	Link models.TelegramCustomer
	Cl   models.Client
} {
	var out []struct {
		Link models.TelegramCustomer
		Cl   models.Client
	}
	if !cfg.Enabled {
		return out
	}
	now := time.Now().UnixMilli()
	// expiry between (daysAfter) and (daysAfter+1) days ago roughly — window 24h
	// Actually: expired at least DaysAfter ago, and we haven't sent yet
	minExp := now - int64(cfg.DaysAfter+2)*24*60*60*1000 // older bound optional
	maxExp := now - int64(cfg.DaysAfter)*24*60*60*1000   // expired at least DaysAfter ago
	_ = minExp
	var links []models.TelegramCustomer
	db.Find(&links)
	for _, link := range links {
		if link.TelegramChatID == 0 && link.TelegramUserID == 0 {
			continue
		}
		var cl models.Client
		if db.First(&cl, link.ClientID).Error != nil {
			continue
		}
		if cl.ExpiryTime <= 0 || cl.ExpiryTime > maxExp {
			continue // not expired long enough
		}
		// already sent?
		var st models.PanelSetting
		if db.Where("key = ?", WinbackOfferKey(cl.ID)).First(&st).Error == nil {
			continue
		}
		// renewed after expiry?
		var n int64
		expTime := time.UnixMilli(cl.ExpiryTime)
		db.Model(&models.ShopOrder{}).
			Where("telegram_user_id = ? AND status = ? AND reviewed_at >= ?", link.TelegramUserID, "approved", expTime).
			Count(&n)
		if n > 0 {
			continue
		}
		out = append(out, struct {
			Link models.TelegramCustomer
			Cl   models.Client
		}{Link: link, Cl: cl})
	}
	return out
}
