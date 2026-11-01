package models

import "time"

// ServerNode represents a panel-managed node. The local node is always present;
// remote nodes can be registered for inventory/health checks and future agent control.
type ServerNode struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Name           string     `gorm:"size:128;uniqueIndex" json:"name"`
	Address        string     `gorm:"size:255" json:"address"`
	Port           int        `json:"port"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	Role           string     `gorm:"size:32;default:node" json:"role"`
	AgentEndpoint  string     `gorm:"size:1024" json:"agentEndpoint"`
	AgentTokenHash string     `gorm:"size:128" json:"-"`
	LastSeenAt     *time.Time `json:"lastSeenAt"`
	LastError      string     `gorm:"size:512" json:"lastError"`
	DrainState     string     `gorm:"size:32;default:active" json:"drainState"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type APIKey struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	Name       string     `gorm:"size:128" json:"name"`
	Prefix     string     `gorm:"size:16;uniqueIndex" json:"prefix"`
	SecretHash string     `gorm:"size:128" json:"-"`
	Scopes     string     `gorm:"type:text" json:"scopes"`
	Enabled    bool       `gorm:"default:true" json:"enabled"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type AuditLog struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	UserID          uint      `gorm:"index" json:"userId,omitempty"`
	Username        string    `gorm:"size:64;index" json:"username"`
	Role            string    `gorm:"size:32;index" json:"role,omitempty"`
	Action          string    `gorm:"size:128;index" json:"action"`
	Target          string    `gorm:"size:255;index" json:"target"`
	IP              string    `gorm:"size:64;index" json:"ip"`
	Source          string    `gorm:"size:32;index;default:panel" json:"source,omitempty"` // panel | telegram | tma | system
	ActorTelegramID int64     `gorm:"index" json:"actorTelegramId,omitempty"`
	Details         string    `gorm:"type:text" json:"details"`
	CreatedAt       time.Time `gorm:"index" json:"createdAt"`
}

type BackupRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Path      string    `gorm:"size:512" json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}
