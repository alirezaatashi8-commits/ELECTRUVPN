package handlers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/version"
	"github.com/gin-gonic/gin"
)

type OpsHandler struct{ S *core.Server }

// NewOpsHandler creates or exposes the NewOpsHandler handler.
func NewOpsHandler(s *core.Server) *OpsHandler { return &OpsHandler{S: s} }

// Health handles the Health API operation.
func (h *OpsHandler) Health(c *gin.Context) {
	type svc struct {
		Running   bool `json:"running"`
		Installed bool `json:"installed"`
		PID       int  `json:"pid,omitempty"`
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"version":  version.Version,
		"time":     time.Now(),
		"xray":     h.S.Xray.Status(),
		"singbox":  h.S.SingBox.Status(),
		"database": h.S.DB.Exec("SELECT 1").Error == nil,
		"disk":     diskFree(h.S.Config.DataDir()),
	}})
	_ = svc{}
}

func diskFree(path string) gin.H {
	dir := filepath.Dir(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return gin.H{"path": dir, "error": err.Error()}
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	total := uint64(st.Blocks) * uint64(st.Bsize)
	return gin.H{"path": dir, "freeBytes": free, "totalBytes": total}
}

// Services handles the Services API operation.
func (h *OpsHandler) Services(c *gin.Context) {
	action := c.Param("action")
	var err error
	switch action {
	case "xray-restart":
		h.S.SetAutoRecovery(true)
		err = h.S.RegenerateAndRestartXray()
	case "xray-start":
		h.S.SetAutoRecovery(true)
		err = h.S.Xray.Start()
	case "xray-stop":
		h.S.SetAutoRecovery(false)
		err = h.S.Xray.Stop()
	case "singbox-start":
		h.S.SetAutoRecovery(true)
		err = h.S.SingBox.Start()
	case "singbox-stop":
		h.S.SetAutoRecovery(false)
		err = h.S.SingBox.Stop()
	case "singbox-restart":
		h.S.SetAutoRecovery(true)
		err = h.S.SingBox.Restart()
	default:
		c.JSON(400, gin.H{"success": false, "msg": "unknown service action"})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "msg": "service action completed"})
}

