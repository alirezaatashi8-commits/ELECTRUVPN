package models

import "time"

// PanelSetting stores small, operator-editable panel settings without requiring
// a schema migration for every new UI preference.
type PanelSetting struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"size:128;uniqueIndex" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConfigTemplate is a reusable inbound/client recipe. The payload is JSON so
// new Xray/sing-box transport fields can be added without a database rewrite.
type ConfigTemplate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Description string    `gorm:"size:512" json:"description"`
	Protocol    string    `gorm:"size:32" json:"protocol"`
	Network     string    `gorm:"size:32" json:"network"`
	Security    string    `gorm:"size:32" json:"security"`
	Payload     string    `gorm:"type:text" json:"payload"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NotificationRule describes a local event notification. Delivery is kept
// deliberately generic so Telegram/webhook/email can be added independently.
type NotificationRule struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128" json:"name"`
	Event     string    `gorm:"size:64;index" json:"event"`
	Channel   string    `gorm:"size:32" json:"channel"`
	Target    string    `gorm:"size:512" json:"target"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
