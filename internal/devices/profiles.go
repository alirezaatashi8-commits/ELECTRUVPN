package devices

import (
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// EnsureDefaultProfiles seeds standard device categories for a client (idempotent).
func EnsureDefaultProfiles(db *gorm.DB, clientID uint) error {
	if clientID == 0 {
		return nil
	}
	for _, p := range models.ProfilePresets {
		var n int64
		db.Model(&models.SubscriptionProfile{}).Where("client_id = ? AND key = ?", clientID, p.Key).Count(&n)
		if n > 0 {
			continue
		}
		row := models.SubscriptionProfile{
			ClientID: clientID, Key: p.Key, Label: p.Label, Icon: p.Icon,
			SortOrder: p.Sort, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := db.Create(&row).Error; err != nil {
			// unique race — ignore
			_ = err
		}
	}
	return nil
}

func ListProfiles(db *gorm.DB, clientID uint) ([]models.SubscriptionProfile, error) {
	_ = EnsureDefaultProfiles(db, clientID)
	var rows []models.SubscriptionProfile
	err := db.Where("client_id = ?", clientID).Order("sort_order asc, id asc").Find(&rows).Error
	return rows, err
}

func UpsertProfile(db *gorm.DB, clientID uint, key, label string) (models.SubscriptionProfile, error) {
	key = normalizeProfileKey(key)
	if key == "" {
		return models.SubscriptionProfile{}, fmt.Errorf("کلید پروفایل نامعتبر است")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		for _, p := range models.ProfilePresets {
			if p.Key == key {
				label = p.Label
				break
			}
		}
		if label == "" {
			label = key
		}
	}
	if len(label) > 64 {
		label = label[:64]
	}
	icon := "📱"
	sort := 100
	for _, p := range models.ProfilePresets {
		if p.Key == key {
			icon = p.Icon
			sort = p.Sort
			break
		}
	}
	var row models.SubscriptionProfile
	err := db.Where("client_id = ? AND key = ?", clientID, key).First(&row).Error
	if err == nil {
		row.Label = label
		row.UpdatedAt = time.Now()
		return row, db.Save(&row).Error
	}
	row = models.SubscriptionProfile{
		ClientID: clientID, Key: key, Label: label, Icon: icon,
		SortOrder: sort, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	return row, db.Create(&row).Error
}

func RenameProfile(db *gorm.DB, clientID, profileID uint, label string) error {
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 64 {
		return fmt.Errorf("نام نامعتبر")
	}
	res := db.Model(&models.SubscriptionProfile{}).
		Where("id = ? AND client_id = ?", profileID, clientID).
		Updates(map[string]any{"label": label, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("پروفایل پیدا نشد")
	}
	return nil
}

// AssignDeviceProfile links a device to a profile key (and profile id if exists).
func AssignDeviceProfile(db *gorm.DB, clientID, deviceRowID uint, profileKey, customName string) error {
	profileKey = normalizeProfileKey(profileKey)
	_ = EnsureDefaultProfiles(db, clientID)
	var profileID uint
	if profileKey != "" {
		var p models.SubscriptionProfile
		if db.Where("client_id = ? AND key = ?", clientID, profileKey).First(&p).Error == nil {
			profileID = p.ID
		} else {
			created, err := UpsertProfile(db, clientID, profileKey, "")
			if err != nil {
				return err
			}
			profileID = created.ID
		}
	}
	updates := map[string]any{
		"profile_id":  profileID,
		"profile_key": profileKey,
		"updated_at":  time.Now(),
	}
	if n := strings.TrimSpace(customName); n != "" {
		if len(n) > 128 {
			n = n[:128]
		}
		updates["name"] = n
	}
	res := db.Model(&models.ClientDevice{}).
		Where("id = ? AND client_id = ?", deviceRowID, clientID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("دستگاه پیدا نشد")
	}
	return nil
}

func normalizeProfileKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	switch k {
	case "ios", "iphone", "ipad", "apple":
		return "ios"
	case "android", "اندروید":
		return "android"
	case "windows", "win", "pc":
		return "windows"
	case "macos", "mac", "osx":
		return "macos"
	case "linux":
		return "linux"
	case "tv", "androidtv", "appletv":
		return "tv"
	case "router", "modem":
		return "router"
	case "other", "unknown", "":
		if k == "" {
			return ""
		}
		return "other"
	default:
		if len(k) > 32 {
			k = k[:32]
		}
		return k
	}
}

// GuessProfileKey from OS / UA-ish fields already stored on device.
func GuessProfileKey(d models.ClientDevice) string {
	blob := strings.ToLower(d.OS + " " + d.Model + " " + d.Manufacturer + " " + d.Name)
	switch {
	case strings.Contains(blob, "ios") || strings.Contains(blob, "iphone") || strings.Contains(blob, "ipad"):
		return "ios"
	case strings.Contains(blob, "android"):
		return "android"
	case strings.Contains(blob, "windows"):
		return "windows"
	case strings.Contains(blob, "mac"):
		return "macos"
	case strings.Contains(blob, "linux"):
		return "linux"
	case strings.Contains(blob, "tv"):
		return "tv"
	default:
		return "other"
	}
}
