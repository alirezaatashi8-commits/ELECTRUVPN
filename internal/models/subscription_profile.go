package models

import "time"

// SubscriptionProfile is a user-facing device category for organizing
// subscription links and observed devices (iPhone, Android, Windows, …).
type SubscriptionProfile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ClientID  uint      `gorm:"uniqueIndex:ux_client_profile_key,priority:1;index" json:"clientId"`
	Key       string    `gorm:"size:32;uniqueIndex:ux_client_profile_key,priority:2" json:"key"` // ios|android|windows|macos|linux|tv|router|other
	Label     string    `gorm:"size:64" json:"label"`                                            // e.g. "آیفون من"
	Icon      string    `gorm:"size:16" json:"icon,omitempty"`                                   // emoji hint
	SortOrder int       `gorm:"default:100" json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Known profile keys for UI pickers.
var ProfilePresets = []struct {
	Key, Label, Icon string
	Sort             int
}{
	{"ios", "آیفون / iOS", "🍎", 10},
	{"android", "اندروید", "🤖", 20},
	{"windows", "ویندوز", "🪟", 30},
	{"macos", "مک", "💻", 40},
	{"linux", "لینوکس", "🐧", 50},
	{"tv", "تلویزیون / TV", "📺", 60},
	{"router", "روتر", "📡", 70},
	{"other", "سایر", "📱", 90},
}
