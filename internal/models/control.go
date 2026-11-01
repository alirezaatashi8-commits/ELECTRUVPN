package models

import "time"

// NodePerformanceSnapshot keeps a short historical series for capacity and
// routing decisions. It deliberately stores measurements, not inferred user data.
type NodePerformanceSnapshot struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	NodeID           uint      `gorm:"index:idx_perf_node_created,priority:1" json:"nodeId"`
	CPUPercent       float64   `json:"cpuPercent"`
	MemoryBytes      int64     `json:"memoryBytes"`
	MemoryTotalBytes int64     `json:"memoryTotalBytes"`
	DiskBytes        int64     `json:"diskBytes"`
	DiskTotalBytes   int64     `json:"diskTotalBytes"`
	RxBytes          int64     `json:"rxBytes"`
	TxBytes          int64     `json:"txBytes"`
	RTTMs            float64   `json:"rttMs"`
	PacketLoss       float64   `json:"packetLoss"`
	Congestion       string    `gorm:"size:32" json:"congestion"`
	Qdisc            string    `gorm:"size:32" json:"qdisc"`
	Healthy          bool      `json:"healthy"`
	CreatedAt        time.Time `gorm:"index:idx_perf_node_created,priority:2" json:"createdAt"`
}

type RoutingPolicy struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128;uniqueIndex" json:"name"`
	Strategy  string    `gorm:"size:32" json:"strategy"` // latency, load, capacity, weighted
	NodeIDs   string    `gorm:"type:text" json:"nodeIds"`
	Failover  bool      `gorm:"default:true" json:"failover"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Deployment struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"size:128" json:"name"`
	Target         string    `gorm:"size:128" json:"target"`
	Kind           string    `gorm:"size:64" json:"kind"`
	Status         string    `gorm:"size:32;index" json:"status"`
	VersionFrom    string    `gorm:"size:64" json:"versionFrom"`
	VersionTo      string    `gorm:"size:64" json:"versionTo"`
	ArtifactHash   string    `gorm:"size:64" json:"artifactHash"`
	Progress       int       `json:"progress"`
	RollbackOnFail bool      `gorm:"default:true" json:"rollbackOnFail"`
	JobID          uint      `json:"jobId"`
	Error          string    `gorm:"type:text" json:"error"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
