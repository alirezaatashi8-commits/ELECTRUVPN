package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// RunSecurityMonitor watches node health and host capacity snapshots.
func RunSecurityMonitor(ctx context.Context, db *gorm.DB) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkNodes(db)
			checkCapacity(db)
		}
	}
}

func checkNodes(db *gorm.DB) {
	var agents []models.AgentNode
	db.Find(&agents)
	for _, a := range agents {
		st := a.HealthState
		if st == "unhealthy" || st == "down" || st == "dead" {
			key := fmt.Sprintf("alert_node_down_%d_%s", a.NodeID, time.Now().Format("2006-01-02-15"))
			if !markOnce(db, key) {
				continue
			}
			sendAdminBroadcast(db, fmt.Sprintf("🚨 <b>نود قطع / ناسالم</b>\nNodeID: %d\nState: %s", a.NodeID, st))
		}
	}
	// High RTT from latest snapshots
	var snaps []models.NodePerformanceSnapshot
	db.Order("id desc").Limit(30).Find(&snaps)
	seen := map[uint]bool{}
	for _, s := range snaps {
		if seen[s.NodeID] {
			continue
		}
		seen[s.NodeID] = true
		if s.RTTMs >= 500 || (!s.Healthy && s.RTTMs > 0) {
			key := fmt.Sprintf("alert_node_rtt_%d_%s", s.NodeID, time.Now().Format("2006-01-02-15"))
			if !markOnce(db, key) {
				continue
			}
			sendAdminBroadcast(db, fmt.Sprintf("⚠️ <b>پینگ بالای نود</b>\nNodeID: %d\nRTT: %.0f ms", s.NodeID, s.RTTMs))
		}
	}
}

func checkCapacity(db *gorm.DB) {
	var snap models.NodePerformanceSnapshot
	if db.Order("id desc").First(&snap).Error != nil {
		return
	}
	memPct := 0.0
	if snap.MemoryTotalBytes > 0 {
		memPct = float64(snap.MemoryBytes) / float64(snap.MemoryTotalBytes) * 100
	}
	diskPct := 0.0
	if snap.DiskTotalBytes > 0 {
		diskPct = float64(snap.DiskBytes) / float64(snap.DiskTotalBytes) * 100
	}
	if memPct >= 90 {
		key := fmt.Sprintf("alert_mem_%s", time.Now().Format("2006-01-02-15"))
		if markOnce(db, key) {
			sendAdminBroadcast(db, fmt.Sprintf("🚨 <b>حافظه RAM بحرانی</b>\n%.0f%% استفاده", memPct))
		}
	}
	if diskPct >= 90 {
		key := fmt.Sprintf("alert_disk_%s", time.Now().Format("2006-01-02-15"))
		if markOnce(db, key) {
			sendAdminBroadcast(db, fmt.Sprintf("🚨 <b>هارد دیسک بحرانی</b>\n%.0f%% استفاده", diskPct))
		}
	}
	if snap.CPUPercent >= 95 {
		key := fmt.Sprintf("alert_cpu_%s", time.Now().Format("2006-01-02-15"))
		if markOnce(db, key) {
			sendAdminBroadcast(db, fmt.Sprintf("⚠️ <b>CPU بالا</b>\n%.0f%%", snap.CPUPercent))
		}
	}
	_ = slog.Info
}
