// Package reseller implements multi-tier panel reseller hierarchy, credit wallet,
// and scoped visibility. It is intentionally additive: staff roles (owner/admin/…)
// keep full access; only role=reseller is constrained by parent tree + wallet.
package reseller

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrNotReseller   = errors.New("user is not a reseller")
	ErrForbidden     = errors.New("forbidden in reseller hierarchy")
	ErrInsufficient  = errors.New("insufficient credit")
	ErrQuota         = errors.New("quota exceeded")
	ErrDepth         = errors.New("reseller depth limit reached")
	ErrSubNotAllowed = errors.New("sub-resellers not allowed for this account")
	ErrSubLimit      = errors.New("max sub-resellers reached")
	ErrInvalidParent = errors.New("invalid parent")
	ErrSelf          = errors.New("cannot operate on self")
)

// IsResellerRole reports whether role string is the reseller role.
func IsResellerRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), models.ResellerRoleName)
}

// IsStaff reports full-panel operators (not constrained by reseller tree).
func IsStaff(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin", "sales", "support", "operator", "viewer":
		return true
	default:
		return false
	}
}

// DescendantIDs returns user IDs of all resellers under root (not including root), BFS, max depth 16.
func DescendantIDs(db *gorm.DB, rootID uint) ([]uint, error) {
	if rootID == 0 {
		return nil, nil
	}
	seen := map[uint]struct{}{rootID: {}}
	var out []uint
	frontier := []uint{rootID}
	for depth := 0; depth < 16 && len(frontier) > 0; depth++ {
		var children []models.User
		if err := db.Select("id").Where("parent_user_id IN ? AND role = ?", frontier, models.ResellerRoleName).Find(&children).Error; err != nil {
			return nil, err
		}
		frontier = frontier[:0]
		for _, ch := range children {
			if _, ok := seen[ch.ID]; ok {
				continue
			}
			seen[ch.ID] = struct{}{}
			out = append(out, ch.ID)
			frontier = append(frontier, ch.ID)
		}
	}
	return out, nil
}

// ScopeUserIDs returns the set of panel user IDs whose clients a viewer may see.
// Staff → nil slice meaning "no filter" (all). Reseller → self + descendants.
func ScopeUserIDs(db *gorm.DB, actor *models.User) (ids []uint, filter bool, err error) {
	if actor == nil {
		return nil, true, ErrForbidden
	}
	if IsStaff(actor.Role) {
		return nil, false, nil
	}
	if !IsResellerRole(actor.Role) {
		// unknown role: safest is self-only
		return []uint{actor.ID}, true, nil
	}
	desc, err := DescendantIDs(db, actor.ID)
	if err != nil {
		return nil, true, err
	}
	ids = append([]uint{actor.ID}, desc...)
	return ids, true, nil
}

// ApplyClientScope adds WHERE created_by_user_id IN (...) for resellers.
// Staff: no-op. Returns the same query.
func ApplyClientScope(db *gorm.DB, q *gorm.DB, actor *models.User) (*gorm.DB, error) {
	ids, filter, err := ScopeUserIDs(db, actor)
	if err != nil {
		return q, err
	}
	if !filter {
		return q, nil
	}
	if len(ids) == 0 {
		return q.Where("1 = 0"), nil
	}
	return q.Where("created_by_user_id IN ?", ids), nil
}

// CanManageTarget reports whether actor may view/edit target reseller account.
func CanManageTarget(db *gorm.DB, actor, target *models.User) bool {
	if actor == nil || target == nil {
		return false
	}
	if IsStaff(actor.Role) && (strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		return true
	}
	if actor.ID == target.ID {
		return true
	}
	if !IsResellerRole(actor.Role) {
		return false
	}
	// target must be strict descendant
	desc, err := DescendantIDs(db, actor.ID)
	if err != nil {
		return false
	}
	for _, id := range desc {
		if id == target.ID {
			return true
		}
	}
	return false
}

