package models

import "time"

// TunnelChain: Client → panel entry → [optional mid hop(s)] → exit → Internet.
// Supports multi-hop chaining for strong DPI evasion (e.g. Iran → Germany → free net).
// SelectionMode: "manual" uses fixed addresses/node IDs; "auto" picks healthy nodes.
type TunnelChain struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"size:128" json:"name"`
	Enabled bool   `gorm:"default:false;index" json:"enabled"`

	// manual | auto
	SelectionMode string `gorm:"size:16;default:manual" json:"selectionMode"`
	// least_load | priority | latency — used when SelectionMode=auto
	AutoStrategy string `gorm:"size:32;default:least_load" json:"autoStrategy"`

	// Optional link to panel ServerNode inventory
	MidNodeID  uint `gorm:"index" json:"midNodeId,omitempty"`  // intermediate hub (e.g. Iran or relay)
	ExitNodeID uint `gorm:"index" json:"exitNodeId,omitempty"` // final exit (e.g. Germany)

	// --- Mid hop (optional) — if empty, traffic goes entry → exit only ---
	MidAddress     string `gorm:"size:255" json:"midAddress,omitempty"`
	MidPort        int    `json:"midPort,omitempty"`
	MidProtocol    string `gorm:"size:32;default:vless" json:"midProtocol,omitempty"`
	MidUUID        string `gorm:"size:64" json:"midUUID,omitempty"`
	MidPassword    string `gorm:"size:128" json:"midPassword,omitempty"`
	MidFlow        string `gorm:"size:64" json:"midFlow,omitempty"`
	MidSNI         string `gorm:"size:255" json:"midSni,omitempty"`
	MidPublicKey   string `gorm:"size:128" json:"midPublicKey,omitempty"`
	MidShortID     string `gorm:"size:32" json:"midShortId,omitempty"`
	MidFingerprint string `gorm:"size:32;default:chrome" json:"midFingerprint,omitempty"`
	MidNetwork     string `gorm:"size:32;default:tcp" json:"midNetwork,omitempty"`

	// --- Second mid hop (optional, for 3-hop: entry→mid1→mid2→exit) ---
	Mid2Address     string `gorm:"size:255" json:"mid2Address,omitempty"`
	Mid2Port        int    `json:"mid2Port,omitempty"`
	Mid2Protocol    string `gorm:"size:32;default:vless" json:"mid2Protocol,omitempty"`
	Mid2UUID        string `gorm:"size:64" json:"mid2UUID,omitempty"`
	Mid2Password    string `gorm:"size:128" json:"mid2Password,omitempty"`
	Mid2Flow        string `gorm:"size:64" json:"mid2Flow,omitempty"`
	Mid2SNI         string `gorm:"size:255" json:"mid2Sni,omitempty"`
	Mid2PublicKey   string `gorm:"size:128" json:"mid2PublicKey,omitempty"`
	Mid2ShortID     string `gorm:"size:32" json:"mid2ShortId,omitempty"`
	Mid2Fingerprint string `gorm:"size:32;default:chrome" json:"mid2Fingerprint,omitempty"`
	Mid2Network     string `gorm:"size:32;default:tcp" json:"mid2Network,omitempty"`
	Mid2NodeID      uint   `gorm:"index" json:"mid2NodeId,omitempty"`

	// Exit hop (required when enabled)
	ExitAddress     string `gorm:"size:255" json:"exitAddress"`
	ExitPort        int    `json:"exitPort"`
	ExitProtocol    string `gorm:"size:32;default:vless" json:"exitProtocol"`
	ExitUUID        string `gorm:"size:64" json:"exitUUID,omitempty"`
	ExitPassword    string `gorm:"size:128" json:"exitPassword,omitempty"`
	ExitFlow        string `gorm:"size:64" json:"exitFlow,omitempty"`
	ExitSNI         string `gorm:"size:255" json:"exitSni,omitempty"`
	ExitPublicKey   string `gorm:"size:128" json:"exitPublicKey,omitempty"`
	ExitShortID     string `gorm:"size:32" json:"exitShortId,omitempty"`
	ExitFingerprint string `gorm:"size:32;default:chrome" json:"exitFingerprint,omitempty"`
	ExitNetwork     string `gorm:"size:32;default:tcp" json:"exitNetwork,omitempty"`

	RotateAddresses string `gorm:"size:1024" json:"rotateAddresses,omitempty"`
	RotateOnFail    bool   `gorm:"default:true" json:"rotateOnFail"`
	// ConfirmBeforeFailover: when true, unhealthy ActiveExitHost triggers a
	// Telegram interactive prompt instead of automatic rotation.
	ConfirmBeforeFailover bool       `gorm:"default:true" json:"confirmBeforeFailover"`
	ActiveExitHost        string     `gorm:"size:255" json:"activeExitHost,omitempty"`
	ActiveMidHost         string     `gorm:"size:255" json:"activeMidHost,omitempty"`
	ActiveMid2Host        string     `gorm:"size:255" json:"activeMid2Host,omitempty"`
	LastFailAt            *time.Time `json:"lastFailAt,omitempty"`
	LastError             string     `gorm:"size:255" json:"lastError,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}
