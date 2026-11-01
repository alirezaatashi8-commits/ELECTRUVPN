package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init opens the database (PostgreSQL preferred for concurrent production load,
// SQLite only as local/dev fallback), runs migrations, and seeds admin.
//
// Set ELECTRU_DB_DSN for PostgreSQL, e.g.:
//
//	host=127.0.0.1 user=electruvpn password=secret dbname=electruvpn port=5432 sslmode=disable TimeZone=UTC
//
// Pool tuning (optional):
//
//	ELECTRU_DB_MAX_OPEN=10
//	ELECTRU_DB_MAX_IDLE=3
//	ELECTRU_DB_CONN_MAX_LIFETIME_MIN=30
func Init(dbPath string) (*gorm.DB, error) {
	dsn := strings.TrimSpace(os.Getenv("ELECTRU_DB_DSN"))
	var (
		db  *gorm.DB
		err error
	)

	cfg := &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		PrepareStmt:            true,
		SkipDefaultTransaction: true, // better throughput under concurrent writes
	}

	if dsn != "" {
		// Append safe defaults if operator did not set them.
		dsn = ensurePostgresDSNDefaults(dsn)
		db, err = gorm.Open(postgres.Open(dsn), cfg)
		if err != nil {
			return nil, fmt.Errorf("postgres open: %w", err)
		}
		if sqlDB, err := db.DB(); err == nil {
			maxOpen := envInt("ELECTRU_DB_MAX_OPEN", 10)
			maxIdle := envInt("ELECTRU_DB_MAX_IDLE", 3)
			lifeMin := envInt("ELECTRU_DB_CONN_MAX_LIFETIME_MIN", 30)
			if maxOpen < 5 {
				maxOpen = 5
			}
			if maxIdle < 2 {
				maxIdle = 2
			}
			if maxIdle > maxOpen {
				maxIdle = maxOpen
			}
			sqlDB.SetMaxOpenConns(maxOpen)
			sqlDB.SetMaxIdleConns(maxIdle)
			sqlDB.SetConnMaxLifetime(time.Duration(lifeMin) * time.Minute)
			sqlDB.SetConnMaxIdleTime(5 * time.Minute)
			slog.Info("database backend", "driver", "postgres", "maxOpen", maxOpen, "maxIdle", maxIdle)
		} else {
			slog.Info("database backend", "driver", "postgres")
		}
	} else {
		if dir := filepath.Dir(dbPath); dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, err
			}
		}
		sqliteDSN := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=on&_cache_size=-4096"
		db, err = gorm.Open(sqlite.Open(sqliteDSN), cfg)
		if err != nil {
			return nil, fmt.Errorf("sqlite open: %w", err)
		}
		if sqlDB, err := db.DB(); err == nil {
			// SQLite serializes writers; keep open conns modest but allow concurrent readers.
			sqlDB.SetMaxOpenConns(4)
			sqlDB.SetMaxIdleConns(2)
			sqlDB.SetConnMaxLifetime(30 * time.Minute)
			sqlDB.SetConnMaxIdleTime(5 * time.Minute)
		}
		slog.Warn("database backend", "driver", "sqlite", "path", dbPath, "hint", "set ELECTRU_DB_DSN for PostgreSQL (recommended under concurrent load)")
	}

	if err := migrateAll(db); err != nil {
		return nil, err
	}

	// Backfill defaults for rows created before subscription manager fields existed.
	_ = db.Model(&models.Client{}).Where("subscription_status = '' OR subscription_status IS NULL").Updates(map[string]any{"subscription_status": "active", "subscription_token_version": 1, "subscription_auto_update": true})
	_ = db.Model(&models.Client{}).Where("subscription_address_type = '' OR subscription_address_type IS NULL").Updates(map[string]any{"subscription_address_type": "all"})
	_ = db.Model(&models.Inbound{}).Where("subscription_preset = '' OR subscription_preset IS NULL").Update("subscription_preset", "standard")
	_ = db.Model(&models.SubscriptionGroup{}).Where("preset = '' OR preset IS NULL").Update("preset", "standard")
	_ = db.Model(&models.SubscriptionGroup{}).Where("health_filter = '' OR health_filter IS NULL").Update("health_filter", "healthy")

	if db.Migrator().HasColumn(&models.ServicePlan{}, "speed_mbps") {
		_ = db.Migrator().DropColumn(&models.ServicePlan{}, "speed_mbps")
	}

	seedSubscriptionPresets(db)
	seedDefaultAdmin(db)

	return db, nil
}

func migrateAll(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.User{},
		&models.ResellerLedger{},
		&models.ResellerCreditOrder{},
		&models.PanelSession{},
		&models.Inbound{},
		&models.InboundRevision{},
		&models.Client{},
		&models.ClientDevice{},
		&models.SubscriptionProfile{},
		&models.ServerNode{},
		&models.APIKey{},
		&models.AuditLog{},
		&models.BackupRecord{},
		&models.PanelSetting{},
		&models.ConfigTemplate{},
		&models.NotificationRule{},
		&models.ServicePlan{},
		&models.UsageSnapshot{},
		&models.AgentNode{},
		&models.NodeClientTraffic{},
		&models.PlatformJob{},
		&models.BillingPlan{},
		&models.Order{},
		&models.WebhookEndpoint{},
		&models.CertificateRecord{},
		&models.PluginRecord{},
		&models.NodePerformanceSnapshot{},
		&models.RoutingPolicy{},
		&models.Deployment{},
		&models.OperationRecord{},
		&models.NodeGroup{},
		&models.NodeGroupMember{},
		&models.DomainRecord{},
		&models.CertificateRevision{},
		&models.AlertEvent{},
		&models.AutomationRule{},
		&models.ScheduledTask{},
		&models.SubscriptionGroup{},
		&models.SubscriptionPreset{},
		&models.SubscriptionAccessLog{},
		&models.ClientConfigProfile{},
		&models.Ticket{},
		&models.TicketMessage{},
		&models.ShopPlan{},
		&models.ShopOrder{},
		&models.TelegramCustomer{},
		&models.CloudBackup{},
		&models.Wallet{},
		&models.WalletLedger{},
		&models.ReferralAttribution{},
		&models.ReferralReward{},
		&models.PromoCode{},
		&models.PromoUsage{},
		&models.GiftCard{},
		&models.BroadcastJob{},
		&models.RealityDestPool{},
		&models.HoneypotProbe{},
		&models.HoneypotBan{},
		&models.ClientReplayEvent{},
		&models.TunnelChain{},
	); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	// V9.7: TelegramCustomer unique was (telegram_user_id) alone; now composite
	// (telegram_user_id, client_id). Drop legacy single-column unique if present.
	_ = dropLegacyTelegramCustomerUnique(db)
	// Backfill ExpiresAt on old cloud backups that have none (30 days from created_at).
	_ = backfillCloudBackupExpiry(db)
	return nil
}