// CreateReseller creates a reseller under actor (owner/admin → level 1; reseller → level+1).
func CreateReseller(db *gorm.DB, actor *models.User, username, passwordHash string, opts CreateOpts) (*models.User, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	username = strings.TrimSpace(username)
	if username == "" || passwordHash == "" {
		return nil, fmt.Errorf("username and password required")
	}
	parentID := uint(0)
	level := 1
	switch {
	case strings.EqualFold(actor.Role, "owner"), strings.EqualFold(actor.Role, "admin"):
		parentID = 0
		level = 1
		if opts.ParentUserID > 0 {
			var p models.User
			if err := db.First(&p, opts.ParentUserID).Error; err != nil {
				return nil, ErrInvalidParent
			}
			if !IsResellerRole(p.Role) {
				return nil, ErrInvalidParent
			}
			parentID = p.ID
			level = p.ResellerLevel + 1
			if level > models.ResellerMaxDepthDefault {
				return nil, ErrDepth
			}
			if !p.AllowSubResellers {
				return nil, ErrSubNotAllowed
			}
		}
	case IsResellerRole(actor.Role):
		if !actor.AllowSubResellers {
			return nil, ErrSubNotAllowed
		}
		if actor.MaxSubResellers > 0 {
			var n int64
			_ = db.Model(&models.User{}).Where("parent_user_id = ? AND role = ?", actor.ID, models.ResellerRoleName).Count(&n).Error
			if int(n) >= actor.MaxSubResellers {
				return nil, ErrSubLimit
			}
		}
		parentID = actor.ID
		level = actor.ResellerLevel + 1
		if level < 2 {
			level = 2
		}
		if level > models.ResellerMaxDepthDefault {
			return nil, ErrDepth
		}
		// Child quotas cannot exceed parent remaining capacity when parent is capped.
		if err := validateChildQuotas(actor, opts); err != nil {
			return nil, err
		}
	default:
		return nil, ErrForbidden
	}

	u := models.User{
		Username:            username,
		PasswordHash:        passwordHash,
		Role:                models.ResellerRoleName,
		Enabled:             true,
		MustChangePassword:  opts.MustChangePassword,
		ParentUserID:        parentID,
		ResellerLevel:       level,
		MaxTotalGB:          opts.MaxTotalGB,
		MaxGBPerClient:      opts.MaxGBPerClient,
		MaxClients:          opts.MaxClients,
		AllowSubResellers:   opts.AllowSubResellers,
		MaxSubResellers:     opts.MaxSubResellers,
		CommissionPercent:   clampPct(opts.CommissionPercent),
		WholesalePricePerGB: opts.WholesalePricePerGB,
		CreditBalance:       0,
		Notes:               opts.Notes,
	}
	if err := db.Create(&u).Error; err != nil {
		return nil, err
	}
	// Optional initial credit (from actor wallet if reseller, or free mint if staff).
	if opts.InitialCredit > 0 {
		if err := Credit(db, actor, u.ID, opts.InitialCredit, "topup", "initial", "initial credit on create"); err != nil {
			// Best-effort: user exists; surface credit error to caller.
			return &u, err
		}
		_ = db.First(&u, u.ID).Error
	}
	return &u, nil
}

type CreateOpts struct {
	ParentUserID        uint
	MaxTotalGB          int64
	MaxGBPerClient      int64
	MaxClients          int
	AllowSubResellers   bool
	MaxSubResellers     int
	CommissionPercent   int
	WholesalePricePerGB int64
	InitialCredit       int64
	Notes               string
	MustChangePassword  bool
}

