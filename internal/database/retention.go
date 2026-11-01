package database

import (
	"log/slog"
	"time"

	"gorm.io/gorm"
)

const (
	snapshotPruneBatch      = 5000
	snapshotPruneMaxBatches = 200
)

// SnapshotRetentionDays returns how many days of hourly usage snapshots are
// kept. Override with ELECTRU_SNAPSHOT_RETENTION_DAYS (minimum 30, default 90,
// which matches the longest history window the panel UI can request).
func SnapshotRetentionDays() int {
	days := envInt("ELECTRU_SNAPSHOT_RETENTION_DAYS", 90)
	if days < 30 {
		days = 30
	}
	return days
}

// PruneUsageSnapshots deletes usage_snapshots older than keepDays in small
// batches so a large table never holds a long write lock. It never touches
// client counters (clients.up / clients.down), only the history rows.
func PruneUsageSnapshots(db *gorm.DB, keepDays int) (int64, error) {
	if db == nil {
		return 0, nil
	}
	if keepDays < 30 {
		keepDays = 30
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepDays) * 24 * time.Hour)
	var total int64
	for i := 0; i < snapshotPruneMaxBatches; i++ {
		res := db.Exec(
			"DELETE FROM usage_snapshots WHERE id IN (SELECT id FROM usage_snapshots WHERE created_at < ? ORDER BY id LIMIT ?)",
			cutoff, snapshotPruneBatch,
		)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < snapshotPruneBatch {
			break
		}
	}
	if total > 0 {
		slog.Info("usage snapshot retention applied", "deleted", total, "keepDays", keepDays)
	}
	return total, nil
}

// PruneSubscriptionAccessLogs deletes subscription_access_logs older than
// keepDays (default 90, override with ELECTRU_ACCESS_LOG_RETENTION_DAYS, min 7).
// The handler that writes these rows only cleaned up when a request happened to
// arrive in the exact second where unix_time%3600==0, i.e. practically never,
// so the table (one row per subscription fetch) grew without bound.
func PruneSubscriptionAccessLogs(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, nil
	}
	keepDays := envInt("ELECTRU_ACCESS_LOG_RETENTION_DAYS", 90)
	if keepDays < 7 {
		keepDays = 7
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepDays) * 24 * time.Hour)
	var total int64
	for i := 0; i < snapshotPruneMaxBatches; i++ {
		res := db.Exec(
			"DELETE FROM subscription_access_logs WHERE id IN (SELECT id FROM subscription_access_logs WHERE created_at < ? ORDER BY id LIMIT ?)",
			cutoff, snapshotPruneBatch,
		)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < snapshotPruneBatch {
			break
		}
	}
	if total > 0 {
		slog.Info("subscription access log retention applied", "deleted", total, "keepDays", keepDays)
	}
	return total, nil
}
