package models

import "time"

// ClientConfigProfile gives one logical client additional VLESS transport
// profiles without duplicating quota, expiry, device policy, or credentials.
// The referenced inbound must be VLESS; the runtime generator injects the
// client's UUID/flow into that inbound when rebuilding Xray.
type ClientConfigProfile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ClientID  uint      `gorm:"index;not null;uniqueIndex:idx_client_profile_inbound" json:"clientId"`
	InboundID uint      `gorm:"index;not null;uniqueIndex:idx_client_profile_inbound" json:"inboundId"`
	Name      string    `gorm:"size:128" json:"name"`
	Priority  int       `gorm:"default:100" json:"priority"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