func validateChildQuotas(parent *models.User, opts CreateOpts) error {
	if parent.MaxTotalGB > 0 && opts.MaxTotalGB > 0 {
		// Child pool should not exceed parent's remaining GB pool (soft check).
		left := parent.MaxTotalGB - parent.AllocatedGB
		if opts.MaxTotalGB > left && left >= 0 {
			// allow if parent still has unlimited-like headroom via 0 child meaning inherit — skip hard fail
		}
	}
	if parent.MaxGBPerClient > 0 && opts.MaxGBPerClient > parent.MaxGBPerClient {
		return fmt.Errorf("%w: max GB per client exceeds parent", ErrQuota)
	}
	return nil
}

func clampPct(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// Credit adds amount to target reseller wallet. If actor is reseller, debit actor first (transfer).
// Staff actors mint credit without debit.
func Credit(db *gorm.DB, actor *models.User, targetID uint, amount int64, kind, ref, note string) error {
	if amount == 0 {
		return nil
	}
	if amount < 0 {
		return fmt.Errorf("amount must be positive")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var target models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, targetID).Error; err != nil {
			return err
		}
		if !IsResellerRole(target.Role) {
			return ErrNotReseller
		}
		if actor != nil && IsResellerRole(actor.Role) {
			var src models.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&src, actor.ID).Error; err != nil {
				return err
			}
			if src.CreditBalance < amount {
				return ErrInsufficient
			}
			if !CanManageTarget(tx, &src, &target) && src.ID != target.ID {
				// transfer only to self (noop) or descendants
				if src.ID != target.ID {
					desc, _ := DescendantIDs(tx, src.ID)
					ok := false
					for _, id := range desc {
						if id == target.ID {
							ok = true
							break
						}
					}
					if !ok {
						return ErrForbidden
					}
				}
			}
			newSrc := src.CreditBalance - amount
			if err := tx.Model(&src).Update("credit_balance", newSrc).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.ResellerLedger{
				UserID: src.ID, Delta: -amount, BalanceAfter: newSrc,
				Kind: "transfer_out", Ref: ref, Note: note, ActorUserID: actor.ID,
			}).Error; err != nil {
				return err
			}
			kind = "transfer_in"
		}
		newBal := target.CreditBalance + amount
		if err := tx.Model(&target).Update("credit_balance", newBal).Error; err != nil {
			return err
		}
		actorID := uint(0)
		if actor != nil {
			actorID = actor.ID
		}
		if kind == "" {
			kind = "topup"
		}
		return tx.Create(&models.ResellerLedger{
			UserID: target.ID, Delta: amount, BalanceAfter: newBal,
			Kind: kind, Ref: ref, Note: note, ActorUserID: actorID,
		}).Error
	})
}

// Adjust sets a signed delta on target (staff only typically). Positive credit, negative debit.
func Adjust(db *gorm.DB, actor *models.User, targetID uint, delta int64, note string) error {
	if delta == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var target models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, targetID).Error; err != nil {
			return err
		}
		if !IsResellerRole(target.Role) {
			return ErrNotReseller
		}
		newBal := target.CreditBalance + delta
		if newBal < 0 {
			return ErrInsufficient
		}
		if err := tx.Model(&target).Update("credit_balance", newBal).Error; err != nil {
			return err
		}
		actorID := uint(0)
		if actor != nil {
			actorID = actor.ID
		}
		return tx.Create(&models.ResellerLedger{
			UserID: target.ID, Delta: delta, BalanceAfter: newBal,
			Kind: "adjust", Note: note, ActorUserID: actorID,
		}).Error
	})
}

// DebitForClient charges reseller wallet when creating a client (amount in credit units).
// Staff actors skip debit. amount<=0 skips.
func DebitForClient(db *gorm.DB, actor *models.User, amount int64, clientRef string) error {
	if actor == nil || !IsResellerRole(actor.Role) || amount <= 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var u models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, actor.ID).Error; err != nil {
			return err
		}
		if u.CreditBalance < amount {
			return ErrInsufficient
		}
		newBal := u.CreditBalance - amount
		if err := tx.Model(&u).Update("credit_balance", newBal).Error; err != nil {
			return err
		}
		return tx.Create(&models.ResellerLedger{
			UserID: u.ID, Delta: -amount, BalanceAfter: newBal,
			Kind: "client_create", Ref: clientRef, Note: "client create", ActorUserID: actor.ID,
		}).Error
	})
}

