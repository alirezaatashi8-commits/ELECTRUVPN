package shop

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GenerateGiftCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	h := strings.ToUpper(hex.EncodeToString(b))
	// EVG-XXXX-XXXXXX
	return fmt.Sprintf("EVG-%s-%s", h[:4], h[4:]), nil
}

func NormalizeGiftCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// IssueGiftFromPlan creates a gift card snapshot from a shop plan (admin or paid purchase).
func IssueGiftFromPlan(db *gorm.DB, plan models.ShopPlan, buyerTG int64, paid int64, adminIssue bool, note string, ttlDays int) (models.GiftCard, error) {
	if plan.ID == 0 || !plan.Enabled {
		return models.GiftCard{}, fmt.Errorf("پلن نامعتبر یا غیرفعال است")
	}
	if ttlDays <= 0 {
		ttlDays = 90
	}
	code, err := GenerateGiftCode()
	if err != nil {
		return models.GiftCard{}, err
	}
	exp := time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour)
	g := models.GiftCard{
		Code: code, PlanID: plan.ID, PlanName: plan.Name,
		TotalGB: plan.TotalGB, DurationDays: plan.DurationDays, InboundID: plan.InboundID,
		FaceValueToman: plan.PriceToman, PaidToman: paid,
		Status: "available", BuyerTGID: buyerTG,
		IssuedByAdmin: adminIssue, AdminNote: note,
		ExpiresAt: &exp, CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&g).Error; err != nil {
		return g, err
	}
	return g, nil
}

// RedeemGift applies the gift to the redeemer's Telegram account (creates/renews client).
func RedeemGift(db *gorm.DB, code string, redeemerTG, chatID int64) (models.Client, models.GiftCard, error) {
	code = NormalizeGiftCode(code)
	if code == "" {
		return models.Client{}, models.GiftCard{}, fmt.Errorf("کد هدیه را وارد کنید")
	}
	var out models.Client
	var gift models.GiftCard
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).First(&gift).Error; err != nil {
			return fmt.Errorf("کد هدیه نامعتبر است")
		}
		if gift.Status != "available" {
			return fmt.Errorf("این کد قبلاً استفاده یا باطل شده است")
		}
		if gift.ExpiresAt != nil && time.Now().UTC().After(*gift.ExpiresAt) {
			_ = tx.Model(&gift).Update("status", "revoked").Error
			return fmt.Errorf("مهلت این کارت هدیه تمام شده است")
		}
		if gift.InboundID == 0 {
			return fmt.Errorf("کارت هدیه ناقص است (اینباند)")
		}
		days := gift.DurationDays
		if days < 1 {
			days = 30
		}
		now := time.Now()
		// renew existing client if linked
		var link models.TelegramCustomer
		renew := false
		var cl models.Client
		if tx.Where("telegram_user_id = ?", redeemerTG).First(&link).Error == nil && link.ClientID > 0 {
			if tx.First(&cl, link.ClientID).Error == nil {
				renew = true
			}
		}
		if renew {
			cl.TotalGB = gift.TotalGB
			cl.TrafficLimitBytes = 0
			cl.TrafficBaseUp = cl.Up + cl.TrafficBaseUp
			cl.TrafficBaseDown = cl.Down + cl.TrafficBaseDown
			cl.Up = 0
			cl.Down = 0
			base := now.UnixMilli()
			if cl.ExpiryTime > base {
				base = cl.ExpiryTime
			}
			cl.ExpiryTime = base + int64(days)*24*60*60*1000
			cl.Enable = true
			cl.SubscriptionEnabled = true
			cl.SubscriptionStatus = "active"
			cl.SubscriptionRevokedAt = nil
			if gift.PlanName != "" {
				cl.SubscriptionName = gift.PlanName + " (هدیه)"
			}
			if gift.InboundID > 0 {
				cl.InboundID = gift.InboundID
			}
			if err := tx.Save(&cl).Error; err != nil {
				return err
			}
		} else {
			sub := strings.ReplaceAll(uuid.NewString(), "-", "")
			if len(sub) > 32 {
				sub = sub[:32]
			}
			cl = models.Client{
				InboundID: gift.InboundID,
				Email:     fmt.Sprintf("gift_tg%d_%d", redeemerTG, now.Unix()),
				UUID:      uuid.NewString(), Password: uuid.NewString(),
				Enable: true, TotalGB: gift.TotalGB,
				ExpiryTime:               now.UnixMilli() + int64(days)*24*60*60*1000,
				SubID:                    sub,
				SubscriptionEnabled:      true,
				SubscriptionName:         gift.PlanName + " (هدیه)",
				SubscriptionStatus:       "active",
				SubscriptionTokenVersion: 1,
				SubscriptionAutoUpdate:   true,
				PurchasedAt:              now,
				SubscriptionRefreshMin:   15,
				SubscriptionAddressType:  "all",
			}
			if err := tx.Create(&cl).Error; err != nil {
				return err
			}
			_ = tx.Where("telegram_user_id = ?", redeemerTG).Delete(&models.TelegramCustomer{}).Error
			if err := tx.Create(&models.TelegramCustomer{
				TelegramUserID: redeemerTG, TelegramChatID: chatID,
				ClientID: cl.ID, SubID: cl.SubID, Email: cl.Email,
			}).Error; err != nil {
				return err
			}
		}
		nowT := now
		gift.Status = "redeemed"
		gift.RedeemerTGID = redeemerTG
		gift.RedeemedAt = &nowT
		if err := tx.Save(&gift).Error; err != nil {
			return err
		}
		out = cl
		return nil
	})
	return out, gift, err
}

func ListGiftCards(db *gorm.DB, limit int) []models.GiftCard {
	if limit <= 0 {
		limit = 50
	}
	var rows []models.GiftCard
	db.Order("id desc").Limit(limit).Find(&rows)
	return rows
}

func ListUserGifts(db *gorm.DB, tgID int64) (bought, redeemed []models.GiftCard) {
	db.Where("buyer_tg_id = ?", tgID).Order("id desc").Limit(30).Find(&bought)
	db.Where("redeemer_tg_id = ?", tgID).Order("id desc").Limit(30).Find(&redeemed)
	return
}

func RevokeGift(db *gorm.DB, code string) error {
	code = NormalizeGiftCode(code)
	res := db.Model(&models.GiftCard{}).Where("code = ? AND status = ?", code, "available").Update("status", "revoked")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("کد فعال پیدا نشد")
	}
	return nil
}

// LoyaltyDiscountPct returns configured loyalty renew discount (0–50).
func LoyaltyDiscountPct(db *gorm.DB) int {
	en := panelSetting(db, models.LoyaltyEnabledKey)
	if en == "0" || strings.EqualFold(en, "false") {
		return 0
	}
	// default enabled if unset
	pct, _ := strconv.Atoi(panelSetting(db, models.LoyaltyDiscountPctKey))
	if pct <= 0 {
		pct = 10
	}
	if pct > 50 {
		pct = 50
	}
	return pct
}

func SaveLoyalty(db *gorm.DB, enabled bool, pct int) error {
	en := "0"
	if enabled {
		en = "1"
	}
	if err := setPanel(db, models.LoyaltyEnabledKey, en); err != nil {
		return err
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 50 {
		pct = 50
	}
	return setPanel(db, models.LoyaltyDiscountPctKey, strconv.Itoa(pct))
}
