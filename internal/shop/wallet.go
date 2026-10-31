package shop

import (
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func GetOrCreateWallet(db *gorm.DB, tgUserID int64) (models.Wallet, error) {
	var w models.Wallet
	if err := db.Where("telegram_user_id = ?", tgUserID).First(&w).Error; err == nil {
		return w, nil
	}
	w = models.Wallet{TelegramUserID: tgUserID, BalanceToman: 0}
	_ = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&w).Error
	if err := db.Where("telegram_user_id = ?", tgUserID).First(&w).Error; err != nil {
		return w, err
	}
	return w, nil
}

func ensureWalletLocked(tx *gorm.DB, tgUserID int64) (models.Wallet, error) {
	// OnConflict DO NOTHING first — never plain Create (Postgres aborts TX on unique violation).
	_ = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Wallet{
		TelegramUserID: tgUserID, BalanceToman: 0,
	}).Error
	var w models.Wallet
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("telegram_user_id = ?", tgUserID).First(&w).Error; err != nil {
		return w, err
	}
	return w, nil
}

func debitWalletTx(tx *gorm.DB, tgUserID, amount int64, kind, ref, note string) (models.Wallet, error) {
	if amount <= 0 {
		return models.Wallet{}, fmt.Errorf("amount must be positive")
	}
	w, err := ensureWalletLocked(tx, tgUserID)
	if err != nil {
		return w, err
	}
	if w.BalanceToman < amount {
		return w, fmt.Errorf("موجودی کافی نیست")
	}
	w.BalanceToman -= amount
	if err := tx.Save(&w).Error; err != nil {
		return w, err
	}
	if err := tx.Create(&models.WalletLedger{
		TelegramUserID: tgUserID, DeltaToman: -amount, BalanceAfter: w.BalanceToman,
		Kind: kind, Ref: ref, Note: note,
	}).Error; err != nil {
		return w, err
	}
	return w, nil
}

func creditWalletTx(tx *gorm.DB, tgUserID, amount int64, kind, ref, note string) (models.Wallet, error) {
	if amount <= 0 {
		return models.Wallet{}, fmt.Errorf("amount must be positive")
	}
	w, err := ensureWalletLocked(tx, tgUserID)
	if err != nil {
		return w, err
	}
	w.BalanceToman += amount
	if err := tx.Save(&w).Error; err != nil {
		return w, err
	}
	if err := tx.Create(&models.WalletLedger{
		TelegramUserID: tgUserID, DeltaToman: amount, BalanceAfter: w.BalanceToman,
		Kind: kind, Ref: ref, Note: note,
	}).Error; err != nil {
		return w, err
	}
	return w, nil
}

func CreditWallet(db *gorm.DB, tgUserID, amount int64, kind, ref, note string) (models.Wallet, error) {
	var w models.Wallet
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		w, err = creditWalletTx(tx, tgUserID, amount, kind, ref, note)
		return err
	})
	return w, err
}

func DebitWallet(db *gorm.DB, tgUserID, amount int64, kind, ref, note string) (models.Wallet, error) {
	var w models.Wallet
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		w, err = debitWalletTx(tx, tgUserID, amount, kind, ref, note)
		return err
	})
	return w, err
}

// FulfillWalletTopup credits wallet after admin approves a wallet_topup order (atomic + row lock).
func ClearAutoRenewMsgCooldown(db *gorm.DB, tgUserID int64) {
	_ = db.Where("key = ?", fmt.Sprintf("autorenew_msg_%d", tgUserID)).Delete(&models.PanelSetting{}).Error
}

