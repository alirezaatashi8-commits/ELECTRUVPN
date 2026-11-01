package models

import "time"

// ShopPlan is a sellable subscription package (GB + days + price in Toman).
type ShopPlan struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:128" json:"name"`
	Description  string    `gorm:"size:512" json:"description"`
	TotalGB      int64     `gorm:"default:0" json:"totalGB"` // 0 = unlimited traffic
	DurationDays int       `gorm:"default:30" json:"durationDays"`
	PriceToman   int64     `gorm:"default:0" json:"priceToman"`
	InboundID    uint      `gorm:"index" json:"inboundId"` // config template / inbound to attach client
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	SortOrder    int       `gorm:"default:0" json:"sortOrder"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ShopOrder is a card-to-card purchase awaiting admin approval.
type ShopOrder struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	TelegramUserID     int64      `gorm:"index" json:"telegramUserId"`
	TelegramChatID     int64      `gorm:"index" json:"telegramChatId"`
	TelegramName       string     `gorm:"size:128" json:"telegramName"`
	PlanID             uint       `gorm:"index" json:"planId"`
	PlanName           string     `gorm:"size:128" json:"planName"`
	TotalGB            int64      `json:"totalGB"`
	DurationDays       int        `json:"durationDays"`
	PriceToman         int64      `json:"priceToman"`
	InboundID          uint       `json:"inboundId"`
	Status             string     `gorm:"size:32;default:pending_payment;index" json:"status"` // pending_payment | pending_review | approved | rejected | cancelled
	Kind               string     `gorm:"size:32;default:plan;index" json:"kind"`              // plan | wallet_topup
	ReceiptFileID      string     `gorm:"size:256" json:"receiptFileId,omitempty"`
	ReceiptNote        string     `gorm:"size:512" json:"receiptNote,omitempty"`
	ClientID           uint       `gorm:"index" json:"clientId,omitempty"`
	AdminNote          string     `gorm:"size:512" json:"adminNote,omitempty"`
	ReviewedBy         string     `gorm:"size:64" json:"reviewedBy,omitempty"`
	ReviewedAt         *time.Time `json:"reviewedAt,omitempty"`
	PromoCode          string     `gorm:"size:64;index" json:"promoCode,omitempty"`
	OriginalPriceToman int64      `json:"originalPriceToman,omitempty"`
	DiscountToman      int64      `json:"discountToman,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// TelegramCustomer links a Telegram user to a VPN client for status/renewal.
// A user may own multiple clients; uniqueness is (telegram_user_id, client_id).
type TelegramCustomer struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	TelegramUserID   int64     `gorm:"uniqueIndex:idx_tg_client;index" json:"telegramUserId"`
	TelegramChatID   int64     `gorm:"index" json:"telegramChatId"`
	ClientID         uint      `gorm:"uniqueIndex:idx_tg_client;index" json:"clientId"`
	SubID            string    `gorm:"size:32" json:"subId"`
	Email            string    `gorm:"size:128" json:"email"`
	AutoRenewEnabled bool      `gorm:"default:false" json:"autoRenewEnabled"`
	AutoRenewPlanID  uint      `gorm:"default:0" json:"autoRenewPlanId"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// ShopSettingsKey constants stored in PanelSetting.
const (
	ShopCardNumberKey   = "shop_card_number"
	ShopCardHolderKey   = "shop_card_holder"
	ShopInstructionsKey = "shop_payment_instructions"
	ShopEnabledKey      = "shop_enabled"
)

// Trial settings keys (PanelSetting).
const (
	TrialEnabledKey      = "trial_enabled"
	TrialMBKey           = "trial_mb"
	TrialHoursKey        = "trial_hours"
	TrialInboundKey      = "trial_inbound_id"
	TrialMaxPerIPDayKey  = "trial_max_per_ip_day"
	TrialMaxPerDeviceKey = "trial_max_per_device"
)

// Wallet holds Toman balance for a Telegram user.
type Wallet struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TelegramUserID int64     `gorm:"uniqueIndex" json:"telegramUserId"`
	BalanceToman   int64     `gorm:"default:0" json:"balanceToman"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// WalletLedger is an append-only movement on a wallet.
