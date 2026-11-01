package models

import "time"

// ClientReplayEvent is a point-in-time snapshot for support "Replay Studio".
// It records what mattered about a client when something happened so staff
// can reconstruct: config state, inbound, limits, and last known error context.
type ClientReplayEvent struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ClientID  uint   `gorm:"index;not null" json:"clientId"`
	Email     string `gorm:"size:128;index" json:"email"`
	InboundID uint   `gorm:"index" json:"inboundId"`
	// EventType: created|updated|enabled|disabled|kick|traffic_reset|secret_rotate|
	// sub_access|sub_denied|device_limit|quota|expired|delete|note|error
	EventType string `gorm:"size:32;index" json:"eventType"`
	Summary   string `gorm:"size:255" json:"summary"`
	// SnapshotJSON stores a compact JSON blob of client+inbound fields at the time.
	SnapshotJSON string `gorm:"type:text" json:"snapshotJson,omitempty"`
	// ContextJSON optional extra (request IP, user-agent, error string, actor).
	ContextJSON string    `gorm:"type:text" json:"contextJson,omitempty"`
	Actor       string    `gorm:"size:64" json:"actor,omitempty"`      // admin username or system
	Source      string    `gorm:"size:32;default:panel" json:"source"` // panel|subscription|system|telegram
	CreatedAt   time.Time `gorm:"index" json:"createdAt"`
}
