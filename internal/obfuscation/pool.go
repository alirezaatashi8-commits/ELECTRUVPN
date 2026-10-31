package obfuscation

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"gorm.io/gorm"
)

func CheckDomain(domain string, port int) (ok bool, latencyMs int64, errMsg string) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if i := strings.Index(domain, "/"); i >= 0 {
		domain = domain[:i]
	}
	if domain == "" {
		return false, 0, "empty domain"
	}
	if port <= 0 {
		port = 443
	}
	addr := fmt.Sprintf("%s:%d", domain, port)
	start := time.Now()
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", addr, &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	latencyMs = time.Since(start).Milliseconds()
	if err != nil {
		return false, latencyMs, err.Error()
	}
	_ = conn.Close()
	return true, latencyMs, ""
}

func UpsertDest(db *gorm.DB, domain, provider, notes string, port, priority int) (models.RealityDestPool, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if i := strings.Index(domain, "/"); i >= 0 {
		domain = domain[:i]
	}
	if domain == "" {
		return models.RealityDestPool{}, fmt.Errorf("domain required")
	}
	if port <= 0 {
		port = 443
	}
	if priority <= 0 {
		priority = 100
	}
	var row models.RealityDestPool
	err := db.Where("domain = ?", domain).First(&row).Error
	if err == nil {
		row.Provider = provider
		row.Notes = notes
		row.DestPort = port
		row.Priority = priority
		row.IsActive = true
		row.UpdatedAt = time.Now()
		return row, db.Save(&row).Error
	}
	row = models.RealityDestPool{
		Domain: domain, DestPort: port, Provider: provider, Notes: notes,
		IsActive: true, Priority: priority, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	return row, db.Create(&row).Error
}

func ListDests(db *gorm.DB, activeOnly bool) []models.RealityDestPool {
	var rows []models.RealityDestPool
	q := db.Order("priority asc, latency_ms asc, id asc")
	if activeOnly {
		q = q.Where("is_active = ?", true)
	}
	q.Find(&rows)
	return rows
}

func HealthCheckAll(db *gorm.DB) (checked, healthy int) {
	var rows []models.RealityDestPool
	db.Where("is_active = ?", true).Find(&rows)
	now := time.Now()
	for _, r := range rows {
		ok, lat, errMsg := CheckDomain(r.Domain, r.DestPort)
		updates := map[string]any{
			"healthy": ok, "latency_ms": lat, "last_check_at": now, "updated_at": now,
		}
		if ok {
			updates["last_error"] = ""
			healthy++
		} else {
			if len(errMsg) > 250 {
				errMsg = errMsg[:250]
			}
			updates["last_error"] = errMsg
		}
		_ = db.Model(&models.RealityDestPool{}).Where("id = ?", r.ID).Updates(updates).Error
		checked++
	}
	return checked, healthy
}

func BestHealthy(db *gorm.DB, limit int) []models.RealityDestPool {
	if limit <= 0 {
		limit = 5
	}
	var rows []models.RealityDestPool
	db.Where("is_active = ? AND healthy = ?", true, true).
		Order("priority asc, latency_ms asc, id asc").Limit(limit).Find(&rows)
	return rows
}

// ApplyDestToInbound updates Reality dest + serverNames WITHOUT rotating private keys.
func ApplyDestToInbound(db *gorm.DB, inboundID uint, destID uint, mergeHealthyNames bool) (models.Inbound, error) {
	var ib models.Inbound
	if err := db.First(&ib, inboundID).Error; err != nil {
		return ib, fmt.Errorf("inbound not found")
	}
	if ib.Security != models.SecurityReality {
		return ib, fmt.Errorf("inbound is not Reality")
	}
	var dest models.RealityDestPool
	if err := db.First(&dest, destID).Error; err != nil {
		return ib, fmt.Errorf("dest not found")
	}
	var stream models.StreamSettings
	if ib.StreamSettings != "" {
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	}
	if stream.Reality == nil {
		stream.Reality = &models.RealitySettings{}
	}
	stream.Reality.Dest = dest.DestAddress()
	names := []string{dest.Domain}
	if mergeHealthyNames {
		for _, h := range BestHealthy(db, 4) {
			if h.Domain == dest.Domain {
				continue
			}
			names = append(names, h.Domain)
		}
	}
	stream.Reality.ServerNames = names
	stream.Security = models.SecurityReality
	b, err := json.Marshal(stream)
	if err != nil {
		return ib, err
	}
	ib.StreamSettings = string(b)
	ib.Security = models.SecurityReality
	if err := db.Model(&ib).Updates(map[string]any{
		"stream_settings": ib.StreamSettings,
		"security":        string(models.SecurityReality),
	}).Error; err != nil {
		return ib, err
	}
	return ib, nil
}

func SeedDefaults(db *gorm.DB) {
	var n int64
	db.Model(&models.RealityDestPool{}).Count(&n)
	if n > 0 {
		return
	}
	defaults := []struct {
		Domain, Provider string
		Priority         int
	}{
		{"www.microsoft.com", "microsoft", 10},
		{"www.cloudflare.com", "cloudflare", 20},
		{"www.apple.com", "apple", 30},
		{"dl.google.com", "google", 40},
		{"www.samsung.com", "samsung", 50},
	}
	for _, d := range defaults {
		_, _ = UpsertDest(db, d.Domain, d.Provider, "seed curated", 443, d.Priority)
	}
}
