package shop

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const tmaPinKeyPrefix = "tma_pin_hash_"

func tmaPinKey(tgUserID int64) string {
	return tmaPinKeyPrefix + strconv.FormatInt(tgUserID, 10)
}

// NormalizePIN validates a 4–6 digit numeric PIN.
func NormalizePIN(pin string) (string, error) {
	pin = strings.TrimSpace(pin)
	if len(pin) < 4 || len(pin) > 6 {
		return "", fmt.Errorf("رمز باید ۴ تا ۶ رقم باشد")
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("رمز فقط باید عدد باشد")
		}
	}
	// weak patterns
	if pin == "0000" || pin == "1111" || pin == "1234" || pin == "123456" {
		return "", fmt.Errorf("رمز خیلی ساده است — عدد دیگری انتخاب کنید")
	}
	return pin, nil
}

// TMAPinEnabled reports whether the user has set a Mini App PIN.
func TMAPinEnabled(db *gorm.DB, tgUserID int64) bool {
	var st models.PanelSetting
	if db.Where("key = ?", tmaPinKey(tgUserID)).First(&st).Error != nil {
		return false
	}
	return strings.TrimSpace(st.Value) != ""
}

// SetTMAPIN stores a bcrypt hash of the PIN for this Telegram user.
func SetTMAPIN(db *gorm.DB, tgUserID int64, pin string) error {
	pin, err := NormalizePIN(pin)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("خطا در ذخیره رمز")
	}
	key := tmaPinKey(tgUserID)
	var st models.PanelSetting
	if db.Where("key = ?", key).First(&st).Error == nil {
		st.Value = string(hash)
		st.UpdatedAt = time.Now()
		return db.Save(&st).Error
	}
	return db.Create(&models.PanelSetting{Key: key, Value: string(hash)}).Error
}

// VerifyTMAPIN checks the PIN against the stored hash.
func VerifyTMAPIN(db *gorm.DB, tgUserID int64, pin string) error {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return fmt.Errorf("رمز را وارد کنید")
	}
	var st models.PanelSetting
	if db.Where("key = ?", tmaPinKey(tgUserID)).First(&st).Error != nil || st.Value == "" {
		return fmt.Errorf("رمز تنظیم نشده است")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(st.Value), []byte(pin)); err != nil {
		return fmt.Errorf("رمز نادرست است")
	}
	return nil
}

// ClearTMAPIN removes the PIN after successful verification.
func ClearTMAPIN(db *gorm.DB, tgUserID int64) error {
	return db.Where("key = ?", tmaPinKey(tgUserID)).Delete(&models.PanelSetting{}).Error
}
