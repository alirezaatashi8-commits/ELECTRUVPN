package models

import "time"

type ClientDevice struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ClientID     uint       `gorm:"uniqueIndex:ux_client_device,priority:1;index" json:"clientId"`
	DeviceID     string     `gorm:"size:128;uniqueIndex:ux_client_device,priority:2;index" json:"deviceId"`
	Name         string     `gorm:"size:128" json:"name"`
	ProfileID    uint       `gorm:"index" json:"profileId,omitempty"`          // SubscriptionProfile.ID
	ProfileKey   string     `gorm:"size:32;index" json:"profileKey,omitempty"` // denormalized ios|android|…
	Manufacturer string     `gorm:"size:64" json:"manufacturer,omitempty"`
	Model        string     `gorm:"size:128" json:"model,omitempty"`
	OS           string     `gorm:"size:32" json:"os,omitempty"`
	OSVersion    string     `gorm:"size:64" json:"osVersion,omitempty"`
	AppName      string     `gorm:"size:64" json:"appName,omitempty"`
	AppVersion   string     `gorm:"size:64" json:"appVersion,omitempty"`
	Architecture string     `gorm:"size:32" json:"architecture,omitempty"`
	Source       string     `gorm:"size:32;index" json:"source,omitempty"` // registration or subscription_refresh
	ModelSource  string     `gorm:"size:24" json:"modelSource,omitempty"`  // reported, client-hint, user-agent, unknown
	Active       bool       `gorm:"default:true;index" json:"active"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	IP           string     `gorm:"size:64" json:"-"`  // keyed HMAC of source IP; raw IP is never persisted or exposed by device APIs
	UserAgent    string     `gorm:"size:512" json:"-"` // intentionally not populated for privacy
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}
