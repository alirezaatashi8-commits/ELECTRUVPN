package handlers

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
)

// V35Handler exposes the integrated operations surface. It intentionally
// composes existing node, deployment, security, backup, audit and analytics
// primitives instead of creating parallel state machines.
type V35Handler struct{ S *core.Server }

// NewV35Handler creates or exposes the NewV35Handler handler.
func NewV35Handler(s *core.Server) *V35Handler { return &V35Handler{S: s} }

// CommandCenter handles the CommandCenter API operation.
func (h *V35Handler) CommandCenter(c *gin.Context) {
	now := time.Now()
	var inbounds, clients, nodes, healthy, deployments, operations, alerts, backups int64
	h.S.DB.Model(&models.Inbound{}).Where("enable = ?", true).Count(&inbounds)
	h.S.DB.Model(&models.Client{}).Count(&clients)
	h.S.DB.Model(&models.ServerNode{}).Where("enabled = ?", true).Count(&nodes)
	h.S.DB.Model(&models.AgentNode{}).Where("enabled = ? AND health_state = ?", true, "healthy").Count(&healthy)
	h.S.DB.Model(&models.Deployment{}).Where("status IN ?", []string{"queued", "running", "failed"}).Count(&deployments)
	h.S.DB.Model(&models.OperationRecord{}).Where("status IN ?", []string{"queued", "running", "failed"}).Count(&operations)
	h.S.DB.Model(&models.AlertEvent{}).Where("resolved_at IS NULL").Count(&alerts)
	h.S.DB.Model(&models.BackupRecord{}).Count(&backups)

	latestVerified := false
	if row, err := latestBackup(h.S); err == nil {
		latestVerified = verifyBackupArchive(h.S, &row)
	}

	var expired int64
	h.S.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ?", now.UnixMilli()).Count(&expired)

	xray := h.S.Xray.Status()
	singbox := h.S.SingBox.Status()
	dbOK := h.S.DB.Exec("SELECT 1").Error == nil
	free := diskFree(h.S.Config.DataDir())

	security := map[string]any{}
	if v := settingBool(h.S, "settings.securityHeaders", true); v {
		security["headers"] = true
	}
	security["csrf"] = settingBool(h.S, "settings.csrf", true)
	security["fail2ban"] = settingBool(h.S, "settings.fail2banEnabled", true)
	security["https"] = c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")

	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"timestamp":  now.UTC(),
		"services":   gin.H{"xray": xray, "singbox": singbox, "database": dbOK},
		"capacity":   gin.H{"inbounds": inbounds, "clients": clients, "nodes": nodes, "healthyNodes": healthy, "expiredClients": expired},
		"operations": gin.H{"deployments": deployments, "activeOperations": operations, "openAlerts": alerts},
		"recovery":   gin.H{"backups": backups, "latestVerified": latestVerified},
		"security":   security,
		"disk":       free,
	}})
}

