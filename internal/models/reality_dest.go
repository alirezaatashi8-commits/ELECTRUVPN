package models

import "time"

// RealityDestPool is a curated list of Reality-compatible destinations (not random CDN noise).
// Health is probed periodically; applying a dest to an inbound is always admin-triggered
// (or explicit API) — never silent key rotation.
type RealityDestPool struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Domain      string     `gorm:"size:255;uniqueIndex;not null" json:"domain"`
	DestPort    int        `gorm:"default:443" json:"destPort"`
	Provider    string     `gorm:"size:64" json:"provider,omitempty"`
	Notes       string     `gorm:"size:255" json:"notes,omitempty"`
	IsActive    bool       `gorm:"default:true;index" json:"isActive"`
	Healthy     bool       `gorm:"default:false;index" json:"healthy"`
	LatencyMs   int64      `gorm:"default:0" json:"latencyMs"`
	LastError   string     `gorm:"size:255" json:"lastError,omitempty"`
	LastCheckAt *time.Time `json:"lastCheckAt,omitempty"`
	Priority    int        `gorm:"default:100" json:"priority"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func (r RealityDestPool) DestAddress() string {
	port := r.DestPort
	if port <= 0 {
		port = 443
	}
	return fmtDomainPort(r.Domain, port)
}

func fmtDomainPort(domain string, port int) string {
	return domain + ":" + itoaPort(port)
}

func itoaPort(n int) string {
	if n <= 0 {
		return "443"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