// Diagnostics handles the Diagnostics API operation.
func (h *OpsHandler) Diagnostics(c *gin.Context) {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		host = h.S.Config.PublicHost
	}
	port, _ := strconv.Atoi(c.Query("port"))
	if port == 0 {
		port = 443
	}
	if host == "" {
		c.JSON(400, gin.H{"success": false, "msg": "host is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	defer cancel()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: 3 * time.Second}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	out := gin.H{"host": host, "port": port, "address": addr, "latencyMs": time.Since(start).Milliseconds()}
	if err != nil {
		out["tcp"] = false
		out["error"] = err.Error()
	} else {
		out["tcp"] = true
		_ = conn.Close()
	}
	_, dnsErr := net.LookupHost(host)
	out["dns"] = dnsErr == nil
	c.JSON(200, gin.H{"success": true, "obj": out})
}

// ValidateConfig handles the ValidateConfig API operation.
func (h *OpsHandler) ValidateConfig(c *gin.Context) {
	var req struct {
		Kind string `json:"kind"`
	}
	_ = c.ShouldBindJSON(&req)
	kind := strings.ToLower(req.Kind)
	if kind == "" {
		kind = "xray"
	}
	var bin, cfg string
	if kind == "singbox" {
		bin, cfg = h.S.Config.SingBoxBinaryPath, h.S.Config.SingBoxConfigPath
	} else {
		bin, cfg = h.S.Config.XrayBinaryPath, h.S.Config.XrayConfigPath
	}
	if _, e := os.Stat(bin); e != nil {
		c.JSON(400, gin.H{"success": false, "msg": fmt.Sprintf("%s binary not found: %s", kind, bin)})
		return
	}
	args := []string{}
	if kind == "singbox" {
		args = []string{"check", "-c", cfg}
	} else {
		args = []string{"run", "-test", "-config", cfg}
	}
	cmd := exec.Command(bin, args...)
	b, e := cmd.CombinedOutput()
	result := gin.H{"kind": kind, "valid": e == nil, "output": strings.TrimSpace(string(b))}
	if e != nil {
		c.JSON(422, gin.H{"success": false, "msg": "configuration validation failed", "obj": result})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": result})
}

// Alerts returns a live operational snapshot suitable for dashboards and external monitors.
// It reports health/capacity conditions only; it never applies bandwidth shaping.
func (h *OpsHandler) Alerts(c *gin.Context) {
	var nodes []models.ServerNode
	_ = h.S.DB.Where("enabled = ?", true).Order("id asc").Find(&nodes).Error
	alerts := make([]gin.H, 0)
	var certs []models.CertificateRecord
	_ = h.S.DB.Where("expires_at IS NOT NULL AND expires_at <= ?", time.Now().Add(14*24*time.Hour)).Find(&certs).Error
	for _, n := range nodes {
		var a models.AgentNode
		_ = h.S.DB.Where("node_id = ?", n.ID).First(&a).Error
		if a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > 45*time.Second {
			alerts = append(alerts, gin.H{"severity": "critical", "type": "node_offline", "nodeId": n.ID, "node": n.Name, "message": "agent heartbeat is stale"})
			continue
		}
		if a.LastError != "" {
			alerts = append(alerts, gin.H{"severity": "critical", "type": "node_error", "nodeId": n.ID, "node": n.Name, "message": a.LastError})
		}
		if a.CPUPercent >= 90 {
			alerts = append(alerts, gin.H{"severity": "warning", "type": "node_cpu", "nodeId": n.ID, "node": n.Name, "message": fmt.Sprintf("CPU %.1f%%", a.CPUPercent)})
		}
	}
	for _, cert := range certs {
		alerts = append(alerts, gin.H{"severity": "warning", "type": "certificate_expiry", "certificateId": cert.ID, "message": "certificate expires within 14 days", "expiresAt": cert.ExpiresAt})
	}
	if h.S.Xray.Status().Running == false {
		alerts = append(alerts, gin.H{"severity": "critical", "type": "xray_down", "message": "Xray is not running"})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"alerts": alerts, "count": len(alerts), "generatedAt": time.Now()}})
}

// Settings handles the Settings API operation.
func (h *OpsHandler) Settings(c *gin.Context) {
	var rows []models.PanelSetting
	h.S.DB.Order("key asc").Find(&rows)
	out := map[string]string{}
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

// SaveSettings handles the SaveSettings API operation.
func (h *OpsHandler) SaveSettings(c *gin.Context) {
	var vals map[string]string
	if c.ShouldBindJSON(&vals) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid settings"})
		return
	}
	for k, v := range vals {
		if strings.TrimSpace(k) == "" || len(k) > 128 {
			continue
		}
		h.S.DB.Where("key = ?", k).Assign(models.PanelSetting{Key: k, Value: v}).FirstOrCreate(&models.PanelSetting{})
	}
	c.JSON(200, gin.H{"success": true})
}

// Templates handles the Templates API operation.
func (h *OpsHandler) Templates(c *gin.Context) {
	var rows []models.ConfigTemplate
	h.S.DB.Order("id desc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateTemplate handles the CreateTemplate API operation.
func (h *OpsHandler) CreateTemplate(c *gin.Context) {
	var r models.ConfigTemplate
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Payload) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name and payload are required"})
		return
	}
	var js any
	if json.Unmarshal([]byte(r.Payload), &js) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "payload must be valid JSON"})
		return
	}
	if e := h.S.DB.Create(&r).Error; e != nil {
		c.JSON(409, gin.H{"success": false, "msg": e.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteTemplate handles the DeleteTemplate API operation.
func (h *OpsHandler) DeleteTemplate(c *gin.Context) {
	if e := h.S.DB.Delete(&models.ConfigTemplate{}, c.Param("id")).Error; e != nil {
		c.JSON(400, gin.H{"success": false, "msg": e.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// Notifications handles the Notifications API operation.
func (h *OpsHandler) Notifications(c *gin.Context) {
	var rows []models.NotificationRule
	h.S.DB.Order("id desc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// SaveNotification handles the SaveNotification API operation.
func (h *OpsHandler) SaveNotification(c *gin.Context) {
	var r models.NotificationRule
	if c.ShouldBindJSON(&r) != nil || r.Name == "" || r.Event == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name and event are required"})
		return
	}
	if r.ID > 0 {
		if e := h.S.DB.Save(&r).Error; e != nil {
			c.JSON(400, gin.H{"success": false, "msg": e.Error()})
			return
		}
	} else if e := h.S.DB.Create(&r).Error; e != nil {
		c.JSON(400, gin.H{"success": false, "msg": e.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteNotification handles the DeleteNotification API operation.
func (h *OpsHandler) DeleteNotification(c *gin.Context) {
	h.S.DB.Delete(&models.NotificationRule{}, c.Param("id"))
	c.JSON(200, gin.H{"success": true})
}

// SystemInfo handles the SystemInfo API operation.
func (h *OpsHandler) SystemInfo(c *gin.Context) {
	hostname, _ := os.Hostname()
	dbBackend := "sqlite"
	if h.S.Config.UsingPostgres() {
		dbBackend = "postgres"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"hostname": hostname, "go": "1.27+", "version": version.Version, "publicHost": h.S.Config.PublicHost, "panelPort": h.S.Config.PanelPort, "basePath": h.S.Config.PanelBasePath, "dbPath": h.S.Config.DBPath, "dbBackend": dbBackend, "dbDSNSet": h.S.Config.UsingPostgres()}})
}

// SecurityScan handles the SecurityScan API operation.
func (h *OpsHandler) SecurityScan(c *gin.Context) {
	checks := []gin.H{}
	checks = append(checks, gin.H{"key": "https", "ok": c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"), "title": "HTTPS"})
	checks = append(checks, gin.H{"key": "jwt_secret", "ok": h.S.Config.JWTSecret != "" && h.S.Config.JWTSecret != "change-me-in-production-please", "title": "Custom JWT secret"})
	checks = append(checks, gin.H{"key": "csrf", "ok": settingBool(h.S, "settings.csrf", true), "title": "CSRF protection"})
	checks = append(checks, gin.H{"key": "headers", "ok": settingBool(h.S, "settings.securityHeaders", true), "title": "Security headers"})
	checks = append(checks, gin.H{"key": "cookie", "ok": settingBool(h.S, "settings.cookieSecure", true), "title": "Secure cookies"})
	checks = append(checks, gin.H{"key": "fail2ban", "ok": settingBool(h.S, "settings.fail2banEnabled", true), "title": "Fail2Ban"})
	checks = append(checks, gin.H{"key": "api_allowlist", "ok": strings.TrimSpace(settingString(h.S, "settings.apiIPAllowlist", "")) != "", "title": "API allowlist"})
	checks = append(checks, gin.H{"key": "panel_allowlist", "ok": strings.TrimSpace(settingString(h.S, "settings.panelIPAllowlist", "")) != "", "title": "Panel allowlist"})
	ok := 0
	for _, x := range checks {
		if x["ok"] == true {
			ok++
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"checks": checks, "passed": ok, "total": len(checks), "generatedAt": time.Now()}})
}

// Sessions handles the Sessions API operation.
func (h *OpsHandler) Sessions(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)
	var rows []models.PanelSession
	h.S.DB.Where("user_id = ?", claims.UserID).Order("last_seen_at desc").Limit(100).Find(&rows)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": rows})
}

// RevokeSession handles the RevokeSession API operation.
func (h *OpsHandler) RevokeSession(c *gin.Context) {
	claims := c.MustGet("claims").(*auth.Claims)
	var row models.PanelSession
	if h.S.DB.Where("id = ? AND user_id = ?", c.Param("id"), claims.UserID).First(&row).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "session not found"})
		return
	}
	now := time.Now()
	h.S.DB.Model(&row).Update("revoked_at", now)
	auditAction(h.S, c, "auth.session.revoke", fmt.Sprint(row.ID), nil)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// VerifyBackup handles the VerifyBackup API operation.
func (h *OpsHandler) VerifyBackup(c *gin.Context) {
	var b models.BackupRecord
	if h.S.DB.First(&b, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "backup not found"})
		return
	}
	z, err := zip.OpenReader(b.Path)
	if err != nil {
		c.JSON(422, gin.H{"success": false, "msg": "invalid backup archive"})
		return
	}
	defer z.Close()
	dbName := filepath.Base(h.S.Config.DBPath)
	if h.S.Config.UsingPostgres() {
		dbName = "DATABASE_IS_POSTGRES.txt"
	}
	foundDB := false
	for _, f := range z.File {
		if f.Name == dbName {
			foundDB = true
			break
		}
	}
	if !foundDB {
		c.JSON(422, gin.H{"success": false, "msg": "database file missing"})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"verified": true, "database": true, "archive": true, "path": b.Path, "size": b.Size}})
}

// DatabaseMaintenance handles the DatabaseMaintenance API operation.
func (h *OpsHandler) DatabaseMaintenance(c *gin.Context) {
	action := strings.ToLower(strings.TrimSpace(c.Param("action")))
	if action != "integrity" && action != "vacuum" && action != "analyze" {
		c.JSON(400, gin.H{"success": false, "msg": "unknown database action"})
		return
	}

	dialect := h.S.DB.Dialector.Name()
	var err error
	switch action {
	case "integrity":
		if dialect != "sqlite" {
			c.JSON(501, gin.H{"success": false, "msg": "integrity check is only supported for SQLite; PostgreSQL manages storage integrity internally"})
			return
		}
		var result string
		if err = h.S.DB.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(result), "ok") {
			c.JSON(500, gin.H{"success": false, "msg": "SQLite integrity check reported: " + result, "obj": gin.H{"action": action, "result": result}})
			return
		}
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"action": action, "result": result}})
		return
	case "vacuum":
		// VACUUM is a statement, not a PRAGMA. PostgreSQL requires it to run
		// outside an explicit transaction; this handler uses a direct Exec.
		if dialect == "sqlite" || dialect == "postgres" {
			err = h.S.DB.Exec("VACUUM").Error
		} else {
			err = fmt.Errorf("vacuum is unsupported for database driver %q", dialect)
		}
	case "analyze":
		if dialect == "sqlite" || dialect == "postgres" {
			err = h.S.DB.Exec("ANALYZE").Error
		} else {
			err = fmt.Errorf("analyze is unsupported for database driver %q", dialect)
		}
	}
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "database."+action, dialect, nil)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"action": action, "completed": true, "driver": dialect}})
}

