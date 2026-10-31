package telegram

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// cloudSnapshot is the portable account state stored in CloudBackup.PayloadJSON.
type cloudSnapshot struct {
	Version   int                 `json:"v"`
	CreatedAt time.Time           `json:"at"`
	Clients   []cloudClientSnap   `json:"clients"`
	Wallet    int64               `json:"wallet"`
	Orders    []cloudOrderSnap    `json:"orders,omitempty"`
	AutoRenew *cloudAutoRenewSnap `json:"autoRenew,omitempty"`
}

type cloudClientSnap struct {
	ID                 uint   `json:"id"`
	Email              string `json:"email"`
	SubID              string `json:"subId"`
	Enable             bool   `json:"enable"`
	TotalGB            int64  `json:"totalGB"`
	Up                 int64  `json:"up"`
	Down               int64  `json:"down"`
	ExpiryTime         int64  `json:"expiry"`
	SubscriptionStatus string `json:"status"`
}

type cloudOrderSnap struct {
	ID         uint      `json:"id"`
	PlanName   string    `json:"plan"`
	PriceToman int64     `json:"price"`
	Status     string    `json:"status"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"at"`
}

type cloudAutoRenewSnap struct {
	Enabled bool `json:"enabled"`
	PlanID  uint `json:"planId"`
}

var (
	pendingRestoreMu sync.Mutex
	pendingRestore   = map[int64]bool{} // tg user typing backup code
)

func SetPendingRestore(tgID int64, on bool) {
	pendingRestoreMu.Lock()
	if on {
		pendingRestore[tgID] = true
	} else {
		delete(pendingRestore, tgID)
	}
	pendingRestoreMu.Unlock()
}

func IsPendingRestore(tgID int64) bool {
	pendingRestoreMu.Lock()
	defer pendingRestoreMu.Unlock()
	return pendingRestore[tgID]
}

func hashBackupCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeBackupCode(code)))
	return hex.EncodeToString(sum[:])
}

// normalizeBackupCode accepts "EV-AB12-CD34EF", "EV AB12 CD34EF", "evab12cd34ef", etc.
func normalizeBackupCode(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	c = strings.ReplaceAll(c, " ", "")
	c = strings.ReplaceAll(c, "—", "-")
	c = strings.ReplaceAll(c, "–", "-")
	var alnum strings.Builder
	for _, r := range c {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			alnum.WriteRune(r)
		}
	}
	s := alnum.String()
	if strings.HasPrefix(s, "EV") {
		s = s[2:]
	}
	if len(s) == 10 {
		return fmt.Sprintf("EV-%s-%s", s[:4], s[4:])
	}
	// Keep original cleaned form if length unexpected (still upper, no spaces)
	out := strings.ToUpper(strings.TrimSpace(code))
	out = strings.ReplaceAll(out, " ", "")
	return out
}

func generateBackupCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 10)
	for i := range out {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	// EV-XXXX-XXXXXX style
	return fmt.Sprintf("EV-%s-%s", string(out[:4]), string(out[4:])), nil
}