func dropLegacyTelegramCustomerUnique(db *gorm.DB) error {
	// Best-effort; ignore errors on drivers that already migrated.
	dialect := db.Dialector.Name()
	switch dialect {
	case "postgres":
		// GORM default name for uniqueIndex on telegram_user_id
		_ = db.Exec(`ALTER TABLE telegram_customers DROP CONSTRAINT IF EXISTS uni_telegram_customers_telegram_user_id`).Error
		_ = db.Exec(`DROP INDEX IF EXISTS uni_telegram_customers_telegram_user_id`).Error
		_ = db.Exec(`DROP INDEX IF EXISTS idx_telegram_customers_telegram_user_id`).Error
	case "sqlite":
		// SQLite cannot easily DROP a single unique index without rebuild; skip.
		// Composite unique from AutoMigrate coexists; inserts with multi-client
		// still work if the old unique was never enforced on empty tables.
	}
	return nil
}

func backfillCloudBackupExpiry(db *gorm.DB) error {
	var rows []models.CloudBackup
	if err := db.Where("expires_at IS NULL AND enabled = ?", true).Limit(500).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		exp := r.CreatedAt.UTC().Add(time.Duration(models.CloudBackupDefaultTTLDays) * 24 * time.Hour)
		_ = db.Model(&r).Update("expires_at", exp).Error
	}
	return nil
}

func ensurePostgresDSNDefaults(dsn string) string {
	low := strings.ToLower(dsn)
	// Prefer explicit UTC and disable statement_cache issues on PgBouncer if used.
	if !strings.Contains(low, "timezone=") && !strings.Contains(low, "timeZone=") {
		dsn = strings.TrimSpace(dsn) + " TimeZone=UTC"
	}
	return dsn
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func seedDefaultAdmin(db *gorm.DB) {
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count > 0 {
		return
	}

	password := os.Getenv("ELECTRU_INITIAL_ADMIN_PASSWORD")
	generatedPassword := false
	if password == "" {
		if f := os.Getenv("ELECTRU_INITIAL_ADMIN_PASSWORD_FILE"); f != "" {
			if b, err := os.ReadFile(f); err == nil {
				password = string(b)
			}
		}
	}
	if password == "" {
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			slog.Error("failed to generate initial admin password", "error", err)
			os.Exit(1)
		}
		password = hex.EncodeToString(b)
		generatedPassword = true
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash default admin password", "error", err)
		os.Exit(1)
	}

	now := time.Now()
	admin := models.User{Username: "admin", PasswordHash: string(hash), Role: "owner", Enabled: true, MustChangePassword: true, PasswordChangedAt: &now}
	if err := db.Create(&admin).Error; err != nil {
		slog.Error("failed to seed default admin user", "error", err)
		os.Exit(1)
	}

	slog.Warn("created default admin account; change the password immediately", "username", "admin", "password_generated", generatedPassword)
}

func seedSubscriptionPresets(db *gorm.DB) {
	presets := []models.SubscriptionPreset{
		{Name: "standard", Description: "Balanced mobile subscription", AddressType: "all", RefreshMin: 15, AutoUpdate: true, SmartEnabled: false, MobileGuide: true, QR: true, Enabled: true},
		{Name: "mobile", Description: "Mobile-first delivery with QR and app guidance", AddressType: "all", RefreshMin: 10, AutoUpdate: true, SmartEnabled: true, MobileGuide: true, QR: true, Enabled: true},
		{Name: "privacy", Description: "Domain-first privacy-oriented endpoint", AddressType: "domain", RefreshMin: 15, AutoUpdate: true, SmartEnabled: true, MobileGuide: true, QR: true, Enabled: true},
		{Name: "dual-stack", Description: "IPv4 + IPv6 delivery", AddressType: "dual", RefreshMin: 15, AutoUpdate: true, SmartEnabled: true, MobileGuide: true, QR: true, Enabled: true},
		{Name: "multi-node", Description: "Multi-inbound / multi-node bundle", AddressType: "all", RefreshMin: 10, AutoUpdate: true, SmartEnabled: true, MobileGuide: true, QR: true, Enabled: true},
		{Name: "gaming", Description: "Multi-node subscription with health filtering", AddressType: "domain", RefreshMin: 5, AutoUpdate: true, SmartEnabled: true, MobileGuide: true, QR: true, Enabled: true},
	}
	for _, preset := range presets {
		var count int64
		db.Model(&models.SubscriptionPreset{}).Where("name = ?", preset.Name).Count(&count)
		if count == 0 {
			_ = db.Create(&preset).Error
		}
	}
}