// Preflight handles the Preflight API operation.
func (h *V35Handler) Preflight(c *gin.Context) {
	checks := make([]gin.H, 0, 8)
	add := func(key, title string, ok bool, detail string) {
		checks = append(checks, gin.H{"key": key, "title": title, "ok": ok, "detail": detail})
	}

	var inbounds []models.Inbound
	if err := h.S.DB.Where("enable = ?", true).Order("port asc").Find(&inbounds).Error; err != nil {
		add("inbounds", "Enabled inbound inventory readable", false, err.Error())
	}
	seen := map[int]bool{}
	duplicate := 0
	xrayNeeded, singboxNeeded := false, false
	for _, ib := range inbounds {
		if seen[ib.Port] {
			duplicate++
		}
		seen[ib.Port] = true
		switch ib.Protocol {
		case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
			singboxNeeded = true
		default:
			xrayNeeded = true
		}
	}
	dbLabel := "PostgreSQL health probe"
	if !h.S.Config.UsingPostgres() {
		dbLabel = "SQLite health probe"
	}
	add("db", "Database reachable", h.S.DB.Exec("SELECT 1").Error == nil, dbLabel)
	add("ports", "Enabled inbound ports unique", duplicate == 0, fmt.Sprintf("%d duplicate port conflicts", duplicate))
	if xrayNeeded {
		add("xray-config", "Xray config present", fileExists(h.S.Config.XrayConfigPath), h.S.Config.XrayConfigPath)
		add("xray", "Xray running", h.S.Xray.Status().Running, "runtime status")
	}
	if singboxNeeded {
		add("singbox-config", "sing-box config present", fileExists(h.S.Config.SingBoxConfigPath), h.S.Config.SingBoxConfigPath)
		add("singbox", "sing-box running", h.S.SingBox.Status().Running, "runtime status")
	}
	add("node-fabric", "Node fabric available", h.S.DB.Model(&models.ServerNode{}).Where("enabled = ?", true).Count(new(int64)).Error == nil, "node inventory query")
	add("rollback", "Rollback history available", h.S.DB.Model(&models.InboundRevision{}).Count(new(int64)).Error == nil, "revision store")
	add("backup-store", "Backup store reachable", h.S.DB.Model(&models.BackupRecord{}).Count(new(int64)).Error == nil, "backup metadata")

	passed := 0
	for _, x := range checks {
		if x["ok"] == true {
			passed++
		}
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"checks": checks, "passed": passed, "total": len(checks), "ready": passed == len(checks), "generatedAt": time.Now().UTC()}})
}

// Recovery handles the Recovery API operation.
func (h *V35Handler) Recovery(c *gin.Context) {
	row, err := latestBackup(h.S)
	if err != nil {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"available": false, "reason": "no backup records"}})
		return
	}
	archiveOK, dbIncluded, sizeOK, readable := inspectBackupArchive(h.S, &row)
	verified := archiveOK && dbIncluded && sizeOK && readable
	integrity := "unknown"
	var result string
	if err := h.S.DB.Raw("PRAGMA integrity_check").Scan(&result).Error; err == nil {
		integrity = result
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"available":         true,
		"backup":            gin.H{"id": row.ID, "size": row.Size, "createdAt": row.CreatedAt, "verified": verified},
		"archiveOK":         archiveOK,
		"databaseIncluded":  dbIncluded,
		"sizeMatches":       sizeOK,
		"entriesReadable":   readable,
		"verified":          verified,
		"databaseIntegrity": integrity,
		"checkedAt":         time.Now().UTC(),
	}})
}

func latestBackup(s *core.Server) (models.BackupRecord, error) {
	var row models.BackupRecord
	if err := s.DB.Order("id desc").First(&row).Error; err != nil {
		return models.BackupRecord{}, err
	}
	return row, nil
}

func inspectBackupArchive(s *core.Server, row *models.BackupRecord) (archiveOK, dbIncluded, sizeOK, readable bool) {
	if row == nil || strings.TrimSpace(row.Path) == "" {
		return false, false, false, false
	}
	st, err := os.Stat(row.Path)
	if err != nil || st.IsDir() {
		return false, false, false, false
	}
	sizeOK = row.Size <= 0 || row.Size == st.Size()
	z, err := zip.OpenReader(row.Path)
	if err != nil {
		return false, false, sizeOK, false
	}
	defer z.Close()
	archiveOK = true
	dbName := filepath.Base(s.Config.DBPath)
	if s.Config.UsingPostgres() {
		dbName = "DATABASE_IS_POSTGRES.txt"
	}
	readable = true
	for _, f := range z.File {
		if f.Name == dbName {
			dbIncluded = true
		}
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			readable = false
			break
		}
		_, err = io.Copy(io.Discard, rc)
		closeErr := rc.Close()
		if err != nil || closeErr != nil {
			readable = false
			break
		}
	}
	return archiveOK, dbIncluded, sizeOK, readable
}

func verifyBackupArchive(s *core.Server, row *models.BackupRecord) bool {
	a, d, sz, r := inspectBackupArchive(s, row)
	return a && d && sz && r
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