func collectCloudSnapshot(db *gorm.DB, tgUserID int64) (cloudSnapshot, []uint, error) {
	snap := cloudSnapshot{Version: 1, CreatedAt: time.Now().UTC()}
	var links []models.TelegramCustomer
	db.Where("telegram_user_id = ?", tgUserID).Find(&links)
	ids := make([]uint, 0, len(links))
	seen := map[uint]bool{}
	for _, link := range links {
		if link.ClientID == 0 || seen[link.ClientID] {
			continue
		}
		seen[link.ClientID] = true
		var cl models.Client
		if db.First(&cl, link.ClientID).Error != nil {
			continue
		}
		ids = append(ids, cl.ID)
		snap.Clients = append(snap.Clients, cloudClientSnap{
			ID: cl.ID, Email: cl.Email, SubID: cl.SubID, Enable: cl.Enable,
			TotalGB: cl.TotalGB, Up: cl.Up, Down: cl.Down, ExpiryTime: cl.ExpiryTime,
			SubscriptionStatus: cl.SubscriptionStatus,
		})
		if link.AutoRenewEnabled {
			snap.AutoRenew = &cloudAutoRenewSnap{Enabled: true, PlanID: link.AutoRenewPlanID}
		}
	}
	// Also include clients from approved shop orders for this TG user (orphans without link).
	var orders []models.ShopOrder
	db.Where("telegram_user_id = ? AND status = ?", tgUserID, "approved").Order("id desc").Limit(20).Find(&orders)
	for _, o := range orders {
		snap.Orders = append(snap.Orders, cloudOrderSnap{
			ID: o.ID, PlanName: o.PlanName, PriceToman: o.PriceToman, Status: o.Status, Kind: o.Kind, CreatedAt: o.CreatedAt,
		})
		if o.ClientID > 0 && !seen[o.ClientID] {
			var cl models.Client
			if db.First(&cl, o.ClientID).Error == nil {
				seen[cl.ID] = true
				ids = append(ids, cl.ID)
				snap.Clients = append(snap.Clients, cloudClientSnap{
					ID: cl.ID, Email: cl.Email, SubID: cl.SubID, Enable: cl.Enable,
					TotalGB: cl.TotalGB, Up: cl.Up, Down: cl.Down, ExpiryTime: cl.ExpiryTime,
					SubscriptionStatus: cl.SubscriptionStatus,
				})
			}
		}
	}
	var w models.Wallet
	if db.Where("telegram_user_id = ?", tgUserID).First(&w).Error == nil {
		snap.Wallet = w.BalanceToman
	}
	return snap, ids, nil
}

// CreateOrRotateCloudBackup builds a fresh backup code and snapshot for the user.
// New codes expire after models.CloudBackupDefaultTTLDays (30 days).
func CreateOrRotateCloudBackup(db *gorm.DB, tgUserID, chatID int64) (code string, snap cloudSnapshot, err error) {
	snap, ids, err := collectCloudSnapshot(db, tgUserID)
	if err != nil {
		return "", snap, err
	}
	if len(snap.Clients) == 0 && snap.Wallet == 0 && len(snap.Orders) == 0 {
		return "", snap, fmt.Errorf("چیزی برای پشتیبان‌گیری نیست — ابتدا اشتراک بخرید یا اکانت لینک کنید")
	}
	code, err = generateBackupCode()
	if err != nil {
		return "", snap, err
	}
	payload, _ := json.Marshal(snap)
	idList := make([]string, 0, len(ids))
	for _, id := range ids {
		idList = append(idList, fmt.Sprintf("%d", id))
	}
	exp := time.Now().UTC().Add(time.Duration(models.CloudBackupDefaultTTLDays) * 24 * time.Hour)
	row := models.CloudBackup{
		BackupCode:      code,
		CodeHash:        hashBackupCode(code),
		OwnerTelegramID: tgUserID,
		OwnerChatID:     chatID,
		PayloadJSON:     string(payload),
		ClientIDs:       strings.Join(idList, ","),
		Enabled:         true,
		FailedAttempts:  0,
		ExpiresAt:       &exp,
	}
	// Disable previous active backups for this owner (keep history but only latest code works for UX clarity).
	_ = db.Model(&models.CloudBackup{}).Where("owner_telegram_id = ? AND enabled = ?", tgUserID, true).
		Update("enabled", false).Error
	if err := db.Create(&row).Error; err != nil {
		return "", snap, err
	}
	return code, snap, nil
}

