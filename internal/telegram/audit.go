package telegram

import (
	"encoding/json"
	"log/slog"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

// AuditTelegram records an admin/user action from Telegram bots into the panel audit log.
func AuditTelegram(db *gorm.DB, actorTG int64, role, action, target string, meta map[string]any) {
	if db == nil {
		return
	}
	raw := ""
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = string(b)
		}
	}
	entry := models.AuditLog{
		Username:        role,
		Role:            role,
		Action:          action,
		Target:          target,
		Source:          "telegram",
		ActorTelegramID: actorTG,
		Details:         raw,
	}
	if err := db.Create(&entry).Error; err != nil {
		slog.Debug("audit telegram", "action", action, "error", err)
	}
}
