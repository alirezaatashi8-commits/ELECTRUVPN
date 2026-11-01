// Package devices implements strict multi-device policy for ElectruVPN (2026).
// Limits are enforced at subscription fetch and explicit device registration;
// revoke invalidates the device and can rotate credentials so live sessions die.
package devices

import (
	"errors"
	"fmt"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRevoked       = errors.New("device revoked")
	ErrLimitReached  = errors.New("device limit reached")
	ErrBindingLocked = errors.New("device binding: unknown device not allowed")
	ErrNeedsApproval = errors.New("new device requires approval")
)

// EffectiveLimit returns the hard cap on concurrent active devices.
// Prefer MaxActiveSessions; fall back to LimitIP when sessions unset.
func EffectiveLimit(cl models.Client) int {
	if cl.MaxActiveSessions > 0 {
		return cl.MaxActiveSessions
	}
	if cl.LimitIP > 0 {
		return cl.LimitIP
	}
	return 0 // unlimited
}

// ActiveRecentCutoff is how long a device counts as "currently connected".
const ActiveRecentCutoff = 10 * time.Minute

// CountActive returns active (non-revoked) devices, optionally only recently seen.
func CountActive(db *gorm.DB, clientID uint, recentOnly bool) (int64, error) {
	q := db.Model(&models.ClientDevice{}).Where("client_id = ? AND active = ? AND revoked_at IS NULL", clientID, true)
	if recentOnly {
		q = q.Where("last_seen_at >= ?", time.Now().Add(-ActiveRecentCutoff))
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// Lookup finds a device by client + deviceId.
func Lookup(db *gorm.DB, clientID uint, deviceID string) (models.ClientDevice, error) {
	var d models.ClientDevice
	err := db.Where("client_id = ? AND device_id = ?", clientID, deviceID).First(&d).Error
	return d, err
}

// AllowAccess decides whether this device may pull the subscription / stay online.
func AllowAccess(db *gorm.DB, cl models.Client, deviceID string) error {
	if deviceID == "" {
		// No device fingerprint: only allow when no hard binding/limit is configured.
		if cl.DeviceBindingEnabled || cl.NewDeviceApproval {
			return ErrBindingLocked
		}
		return nil
	}

	d, err := Lookup(db, cl.ID, deviceID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	exists := err == nil

	if exists {
		if !d.Active || d.RevokedAt != nil {
			return ErrRevoked
		}
		// Known active device — always allowed.
		return nil
	}

	// Unknown device
	if cl.DeviceBindingEnabled {
		var n int64
		if err := db.Model(&models.ClientDevice{}).Where("client_id = ? AND active = ?", cl.ID, true).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrBindingLocked
		}
	}
	if cl.NewDeviceApproval {
		var n int64
		if err := db.Model(&models.ClientDevice{}).Where("client_id = ?", cl.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrNeedsApproval
		}
	}
	limit := EffectiveLimit(cl)
	if limit > 0 {
		// Count recently-active devices so offline slots can free up.
		n, err := CountActive(db, cl.ID, true)
		if err != nil {
			return err
		}
		if n >= int64(limit) {
			return ErrLimitReached
		}
	}
	return nil
}

// RegisterOrTouch creates or updates a device under the active policy (transactional).
func RegisterOrTouch(db *gorm.DB, cl models.Client, d *models.ClientDevice) error {
	if d == nil || d.DeviceID == "" {
		return fmt.Errorf("deviceId required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var cur models.ClientDevice
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("client_id = ? AND device_id = ?", cl.ID, d.DeviceID).First(&cur).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if e := AllowAccess(tx, cl, d.DeviceID); e != nil {
				return e
			}
			d.ClientID = cl.ID
			if cl.NewDeviceApproval {
				var known int64
				_ = tx.Model(&models.ClientDevice{}).Where("client_id = ?", cl.ID).Count(&known).Error
				d.Active = known == 0
			} else {
				d.Active = true
			}
			d.RevokedAt = nil
			d.LastSeenAt = time.Now()
			if d.ProfileKey == "" {
				d.ProfileKey = GuessProfileKey(*d)
			}
			return tx.Create(d).Error
		}
		// Existing
		if !cur.Active || cur.RevokedAt != nil {
			return ErrRevoked
		}
		// Merge metadata
		if d.Name != "" {
			cur.Name = d.Name
		}
		if d.Manufacturer != "" {
			cur.Manufacturer = d.Manufacturer
		}
		if d.Model != "" {
			cur.Model = d.Model
		}
		if d.OS != "" {
			cur.OS = d.OS
		}
		if d.OSVersion != "" {
			cur.OSVersion = d.OSVersion
		}
		if d.AppName != "" {
			cur.AppName = d.AppName
		}
		if d.AppVersion != "" {
			cur.AppVersion = d.AppVersion
		}
		if d.Architecture != "" {
			cur.Architecture = d.Architecture
		}
		if d.Source != "" {
			cur.Source = d.Source
		}
		if d.ModelSource != "" {
			cur.ModelSource = d.ModelSource
		}
		if d.IP != "" {
			cur.IP = d.IP
		}
		cur.LastSeenAt = time.Now()
		cur.Active = true
		cur.RevokedAt = nil
		*d = cur
		return tx.Save(&cur).Error
	})
}

// Revoke marks one device inactive. Returns true if a row was updated.
func Revoke(db *gorm.DB, clientID, deviceRowID uint) (bool, error) {
	now := time.Now()
	res := db.Model(&models.ClientDevice{}).
		Where("id = ? AND client_id = ? AND active = ?", deviceRowID, clientID, true).
		Updates(map[string]any{"active": false, "revoked_at": now})
	return res.RowsAffected > 0, res.Error
}

// RevokeAll deactivates every active device for a client.
func RevokeAll(db *gorm.DB, clientID uint) (int64, error) {
	now := time.Now()
	res := db.Model(&models.ClientDevice{}).
		Where("client_id = ? AND active = ?", clientID, true).
		Updates(map[string]any{"active": false, "revoked_at": now})
	return res.RowsAffected, res.Error
}

// ListForClient returns devices newest-first (active first).
func ListForClient(db *gorm.DB, clientID uint, limit int) ([]models.ClientDevice, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows []models.ClientDevice
	err := db.Where("client_id = ?", clientID).
		Order("active desc, last_seen_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// PublicView strips internal fields for API/TMA responses.
type PublicView struct {
	ID         uint      `json:"id"`
	DeviceID   string    `json:"deviceId"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	OSVersion  string    `json:"osVersion,omitempty"`
	Model      string    `json:"model,omitempty"`
	AppName    string    `json:"appName,omitempty"`
	ProfileID  uint      `json:"profileId,omitempty"`
	ProfileKey string    `json:"profileKey,omitempty"`
	Active     bool      `json:"active"`
	Revoked    bool      `json:"revoked"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	IsStale    bool      `json:"isStale"` // not seen within ActiveRecentCutoff
}

func ToPublic(d models.ClientDevice) PublicView {
	stale := d.Active && time.Since(d.LastSeenAt) > ActiveRecentCutoff
	return PublicView{
		ID: d.ID, DeviceID: d.DeviceID, Name: d.Name, OS: d.OS, OSVersion: d.OSVersion,
		Model: d.Model, AppName: d.AppName, ProfileID: d.ProfileID, ProfileKey: d.ProfileKey,
		Active:  d.Active && d.RevokedAt == nil,
		Revoked: d.RevokedAt != nil || !d.Active, LastSeenAt: d.LastSeenAt,
		CreatedAt: d.CreatedAt, IsStale: stale,
	}
}