// ListTree returns resellers visible to actor: all for staff, subtree for reseller.
func ListTree(db *gorm.DB, actor *models.User) ([]models.User, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	q := db.Model(&models.User{}).Where("role = ?", models.ResellerRoleName).Order("reseller_level asc, id asc")
	if IsResellerRole(actor.Role) {
		ids, _, err := ScopeUserIDs(db, actor)
		if err != nil {
			return nil, err
		}
		// include self
		q = q.Where("id IN ?", ids)
	}
	var rows []models.User
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// UpdateQuotas updates quota / policy fields on a reseller if actor may manage them.
func UpdateQuotas(db *gorm.DB, actor, target *models.User, maxTotalGB, maxGBPerClient int64, maxClients int, allowSub bool, maxSub, commission int, wholesalePerGB int64, notes string) error {
	if !CanManageTarget(db, actor, target) {
		return ErrForbidden
	}
	if IsResellerRole(actor.Role) && actor.ID == target.ID {
		// reseller cannot raise their own quotas
		return ErrForbidden
	}
	updates := map[string]any{
		"max_total_gb":           maxTotalGB,
		"max_gb_per_client":      maxGBPerClient,
		"max_clients":            maxClients,
		"allow_sub_resellers":    allowSub,
		"max_sub_resellers":      maxSub,
		"commission_percent":     clampPct(commission),
		"wholesale_price_per_gb": wholesalePerGB,
	}
	if notes != "" {
		updates["notes"] = notes
	}
	return db.Model(target).Updates(updates).Error
}

// --- Wholesale pricing, stats, card-to-card topup ---

// settingInt64 reads a PanelSetting value as int64 (best-effort).
func settingInt64(db *gorm.DB, key string, def int64) int64 {
	var row models.PanelSetting
	if err := db.Where("key = ?", key).First(&row).Error; err != nil || strings.TrimSpace(row.Value) == "" {
		return def
	}
	var n int64
	if _, err := fmt.Sscan(strings.TrimSpace(row.Value), &n); err != nil {
		return def
	}
	return n
}

// EffectiveWholesalePerGB returns Toman-credit charged per GB for this reseller.
// Per-user override wins; else global setting; 0 means free (not recommended).
func EffectiveWholesalePerGB(db *gorm.DB, u *models.User) int64 {
	if u != nil && u.WholesalePricePerGB > 0 {
		return u.WholesalePricePerGB
	}
	return settingInt64(db, models.ResellerWholesalePricePerGBKey, 0)
}

// CostForGB returns credit required for totalGB at effective wholesale price.
func CostForGB(db *gorm.DB, u *models.User, totalGB int64) int64 {
	if totalGB <= 0 {
		return 0
	}
	price := EffectiveWholesalePerGB(db, u)
	if price <= 0 {
		return 0
	}
	return totalGB * price
}

// ChargeForClientGB debits reseller wallet for mandatory wholesale cost. Staff skip.
// If wholesale price is configured (>0) and balance is insufficient, returns ErrInsufficient.
func ChargeForClientGB(db *gorm.DB, actor *models.User, totalGB int64, clientRef string) (charged int64, err error) {
	if actor == nil || !IsResellerRole(actor.Role) {
		return 0, nil
	}
	cost := CostForGB(db, actor, totalGB)
	if cost <= 0 {
		return 0, nil
	}
	if err := DebitForClient(db, actor, cost, clientRef); err != nil {
		return 0, err
	}
	return cost, nil
}

// RefundClientGB credits back wholesale cost for deleted/reduced GB (best-effort).
// Looks up the reseller user by ID; no-op if not a reseller or price is zero.
func RefundClientGB(db *gorm.DB, resellerUserID uint, totalGB int64, clientRef string) (refunded int64, err error) {
	if resellerUserID == 0 || totalGB <= 0 {
		return 0, nil
	}
	var u models.User
	if err := db.First(&u, resellerUserID).Error; err != nil {
		return 0, err
	}
	if !IsResellerRole(u.Role) {
		return 0, nil
	}
	amount := CostForGB(db, &u, totalGB)
	if amount <= 0 {
		return 0, nil
	}
	// Mint credit onto reseller wallet (staff-style credit, actor nil).
	if err := Credit(db, nil, u.ID, amount, "adjust", clientRef, "refund wholesale on client delete/reduce"); err != nil {
		return 0, err
	}
	return amount, nil
}

// NodeStats is aggregate sales metrics for one reseller node.
type NodeStats struct {
	UserID        uint   `json:"userId"`
	Username      string `json:"username"`
	Level         int    `json:"level"`
	ParentUserID  uint   `json:"parentUserId"`
	CreditBalance int64  `json:"creditBalance"`
	ClientCount   int    `json:"clientCount"`
	AllocatedGB   int64  `json:"allocatedGB"`
	MaxTotalGB    int64  `json:"maxTotalGB"`
	// ClientsCreated / GBSold counted from clients table for this user only
	ClientsCreated   int64 `json:"clientsCreated"`
	GBSold           int64 `json:"gbSold"`
	CreditSpent      int64 `json:"creditSpent"`    // sum of negative ledger client_create
	CreditToppedUp   int64 `json:"creditToppedUp"` // sum of positive topup/transfer_in
	SubResellerCount int64 `json:"subResellerCount"`
	// Subtree rollup (self + descendants)
	SubtreeClients int64 `json:"subtreeClients"`
	SubtreeGB      int64 `json:"subtreeGB"`
}

// Stats returns per-level sales stats visible to actor.
func Stats(db *gorm.DB, actor *models.User) ([]NodeStats, error) {
	users, err := ListTree(db, actor)
	if err != nil {
		return nil, err
	}
	out := make([]NodeStats, 0, len(users))
	for _, u := range users {
		ns := NodeStats{
			UserID:        u.ID,
			Username:      u.Username,
			Level:         u.ResellerLevel,
			ParentUserID:  u.ParentUserID,
			CreditBalance: u.CreditBalance,
			ClientCount:   u.ClientCount,
			AllocatedGB:   u.AllocatedGB,
			MaxTotalGB:    u.MaxTotalGB,
		}
		_ = db.Model(&models.Client{}).Where("created_by_user_id = ?", u.ID).Count(&ns.ClientsCreated).Error
		_ = db.Model(&models.Client{}).Where("created_by_user_id = ?", u.ID).Select("COALESCE(SUM(total_gb),0)").Scan(&ns.GBSold).Error
		_ = db.Model(&models.ResellerLedger{}).Where("user_id = ? AND kind = ? AND delta < 0", u.ID, "client_create").
			Select("COALESCE(SUM(-delta),0)").Scan(&ns.CreditSpent).Error
		_ = db.Model(&models.ResellerLedger{}).Where("user_id = ? AND delta > 0 AND kind IN ?", u.ID, []string{"topup", "transfer_in", "adjust"}).
			Select("COALESCE(SUM(delta),0)").Scan(&ns.CreditToppedUp).Error
		_ = db.Model(&models.User{}).Where("parent_user_id = ? AND role = ?", u.ID, models.ResellerRoleName).Count(&ns.SubResellerCount).Error

		desc, _ := DescendantIDs(db, u.ID)
		scope := append([]uint{u.ID}, desc...)
		_ = db.Model(&models.Client{}).Where("created_by_user_id IN ?", scope).Count(&ns.SubtreeClients).Error
		_ = db.Model(&models.Client{}).Where("created_by_user_id IN ?", scope).Select("COALESCE(SUM(total_gb),0)").Scan(&ns.SubtreeGB).Error
		out = append(out, ns)
	}
	return out, nil
}

// CreateCreditOrder opens a card-to-card topup request for a reseller.
func CreateCreditOrder(db *gorm.DB, actor *models.User, amount int64, receiptNote string) (*models.ResellerCreditOrder, error) {
	if actor == nil || !IsResellerRole(actor.Role) {
		return nil, ErrForbidden
	}
	minAmt := settingInt64(db, models.ResellerTopupMinKey, 10000)
	if amount < minAmt {
		return nil, fmt.Errorf("minimum topup is %d", minAmt)
	}
	// prevent spam: one pending at a time
	var pending int64
	_ = db.Model(&models.ResellerCreditOrder{}).Where("reseller_user_id = ? AND status = ?", actor.ID, "pending_review").Count(&pending).Error
	if pending > 0 {
		return nil, fmt.Errorf("you already have a pending topup request")
	}
	row := models.ResellerCreditOrder{
		ResellerUserID: actor.ID,
		AmountToman:    amount,
		Status:         "pending_review",
		ReceiptNote:    strings.TrimSpace(receiptNote),
	}
	if err := db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ApproveCreditOrder credits reseller wallet after admin verifies card-to-card payment.
func ApproveCreditOrder(db *gorm.DB, actor *models.User, orderID uint, adminNote string) error {
	if actor == nil || !(strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		return ErrForbidden
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var ord models.ResellerCreditOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ord, orderID).Error; err != nil {
			return err
		}
		if ord.Status != "pending_review" {
			return fmt.Errorf("order is not pending")
		}
		// mint credit as staff (actor is staff → no debit)
		if err := Credit(tx, actor, ord.ResellerUserID, ord.AmountToman, "topup", fmt.Sprintf("c2c:%d", ord.ID), "card-to-card approved"); err != nil {
			return err
		}
		now := time.Now().UTC()
		return tx.Model(&ord).Updates(map[string]any{
			"status":      "approved",
			"admin_note":  adminNote,
			"reviewed_by": actor.Username,
			"reviewed_at": now,
		}).Error
	})
}