// RestoreCloudBackup re-links clients/wallet from a backup code onto the current Telegram user.
// Enforces expiry and failed-attempt limits. On invalid/expired codes, increments
// FailedAttempts (when a matching disabled/expired row is found by hash) or returns
// a generic error to avoid leaking existence.
func RestoreCloudBackup(db *gorm.DB, code string, tgUserID, chatID int64) (cloudSnapshot, error) {
	code = normalizeBackupCode(code)
	if code == "" || code == "EV--" {
		return cloudSnapshot{}, fmt.Errorf("کد پشتیبان خالی است")
	}
	var row models.CloudBackup
	// Prefer exact code match; fallback to hash for rotated storage variants.
	if err := db.Where("backup_code = ? AND enabled = ?", code, true).First(&row).Error; err != nil {
		h := hashBackupCode(code)
		if err2 := db.Where("code_hash = ? AND enabled = ?", h, true).First(&row).Error; err2 != nil {
			// Also try any matching (even disabled) to count failed attempts against it.
			var any models.CloudBackup
			if db.Where("code_hash = ?", h).Order("id desc").First(&any).Error == nil {
				_ = incrementFailedAttempts(db, &any)
			}
			return cloudSnapshot{}, fmt.Errorf("کد پشتیبان نامعتبر یا منقضی است")
		}
	}
	// Expiry check
	if row.ExpiresAt != nil && time.Now().UTC().After(*row.ExpiresAt) {
		_ = db.Model(&row).Update("enabled", false).Error
		return cloudSnapshot{}, fmt.Errorf("کد پشتیبان منقضی شده است — دوباره از حساب قبلی «پشتیبان ابری» بسازید")
	}
	// Failed-attempt lockout
	if row.FailedAttempts >= models.MaxCloudBackupFailedAttempts {
		_ = db.Model(&row).Update("enabled", false).Error
		return cloudSnapshot{}, fmt.Errorf("کد به‌خاطر تلاش‌های ناموفق زیاد قفل شده است")
	}
	var snap cloudSnapshot
	if err := json.Unmarshal([]byte(row.PayloadJSON), &snap); err != nil {
		return cloudSnapshot{}, fmt.Errorf("داده پشتیبان خراب است")
	}
	if len(snap.Clients) == 0 && snap.Wallet == 0 {
		return snap, fmt.Errorf("پشتیبان خالی است")
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		linked := 0
		for _, cs := range snap.Clients {
			var cl models.Client
			if tx.First(&cl, cs.ID).Error != nil {
				// Client deleted — try by SubID
				if cs.SubID == "" || tx.Where("sub_id = ?", cs.SubID).First(&cl).Error != nil {
					continue
				}
			}
			// Remove other TG accounts linked to this client so restore is authoritative
			_ = tx.Where("client_id = ? AND telegram_user_id <> ?", cl.ID, tgUserID).
				Delete(&models.TelegramCustomer{}).Error
			// Upsert TelegramCustomer for this TG user → client
			var link models.TelegramCustomer
			if tx.Where("telegram_user_id = ? AND client_id = ?", tgUserID, cl.ID).First(&link).Error != nil {
				link = models.TelegramCustomer{
					TelegramUserID: tgUserID,
					TelegramChatID: chatID,
					ClientID:       cl.ID,
					SubID:          cl.SubID,
					Email:          cl.Email,
				}
				if snap.AutoRenew != nil {
					link.AutoRenewEnabled = snap.AutoRenew.Enabled
					link.AutoRenewPlanID = snap.AutoRenew.PlanID
				}
				if err := tx.Create(&link).Error; err != nil {
					return err
				}
			} else {
				link.TelegramChatID = chatID
				link.SubID = cl.SubID
				link.Email = cl.Email
				if snap.AutoRenew != nil {
					link.AutoRenewEnabled = snap.AutoRenew.Enabled
					link.AutoRenewPlanID = snap.AutoRenew.PlanID
				}
				if err := tx.Save(&link).Error; err != nil {
					return err
				}
			}
			linked++
		}
		if linked == 0 && snap.Wallet == 0 {
			return fmt.Errorf("هیچ اکانت قابل بازیابی در دیتابیس پیدا نشد (حذف شده‌اند)")
		}
		// Wallet: move balance from owner if restore is on a different TG, or ensure row exists.
		if snap.Wallet > 0 {
			var dest models.Wallet
			if tx.Where("telegram_user_id = ?", tgUserID).First(&dest).Error != nil {
				dest = models.Wallet{TelegramUserID: tgUserID, BalanceToman: snap.Wallet}
				if err := tx.Create(&dest).Error; err != nil {
					return err
				}
				_ = tx.Create(&models.WalletLedger{
					TelegramUserID: tgUserID, DeltaToman: snap.Wallet, BalanceAfter: snap.Wallet,
					Kind: "restore", Ref: row.BackupCode, Note: "cloud backup restore",
				}).Error
			} else if dest.BalanceToman < snap.Wallet && row.OwnerTelegramID != tgUserID {
				// Only top-up missing amount when migrating to a new Telegram account
				delta := snap.Wallet - dest.BalanceToman
				dest.BalanceToman = snap.Wallet
				if err := tx.Save(&dest).Error; err != nil {
					return err
				}
				_ = tx.Create(&models.WalletLedger{
					TelegramUserID: tgUserID, DeltaToman: delta, BalanceAfter: dest.BalanceToman,
					Kind: "restore", Ref: row.BackupCode, Note: "cloud backup migrate",
				}).Error
			}
		}
		now := time.Now()
		return tx.Model(&row).Updates(map[string]any{
			"restore_count":      gorm.Expr("restore_count + 1"),
			"last_restore_at":    now,
			"last_restore_tg_id": tgUserID,
			"failed_attempts":    0,
		}).Error
	})
	if err != nil {
		return snap, err
	}
	return snap, nil
}

