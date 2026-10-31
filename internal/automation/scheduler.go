package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"gorm.io/gorm"
)

var schedulerMu sync.Mutex

// Tick executes due scheduled tasks and deterministic automation rules. It is
// deliberately idempotent: a row is updated before/after execution and a
// per-process mutex prevents overlapping scheduler passes.
func Tick(db *gorm.DB) {
	TickContext(context.Background(), db)
}

// TickContext executes due automation work and stops promptly when ctx is cancelled.
func TickContext(ctx context.Context, db *gorm.DB) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !schedulerMu.TryLock() {
		return
	}
	defer schedulerMu.Unlock()
	now := time.Now()
	db = db.WithContext(ctx)
	var tasks []models.ScheduledTask
	if err := db.Where("enabled = ? AND (next_run_at IS NULL OR next_run_at <= ?)", true, now).Find(&tasks).Error; err != nil {
		return
	}
	for i := range tasks {
		if ctx.Err() != nil {
			return
		}
		runTask(db, &tasks[i], now)
	}
	runRules(ctx, db, now)
}

func runTask(db *gorm.DB, task *models.ScheduledTask, now time.Time) {
	if err := executeAction(db, task.Kind, task.Payload); err != nil {
		task.LastError = err.Error()
	} else {
		task.LastError = ""
		task.RunCount++
	}
	task.LastRunAt = &now
	next := nextSchedule(task.Schedule, now)
	if next == nil {
		task.Enabled = false
		if task.LastError == "" {
			task.LastError = "invalid or unsupported schedule; task disabled"
		}
	} else {
		task.NextRunAt = next
	}
	_ = db.Save(task).Error
}

func runRules(ctx context.Context, db *gorm.DB, now time.Time) {
	var rules []models.AutomationRule
	if db.Where("enabled = ?", true).Find(&rules).Error != nil {
		return
	}
	for i := range rules {
		if ctx.Err() != nil {
			return
		}
		r := &rules[i]
		if r.LastRunAt != nil && now.Sub(*r.LastRunAt) < time.Duration(max(r.CooldownSec, 1))*time.Second {
			continue
		}
		ok, payload := evaluateRule(db, r.Event, r.Condition, now)
		if !ok {
			continue
		}
		if err := executeAction(db, r.Action, r.Payload); err != nil {
			r.LastError = err.Error()
		} else {
			r.LastError = ""
			r.RunCount++
		}
		r.LastRunAt = &now
		if payload != "" {
			r.LastError = payload
		}
		_ = db.Save(r).Error
	}
}

func evaluateRule(db *gorm.DB, event, condition string, now time.Time) (bool, string) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, ""
	}
	var c map[string]any
	if json.Unmarshal([]byte(condition), &c) != nil {
		return false, "invalid condition JSON"
	}
	switch event {
	case "node_health":
		threshold, _ := c["staleSeconds"].(float64)
		if threshold <= 0 {
			threshold = 90
		}
		// Ask SQLite whether any stale node exists instead of loading the full
		// agent table into memory on every scheduler pass.
		cutoff := now.Add(-time.Duration(threshold) * time.Second)
		var stale models.AgentNode
		err := db.Select("id").
			Where("last_seen_at IS NULL OR last_seen_at < ?", cutoff).
			First(&stale).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ""
		}
		return err == nil, ""
	case "client_expiry":
		days, _ := c["withinDays"].(float64)
		if days <= 0 {
			days = 3
		}
		var count int64
		db.Model(&models.Client{}).Where("enable = ? AND expiry_time > 0 AND expiry_time <= ?", true, now.Add(time.Duration(days)*24*time.Hour).UnixMilli()).Count(&count)
		return count > 0, ""
	case "certificate_expiry":
		days, _ := c["withinDays"].(float64)
		if days <= 0 {
			days = 30
		}
		var count int64
		db.Model(&models.CertificateRecord{}).Where("expires_at IS NOT NULL AND expires_at <= ?", now.Add(time.Duration(days)*24*time.Hour)).Count(&count)
		return count > 0, ""
	case "always":
		return true, ""
	default:
		return false, fmt.Sprintf("unsupported event %q", event)
	}
}

func executeAction(db *gorm.DB, action, payload string) error {
	switch strings.TrimSpace(action) {
	case "notify_telegram":
		var p struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		if strings.TrimSpace(p.Message) == "" {
			return fmt.Errorf("message is required")
		}
		return notify.Telegram(p.Message)
	case "refresh_alerts":
		// Alert computation is intentionally kept in the API layer; creating a
		// persistent AlertEvent here is not necessary and would duplicate logic.
		return nil
	case "cleanup_sessions":
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		return db.Where("created_at < ?", cutoff).Delete(&models.PanelSession{}).Error
	case "snapshot_usage":
		var clients []models.Client
		if err := db.Select("id", "up", "down").Find(&clients).Error; err != nil {
			return err
		}
		if len(clients) == 0 {
			return nil
		}
		// Batch writes dramatically reduce SQLite transaction/statement overhead
		// when a deployment has thousands of clients.
		snapshots := make([]models.UsageSnapshot, 0, len(clients))
		for _, c := range clients {
			snapshots = append(snapshots, models.UsageSnapshot{ClientID: c.ID, Up: c.Up, Down: c.Down})
		}
		return db.CreateInBatches(&snapshots, 500).Error
	default:
		return fmt.Errorf("unsupported automation action %q", action)
	}
}

func nextSchedule(expr string, now time.Time) *time.Time {
	expr = strings.TrimSpace(expr)
	var d time.Duration
	switch expr {
	case "@hourly":
		d = time.Hour
	case "@daily":
		d = 24 * time.Hour
	default:
		if strings.HasPrefix(expr, "@every ") {
			if v, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expr, "@every "))); err == nil {
				d = v
			}
		}
	}
	if d <= 0 {
		return nil
	}
	n := now.Add(d)
	return &n
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
