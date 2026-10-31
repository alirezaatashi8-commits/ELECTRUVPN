package shop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TrialConfig struct {
	Enabled      bool  `json:"enabled"`
	MB           int64 `json:"mb"`
	Hours        int   `json:"hours"`
	InboundID    uint  `json:"inboundId"`
	MaxPerIPDay  int   `json:"maxPerIpDay"`  // anti-abuse: max trials from one IP per day
	MaxPerDevice int   `json:"maxPerDevice"` // anti-abuse: max trials per device fingerprint
}

// TrialClaimMeta carries request-side signals for anti-abuse checks.
type TrialClaimMeta struct {
	ClientIP string // HTTP client IP (TMA / API); empty for pure bot claims
	DeviceFP string // optional stable device fingerprint from client
}

func LoadTrialConfig(db *gorm.DB) TrialConfig {
	en := panelSetting(db, models.TrialEnabledKey)
	mb, _ := strconv.ParseInt(panelSetting(db, models.TrialMBKey), 10, 64)
	hours, _ := strconv.Atoi(panelSetting(db, models.TrialHoursKey))
	inb, _ := strconv.ParseUint(panelSetting(db, models.TrialInboundKey), 10, 64)
	maxIP, _ := strconv.Atoi(panelSetting(db, models.TrialMaxPerIPDayKey))
	maxDev, _ := strconv.Atoi(panelSetting(db, models.TrialMaxPerDeviceKey))
	if mb <= 0 {
		mb = 200
	}
	if hours <= 0 {
		hours = 2
	}
	if maxIP <= 0 {
		maxIP = 3
	}
	if maxDev <= 0 {
		maxDev = 1
	}
	return TrialConfig{
		Enabled:      en == "1" || strings.EqualFold(en, "true"),
		MB:           mb,
		Hours:        hours,
		InboundID:    uint(inb),
		MaxPerIPDay:  maxIP,
		MaxPerDevice: maxDev,
	}
}

func trialUsedKey(tgUserID int64) string {
	return fmt.Sprintf("trial_used_%d", tgUserID)
}

func hashToken(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:12])
}

func trialIPDayKey(ip string, day string) string {
	return fmt.Sprintf("trial_ip_%s_%s", day, hashToken(ip))
}

func trialDeviceKey(fp string) string {
	return fmt.Sprintf("trial_device_%s", hashToken(fp))
}

// HasUsedTrial reports whether this Telegram user already received a trial.
func HasUsedTrial(db *gorm.DB, tgUserID int64) bool {
	var st models.PanelSetting
	if db.Where("key = ?", trialUsedKey(tgUserID)).First(&st).Error == nil {
		return true
	}
	var n int64
	db.Model(&models.Client{}).Where("email LIKE ?", fmt.Sprintf("trial_tg%d_%%", tgUserID)).Count(&n)
	return n > 0
}

// CheckTrialAbuse returns an error if IP/device limits are exceeded.
// Empty IP/device skips that dimension (e.g. Telegram bot has no end-user IP).
func CheckTrialAbuse(db *gorm.DB, cfg TrialConfig, meta TrialClaimMeta) error {
	day := time.Now().UTC().Format("20060102")
	if ip := strings.TrimSpace(meta.ClientIP); ip != "" && cfg.MaxPerIPDay > 0 {
		// Skip obviously private/local if somehow present as only signal — still count real public IPs.
		key := trialIPDayKey(ip, day)
		var st models.PanelSetting
		count := 0
		if db.Where("key = ?", key).First(&st).Error == nil {
			count, _ = strconv.Atoi(st.Value)
		}
		if count >= cfg.MaxPerIPDay {
			return fmt.Errorf("از این شبکه امروز تعداد مجاز تست رایگان تمام شده است. لطفاً بعداً یا با خرید ادامه دهید")
		}
	}
	if fp := strings.TrimSpace(meta.DeviceFP); fp != "" && cfg.MaxPerDevice > 0 {
		key := trialDeviceKey(fp)
		var st models.PanelSetting
		if db.Where("key = ?", key).First(&st).Error == nil {
			// one claim recorded — enforce maxPerDevice (default 1)
			// count additional by value "n" if we store count
			n, _ := strconv.Atoi(st.Value)
			if n <= 0 {
				n = 1
			}
			if n >= cfg.MaxPerDevice {
				return fmt.Errorf("این دستگاه قبلاً تست رایگان دریافت کرده است")
			}
		}
	}
	return nil
}