// Runtime handles the Runtime API operation.
func (h *OpsHandler) Runtime(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"goroutines": runtime.NumGoroutine(), "cpu": runtime.NumCPU(), "heapAlloc": m.HeapAlloc, "heapSys": m.HeapSys, "gc": m.NumGC, "go": runtime.Version()}})
}

// LogTail handles the LogTail API operation.
func (h *OpsHandler) LogTail(c *gin.Context) {
	lines := 100
	if n, _ := strconv.Atoi(c.Query("lines")); n > 0 && n <= 1000 {
		lines = n
	}
	cmd := exec.Command("journalctl", "-u", "electruvpn", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso")
	b, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"source": "journalctl", "available": false, "text": string(b)}})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"source": "journalctl", "available": true, "text": string(b)}})
}

// Emergency handles the Emergency API operation.
func (h *OpsHandler) Emergency(c *gin.Context) {
	var req struct {
		Maintenance *bool `json:"maintenance"`
		PublicAPI   *bool `json:"publicApi"`
		NewLogins   *bool `json:"newLogins"`
		ReadOnly    *bool `json:"readOnly"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid request"})
		return
	}
	vals := map[string]*bool{"settings.emergencyMaintenance": req.Maintenance, "settings.emergencyPublicAPI": req.PublicAPI, "settings.emergencyNewLogins": req.NewLogins, "settings.readOnly": req.ReadOnly}
	for k, v := range vals {
		if v != nil {
			h.S.DB.Where("key = ?", k).Assign(models.PanelSetting{Key: k, Value: strconv.FormatBool(*v)}).FirstOrCreate(&models.PanelSetting{})
		}
	}
	auditAction(h.S, c, "emergency.change", "panel", req)
	c.JSON(200, gin.H{"success": true})
}

// EmergencyState handles the EmergencyState API operation.
func (h *OpsHandler) EmergencyState(c *gin.Context) {
	keys := []string{"settings.emergencyMaintenance", "settings.emergencyPublicAPI", "settings.emergencyNewLogins", "settings.readOnly"}
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = settingBool(h.S, k, false)
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

func settingString(s *core.Server, key, def string) string {
	var r models.PanelSetting
	if s.DB.Where("key = ?", key).First(&r).Error != nil {
		return def
	}
	return r.Value
}
func settingBool(s *core.Server, key string, def bool) bool {
	v := settingString(s, key, strconv.FormatBool(def))
	b, e := strconv.ParseBool(v)
	if e != nil {
		return def
	}
	return b
}

// PublicHostStatus returns detected outbound IPs and configured public hosts.
// Calling this also runs auto-IP adapt when the configured host is still an IP.
func (h *OpsHandler) PublicHostStatus(c *gin.Context) {
	st := h.S.AdaptPublicIPIfNeeded()
	c.JSON(200, gin.H{"success": true, "obj": st})
}

// SetPublicDomain lets the operator replace a filtered domain without touching IP fields.
func (h *OpsHandler) SetPublicDomain(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Domain) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "domain required"})
		return
	}
	if err := h.S.SetPublicDomain(req.Domain); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "ops.public_domain.set", req.Domain, nil)
	c.JSON(200, gin.H{"success": true, "obj": h.S.AdaptPublicIPIfNeeded()})
}

// SetPublicIPv4 lets an administrator correct the public IPv4 manually.
func (h *OpsHandler) SetPublicIPv4(c *gin.Context) {
	var req struct {
		IPv4 string `json:"ipv4"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.IPv4) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "ipv4 required"})
		return
	}
	if err := h.S.SetPublicIPv4(req.IPv4); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "ops.public_ipv4.set", strings.TrimSpace(req.IPv4), nil)
	c.JSON(200, gin.H{"success": true, "obj": h.S.AdaptPublicIPIfNeeded()})
}

// SetAutoPublicIP enables/disables automatic IP adaptation.
func (h *OpsHandler) SetAutoPublicIP(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "enabled required"})
		return
	}
	if err := h.S.SetAutoPublicIP(req.Enabled); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	auditAction(h.S, c, "ops.public_ip.auto", fmt.Sprintf("enabled=%t", req.Enabled), nil)
	c.JSON(200, gin.H{"success": true, "obj": h.S.AdaptPublicIPIfNeeded()})
}
