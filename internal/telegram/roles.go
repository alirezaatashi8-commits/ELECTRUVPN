package telegram

import (
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const KeyAdminRoles = "telegram_admin_roles" // JSON map: {"123456":"owner","789":"support"}

// Roles: owner | finance | support
// owner = full; finance = orders+reports; support = tickets only

func ParseAdminRoles(raw string) map[int64]string {
	out := map[int64]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	// try JSON
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil {
		for k, v := range m {
			id, err := strconv.ParseInt(strings.TrimSpace(k), 10, 64)
			if err == nil {
				out[id] = strings.ToLower(strings.TrimSpace(v))
			}
		}
		return out
	}
	// fallback: id:role,id:role
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts := strings.SplitN(p, ":", 2)
		id, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			continue
		}
		role := "owner"
		if len(parts) == 2 {
			role = strings.ToLower(strings.TrimSpace(parts[1]))
		}
		out[id] = role
	}
	return out
}

func AdminRole(db *gorm.DB, tgID int64) string {
	roles := ParseAdminRoles(GetSetting(db, KeyAdminRoles))
	if r, ok := roles[tgID]; ok && r != "" {
		return r
	}
	// legacy: listed in admin IDs without role → owner
	if ParseAdminIDs(GetSetting(db, KeyAdminIDs))[tgID] {
		return "owner"
	}
	return ""
}

func RoleAllowed(role, action string) bool {
	role = strings.ToLower(role)
	switch role {
	case "owner", "admin":
		return true
	case "finance":
		switch action {
		case "orders", "report", "stats", "help", "menu":
			return true
		default:
			return false
		}
	case "support":
		switch action {
		case "tickets", "ticket_reply", "help", "menu":
			return true
		default:
			return false
		}
	default:
		return false
	}
}