func recordTrialAbuseSignals(tx *gorm.DB, cfg TrialConfig, meta TrialClaimMeta) error {
	day := time.Now().UTC().Format("20060102")
	if ip := strings.TrimSpace(meta.ClientIP); ip != "" {
		key := trialIPDayKey(ip, day)
		var st models.PanelSetting
		if tx.Where("key = ?", key).First(&st).Error == nil {
			n, _ := strconv.Atoi(st.Value)
			st.Value = strconv.Itoa(n + 1)
			if err := tx.Save(&st).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(&models.PanelSetting{Key: key, Value: "1"}).Error; err != nil {
				return err
			}
		}
	}
	if fp := strings.TrimSpace(meta.DeviceFP); fp != "" {
		key := trialDeviceKey(fp)
		var st models.PanelSetting
		if tx.Where("key = ?", key).First(&st).Error == nil {
			n, _ := strconv.Atoi(st.Value)
			if n <= 0 {
				n = 1
			}
			st.Value = strconv.Itoa(n + 1)
			if err := tx.Save(&st).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(&models.PanelSetting{Key: key, Value: "1"}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateTrialAccount creates a short-lived limited client (one per Telegram account).
// meta may carry IP / device fingerprint for anti-abuse (optional).
func CreateTrialAccount(db *gorm.DB, tgUserID, chatID int64, cfg TrialConfig, meta ...TrialClaimMeta) (models.Client, error) {
	var m TrialClaimMeta
	if len(meta) > 0 {
		m = meta[0]
	}
	if !cfg.Enabled {
		return models.Client{}, fmt.Errorf("تست رایگان فعلاً غیرفعال است")
	}
	if cfg.InboundID == 0 {
		return models.Client{}, fmt.Errorf("اینباند تست در پنل تنظیم نشده — Settings → فروشگاه → تست رایگان")
	}
	if cfg.Hours < 1 {
		cfg.Hours = 2
	}
	if cfg.MB < 1 {
		cfg.MB = 200
	}
	if HasUsedTrial(db, tgUserID) {
		return models.Client{}, fmt.Errorf("هر اکانت تلگرام فقط یک‌بار می‌تواند تست رایگان بگیرد")
	}
	if err := CheckTrialAbuse(db, cfg, m); err != nil {
		return models.Client{}, err
	}

	var out models.Client
	err := db.Transaction(func(tx *gorm.DB) error {
		// Claim slot first (unique key prevents double trial under concurrency)
		claim := models.PanelSetting{
			Key: trialUsedKey(tgUserID), Value: time.Now().UTC().Format(time.RFC3339),
		}
		if err := tx.Create(&claim).Error; err != nil {
			return fmt.Errorf("هر اکانت تلگرام فقط یک‌بار می‌تواند تست رایگان بگیرد")
		}
		// Re-check abuse inside tx and record counters
		if err := CheckTrialAbuse(tx, cfg, m); err != nil {
			return err
		}
		if err := recordTrialAbuseSignals(tx, cfg, m); err != nil {
			return err
		}

		var ib models.Inbound
		if err := tx.First(&ib, cfg.InboundID).Error; err != nil {
			return fmt.Errorf("اینباند تست پیدا نشد")
		}
		if !ib.Enable {
			return fmt.Errorf("اینباند تست غیرفعال است")
		}
		if ib.IsLimited() {
			return fmt.Errorf("اینباند تست به سقف حجم یا زمان رسیده است")
		}
		if ib.Protocol == models.ProtocolWireGuard {
			return fmt.Errorf("اینباند تست نباید WireGuard باشد (کلید اختصاصی هر کلاینت لازم است)")
		}

		now := time.Now()
		limitBytes := cfg.MB * 1024 * 1024
		totalGB := int64(0)
		if cfg.MB >= 1024 {
			totalGB = (cfg.MB + 1023) / 1024
		}
		sub := strings.ReplaceAll(uuid.NewString(), "-", "")
		if len(sub) > 32 {
			sub = sub[:32]
		}
		cl := models.Client{
			InboundID:                cfg.InboundID,
			Email:                    fmt.Sprintf("trial_tg%d_%d", tgUserID, now.Unix()),
			UUID:                     uuid.NewString(),
			Password:                 shopClientPassword(ib),
			Enable:                   true,
			TotalGB:                  totalGB,
			TrafficLimitBytes:        limitBytes,
			ExpiryTime:               now.UnixMilli() + int64(cfg.Hours)*3600*1000,
			SubID:                    sub,
			SubscriptionEnabled:      true,
			SubscriptionName:         "تست رایگان",
			SubscriptionStatus:       "active",
			SubscriptionTokenVersion: 1,
			SubscriptionAutoUpdate:   true,
			PurchasedAt:              now,
			SubscriptionRefreshMin:   15,
			SubscriptionAddressType:  "all",
			Group:                    fmt.Sprintf("trial:%dmb", cfg.MB),
			LimitIP:                  1,
			MaxActiveSessions:        1,
			DeviceBindingEnabled:     true,
		}
		if err := tx.Create(&cl).Error; err != nil {
			return err
		}

		var prev models.TelegramCustomer
		if tx.Where("telegram_user_id = ?", tgUserID).First(&prev).Error == nil {
			if err := tx.Model(&prev).Updates(map[string]interface{}{
				"client_id": cl.ID, "telegram_chat_id": chatID,
				"sub_id": cl.SubID, "email": cl.Email,
			}).Error; err != nil {
				return err
			}
		} else {
			link := models.TelegramCustomer{
				TelegramUserID: tgUserID,
				TelegramChatID: chatID,
				ClientID:       cl.ID,
				SubID:          cl.SubID,
				Email:          cl.Email,
			}
			if err := tx.Create(&link).Error; err != nil {
				return err
			}
		}
		out = cl
		return nil
	})
	return out, err
}

// DisableExpiredTrials turns off trial clients whose expiry has passed.
// Returns the number of clients disabled (for traffic sync / regen).
func DisableExpiredTrials(db *gorm.DB) int {
	if db == nil {
		return 0
	}
	nowMs := time.Now().UnixMilli()
	// Trials are tagged by email prefix trial_tg... (see CreateTrialAccount).
	res := db.Model(&models.Client{}).
		Where("enable = ? AND expiry_time > 0 AND expiry_time <= ?", true, nowMs).
		Where("email LIKE ?", "trial_%").
		Update("enable", false)
	if res.Error != nil {
		return 0
	}
	return int(res.RowsAffected)
}
