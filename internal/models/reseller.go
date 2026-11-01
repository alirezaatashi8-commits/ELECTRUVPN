package models

import "time"

// ResellerLedger is an append-only credit movement on a panel user's reseller wallet.
// Separate from Telegram shop Wallet / WalletLedger.
type ResellerLedger struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `gorm:"index;not null" json:"userId"`
	Delta        int64     `json:"delta"` // +credit / -debit
	BalanceAfter int64     `json:"balanceAfter"`
	Kind         string    `gorm:"size:32;index" json:"kind"` // topup | transfer_in | transfer_out | client_create | adjust | revoke
	Ref          string    `gorm:"size:64" json:"ref,omitempty"`
	Note         string    `gorm:"size:255" json:"note,omitempty"`
	ActorUserID  uint      `gorm:"index" json:"actorUserId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ResellerCreditOrder is a card-to-card request to top up a reseller's CreditBalance.
// Reuses shop card number / holder settings; no payment gateway.
type ResellerCreditOrder struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	ResellerUserID uint       `gorm:"index;not null" json:"resellerUserId"`
	AmountToman    int64      `gorm:"not null" json:"amountToman"`
	Status         string     `gorm:"size:32;default:pending_review;index" json:"status"` // pending_review | approved | rejected | cancelled
	ReceiptNote    string     `gorm:"size:512" json:"receiptNote,omitempty"`
	AdminNote      string     `gorm:"size:512" json:"adminNote,omitempty"`
	ReviewedBy     string     `gorm:"size:64" json:"reviewedBy,omitempty"`
	ReviewedAt     *time.Time `json:"reviewedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Reseller tree / policy / settings constants.
const (
	ResellerMaxDepthDefault = 5 // hard ceiling for sub-reseller nesting
	ResellerRoleName        = "reseller"
	// PanelSetting: global wholesale price (Toman credit units) charged per GB on client create.
	ResellerWholesalePricePerGBKey = "reseller_wholesale_price_per_gb"
	// PanelSetting: minimum card-to-card topup amount (Toman).
	ResellerTopupMinKey = "reseller_topup_min_toman"
)