type WalletLedger struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TelegramUserID int64     `gorm:"index" json:"telegramUserId"`
	DeltaToman     int64     `json:"deltaToman"` // +credit / -debit
	BalanceAfter   int64     `json:"balanceAfter"`
	Kind           string    `gorm:"size:32" json:"kind"` // topup | renew | adjust | refund
	Ref            string    `gorm:"size:64" json:"ref,omitempty"`
	Note           string    `gorm:"size:255" json:"note,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Winback / churn-prevention settings (PanelSetting keys).
const (
	WinbackEnabledKey     = "winback_enabled"
	WinbackDaysAfterKey   = "winback_days_after"   // days after expiry before message (default 3)
	WinbackDiscountPctKey = "winback_discount_pct" // e.g. 30
	WinbackPlanIDsKey     = "winback_plan_ids"     // comma-separated plan IDs; empty = all enabled plans
	WinbackMessageKey     = "winback_message"      // optional custom text
)

// Custom volume (per-GB) purchase settings.
const (
	CustomGBEnabledKey = "shop_custom_gb_enabled"
	CustomGBPriceKey   = "shop_custom_gb_price"      // toman per 1 GB (monthly)
	CustomGBMinKey     = "shop_custom_gb_min"        // default 5
	CustomGBMaxKey     = "shop_custom_gb_max"        // default 500
	CustomGBDaysKey    = "shop_custom_gb_days"       // default 30
	CustomGBInboundKey = "shop_custom_gb_inbound_id" // 0 = first enabled inbound
)

// CloudBackup stores a recoverable snapshot so a user can restore VPN accounts
// after changing device or resetting Telegram. The BackupCode is the only
// secret the user must keep (shown once per create/rotate).
//
// Security (V9.7+):
//   - ExpiresAt: codes auto-expire (default 30 days) and cannot be restored after.
//   - FailedAttempts: after MaxCloudBackupFailedAttempts the code is disabled.
type CloudBackup struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	BackupCode      string     `gorm:"size:32;uniqueIndex" json:"backupCode"`
	CodeHash        string     `gorm:"size:64;index" json:"-"`
	OwnerTelegramID int64      `gorm:"index" json:"ownerTelegramId"`
	OwnerChatID     int64      `json:"ownerChatId"`
	PayloadJSON     string     `gorm:"type:text" json:"-"`        // snapshot of clients/orders/wallet meta
	ClientIDs       string     `gorm:"size:512" json:"clientIds"` // comma-separated for quick lookup
	Label           string     `gorm:"size:128" json:"label,omitempty"`
	RestoreCount    int        `gorm:"default:0" json:"restoreCount"`
	LastRestoreAt   *time.Time `json:"lastRestoreAt,omitempty"`
	LastRestoreTGID int64      `json:"lastRestoreTelegramId,omitempty"`
	FailedAttempts  int        `gorm:"default:0" json:"failedAttempts"`
	ExpiresAt       *time.Time `gorm:"index" json:"expiresAt,omitempty"`
	Enabled         bool       `gorm:"default:true;index" json:"enabled"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// Cloud backup security defaults.
const (
	CloudBackupDefaultTTLDays    = 30
	MaxCloudBackupFailedAttempts = 5
)

// Referral settings (PanelSetting keys). Admin controls whether and how much
// of a referred user's paid plan purchase is credited to the referrer's wallet.
const (
	ReferralEnabledKey           = "referral_enabled"             // "1" / "0"
	ReferralPercentKey           = "referral_percent"             // 0–50; 0 = no reward even if enabled
	ReferralFirstPurchaseOnlyKey = "referral_first_purchase_only" // "1" default: only first paid plan
	ReferralMinOrderTomanKey     = "referral_min_order_toman"     // minimum order amount to qualify
	ReferralBotUsernameKey       = "referral_bot_username"        // cached @username for deep links
)

// ReferralAttribution stores who invited a Telegram user (set once on /start ref_…).
type ReferralAttribution struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	TelegramUserID     int64     `gorm:"uniqueIndex" json:"telegramUserId"` // invitee
	ReferrerTelegramID int64     `gorm:"index" json:"referrerTelegramId"`
	Code               string    `gorm:"size:32;index" json:"code"`
	CreatedAt          time.Time `json:"createdAt"`
}

