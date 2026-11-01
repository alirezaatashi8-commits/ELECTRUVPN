package handlers

import (
	"encoding/json"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func auditAction(s *core.Server, c *gin.Context, action, target string, details any) {
	b, _ := json.Marshal(details)
	username := "system"
	var userID uint
	role := ""
	if cl, ok := c.Get("claims"); ok {
		if x, ok := cl.(*auth.Claims); ok {
			username = x.Username
			userID = x.UserID
		}
	}
	if u := auth.CurrentUser(c); u != nil {
		userID = u.ID
		username = u.Username
		role = u.Role
	}
	s.DB.Create(&models.AuditLog{
		UserID: userID, Username: username, Role: role,
		Action: action, Target: target, IP: c.ClientIP(),
		Source: "panel", Details: string(b),
	})
}

// AuditTelegram records an action performed via Telegram admin bot (RBAC roles).
func AuditTelegram(db *gorm.DB, tgUserID int64, role, action, target string, details any) {
	if db == nil {
		return
	}
	b, _ := json.Marshal(details)
	username := "tg:" + formatTG(tgUserID)
	db.Create(&models.AuditLog{
		Username: username, Role: role, Action: action, Target: target,
		Source: "telegram", ActorTelegramID: tgUserID, Details: string(b),
	})
}

// AuditTMA records Mini App user actions.
func AuditTMA(db *gorm.DB, tgUserID int64, action, target string, details any) {
	if db == nil {
		return
	}
	b, _ := json.Marshal(details)
	db.Create(&models.AuditLog{
		Username: "tma:" + formatTG(tgUserID), Role: "user",
		Action: action, Target: target, Source: "tma",
		ActorTelegramID: tgUserID, Details: string(b),
	})
}

func formatTG(id int64) string {
	if id == 0 {
		return "0"
	}
	// avoid fmt import cycle issues — simple itoa
	const digits = "0123456789"
	if id < 0 {
		id = -id
	}
	var buf [20]byte
	i := len(buf)
	for id >= 10 {
		i--
		buf[i] = digits[id%10]
		id /= 10
	}
	i--
	buf[i] = digits[id]
	return string(buf[i:])
}
