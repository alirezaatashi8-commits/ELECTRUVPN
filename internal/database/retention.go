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
