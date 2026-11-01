package models

import "time"

// AutomationRule describes a deterministic condition/action pair evaluated by
// the control-plane scheduler. Payload is JSON so new actions can be added
// without changing the schema.
type AutomationRule struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"size:128;uniqueIndex" json:"name"`
	Enabled     bool       `gorm:"default:true;index" json:"enabled"`
	Event       string     `gorm:"size:64;index" json:"event"`
	Condition   string     `gorm:"type:text" json:"condition"`
	Action      string     `gorm:"size:64" json:"action"`
	Payload     string     `gorm:"type:text" json:"payload"`
	CooldownSec int        `gorm:"default:300" json:"cooldownSec"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	LastError   string     `gorm:"size:512" json:"lastError,omitempty"`
	RunCount    int64      `gorm:"default:0" json:"runCount"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type ScheduledTask struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Name      string     `gorm:"size:128;uniqueIndex" json:"name"`
	Enabled   bool       `gorm:"default:true;index" json:"enabled"`
	Schedule  string     `gorm:"size:64" json:"schedule"` // @every 5m, @hourly, or 5-part cron
	Kind      string     `gorm:"size:64;index" json:"kind"`
	Payload   string     `gorm:"type:text" json:"payload"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	LastRunAt *time.Time `json:"lastRunAt,omitempty"`
	LastError string     `gorm:"size:512" json:"lastError,omitempty"`
	RunCount  int64      `gorm:"default:0" json:"runCount"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}
