package telegram

import (
	"os"
	"strconv"
	"strings"

	cfgcrypto "github.com/electruvpn/electruvpn/internal/crypto"
	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

const (
	KeyAdminToken = "telegram_admin_bot_token"
	KeyAdminIDs   = "telegram_admin_ids" // comma-separated numeric chat/user ids
	KeyUserToken  = "telegram_user_bot_token"
)

func isSecretSettingKey(key string) bool {
	switch key {
	case KeyAdminToken, KeyUserToken:
		return true
	default:
		return strings.Contains(key, "bot_token") || strings.Contains(key, "_secret")
	}
}

func masterKey() []byte {
	return cfgcrypto.MasterKeyFromEnv(os.Getenv("ELECTRU_CONFIG_ENCRYPTION_KEY"))
}

func GetSetting(db *gorm.DB, key string) string {
	var r models.PanelSetting
	if db.Where("key = ?", key).First(&r).Error != nil {
		return ""
	}
	val := strings.TrimSpace(r.Value)
	if !isSecretSettingKey(key) {
		return val
	}
	// Encrypted blob: decrypt or fail closed (never treat ciphertext as bot token).
	if strings.HasPrefix(val, "ev1:") {
		dec, err := cfgcrypto.DecryptWithMaster(masterKey(), val)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(dec)
	}
	// Legacy plaintext token still readable until MigrateSecretSettings encrypts it.
	return val
}

// MigrateSecretSettings encrypts legacy plaintext secret settings (bot tokens etc.)
// in place. It is a no-op when ELECTRU_CONFIG_ENCRYPTION_KEY is not configured and
// returns how many rows were encrypted.
func MigrateSecretSettings(db *gorm.DB) (int, error) {
	master := masterKey()
	if len(master) != 32 {
		return 0, nil
	}
	var rows []models.PanelSetting
	if err := db.Find(&rows).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if !isSecretSettingKey(r.Key) {
			continue
		}
		val := strings.TrimSpace(r.Value)
		if val == "" || strings.HasPrefix(val, "ev1:") {
			continue
		}
		enc, err := cfgcrypto.EncryptWithMaster(master, val)
		if err != nil {
			return n, err
		}
		if enc == "" || enc == val {
			continue
		}
		if err := db.Model(&models.PanelSetting{}).Where("id = ?", r.ID).Update("value", enc).Error; err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func SetSetting(db *gorm.DB, key, value string) error {
	value = strings.TrimSpace(value)
	if isSecretSettingKey(key) && value != "" {
		if enc, err := cfgcrypto.EncryptWithMaster(masterKey(), value); err == nil && enc != "" {
			value = enc
		}
	}
	var r models.PanelSetting
	err := db.Where("key = ?", key).First(&r).Error
	if err != nil {
		return db.Create(&models.PanelSetting{Key: key, Value: value}).Error
	}
	return db.Model(&r).Update("value", value).Error
}

func ParseAdminIDs(raw string) map[int64]bool {
	out := map[int64]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err == nil {
			out[n] = true
		}
	}
	return out
}

const (
	KeyBotMaintenance  = "telegram_bot_maintenance"  // "1" = on
	KeyRequiredChannel = "telegram_required_channel" // @channel or -100id
	KeyChannelEnforce  = "telegram_channel_enforce"  // "1" = must join
)

func MaintenanceOn(db *gorm.DB) bool {
	v := GetSetting(db, KeyBotMaintenance)
	return v == "1" || strings.EqualFold(v, "true")
}

func ChannelEnforceOn(db *gorm.DB) bool {
	v := GetSetting(db, KeyChannelEnforce)
	return (v == "1" || strings.EqualFold(v, "true")) && len(RequiredChannelList(db)) > 0
}

func RequiredChannel(db *gorm.DB) string {
	chs := RequiredChannelList(db)
	if len(chs) == 0 {
		return ""
	}
	return chs[0]
}

const (
	// Multi-channel: comma-separated @ch1,@ch2,-100id
	KeyRequiredChannels = "telegram_required_channels"
	// Exempt users with active (non-expired, enabled) subscription from channel gate
	KeyChannelExemptActive = "telegram_channel_exempt_active" // "1" = yes
)

// RequiredChannelList returns all required channels (multi + legacy single).
func RequiredChannelList(db *gorm.DB) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(GetSetting(db, KeyRequiredChannels))
	add(GetSetting(db, KeyRequiredChannel)) // legacy
	return out
}

func ChannelExemptActive(db *gorm.DB) bool {
	v := GetSetting(db, KeyChannelExemptActive)
	return v == "1" || strings.EqualFold(v, "true") || v == ""
}
