package models

import "time"

// SubscriptionGroup is an administrator-managed bundle of clients. It is
// useful for multi-node or multi-inbound subscriptions while keeping clients
// embedded in their original Inbound in the UI.
type SubscriptionGroup struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:128;uniqueIndex" json:"name"`
	Token        string    `gorm:"size:64;uniqueIndex" json:"token"`
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	AddressType  string    `gorm:"size:16;default:all" json:"addressType"`
	RefreshMin   int       `gorm:"default:15" json:"refreshMin"`
	Preset       string    `gorm:"size:64;default:standard" json:"preset"`
	SmartEnabled bool      `gorm:"default:false" json:"smartEnabled"`
	HealthFilter string    `gorm:"size:32;default:healthy" json:"healthFilter"`
	NodeGroupID  uint      `gorm:"index" json:"nodeGroupId"`
	MaxLinks     int       `gorm:"default:0" json:"maxLinks"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
