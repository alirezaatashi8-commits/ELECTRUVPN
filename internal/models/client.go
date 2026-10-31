package models

import "time"

// Client is a single end-user inside an Inbound: one VLESS UUID, one Trojan
// password, etc. Traffic and IP-limit enforcement all key off this row.
type Client struct {
	ID                       uint       `gorm:"primaryKey" json:"id"`
	InboundID                uint       `gorm:"index" json:"inboundId"`
	PlanID                   uint       `gorm:"index" json:"planId"`
	CreatedByUserID          uint       `gorm:"index" json:"createdByUserId,omitempty"` // panel admin who created this client (sales attribution)
	Email                    string     `gorm:"size:128;uniqueIndex" json:"email"`      // human label, also the xray "email" tag used for per-client stats
	UUID                     string     `gorm:"size:64" json:"uuid,omitempty"`          // vless / vmess
	Password                 string     `gorm:"size:128" json:"password,omitempty"`     // trojan / shadowsocks
	PrivateKey               string     `gorm:"size:128" json:"privateKey,omitempty"`
	PublicKey                string     `gorm:"size:128" json:"publicKey,omitempty"`
	Address                  string     `gorm:"size:64" json:"address,omitempty"`
	Flow                     string     `gorm:"size:32" json:"flow,omitempty"` // vless flow, e.g. xtls-rprx-vision
	Enable                   bool       `gorm:"default:true" json:"enable"`
	LimitIP                  int        `gorm:"default:0" json:"limitIp"`                     // 0 = unlimited concurrent IPs
	TotalGB                  int64      `gorm:"default:0" json:"totalGB"`                     // 0 = unlimited (unless TrafficLimitBytes > 0)
	TrafficLimitBytes        int64      `gorm:"default:0" json:"trafficLimitBytes,omitempty"` // precise byte cap (trials); 0 = use TotalGB only
	ExpiryTime               int64      `gorm:"default:0" json:"expiryTime"`                  // unix ms, 0 = never
	Up                       int64      `json:"up"`
	Down                     int64      `json:"down"`
	TrafficBaseUp            int64      `json:"-"`
	TrafficBaseDown          int64      `json:"-"`
	Group                    string     `gorm:"size:64;index" json:"group,omitempty"`
	AutoExtendGB             int64      `gorm:"default:0" json:"autoExtendGB,omitempty"`
	AutoExtendDays           int        `gorm:"default:0" json:"autoExtendDays,omitempty"`
	SubID                    string     `gorm:"size:32;uniqueIndex" json:"subId"` // token used in the subscription URL
	SubscriptionEnabled      bool       `gorm:"default:true" json:"subscriptionEnabled"`
	SubscriptionName         string     `gorm:"size:128" json:"subscriptionName,omitempty"`
	SubscriptionGroup        string     `gorm:"size:64;index" json:"subscriptionGroup,omitempty"`
	SubscriptionRefreshMin   int        `gorm:"default:15" json:"subscriptionRefreshMin"`
	SubscriptionAddressType  string     `gorm:"size:16;default:all" json:"subscriptionAddressType"`
	SubscriptionAutoUpdate   bool       `gorm:"default:true" json:"subscriptionAutoUpdate"`
	SubscriptionSmart        bool       `gorm:"default:false" json:"subscriptionSmart"`
	SubscriptionStatus       string     `gorm:"size:24;default:active;index" json:"subscriptionStatus"`
	SubscriptionTokenVersion int        `gorm:"default:1" json:"subscriptionTokenVersion"`
	SubscriptionRevokedAt    *time.Time `json:"subscriptionRevokedAt,omitempty"`
	SubscriptionHits         int64      `json:"subscriptionHits"`
	SubscriptionLastSeenAt   time.Time  `json:"subscriptionLastSeenAt,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	PurchasedAt              time.Time  `json:"purchasedAt,omitempty"`
	GracePeriodHours         int        `gorm:"default:0" json:"gracePeriodHours"`
	MaxActiveSessions        int        `gorm:"default:0" json:"maxActiveSessions"`
	DeviceBindingEnabled     bool       `gorm:"default:false" json:"deviceBindingEnabled"`
	NewDeviceApproval        bool       `gorm:"default:false" json:"newDeviceApproval"`
	LinkLeakThreshold        int        `gorm:"default:0" json:"linkLeakThreshold"`
	PrimaryAddressType       string     `gorm:"size:16;default:domain" json:"primaryAddressType"`
	RenewalAuto              bool       `gorm:"default:false" json:"renewalAuto"`
	RenewalDays              int        `gorm:"default:0" json:"renewalDays"`
	RenewalGraceUsed         bool       `gorm:"default:false" json:"renewalGraceUsed"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

// IsExpired reports whether the client's expiry date has passed.
func (c Client) IsExpired() bool {
	if c.ExpiryTime == 0 {
		return false
	}
	return time.Now().UnixMilli() >= c.ExpiryTime
}

// IsOverQuota reports whether the client has used up its traffic allowance.
func (c Client) IsOverQuota() bool {
	return quotaReached(c.Up, c.Down, c.TotalGB, c.TrafficLimitBytes)
}