func formatCloudSnapshotSummary(snap cloudSnapshot) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📦 <b>%d</b> اکانت VPN\n", len(snap.Clients)))
	for _, c := range snap.Clients {
		st := c.SubscriptionStatus
		if st == "" {
			st = "—"
		}
		b.WriteString(fmt.Sprintf("• <code>%s</code> · sub <code>%s</code> · %s\n", c.Email, c.SubID, st))
	}
	if snap.Wallet > 0 {
		b.WriteString(fmt.Sprintf("💰 کیف پول: <b>%s</b> تومان\n", formatToman(snap.Wallet)))
	}
	if len(snap.Orders) > 0 {
		b.WriteString(fmt.Sprintf("🧾 سفارش‌های ذخیره‌شده: %d\n", len(snap.Orders)))
	}
	return b.String()
}

func incrementFailedAttempts(db *gorm.DB, row *models.CloudBackup) error {
	if row == nil || row.ID == 0 {
		return nil
	}
	n := row.FailedAttempts + 1
	upd := map[string]any{"failed_attempts": n}
	if n >= models.MaxCloudBackupFailedAttempts {
		upd["enabled"] = false
	}
	return db.Model(row).Updates(upd).Error
}

// RevokeCloudBackup disables the active backup code(s) for the given Telegram user.
func RevokeCloudBackup(db *gorm.DB, tgUserID int64) (int64, error) {
	res := db.Model(&models.CloudBackup{}).
		Where("owner_telegram_id = ? AND enabled = ?", tgUserID, true).
		Update("enabled", false)
	return res.RowsAffected, res.Error
}

// ActiveCloudBackupInfo is a public summary of the user's current active backup (no secret payload).
type ActiveCloudBackupInfo struct {
	HasActive      bool       `json:"hasActive"`
	CodeMasked     string     `json:"codeMasked,omitempty"` // e.g. EV-AB12-******
	CreatedAt      *time.Time `json:"createdAt,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	ClientCount    int        `json:"clientCount"`
	Wallet         int64      `json:"wallet"`
	RestoreCount   int        `json:"restoreCount"`
	FailedAttempts int        `json:"failedAttempts"`
	DaysLeft       int        `json:"daysLeft,omitempty"`
}

// GetActiveCloudBackup returns non-secret info about the user's current enabled backup.
func GetActiveCloudBackup(db *gorm.DB, tgUserID int64) ActiveCloudBackupInfo {
	var row models.CloudBackup
	info := ActiveCloudBackupInfo{}
	if err := db.Where("owner_telegram_id = ? AND enabled = ?", tgUserID, true).
		Order("id desc").First(&row).Error; err != nil {
		return info
	}
	if row.ExpiresAt != nil && time.Now().UTC().After(*row.ExpiresAt) {
		_ = db.Model(&row).Update("enabled", false).Error
		return info
	}
	info.HasActive = true
	info.CreatedAt = &row.CreatedAt
	info.ExpiresAt = row.ExpiresAt
	info.RestoreCount = row.RestoreCount
	info.FailedAttempts = row.FailedAttempts
	if row.ExpiresAt != nil {
		d := int(time.Until(*row.ExpiresAt).Hours() / 24)
		if d < 0 {
			d = 0
		}
		info.DaysLeft = d
	}
	// Mask code: EV-XXXX-******
	c := row.BackupCode
	if len(c) >= 8 {
		info.CodeMasked = c[:8] + "******"
	} else {
		info.CodeMasked = "EV-****-******"
	}
	var snap cloudSnapshot
	if json.Unmarshal([]byte(row.PayloadJSON), &snap) == nil {
		info.ClientCount = len(snap.Clients)
		info.Wallet = snap.Wallet
	}
	return info
}

// SnapshotSummaryJSON is a JSON-friendly summary returned by TMA APIs after create/restore.
type SnapshotSummaryJSON struct {
	ClientCount int                 `json:"clientCount"`
	Clients     []map[string]any    `json:"clients"`
	Wallet      int64               `json:"wallet"`
	OrderCount  int                 `json:"orderCount"`
	AutoRenew   *cloudAutoRenewSnap `json:"autoRenew,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
}

