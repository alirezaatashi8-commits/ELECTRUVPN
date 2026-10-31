package models

import "time"

// User is a panel operator account. Roles control API surface; quota fields
// limit how much traffic/config a non-owner admin (e.g. sales/reseller) may allocate.
type User struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	Username           string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash       string     `gorm:"size:255" json:"-"`
	Role               string     `gorm:"size:32;default:owner;index" json:"role"` // owner | admin | sales | support | operator | reseller
	Enabled            bool       `gorm:"default:true" json:"enabled"`
	MustChangePassword bool       `gorm:"default:true" json:"mustChangePassword"`
	PasswordChangedAt  *time.Time `json:"-"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
	LastLoginIP        string     `gorm:"size:64" json:"lastLoginIp"`
	// Quota: MaxTotalGB is the lifetime (or resettable) pool of GB this user may
	// assign to clients they create. 0 = unlimited (owners/admins typically).
	MaxTotalGB int64 `gorm:"default:0" json:"maxTotalGB"`
	// MaxGBPerClient caps TotalGB on any single client this user creates. 0 = unlimited.
	MaxGBPerClient int64 `gorm:"default:0" json:"maxGBPerClient"`
	// MaxClients caps how many clients this user may own/create. 0 = unlimited.
	MaxClients int `gorm:"default:0" json:"maxClients"`
	// AllocatedGB is the sum of TotalGB currently attributed to this user (maintained on create/update/delete).
	AllocatedGB int64 `gorm:"default:0" json:"allocatedGB"`
	// ClientCount is the number of clients attributed to this user.
	ClientCount int `gorm:"default:0" json:"clientCount"`

	// --- Multi-tier reseller (additive; zero values = not a reseller tree node) ---
	// ParentUserID links this account under another panel user (reseller hierarchy).
	// 0 means top-level (created by owner/admin, no parent).
	ParentUserID uint `gorm:"index;default:0" json:"parentUserId,omitempty"`
	// ResellerLevel is depth in the tree: 0 = not in tree / staff, 1 = direct reseller, 2 = sub-reseller, ...
	ResellerLevel int `gorm:"default:0" json:"resellerLevel,omitempty"`
	// CreditBalance is the reseller wallet (Toman or abstract credit units). Used to fund
	// client creation and to transfer credit to sub-resellers. Owners/admins are unlimited.
	CreditBalance int64 `gorm:"default:0" json:"creditBalance"`
	// AllowSubResellers permits this reseller to create child reseller accounts.
	AllowSubResellers bool `gorm:"default:false" json:"allowSubResellers"`
	// MaxSubResellers caps how many direct children this reseller may create. 0 = unlimited (if AllowSubResellers).
	MaxSubResellers int `gorm:"default:0" json:"maxSubResellers"`
	// CommissionPercent is informational wholesale margin (0–100) shown in reports; enforcement is optional.
	CommissionPercent int `gorm:"default:0" json:"commissionPercent"`
	// WholesalePricePerGB is the mandatory credit charge per GB when this reseller creates clients.
	// 0 = use global panel setting reseller_wholesale_price_per_gb.
	WholesalePricePerGB int64 `gorm:"default:0" json:"wholesalePricePerGB"`

	Notes     string    `gorm:"size:512" json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RemainingGB returns how many GB this user may still allocate. -1 means unlimited.
func (u User) RemainingGB() int64 {
	if u.MaxTotalGB <= 0 {
		return -1
	}
	left := u.MaxTotalGB - u.AllocatedGB
	if left < 0 {
		return 0
	}
	return left
}

// PanelSession tracks issued browser sessions so operators can inspect and revoke
// individual sessions without storing the raw JWT.
type PanelSession struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"index" json:"userId"`
	TokenID    string     `gorm:"size:64;uniqueIndex" json:"-"`
	IP         string     `gorm:"size:64" json:"ip"`
	UserAgent  string     `gorm:"size:512" json:"userAgent"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}
