package models

import "time"

// AgentNode extends a registered ServerNode with an optional remote agent.
type AgentNode struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	NodeID           uint       `gorm:"uniqueIndex" json:"nodeId"`
	TokenHash        string     `gorm:"size:128" json:"-"`
	Version          string     `gorm:"size:64" json:"version"`
	OS               string     `gorm:"size:128" json:"os"`
	Arch             string     `gorm:"size:64" json:"arch"`
	Hostname         string     `gorm:"size:255" json:"hostname"`
	AgentID          string     `gorm:"size:128;index" json:"agentId"`
	RemoteIP         string     `gorm:"size:64" json:"remoteIp"`
	CPUPercent       float64    `json:"cpuPercent"`
	MemoryBytes      int64      `json:"memoryBytes"`
	MemoryTotalBytes int64      `json:"memoryTotalBytes"`
	DiskBytes        int64      `json:"diskBytes"`
	DiskTotalBytes   int64      `json:"diskTotalBytes"`
	RxBytes          int64      `json:"rxBytes"`
	TxBytes          int64      `json:"txBytes"`
	LastSeenAt       *time.Time `json:"lastSeenAt"`
	LastError        string     `gorm:"size:512" json:"lastError"`
	Capabilities     string     `gorm:"type:text" json:"capabilities"`
	HealthState      string     `gorm:"size:32;default:unknown" json:"healthState"`
	Enabled          bool       `gorm:"default:true" json:"enabled"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type PlatformJob struct {
	ID          uint       `gorm:"primaryKey;index:idx_job_status_id,priority:2" json:"id"`
	Kind        string     `gorm:"size:64;index" json:"kind"`
	Status      string     `gorm:"size:32;index:idx_job_status_id,priority:1" json:"status"`
	Progress    int        `json:"progress"`
	Target      string     `gorm:"size:255" json:"target"`
	Payload     string     `gorm:"type:text" json:"payload"`
	Result      string     `gorm:"type:text" json:"result"`
	Error       string     `gorm:"type:text" json:"error"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	LeaseUntil  *time.Time `json:"leaseUntil"`
	LeaseToken  string     `gorm:"size:64;index" json:"-"`
	Attempts    int        `gorm:"default:0" json:"attempts"`
	OperationID string     `gorm:"size:64;index" json:"operationId"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type BillingPlan struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Currency    string    `gorm:"size:8;default:EUR" json:"currency"`
	AmountCents int64     `json:"amountCents"`
	PeriodDays  int       `json:"periodDays"`
	TrafficGB   int64     `json:"trafficGB"`
	DeviceLimit int       `json:"deviceLimit"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Order struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OrderNo     string    `gorm:"size:64;uniqueIndex" json:"orderNo"`
	ClientID    uint      `gorm:"index" json:"clientId"`
	PlanID      uint      `gorm:"index" json:"planId"`
	AmountCents int64     `json:"amountCents"`
	Currency    string    `gorm:"size:8" json:"currency"`
	Status      string    `gorm:"size:32;index" json:"status"`
	Provider    string    `gorm:"size:64" json:"provider"`
	ProviderRef string    `gorm:"size:255" json:"providerRef"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type WebhookEndpoint struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128" json:"name"`
	URL       string    `gorm:"size:1024" json:"url"`
	Secret    string    `gorm:"size:255" json:"-"`
	Events    string    `gorm:"type:text" json:"events"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CertificateRecord struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Name      string     `gorm:"size:128" json:"name"`
	Domain    string     `gorm:"size:255;index" json:"domain"`
	Issuer    string     `gorm:"size:255" json:"issuer"`
	ExpiresAt *time.Time `json:"expiresAt"`
	AutoRenew bool       `gorm:"default:true" json:"autoRenew"`
	CertPath  string     `gorm:"size:512" json:"certPath"`
	KeyPath   string     `gorm:"size:512" json:"keyPath"`
	Status    string     `gorm:"size:32" json:"status"`
	LastError string     `gorm:"size:512" json:"lastError"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type PluginRecord struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex" json:"name"`
	Version     string    `gorm:"size:64" json:"version"`
	Description string    `gorm:"size:512" json:"description"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Config      string    `gorm:"type:text" json:"config"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NodeClientTraffic stores the last absolute Xray counters seen from a specific
// node for a client email. Used by multi-node traffic accounting so each node's
// independent counters contribute only their positive deltas to the global
// client Up/Down totals. Unique on (node_id, email).
type NodeClientTraffic struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	NodeID    uint      `gorm:"uniqueIndex:idx_node_email,priority:1;index" json:"nodeId"`
	Email     string    `gorm:"size:128;uniqueIndex:idx_node_email,priority:2;index" json:"email"`
	LastUp    int64     `json:"lastUp"`
	LastDown  int64     `json:"lastDown"`
	UpdatedAt time.Time `json:"updatedAt"`
}
