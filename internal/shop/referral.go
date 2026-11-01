package shop

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// ReferralConfig is operator-controlled affiliate settings.
type ReferralConfig struct {
	Enabled           bool   `json:"enabled"`
	Percent           int    `json:"percent"`           // 0–50; 0 means no cash reward
	FirstPurchaseOnly bool   `json:"firstPurchaseOnly"` // only the invitee's first paid plan
	MinOrderToman     int64  `json:"minOrderToman"`
	BotUsername       string `json:"botUsername,omitempty"`
}

func LoadReferralConfig(db *gorm.DB) ReferralConfig {
	en := panelSetting(db, models.ReferralEnabledKey)
	pct, _ := strconv.Atoi(panelSetting(db, models.ReferralPercentKey))
	if pct < 0 {
		pct = 0
	}
	if pct > 50 {
		pct = 50
	}
	fpo := panelSetting(db, models.ReferralFirstPurchaseOnlyKey)
	firstOnly := fpo == "" || fpo == "1" || strings.EqualFold(fpo, "true")
	minO, _ := strconv.ParseInt(panelSetting(db, models.ReferralMinOrderTomanKey), 10, 64)
	if minO < 0 {
		minO = 0
	}
	user := strings.TrimPrefix(strings.TrimSpace(panelSetting(db, models.ReferralBotUsernameKey)), "@")
	return ReferralConfig{
		Enabled:           en == "1" || strings.EqualFold(en, "true"),
		Percent:           pct,
		FirstPurchaseOnly: firstOnly,
		MinOrderToman:     minO,
		BotUsername:       user,
	}
}

func SaveReferralConfig(db *gorm.DB, cfg ReferralConfig) ReferralConfig {
	en := "0"
	if cfg.Enabled {
		en = "1"
	}
	_ = setPanel(db, models.ReferralEnabledKey, en)
	pct := cfg.Percent
	if pct < 0 {
		pct = 0
	}
	if pct > 50 {
		pct = 50
	}
	_ = setPanel(db, models.ReferralPercentKey, strconv.Itoa(pct))
	fpo := "0"
	if cfg.FirstPurchaseOnly {
		fpo = "1"
	}
	_ = setPanel(db, models.ReferralFirstPurchaseOnlyKey, fpo)
	if cfg.MinOrderToman < 0 {
		cfg.MinOrderToman = 0
	}
	_ = setPanel(db, models.ReferralMinOrderTomanKey, strconv.FormatInt(cfg.MinOrderToman, 10))
	if u := strings.TrimPrefix(strings.TrimSpace(cfg.BotUsername), "@"); u != "" {
		_ = setPanel(db, models.ReferralBotUsernameKey, u)
	}
	return LoadReferralConfig(db)
}

func setPanel(db *gorm.DB, key, value string) error {
	var st models.PanelSetting
	if db.Where("key = ?", key).First(&st).Error == nil {
		return db.Model(&st).Update("value", value).Error
	}
	return db.Create(&models.PanelSetting{Key: key, Value: value}).Error
}

// ReferralCodeForUser is the stable deep-link payload for a referrer.
func ReferralCodeForUser(tgUserID int64) string {
	return fmt.Sprintf("ref%d", tgUserID)
}

