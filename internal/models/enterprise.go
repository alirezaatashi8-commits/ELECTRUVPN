package models

import "time"

// AlertEvent is a persisted, deduplicated operational alert. Payload is kept
// as JSON so new metrics can be added without schema churn.
type AlertEvent struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Fingerprint string     `gorm:"size:128;uniqueIndex" json:"fingerprint"`
	Severity    string     `gorm:"size:16;index" json:"severity"`
	Kind        string     `gorm:"size:64;index" json:"kind"`
	Target      string     `gorm:"size:255;index" json:"target"`
	Message     string     `gorm:"size:1024" json:"message"`
	Payload     string     `gorm:"type:text" json:"payload"`
	FirstSeen   time.Time  `json:"firstSeen"`
	LastSeen    time.Time  `json:"lastSeen"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}
