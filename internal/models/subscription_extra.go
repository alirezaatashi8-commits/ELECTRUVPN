package models

import "time"

// SubscriptionPreset stores reusable delivery/security defaults for clients and groups.
type SubscriptionPreset struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Name              string    `gorm:"size:64;uniqueIndex" json:"name"`
	Description       string    `gorm:"size:255" json:"description"`
	AddressType       string    `gorm:"size:16;default:all" json:"addressType"`
	RefreshMin        int       `gorm:"default:15" json:"refreshMin"`
	AutoUpdate        bool      `gorm:"default:true" json:"autoUpdate"`
	SmartEnabled      bool      `gorm:"default:false" json:"smartEnabled"`
	MobileGuide       bool      `gorm:"default:true" json:"mobileGuide"`
	QR                bool      `gorm:"default:true" json:"qr"`
	Enabled           bool      `gorm:"default:true" json:"enabled"`
	ConfigJSON        string    `gorm:"type:text" json:"configJson"`
	GracePeriodHours  int       `gorm:"default:0" json:"gracePeriodHours"`
	MaxActiveSessions int       `gorm:"default:0" json:"maxActiveSessions"`
	DeviceBinding     bool      `gorm:"default:false" json:"deviceBinding"`
	NewDeviceApproval bool      `gorm:"default:false" json:"newDeviceApproval"`
	LinkLeakThreshold int       `gorm:"default:0" json:"linkLeakThreshold"`
	RenewalAuto       bool      `gorm:"default:false" json:"renewalAuto"`
	RenewalDays       int       `gorm:"default:0" json:"renewalDays"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// SubscriptionAccessLog is privacy-safe history: raw source IP is never stored.
type SubscriptionAccessLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ClientID      uint      `gorm:"index" json:"clientId"`
	GroupID       uint      `gorm:"index" json:"groupId"`
	AddressType   string    `gorm:"size:16;index" json:"addressType"`
	UserAgentHash string    `gorm:"size:64" json:"-"`
	IPHash        string    `gorm:"size:64" json:"-"`
	Status        int       `gorm:"index" json:"status"`
	Links         int       `json:"links"`
	CreatedAt     time.Time `gorm:"index" json:"createdAt"`
}