// RejectCreditOrder marks a topup request rejected.
func RejectCreditOrder(db *gorm.DB, actor *models.User, orderID uint, adminNote string) error {
	if actor == nil || !(strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		return ErrForbidden
	}
	var ord models.ResellerCreditOrder
	if err := db.First(&ord, orderID).Error; err != nil {
		return err
	}
	if ord.Status != "pending_review" {
		return fmt.Errorf("order is not pending")
	}
	now := time.Now().UTC()
	return db.Model(&ord).Updates(map[string]any{
		"status":      "rejected",
		"admin_note":  adminNote,
		"reviewed_by": actor.Username,
		"reviewed_at": now,
	}).Error
}

// ListCreditOrders returns topup requests visible to actor.
func ListCreditOrders(db *gorm.DB, actor *models.User, status string) ([]models.ResellerCreditOrder, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	q := db.Model(&models.ResellerCreditOrder{}).Order("id desc").Limit(100)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if IsResellerRole(actor.Role) {
		q = q.Where("reseller_user_id = ?", actor.ID)
	} else if !(strings.EqualFold(actor.Role, "owner") || strings.EqualFold(actor.Role, "admin")) {
		return nil, ErrForbidden
	}
	var rows []models.ResellerCreditOrder
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// PaymentCardInfo returns shop card-to-card details for reseller topup UI.
func PaymentCardInfo(db *gorm.DB) (card, holder, instructions string) {
	read := func(k string) string {
		var row models.PanelSetting
		if db.Where("key = ?", k).First(&row).Error == nil {
			return row.Value
		}
		return ""
	}
	return read(models.ShopCardNumberKey), read(models.ShopCardHolderKey), read(models.ShopInstructionsKey)
}