func FulfillWalletTopup(db *gorm.DB, order *models.ShopOrder) error {
	if order == nil || order.PriceToman <= 0 {
		return fmt.Errorf("invalid topup amount")
	}
	tgID := order.TelegramUserID
	err := db.Transaction(func(tx *gorm.DB) error {
		var cur models.ShopOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&cur, order.ID).Error; err != nil {
			return err
		}
		if cur.Status == "approved" {
			*order = cur
			return nil
		}
		if cur.Status != "pending_review" && cur.Status != "pending_payment" && cur.Status != "processing" {
			return fmt.Errorf("وضعیت سفارش: %s", cur.Status)
		}
		if _, err := creditWalletTx(tx, cur.TelegramUserID, cur.PriceToman, "topup", fmt.Sprintf("order:%d", cur.ID), cur.PlanName); err != nil {
			return err
		}
		now := time.Now()
		cur.Status = "approved"
		cur.ReviewedAt = &now
		if order.ReviewedBy != "" {
			cur.ReviewedBy = order.ReviewedBy
		}
		if err := tx.Save(&cur).Error; err != nil {
			return err
		}
		*order = cur
		tgID = cur.TelegramUserID
		return nil
	})
	if err == nil {
		ClearAutoRenewMsgCooldown(db, tgID)
	}
	return err
}

// TryAutoRenew debits wallet and extends client only when due (expired / over-quota / disabled).
// TryAutoRenew only when expired or over-quota — never for admin-disabled accounts.
// No renew-cooldown: after top-up the next tick can renew immediately.
// Message spam for low balance is rate-limited only in the worker (autorenew_msg_*).
func TryAutoRenew(db *gorm.DB, link models.TelegramCustomer) (bool, string) {
	if !link.AutoRenewEnabled || link.AutoRenewPlanID == 0 || link.ClientID == 0 {
		return false, "auto-renew off"
	}
	var cl models.Client
	if db.First(&cl, link.ClientID).Error != nil {
		return false, "client missing"
	}
	st := strings.ToLower(strings.TrimSpace(cl.SubscriptionStatus))
	if st == "suspended" || st == "banned" || st == "disabled" || st == "manual_off" || st == "revoked" {
		return false, "manually disabled"
	}
	if cl.SubscriptionRevokedAt != nil {
		return false, "revoked"
	}
	// Due = time or quota only. Admin Enable=false without status is NOT auto-renewed
	// unless also expired/over-quota (paid renew may re-enable — intentional for expired).
	due := cl.IsExpired() || cl.IsOverQuota()
	if !due {
		return false, "not due"
	}
	var plan models.ShopPlan
	if db.First(&plan, link.AutoRenewPlanID).Error != nil || !plan.Enabled {
		return false, "plan missing"
	}
	w, err := GetOrCreateWallet(db, link.TelegramUserID)
	if err != nil {
		return false, err.Error()
	}
	if plan.PriceToman > 0 && w.BalanceToman < plan.PriceToman {
		return false, "موجودی کافی نیست"
	}
	order := &models.ShopOrder{
		TelegramUserID: link.TelegramUserID, TelegramChatID: link.TelegramChatID,
		PlanID: plan.ID, PlanName: plan.Name + " (auto-renew)", TotalGB: plan.TotalGB,
		DurationDays: plan.DurationDays, PriceToman: plan.PriceToman,
		InboundID: plan.InboundID, Status: "pending_review", Kind: "plan",
	}
	// Order row created inside DebitAndFulfill only after debit succeeds
	if _, err := DebitAndFulfill(db, order, "renew", fmt.Sprintf("plan:%d", plan.ID), plan.Name); err != nil {
		return false, err.Error()
	}
	return true, plan.Name
}

func ListWalletLedger(db *gorm.DB, tgUserID int64, limit int) []models.WalletLedger {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var rows []models.WalletLedger
	db.Where("telegram_user_id = ?", tgUserID).Order("id desc").Limit(limit).Find(&rows)
	return rows
}

// ListUserOrders returns shop orders for transparency / invoices.
func ListUserOrders(db *gorm.DB, tgUserID int64, limit int) []models.ShopOrder {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var rows []models.ShopOrder
	db.Where("telegram_user_id = ?", tgUserID).Order("id desc").Limit(limit).Find(&rows)
	return rows
}
