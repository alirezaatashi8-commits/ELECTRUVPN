package shop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

const (
	SensKindRevokeAll = "revoke_all"
	SensKindResetUUID = "reset_uuid"
	sensKeyPrefix     = "sens_act_"
	sensTTL           = 10 * time.Minute
)

type SensitivePending struct {
	Token     string    `json:"token"`
	TgUserID  int64     `json:"tgUserId"`
	ChatID    int64     `json:"chatId"`
	Kind      string    `json:"kind"`
	ClientID  uint      `json:"clientId"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func sensKey(token string) string { return sensKeyPrefix + token }

func newSensToken() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSensitivePending stores a one-shot confirmation for a destructive action.
func CreateSensitivePending(db *gorm.DB, tgUserID, chatID int64, kind string, clientID uint) (SensitivePending, error) {
	kind = strings.TrimSpace(kind)
	if kind != SensKindRevokeAll && kind != SensKindResetUUID {
		return SensitivePending{}, fmt.Errorf("نوع عملیات نامعتبر")
	}
	if clientID == 0 {
		return SensitivePending{}, fmt.Errorf("کلاینت مشخص نیست")
	}
	// clear previous pending for this user+kind
	_ = clearUserPending(db, tgUserID, kind)

	tok, err := newSensToken()
	if err != nil {
		return SensitivePending{}, err
	}
	now := time.Now().UTC()
	p := SensitivePending{
		Token: tok, TgUserID: tgUserID, ChatID: chatID, Kind: kind, ClientID: clientID,
		CreatedAt: now, ExpiresAt: now.Add(sensTTL),
	}
	raw, _ := json.Marshal(p)
	if err := db.Create(&models.PanelSetting{Key: sensKey(tok), Value: string(raw)}).Error; err != nil {
		return SensitivePending{}, err
	}
	return p, nil
}

func clearUserPending(db *gorm.DB, tgUserID int64, kind string) error {
	var rows []models.PanelSetting
	db.Where("key LIKE ?", sensKeyPrefix+"%").Find(&rows)
	for _, r := range rows {
		var p SensitivePending
		if json.Unmarshal([]byte(r.Value), &p) != nil {
			continue
		}
		if p.TgUserID == tgUserID && (kind == "" || p.Kind == kind) {
			_ = db.Delete(&models.PanelSetting{}, r.ID).Error
		}
	}
	return nil
}

// LoadSensitivePending returns pending action if valid and owned by tgUserID.
func LoadSensitivePending(db *gorm.DB, token string, tgUserID int64) (SensitivePending, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return SensitivePending{}, fmt.Errorf("توکن نامعتبر")
	}
	var st models.PanelSetting
	if db.Where("key = ?", sensKey(token)).First(&st).Error != nil {
		return SensitivePending{}, fmt.Errorf("درخواست تأیید پیدا نشد یا منقضی شده")
	}
	var p SensitivePending
	if err := json.Unmarshal([]byte(st.Value), &p); err != nil {
		return SensitivePending{}, fmt.Errorf("داده خراب است")
	}
	if p.TgUserID != tgUserID {
		return SensitivePending{}, fmt.Errorf("اجازه ندارید")
	}
	if time.Now().UTC().After(p.ExpiresAt) {
		_ = db.Where("key = ?", sensKey(token)).Delete(&models.PanelSetting{}).Error
		return SensitivePending{}, fmt.Errorf("مهلت تأیید تمام شده — دوباره از مینی‌اپ درخواست دهید")
	}
	return p, nil
}

// ConsumeSensitivePending deletes the pending row (one-shot).
func ConsumeSensitivePending(db *gorm.DB, token string) {
	_ = db.Where("key = ?", sensKey(token)).Delete(&models.PanelSetting{}).Error
}

func SensitiveKindLabel(kind string) string {
	switch kind {
	case SensKindRevokeAll:
		return "قطع همه دستگاه‌ها + کلید جدید"
	case SensKindResetUUID:
		return "Reset UUID / کلید جدید"
	default:
		return kind
	}
}
