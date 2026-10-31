package models

import "time"

// HoneypotProbe is one recorded scanner/probe interaction (for admin intel).
type HoneypotProbe struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	RemoteIP      string    `gorm:"size:64;index" json:"remoteIp"`
	RemotePort    int       `json:"remotePort,omitempty"`
	BytesRead     int64     `json:"bytesRead"`
	BytesWritten  int64     `json:"bytesWritten"`
	DurationMs    int64     `json:"durationMs"`
	Mode          string    `gorm:"size:32" json:"mode"` // tarpit | http_fake | tls_junk | fake_login | ssh_tarpit
	UserAgentHint string    `gorm:"size:128" json:"userAgentHint,omitempty"`
	Banned        bool      `gorm:"default:false;index" json:"banned"`
	Notified      bool      `gorm:"default:false" json:"notified"`
	CreatedAt     time.Time `gorm:"index" json:"createdAt"`
}

// HoneypotBan tracks IPs that were auto-banned (local + optional global list).
type HoneypotBan struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	IP         string     `gorm:"size:64;uniqueIndex" json:"ip"`
	Reason     string     `gorm:"size:255" json:"reason"`
	ProbeCount int        `json:"probeCount"`
	Source     string     `gorm:"size:32;default:honeypot" json:"source"` // honeypot | fail2ban | manual
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Panel setting keys for honeypot (advanced 2026).
const (
	HoneypotEnabledKey       = "honeypot_enabled"
	HoneypotListenKey        = "honeypot_listen"         // default 127.0.0.1:19601
	HoneypotExtraPortsKey    = "honeypot_extra_ports"    // comma list e.g. 2222,3389,8080 for fake services
	HoneypotMaxConnKey       = "honeypot_max_conn"       // global concurrent
	HoneypotPerIPKey         = "honeypot_per_ip"         // concurrent per IP
	HoneypotMinHoldKey       = "honeypot_min_hold_sec"   // min hold seconds
	HoneypotMaxHoldKey       = "honeypot_max_hold_sec"   // max hold seconds
	HoneypotAggressionKey    = "honeypot_aggression"     // 1-10 intensity
	HoneypotAutoInjectKey    = "honeypot_auto_inject"    // inject as Xray fallback
	HoneypotAutoBanKey       = "honeypot_auto_ban"       // ban IP after N probes
	HoneypotBanThresholdKey  = "honeypot_ban_threshold"  // default 3 probes in window
	HoneypotBanWindowKey     = "honeypot_ban_window_min" // default 30 minutes
	HoneypotBanDurationKey   = "honeypot_ban_duration_h" // hours, 0 = permanent until manual
	HoneypotTelegramAlertKey = "honeypot_telegram_alert" // 1 = notify admin group
	HoneypotFakeLoginKey     = "honeypot_fake_login"     // serve realistic fake admin login page
	HoneypotGlobalListKey    = "honeypot_global_list"    // optional URL to POST banned IPs
)
