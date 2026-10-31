package models

import "time"

// ServicePlan is a reusable quota/expiry/device policy. It is intentionally
// payment-provider agnostic so the panel can later be connected to a billing
// system without coupling the core database to one provider.
type ServicePlan struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Description string    `gorm:"size:512" json:"description"`
	TotalGB     int64     `gorm:"default:0" json:"totalGB"`
	LimitIP     int       `gorm:"default:0" json:"limitIp"`
	DurationDay int       `gorm:"default:0" json:"durationDay"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type UsageSnapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ClientID  uint      `gorm:"index:idx_usage_client_created,priority:1" json:"clientId"`
	Up        int64     `json:"up"`
	Down      int64     `json:"down"`
	CreatedAt time.Time `gorm:"index:idx_usage_client_created,priority:2" json:"createdAt"`
}
