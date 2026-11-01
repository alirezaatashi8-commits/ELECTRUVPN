package shop

import (
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PromoQuote is the result of validating a code against an order amount.
type PromoQuote struct {
	Code          string     `json:"code"`
	DiscountType  string     `json:"discountType"`
	DiscountValue int64      `json:"discountValue"`
	OriginalToman int64      `json:"originalToman"`
	DiscountToman int64      `json:"discountToman"`
	FinalToman    int64      `json:"finalToman"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	Message       string     `json:"message"`
}

func NormalizePromoCode(code string) string {
	code = strings.TrimSpace(code)
	code = strings.ToUpper(code)
	code = strings.ReplaceAll(code, " ", "")
	if len(code) > 64 {
		code = code[:64]
	}
	// strip non-alphanumeric except _ -
	var b strings.Builder
	for _, r := range code {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// QuotePromo validates a promo for a user + base price (does not increment usage).
func QuotePromo(db *gorm.DB, code string, tgUserID, originalToman int64) (PromoQuote, error) {
	code = NormalizePromoCode(code)
	q := PromoQuote{Code: code, OriginalToman: originalToman, FinalToman: originalToman}
	if code == "" {
		return q, fmt.Errorf("کد تخفیف را وارد کنید")
	}
	if originalToman < 0 {
		originalToman = 0
		q.OriginalToman = 0
	}
	var p models.PromoCode
	if err := db.Where("code = ?", code).First(&p).Error; err != nil {
		return q, fmt.Errorf("کد تخفیف نامعتبر است")
	}
	if !p.IsActive {
		return q, fmt.Errorf("این کد غیرفعال است")
	}
	if p.ExpiresAt != nil && time.Now().UTC().After(*p.ExpiresAt) {
		return q, fmt.Errorf("این کد منقضی شده است")
	}
	if p.MaxUses > 0 && p.UsedCount >= p.MaxUses {
		return q, fmt.Errorf("ظرفیت این کد تمام شده است")
	}
	if p.MinOrderToman > 0 && originalToman < p.MinOrderToman {
		return q, fmt.Errorf("حداقل مبلغ سفارش برای این کد %d تومان است", p.MinOrderToman)
	}
	if p.PerUserLimit > 0 && tgUserID > 0 {
		var n int64
		db.Model(&models.PromoUsage{}).Where("promo_code_id = ? AND telegram_user_id = ?", p.ID, tgUserID).Count(&n)
		if int(n) >= p.PerUserLimit {
			return q, fmt.Errorf("شما قبلاً از این کد استفاده کرده‌اید")
		}
	}

	disc := int64(0)
	switch strings.ToLower(p.DiscountType) {
	case "percent", "pct", "%":
		if p.DiscountValue < 1 || p.DiscountValue > 100 {
			return q, fmt.Errorf("تنظیمات کد نامعتبر است")
		}
		disc = originalToman * p.DiscountValue / 100
	case "fixed", "amount", "toman":
		disc = p.DiscountValue
		if disc > originalToman {
			disc = originalToman
		}
	default:
		return q, fmt.Errorf("نوع تخفیف نامعتبر است")
	}
	if disc < 0 {
		disc = 0
	}
	final := originalToman - disc
	if final < 0 {
		final = 0
	}
	q.DiscountType = p.DiscountType
	q.DiscountValue = p.DiscountValue
	q.DiscountToman = disc
	q.FinalToman = final
	q.ExpiresAt = p.ExpiresAt
	if strings.EqualFold(p.DiscountType, "percent") || p.DiscountType == "pct" || p.DiscountType == "%" {
		q.Message = fmt.Sprintf("%d%% تخفیف · %d تومان کمتر", p.DiscountValue, disc)
	} else {
		q.Message = fmt.Sprintf("%d تومان تخفیف", disc)
	}
	return q, nil
}

// CommitPromoUsage increments counters after an order is approved/paid.
// Idempotent per order_id.
func CommitPromoUsage(db *gorm.DB, order *models.ShopOrder) error {
	if order == nil || order.ID == 0 || order.PromoCode == "" {
		return nil
	}
	code := NormalizePromoCode(order.PromoCode)
	if len(code) > 64 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing models.PromoUsage
		if tx.Where("order_id = ?", order.ID).First(&existing).Error == nil {
			return nil
		}
		var p models.PromoCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).First(&p).Error; err != nil {
			return nil // code removed — ignore
		}
		if !p.IsActive {
			return nil
		}
		if p.ExpiresAt != nil && time.Now().UTC().After(*p.ExpiresAt) {
			return nil
		}
		if p.MaxUses > 0 && p.UsedCount >= p.MaxUses {
			return fmt.Errorf("ظرفیت کد تمام شده")
		}
		if err := tx.Create(&models.PromoUsage{
			PromoCodeID: p.ID, TelegramUserID: order.TelegramUserID,
			OrderID: order.ID, DiscountToman: order.DiscountToman,
			CreatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		// Atomic increment with capacity guard (prevents concurrent overshoot)
		q := tx.Model(&models.PromoCode{}).Where("id = ?", p.ID)
		if p.MaxUses > 0 {
			q = q.Where("used_count < ?", p.MaxUses)
		}
		res := q.Update("used_count", gorm.Expr("used_count + 1"))
		if res.Error != nil {
			return res.Error
		}
		if p.MaxUses > 0 && res.RowsAffected == 0 {
			return fmt.Errorf("ظرفیت کد تمام شده")
		}
		return nil
	})
}

// AdminCreatePromo creates or updates a promo code.
func AdminCreatePromo(db *gorm.DB, code, dtype string, value int64, maxUses int, expires *time.Time, minOrder int64, perUser int) (models.PromoCode, error) {
	code = NormalizePromoCode(code)
	if code == "" || len(code) > 64 {
		return models.PromoCode{}, fmt.Errorf("کد نامعتبر")
	}
	dtype = strings.ToLower(strings.TrimSpace(dtype))
	switch dtype {
	case "percent", "pct", "%":
		dtype = "percent"
		if value < 1 || value > 100 {
			return models.PromoCode{}, fmt.Errorf("درصد باید ۱ تا ۱۰۰ باشد")
		}
	case "fixed", "amount", "toman":
		dtype = "fixed"
		if value < 1 {
			return models.PromoCode{}, fmt.Errorf("مبلغ تخفیف باید مثبت باشد")
		}
	default:
		return models.PromoCode{}, fmt.Errorf("نوع: percent یا fixed")
	}
	if perUser < 0 {
		perUser = 1
	}
	var p models.PromoCode
	if db.Where("code = ?", code).First(&p).Error == nil {
		p.DiscountType = dtype
		p.DiscountValue = value
		p.MaxUses = maxUses
		p.MinOrderToman = minOrder
		p.PerUserLimit = perUser
		p.ExpiresAt = expires
		p.IsActive = true
		if err := db.Save(&p).Error; err != nil {
			return p, err
		}
		return p, nil
	}
	p = models.PromoCode{
		Code: code, DiscountType: dtype, DiscountValue: value,
		MaxUses: maxUses, MinOrderToman: minOrder, PerUserLimit: perUser,
		ExpiresAt: expires, IsActive: true, CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&p).Error; err != nil {
		return p, err
	}
	return p, nil
}

func AdminDisablePromo(db *gorm.DB, code string) error {
	code = NormalizePromoCode(code)
	res := db.Model(&models.PromoCode{}).Where("code = ?", code).Update("is_active", false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("کد پیدا نشد")
	}
	return nil
}

func ListPromos(db *gorm.DB, limit int) []models.PromoCode {
	if limit <= 0 {
		limit = 50
	}
	var rows []models.PromoCode
	db.Order("id desc").Limit(limit).Find(&rows)
	return rows
}