// ReferralReward is an audit row for each commission paid (idempotent per order).
type ReferralReward struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	OrderID            uint      `gorm:"uniqueIndex" json:"orderId"`
	ReferrerTelegramID int64     `gorm:"index" json:"referrerTelegramId"`
	InviteeTelegramID  int64     `gorm:"index" json:"inviteeTelegramId"`
	OrderAmountToman   int64     `json:"orderAmountToman"`
	Percent            int       `json:"percent"`
	RewardToman        int64     `json:"rewardToman"`
	CreatedAt          time.Time `json:"createdAt"`
}

// PromoCode is a dynamic discount / campaign code.
type PromoCode struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Code          string     `gorm:"size:64;uniqueIndex" json:"code"`      // normalized UPPER
	DiscountType  string     `gorm:"size:16;not null" json:"discountType"` // percent | fixed
	DiscountValue int64      `gorm:"not null" json:"discountValue"`        // percent 1-100 or fixed toman
	MaxUses       int        `gorm:"default:0" json:"maxUses"`             // 0 = unlimited
	UsedCount     int        `gorm:"default:0" json:"usedCount"`
	PerUserLimit  int        `gorm:"default:1" json:"perUserLimit"` // 0 = unlimited per user
	MinOrderToman int64      `gorm:"default:0" json:"minOrderToman"`
	ExpiresAt     *time.Time `gorm:"index" json:"expiresAt,omitempty"`
	IsActive      bool       `gorm:"default:true;index" json:"isActive"`
	Note          string     `gorm:"size:255" json:"note,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// PromoUsage records one successful application of a promo on an approved/paid order.
type PromoUsage struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	PromoCodeID    uint      `gorm:"index;uniqueIndex:idx_promo_user_order,priority:1" json:"promoCodeId"`
	TelegramUserID int64     `gorm:"index;uniqueIndex:idx_promo_user_order,priority:2" json:"telegramUserId"`
	OrderID        uint      `gorm:"uniqueIndex;uniqueIndex:idx_promo_user_order,priority:3" json:"orderId"`
	DiscountToman  int64     `json:"discountToman"`
	CreatedAt      time.Time `json:"createdAt"`
}

// GiftCard is a transferable subscription voucher (bought by user or issued by admin).
type GiftCard struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Code           string     `gorm:"size:32;uniqueIndex" json:"code"` // EVG-XXXX-XXXXXX
	PlanID         uint       `gorm:"index" json:"planId"`
	PlanName       string     `gorm:"size:128" json:"planName"`
	TotalGB        int64      `json:"totalGB"`
	DurationDays   int        `json:"durationDays"`
	InboundID      uint       `json:"inboundId"`
	FaceValueToman int64      `json:"faceValueToman"`                                // original plan price
	PaidToman      int64      `json:"paidToman"`                                     // what buyer paid (0 = admin issue)
	Status         string     `gorm:"size:16;index;default:available" json:"status"` // available | redeemed | revoked
	BuyerTGID      int64      `gorm:"column:buyer_tg_id;index" json:"buyerTelegramId,omitempty"`
	RedeemerTGID   int64      `gorm:"column:redeemer_tg_id;index" json:"redeemerTelegramId,omitempty"`
	OrderID        uint       `gorm:"index" json:"orderId,omitempty"` // purchase order if any
	IssuedByAdmin  bool       `gorm:"default:false" json:"issuedByAdmin"`
	AdminNote      string     `gorm:"size:255" json:"adminNote,omitempty"`
	RedeemedAt     *time.Time `json:"redeemedAt,omitempty"`
	ExpiresAt      *time.Time `gorm:"index" json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// Loyalty alert settings (PanelSetting keys).
const (
	LoyaltyDiscountPctKey = "loyalty_discount_pct" // percent off for near-expiry renew CTA (default 10)
	LoyaltyEnabledKey     = "loyalty_enabled"      // "1" / "0"
)

// BroadcastJob records a targeted Telegram push for audit/history.
type BroadcastJob struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Target      string    `gorm:"size:64;index" json:"target"`
	Message     string    `gorm:"type:text" json:"message"`
	Matched     int       `json:"matched"`
	SentOK      int       `json:"sentOk"`
	SentFail    int       `json:"sentFail"`
	ActorUserID uint      `gorm:"index" json:"actorUserId,omitempty"`
	ActorName   string    `gorm:"size:64" json:"actorName,omitempty"`
	Source      string    `gorm:"size:32" json:"source"` // panel | telegram
	CreatedAt   time.Time `gorm:"index" json:"createdAt"`
}