func snapshotToJSON(snap cloudSnapshot) SnapshotSummaryJSON {
	out := SnapshotSummaryJSON{
		ClientCount: len(snap.Clients),
		Wallet:      snap.Wallet,
		OrderCount:  len(snap.Orders),
		AutoRenew:   snap.AutoRenew,
		CreatedAt:   snap.CreatedAt,
	}
	for _, c := range snap.Clients {
		out.Clients = append(out.Clients, map[string]any{
			"email":   c.Email,
			"subId":   c.SubID,
			"enable":  c.Enable,
			"status":  c.SubscriptionStatus,
			"totalGB": c.TotalGB,
			"expiry":  c.ExpiryTime,
		})
	}
	return out
}

// CreateCloudBackupForAPI is the TMA/API-facing wrapper; returns code + summary.
func CreateCloudBackupForAPI(db *gorm.DB, tgUserID, chatID int64) (code string, summary SnapshotSummaryJSON, expiresAt *time.Time, err error) {
	code, snap, err := CreateOrRotateCloudBackup(db, tgUserID, chatID)
	if err != nil {
		return "", summary, nil, err
	}
	var row models.CloudBackup
	_ = db.Where("backup_code = ?", code).First(&row)
	return code, snapshotToJSON(snap), row.ExpiresAt, nil
}

// RestoreCloudBackupForAPI is the TMA/API-facing wrapper.
func RestoreCloudBackupForAPI(db *gorm.DB, code string, tgUserID, chatID int64) (SnapshotSummaryJSON, error) {
	snap, err := RestoreCloudBackup(db, code, tgUserID, chatID)
	if err != nil {
		return SnapshotSummaryJSON{}, err
	}
	return snapshotToJSON(snap), nil
}

// --- Admin support helpers ---

// ResolveTelegramUserID finds a telegram user id from numeric id, email, or sub_id.
func ResolveTelegramUserID(db *gorm.DB, query string) (int64, string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, ""
	}
	if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
		var link models.TelegramCustomer
		if db.Where("telegram_user_id = ?", id).First(&link).Error == nil {
			return id, "telegram_id"
		}
		// even if no link, still treat as tg id for backup lookup by owner
		var n int64
		db.Model(&models.CloudBackup{}).Where("owner_telegram_id = ?", id).Count(&n)
		if n > 0 {
			return id, "telegram_id"
		}
		var ord models.ShopOrder
		if db.Where("telegram_user_id = ?", id).First(&ord).Error == nil {
			return id, "telegram_id"
		}
		return id, "telegram_id_guess"
	}
	// email / sub
	var cl models.Client
	if db.Where("email = ? OR sub_id = ?", query, query).First(&cl).Error == nil {
		var link models.TelegramCustomer
		if db.Where("client_id = ?", cl.ID).First(&link).Error == nil {
			return link.TelegramUserID, "client:" + cl.Email
		}
	}
	var link models.TelegramCustomer
	if db.Where("email = ? OR sub_id = ?", query, query).First(&link).Error == nil {
		return link.TelegramUserID, "link"
	}
	return 0, ""
}

// AdminListCloudBackups returns recent backups for a telegram user (includes full code for support).
func AdminListCloudBackups(db *gorm.DB, tgUserID int64, limit int) []models.CloudBackup {
	if limit <= 0 {
		limit = 5
	}
	var rows []models.CloudBackup
	db.Where("owner_telegram_id = ?", tgUserID).Order("id desc").Limit(limit).Find(&rows)
	return rows
}