// ParseReferrerFromStart extracts referrer TG id from /start payload (ref123 or ref_123).
func ParseReferrerFromStart(payload string) int64 {
	payload = strings.TrimSpace(payload)
	payload = strings.TrimPrefix(payload, "start=")
	low := strings.ToLower(payload)
	if !strings.HasPrefix(low, "ref") {
		return 0
	}
	rest := payload[3:]
	rest = strings.TrimPrefix(rest, "_")
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// AttributeReferrer binds invitee → referrer once (never overwrite, no self-ref).
func AttributeReferrer(db *gorm.DB, inviteeTG, referrerTG int64, code string) error {
	if inviteeTG <= 0 || referrerTG <= 0 || inviteeTG == referrerTG {
		return fmt.Errorf("لینک دعوت نامعتبر است")
	}
	var existing models.ReferralAttribution
	if db.Where("telegram_user_id = ?", inviteeTG).First(&existing).Error == nil {
		return nil // already attributed — keep first touch
	}
	// referrer should look like a real prior user (optional soft check)
	row := models.ReferralAttribution{
		TelegramUserID:     inviteeTG,
		ReferrerTelegramID: referrerTG,
		Code:               code,
		CreatedAt:          time.Now().UTC(),
	}
	return db.Create(&row).Error
}

// ProcessReferralReward credits the referrer after a paid plan order is approved.
// Safe to call multiple times (unique order_id on ReferralReward).
func ProcessReferralReward(db *gorm.DB, order *models.ShopOrder) (rewarded int64, err error) {
	if order == nil || order.ID == 0 {
		return 0, nil
	}
	if order.Kind != "" && order.Kind != "plan" && order.Kind != "custom_gb" {
		return 0, nil // wallet topups do not generate commission
	}
	if order.Status != "approved" {
		return 0, nil
	}
	if order.PriceToman <= 0 {
		return 0, nil
	}

	cfg := LoadReferralConfig(db)
	if !cfg.Enabled || cfg.Percent <= 0 {
		return 0, nil
	}
	if order.PriceToman < cfg.MinOrderToman {
		return 0, nil
	}

	var attr models.ReferralAttribution
	if db.Where("telegram_user_id = ?", order.TelegramUserID).First(&attr).Error != nil {
		return 0, nil // no referrer
	}
	if attr.ReferrerTelegramID == 0 || attr.ReferrerTelegramID == order.TelegramUserID {
		return 0, nil
	}

	if cfg.FirstPurchaseOnly {
		var prior int64
		db.Model(&models.ShopOrder{}).
			Where("telegram_user_id = ? AND status = ? AND kind IN ? AND id < ?",
				order.TelegramUserID, "approved", []string{"plan", "custom_gb", ""}, order.ID).
			Count(&prior)
		if prior > 0 {
			return 0, nil
		}
	}

	// Idempotency: already rewarded for this order?
	var prev models.ReferralReward
	if db.Where("order_id = ?", order.ID).First(&prev).Error == nil {
		return prev.RewardToman, nil
	}

	reward := order.PriceToman * int64(cfg.Percent) / 100
	if reward < 1 {
		return 0, nil
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var again models.ReferralReward
		if tx.Where("order_id = ?", order.ID).First(&again).Error == nil {
			reward = again.RewardToman
			return nil
		}
		row := models.ReferralReward{
			OrderID:            order.ID,
			ReferrerTelegramID: attr.ReferrerTelegramID,
			InviteeTelegramID:  order.TelegramUserID,
			OrderAmountToman:   order.PriceToman,
			Percent:            cfg.Percent,
			RewardToman:        reward,
			CreatedAt:          time.Now().UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		_, err := creditWalletTx(tx, attr.ReferrerTelegramID, reward, "referral",
			fmt.Sprintf("order:%d", order.ID),
			fmt.Sprintf("پاداش معرف %d%% از سفارش #%d", cfg.Percent, order.ID))
		return err
	})
	if err != nil {
		slog.Warn("referral reward failed", "order", order.ID, "error", err)
		return 0, err
	}
	slog.Info("referral reward paid", "order", order.ID, "referrer", attr.ReferrerTelegramID, "amount", reward)
	return reward, nil
}

// ReferralStats for a referrer (TMA / bot UI).
type ReferralStats struct {
	InviteCount    int64 `json:"inviteCount"`
	RewardCount    int64 `json:"rewardCount"`
	TotalEarned    int64 `json:"totalEarned"`
	PendingInvites int64 `json:"pendingInvites"` // attributed but no rewarded purchase yet
}

func GetReferralStats(db *gorm.DB, referrerTG int64) ReferralStats {
	var s ReferralStats
	db.Model(&models.ReferralAttribution{}).Where("referrer_telegram_id = ?", referrerTG).Count(&s.InviteCount)
	db.Model(&models.ReferralReward{}).Where("referrer_telegram_id = ?", referrerTG).Count(&s.RewardCount)
	db.Model(&models.ReferralReward{}).Where("referrer_telegram_id = ?", referrerTG).
		Select("COALESCE(SUM(reward_toman),0)").Scan(&s.TotalEarned)
	s.PendingInvites = s.InviteCount - s.RewardCount
	if s.PendingInvites < 0 {
		s.PendingInvites = 0
	}
	return s
}

// BuildReferralLink returns https://t.me/bot?start=ref123
func BuildReferralLink(db *gorm.DB, tgUserID int64) string {
	cfg := LoadReferralConfig(db)
	user := cfg.BotUsername
	if user == "" {
		user = strings.TrimPrefix(panelSetting(db, models.ReferralBotUsernameKey), "@")
	}
	code := ReferralCodeForUser(tgUserID)
	if user == "" {
		return "ref:" + code // fallback; bot will still accept /start ref…
	}
	return fmt.Sprintf("https://t.me/%s?start=%s", user, code)
}
