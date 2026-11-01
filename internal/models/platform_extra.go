package models

import "time"

type OperationRecord struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	OperationID    string     `gorm:"size:64;uniqueIndex" json:"operationId"`
	IdempotencyKey string     `gorm:"size:128;uniqueIndex" json:"-"`
	Type           string     `gorm:"size:96;index" json:"type"`
	Status         string     `gorm:"size:32;index" json:"status"`
	Target         string     `gorm:"size:255" json:"target"`
	Actor          string     `gorm:"size:64" json:"actor"`
	Phase          string     `gorm:"size:64" json:"phase"`
	Progress       int        `json:"progress"`
	RollbackOnFail bool       `gorm:"default:true" json:"rollbackOnFail"`
	SnapshotRef    string     `gorm:"size:255" json:"snapshotRef"`
	Payload        string     `gorm:"type:text" json:"payload"`
	Result         string     `gorm:"type:text" json:"result"`
	Error          string     `gorm:"type:text" json:"error"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type NodeGroup struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Strategy    string    `gorm:"size:32;default:health" json:"strategy"`
	Failover    bool      `gorm:"default:true" json:"failover"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Description string    `gorm:"size:512" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type NodeGroupMember struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GroupID   uint      `gorm:"uniqueIndex:idx_group_node,priority:1;index" json:"groupId"`
	NodeID    uint      `gorm:"uniqueIndex:idx_group_node,priority:2;index" json:"nodeId"`
	Priority  int       `gorm:"default:100" json:"priority"`
	Weight    int       `gorm:"default:100" json:"weight"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DomainRecord struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Domain         string     `gorm:"size:255;uniqueIndex" json:"domain"`
	Provider       string     `gorm:"size:32;default:manual" json:"provider"`
	Challenge      string     `gorm:"size:32;default:http-01" json:"challenge"`
	CertificateID  uint       `json:"certificateId"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	Primary        bool       `gorm:"default:false" json:"primary"`
	Priority       int        `gorm:"default:100" json:"priority"`
	HealthStatus   string     `gorm:"size:32;default:unknown" json:"healthStatus"`
	HealthFailures int        `json:"healthFailures"`
	LastHealthAt   *time.Time `json:"lastHealthAt"`
	LastLatencyMs  int64      `json:"lastLatencyMs"`
	LastError      string     `gorm:"size:512" json:"lastError"`
	LastFailoverAt *time.Time `json:"lastFailoverAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type CertificateRevision struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	CertificateID uint       `gorm:"index" json:"certificateId"`
	Version       int        `json:"version"`
	Issuer        string     `gorm:"size:255" json:"issuer"`
	Fingerprint   string     `gorm:"size:128" json:"fingerprint"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	CertPath      string     `gorm:"size:512" json:"certPath"`
	KeyPath       string     `gorm:"size:512" json:"keyPath"`
	Status        string     `gorm:"size:32" json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
}