// AdminFormatCloudBackups builds a support message with full recovery codes.
func AdminFormatCloudBackups(db *gorm.DB, query string) string {
	tgID, how := ResolveTelegramUserID(db, query)
	if tgID == 0 {
		return "کاربر پیدا نشد: " + query + "\nمثال: /backupcode 123456789  یا  /backupcode user@email"
	}
	rows := AdminListCloudBackups(db, tgID, 8)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("☁️ <b>پشتیبان‌های ابری</b> · TG <code>%d</code>", tgID))
	if how != "" && how != "telegram_id" && how != "telegram_id_guess" {
		sb.WriteString(" (" + how + ")")
	}
	sb.WriteString("\n\n")
	if len(rows) == 0 {
		sb.WriteString("هیچ کدی برای این کاربر ثبت نشده.\nمی‌توانید با /issuebackup " + fmt.Sprint(tgID) + " کد جدید بسازید (اگر اکانت لینک داشته باشد).")
		return sb.String()
	}
	now := time.Now().UTC()
	for i, r := range rows {
		st := "❌ باطل"
		if r.Enabled {
			st = "✅ فعال"
			if r.ExpiresAt != nil && now.After(*r.ExpiresAt) {
				st = "⏰ منقضی"
			}
			if r.FailedAttempts >= models.MaxCloudBackupFailedAttempts {
				st = "🔒 قفل"
			}
		}
		exp := "—"
		if r.ExpiresAt != nil {
			exp = r.ExpiresAt.Local().Format("2006-01-02 15:04")
		}
		sb.WriteString(fmt.Sprintf("%d) %s\nکد: <code>%s</code>\nساخته: %s · انقضا: %s\nبازیابی: %d · تلاش ناموفق: %d\n\n",
			i+1, st, r.BackupCode,
			r.CreatedAt.Local().Format("2006-01-02 15:04"), exp,
			r.RestoreCount, r.FailedAttempts))
	}
	// live snapshot summary
	snap, _, _ := collectCloudSnapshot(db, tgID)
	if len(snap.Clients) > 0 || snap.Wallet > 0 {
		sb.WriteString("📦 وضعیت فعلی حساب:\n")
		sb.WriteString(formatCloudSnapshotSummary(snap))
	}
	sb.WriteString("\nدستورات:\n/issuebackup " + fmt.Sprint(tgID) + " — صدور کد جدید\n/sendbackup " + fmt.Sprint(tgID) + " — ارسال کد فعال به کاربر")
	return sb.String()
}

// AdminIssueBackup creates/rotates a backup for the target user and returns the plain code.
func AdminIssueBackup(db *gorm.DB, tgUserID int64) (code string, summary string, err error) {
	var chatID int64
	var link models.TelegramCustomer
	if db.Where("telegram_user_id = ?", tgUserID).First(&link).Error == nil {
		chatID = link.TelegramChatID
	}
	if chatID == 0 {
		chatID = tgUserID
	}
	code, snap, err := CreateOrRotateCloudBackup(db, tgUserID, chatID)
	if err != nil {
		return "", "", err
	}
	return code, formatCloudSnapshotSummary(snap), nil
}

// AdminReenableBackup re-enables a backup by exact code and resets failed attempts + extends expiry.
func AdminReenableBackup(db *gorm.DB, code string) (models.CloudBackup, error) {
	code = normalizeBackupCode(code)
	var row models.CloudBackup
	if err := db.Where("backup_code = ?", code).First(&row).Error; err != nil {
		h := hashBackupCode(code)
		if err2 := db.Where("code_hash = ?", h).Order("id desc").First(&row).Error; err2 != nil {
			return row, fmt.Errorf("کد پیدا نشد")
		}
	}
	exp := time.Now().UTC().Add(time.Duration(models.CloudBackupDefaultTTLDays) * 24 * time.Hour)
	// disable other active for same owner
	_ = db.Model(&models.CloudBackup{}).Where("owner_telegram_id = ? AND enabled = ? AND id <> ?", row.OwnerTelegramID, true, row.ID).
		Update("enabled", false).Error
	err := db.Model(&row).Updates(map[string]any{
		"enabled":         true,
		"failed_attempts": 0,
		"expires_at":      exp,
	}).Error
	if err != nil {
		return row, err
	}
	row.Enabled = true
	row.FailedAttempts = 0
	row.ExpiresAt = &exp
	return row, nil
}
