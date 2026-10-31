package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/backup"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ProHandler struct{ S *core.Server }

// NewProHandler creates or exposes the NewProHandler handler.
func NewProHandler(s *core.Server) *ProHandler { return &ProHandler{S: s} }
func (h *ProHandler) audit(c *gin.Context, action, target string, details any) {
	b, _ := json.Marshal(details)
	username := "unknown"
	if cl, ok := c.Get("claims"); ok {
		if x, ok := cl.(*auth.Claims); ok {
			username = x.Username
		}
	}
	h.S.DB.Create(&models.AuditLog{Username: username, Action: action, Target: target, IP: c.ClientIP(), Details: string(b)})
}

// System handles the System API operation.
func (h *ProHandler) System(c *gin.Context) {
	var users, clients, inbounds, nodes, keys int64
	h.S.DB.Model(&models.User{}).Count(&users)
	h.S.DB.Model(&models.Client{}).Count(&clients)
	h.S.DB.Model(&models.Inbound{}).Count(&inbounds)
	h.S.DB.Model(&models.ServerNode{}).Count(&nodes)
	h.S.DB.Model(&models.APIKey{}).Count(&keys)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"xray": h.S.Xray.Status(), "singbox": h.S.SingBox.Status(), "users": users, "clients": clients, "inbounds": inbounds, "nodes": nodes, "apiKeys": keys, "protocols": []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "naive", "wireguard", "anytls", "shadowtls"}}})
}

// Audit handles the Audit API operation.
func (h *ProHandler) Audit(c *gin.Context) {
	var rows []models.AuditLog
	q := h.S.DB.Order("id desc").Limit(300)
	if v := c.Query("action"); v != "" {
		q = q.Where("action LIKE ?", "%"+v+"%")
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Nodes handles the Nodes API operation.
func (h *ProHandler) Nodes(c *gin.Context) {
	var rows []models.ServerNode
	if err := h.S.DB.Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	ids := make([]uint, 0, len(rows))
	for _, n := range rows {
		ids = append(ids, n.ID)
	}
	var agents []models.AgentNode
	if len(ids) > 0 {
		_ = h.S.DB.Where("node_id IN ?", ids).Find(&agents).Error
	}
	agentByNode := make(map[uint]models.AgentNode, len(agents))
	for _, a := range agents {
		agentByNode[a.NodeID] = a
	}
	type nodeView struct {
		models.ServerNode
		AgentEnabled  bool       `json:"agentEnabled"`
		AgentID       string     `json:"agentId"`
		AgentVersion  string     `json:"agentVersion"`
		AgentLastSeen *time.Time `json:"agentLastSeen"`
	}
	out := make([]nodeView, 0, len(rows))
	for _, n := range rows {
		a := agentByNode[n.ID]
		out = append(out, nodeView{ServerNode: n, AgentEnabled: a.ID != 0 && a.Enabled, AgentID: a.AgentID, AgentVersion: a.Version, AgentLastSeen: a.LastSeenAt})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

type nodeReq struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	AgentEndpoint string `json:"agentEndpoint"`
	Role          string `json:"role"`
	Enabled       *bool  `json:"enabled"`
}

func normalizeAgentEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("agent endpoint must be a clean HTTPS URL without credentials, query or fragment")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("agent endpoint host is required")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("agent endpoint has an invalid port")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api/agent") {
		return "", fmt.Errorf("agent endpoint must end with /api/agent")
	}
	u.RawPath = ""
	return u.String(), nil
}

// AddNode handles the AddNode API operation.
func (h *ProHandler) AddNode(c *gin.Context) {
	var r nodeReq
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Address) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name and address are required"})
		return
	}
	if r.Port == 0 {
		r.Port = 22
	}
	if r.Port < 1 || r.Port > 65535 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid port"})
		return
	}
	endpoint, err := normalizeAgentEndpoint(r.AgentEndpoint)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	role := strings.ToLower(strings.TrimSpace(r.Role))
	if role == "" {
		role = "node"
	}
	if role != "node" && role != "edge" && role != "master" {
		c.JSON(400, gin.H{"success": false, "msg": "invalid node role"})
		return
	}
	n := models.ServerNode{Name: strings.TrimSpace(r.Name), Address: strings.TrimSpace(r.Address), Port: r.Port, AgentEndpoint: endpoint, Enabled: enabled, Role: role}
	if e := h.S.DB.Create(&n).Error; e != nil {
		c.JSON(400, gin.H{"success": false, "msg": e.Error()})
		return
	}
	h.audit(c, "node.create", fmt.Sprint(n.ID), r)
	c.JSON(200, gin.H{"success": true, "obj": n})
}

// DeleteNode handles the DeleteNode API operation.
func (h *ProHandler) DeleteNode(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var n models.ServerNode
	if err := h.S.DB.First(&n, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("node_id = ?", n.ID).Delete(&models.AgentNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("node_id = ?", n.ID).Delete(&models.NodePerformanceSnapshot{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target = ? AND status IN ?", fmt.Sprint(n.ID), []string{"queued", "running"}).Delete(&models.PlatformJob{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target = ? AND status IN ?", fmt.Sprint(n.ID), []string{"queued", "running", "failed"}).Delete(&models.Deployment{}).Error; err != nil {
			return err
		}
		return tx.Delete(&n).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "node.delete", id, gin.H{"node": n.Name})
	c.JSON(200, gin.H{"success": true})
}

// RotateNodeAgentToken handles the RotateNodeAgentToken API operation.
func (h *ProHandler) RotateNodeAgentToken(c *gin.Context) {
	var n models.ServerNode
	if h.S.DB.First(&n, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	token, err := randomToken(32)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "random generator failed"})
		return
	}
	n.AgentTokenHash = hashToken(token)
	var a models.AgentNode
	if h.S.DB.Where("node_id = ?", n.ID).First(&a).Error != nil {
		a = models.AgentNode{NodeID: n.ID}
	}
	a.TokenHash, a.Enabled = n.AgentTokenHash, true
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&n).Error; err != nil {
			return err
		}
		return tx.Save(&a).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "agent.token.rotate", fmt.Sprint(n.ID), gin.H{"node": n.Name})
	endpoint := n.AgentEndpoint
	if endpoint == "" {
		endpoint = strings.TrimRight(requestPanelBase(c, h.S.Config.PanelBasePath), "/") + "/api/agent"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"nodeId": n.ID, "token": token, "endpoint": endpoint, "warning": "This token is shown once. Store it securely."}})
}

// SetNodeAgentEnabled handles the SetNodeAgentEnabled API operation.
func (h *ProHandler) SetNodeAgentEnabled(c *gin.Context) {
	var r struct {
		Enabled bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "enabled required"})
		return
	}
	var n models.ServerNode
	if h.S.DB.First(&n, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	var a models.AgentNode
	if err := h.S.DB.Where("node_id = ?", n.ID).First(&a).Error; err != nil {
		if !r.Enabled {
			c.JSON(404, gin.H{"success": false, "msg": "agent is not enrolled for this node"})
			return
		}
		a = models.AgentNode{NodeID: n.ID}
	}
	a.Enabled = r.Enabled
	if err := h.S.DB.Save(&a).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "agent.enabled", fmt.Sprint(n.ID), gin.H{"enabled": r.Enabled})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"node": n, "agent": a}})
}

// NodeConnectionInfo handles the NodeConnectionInfo API operation.
func (h *ProHandler) NodeConnectionInfo(c *gin.Context) {
	var n models.ServerNode
	if h.S.DB.First(&n, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	var a models.AgentNode
	_ = h.S.DB.Where("node_id = ?", n.ID).First(&a).Error
	healthy := a.LastSeenAt != nil && time.Since(*a.LastSeenAt) < 30*time.Second && a.LastError == "" && a.Enabled && n.Enabled
	endpoint := n.AgentEndpoint
	if endpoint == "" {
		endpoint = strings.TrimRight(requestPanelBase(c, h.S.Config.PanelBasePath), "/") + "/api/agent"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"node": n, "agent": a, "healthy": healthy, "endpoint": endpoint}})
}

// APIKeys handles the APIKeys API operation.
func (h *ProHandler) APIKeys(c *gin.Context) {
	var rows []models.APIKey
	h.S.DB.Order("id desc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

type apiKeyReq struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// CreateAPIKey handles the CreateAPIKey API operation.
func (h *ProHandler) CreateAPIKey(c *gin.Context) {
	var r apiKeyReq
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name is required"})
		return
	}
	buf := make([]byte, 32)
	if _, e := rand.Read(buf); e != nil {
		c.JSON(500, gin.H{"success": false, "msg": "random generator failed"})
		return
	}
	secret := hex.EncodeToString(buf)
	prefix := secret[:10]
	token := prefix + "." + secret
	sum := sha256.Sum256([]byte(token))
	allowed := map[string]bool{"system:read": true, "system:write": true, "inbounds:read": true, "inbounds:write": true, "clients:read": true, "clients:write": true, "nodes:read": true, "nodes:write": true, "plans:read": true, "plans:write": true, "backups:read": true, "backups:write": true, "audit:read": true}
	if len(r.Scopes) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "at least one API scope is required"})
		return
	}
	seen := map[string]bool{}
	cleanScopes := make([]string, 0, len(r.Scopes))
	for _, scope := range r.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || !allowed[scope] || seen[scope] {
			continue
		}
		seen[scope] = true
		cleanScopes = append(cleanScopes, scope)
	}
	if len(cleanScopes) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "no valid API scopes supplied"})
		return
	}
	r.Scopes = cleanScopes
	sc, _ := json.Marshal(r.Scopes)
	k := models.APIKey{Name: strings.TrimSpace(r.Name), Prefix: prefix, SecretHash: hex.EncodeToString(sum[:]), Scopes: string(sc), Enabled: true}
	if e := h.S.DB.Create(&k).Error; e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	h.audit(c, "apikey.create", prefix, r)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": k.ID, "name": k.Name, "prefix": prefix, "secret": token, "scopes": r.Scopes}})
}

// DeleteAPIKey handles the DeleteAPIKey API operation.
func (h *ProHandler) DeleteAPIKey(c *gin.Context) {
	h.S.DB.Delete(&models.APIKey{}, c.Param("id"))
	h.audit(c, "apikey.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// Backup handles the Backup API operation.
func (h *ProHandler) Backup(c *gin.Context) {
	dir := filepath.Join(h.S.Config.DataDir(), "backups")
	row, err := backup.Create(h.S.DB, h.S.Config.DBPath, h.S.Config.XrayConfigPath, h.S.Config.SingBoxConfigPath, dir, h.S.Config.BackupRemoteDir)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = backup.Prune(h.S.DB, dir, h.S.Config.BackupRetention)
	h.audit(c, "backup.create", row.Path, nil)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// Backups handles the Backups API operation.
func (h *ProHandler) Backups(c *gin.Context) {
	var rows []models.BackupRecord
	h.S.DB.Order("id desc").Limit(100).Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// DownloadBackup handles the DownloadBackup API operation.
func (h *ProHandler) DownloadBackup(c *gin.Context) {
	var b models.BackupRecord
	if h.S.DB.First(&b, c.Param("id")).Error != nil {
		c.Status(404)
		return
	}
	c.FileAttachment(b.Path, filepath.Base(b.Path))
}

// RestoreBackup restores a previously registered backup by id.
func (h *ProHandler) RestoreBackup(c *gin.Context) {
	var b models.BackupRecord
	if h.S.DB.First(&b, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "backup not found"})
		return
	}
	if b.Path == "" {
		c.JSON(400, gin.H{"success": false, "msg": "backup path is empty"})
		return
	}
	if err := h.startRestore(b.Path); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "backup.restore", fmt.Sprint(b.ID), gin.H{"path": b.Path})
	c.JSON(200, gin.H{"success": true, "restartRequired": true, "msg": "restore started; the panel is restarting"})
}

// UploadBackup accepts a panel backup zip (from another server), stores it,
// registers a BackupRecord, and optionally starts restore immediately.
// Form fields:
//   - file: multipart zip (required)
//   - restore: "1" / "true" to restore right after upload (default true for migration)
func (h *ProHandler) UploadBackup(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "msg": "backup upload exceeds the 1.5 GiB limit"})
			return
		}
		c.JSON(400, gin.H{"success": false, "msg": "file is required (multipart field name: file)"})
		return
	}
	if fh.Size <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "empty upload"})
		return
	}
	// Soft limit 1.5 GiB — full panel DB + configs.
	if fh.Size > 1536<<20 {
		c.JSON(400, gin.H{"success": false, "msg": "backup file too large (max 1.5 GiB)"})
		return
	}
	name := strings.ToLower(fh.Filename)
	if !strings.HasSuffix(name, ".zip") {
		c.JSON(400, gin.H{"success": false, "msg": "backup must be a .zip archive"})
		return
	}

	dir := filepath.Join(h.S.Config.DataDir(), "backups")
	if err := os.MkdirAll(dir, 0750); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "cannot create backup dir: " + err.Error()})
		return
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	dest := filepath.Join(dir, "electruvpn-upload-"+stamp+".zip")

	src, err := fh.Open()
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	written, copyErr := io.Copy(out, src)
	_ = out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		c.JSON(500, gin.H{"success": false, "msg": "failed to store upload: " + copyErr.Error()})
		return
	}

	preferred := filepath.Base(h.S.Config.DBPath)
	if h.S.Config.UsingPostgres() {
		preferred = "DATABASE_IS_POSTGRES.txt"
	}
	if _, err := backup.FindDatabaseMember(dest, preferred); err != nil {
		// Allow migration from another host even if basename differs.
		if _, err2 := backup.FindDatabaseMember(dest, ""); err2 != nil {
			_ = os.Remove(dest)
			c.JSON(400, gin.H{"success": false, "msg": "invalid backup archive: " + err2.Error()})
			return
		}
	}

	row := models.BackupRecord{Path: dest, Size: written}
	if err := h.S.DB.Create(&row).Error; err != nil {
		_ = os.Remove(dest)
		c.JSON(500, gin.H{"success": false, "msg": "failed to register backup: " + err.Error()})
		return
	}
	h.audit(c, "backup.upload", fmt.Sprint(row.ID), gin.H{"path": dest, "size": written, "filename": fh.Filename})

	doRestore := true
	switch strings.ToLower(strings.TrimSpace(c.PostForm("restore"))) {
	case "0", "false", "no", "off":
		doRestore = false
	}
	if !doRestore {
		c.JSON(200, gin.H{"success": true, "obj": row, "msg": "backup uploaded; restore not requested"})
		return
	}
	if err := h.startRestore(dest); err != nil {
		c.JSON(400, gin.H{"success": false, "obj": row, "msg": "uploaded but restore failed: " + err.Error()})
		return
	}
	h.audit(c, "backup.restore", fmt.Sprint(row.ID), gin.H{"path": dest, "source": "upload"})
	c.JSON(200, gin.H{"success": true, "obj": row, "restartRequired": true, "msg": "backup uploaded; restore started — panel is restarting"})
}

// startRestore validates the archive, takes a safety snapshot, and launches restore-backup.sh via systemd-run.
func (h *ProHandler) startRestore(archivePath string) error {
	preferred := filepath.Base(h.S.Config.DBPath)
	if h.S.Config.UsingPostgres() {
		preferred = "DATABASE_IS_POSTGRES.txt"
	}
	if _, err := backup.FindDatabaseMember(archivePath, preferred); err != nil {
		if _, err2 := backup.FindDatabaseMember(archivePath, ""); err2 != nil {
			return fmt.Errorf("backup does not contain the panel database: %w", err2)
		}
	}
	// Safety snapshot of current state (best-effort on a brand-new install).
	dir := filepath.Join(h.S.Config.DataDir(), "backups")
	if _, err := backup.Create(h.S.DB, h.S.Config.DBPath, h.S.Config.XrayConfigPath, h.S.Config.SingBoxConfigPath, dir, h.S.Config.BackupRemoteDir); err != nil {
		// On empty fresh install this may fail; still allow restore for migration.
		_ = err
	}
	helper := filepath.Join(filepath.Dir(h.S.Config.EnvFilePath), "restore-backup.sh")
	if _, err := os.Stat(helper); err != nil {
		helper = filepath.Join("/opt/electruvpn", "restore-backup.sh")
	}
	if _, err := os.Stat(helper); err != nil {
		helper = filepath.Join("/opt/electruvpn", "scripts", "restore-backup.sh")
	}
	if _, err := os.Stat(helper); err != nil {
		return fmt.Errorf("restore-backup.sh not found")
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return fmt.Errorf("systemd-run is required for safe database restore")
	}
	unit := fmt.Sprintf("electruvpn-restore-%d", time.Now().UnixNano())
	cmd := exec.Command("systemd-run", "--unit="+unit, "--collect", "--property=NoNewPrivileges=no",
		helper, archivePath, h.S.Config.DBPath, h.S.Config.XrayConfigPath, h.S.Config.SingBoxConfigPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restore helper could not start: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteBackup handles the DeleteBackup API operation.
func (h *ProHandler) DeleteBackup(c *gin.Context) {
	var b models.BackupRecord
	if h.S.DB.First(&b, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "backup not found"})
		return
	}
	if b.Path != "" {
		_ = os.Remove(b.Path)
	}
	if err := h.S.DB.Delete(&b).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "backup.delete", fmt.Sprint(b.ID), nil)
	c.JSON(200, gin.H{"success": true})
}

// RotateJWT handles the RotateJWT API operation.
func (h *ProHandler) RotateJWT(c *gin.Context) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "msg": "JWT secret generated; update ELECTRU_JWT_SECRET in the environment and restart the service", "obj": gin.H{"secret": hex.EncodeToString(b)}})
}

type createUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUser handles the CreateUser API operation.
func (h *ProHandler) CreateUser(c *gin.Context) {
	var r createUserReq
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Username) == "" || len(r.Password) < 8 {
		c.JSON(400, gin.H{"success": false, "msg": "username and an 8+ character password are required"})
		return
	}
	if r.Role == "" {
		r.Role = "operator"
	}
	if !map[string]bool{"owner": true, "admin": true, "operator": true, "viewer": true}[r.Role] {
		c.JSON(400, gin.H{"success": false, "msg": "invalid role"})
		return
	}
	if r.Role == "owner" {
		claims := c.MustGet("claims").(*auth.Claims)
		var actor models.User
		if h.S.DB.First(&actor, claims.UserID).Error != nil || actor.Role != "owner" {
			c.JSON(403, gin.H{"success": false, "msg": "only an owner can create another owner"})
			return
		}
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if e != nil {
		c.JSON(500, gin.H{"success": false, "msg": e.Error()})
		return
	}
	u := models.User{Username: r.Username, PasswordHash: string(hash), Role: r.Role, MustChangePassword: false}
	if e = h.S.DB.Create(&u).Error; e != nil {
		c.JSON(409, gin.H{"success": false, "msg": e.Error()})
		return
	}
	h.audit(c, "user.create", r.Username, r.Role)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": u.ID, "username": u.Username, "role": u.Role}})
}

// ChangeRole handles the ChangeRole API operation.
func (h *ProHandler) ChangeRole(c *gin.Context) {
	var r struct {
		Role string `json:"role"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid role"})
		return
	}
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
	if !map[string]bool{"owner": true, "admin": true, "operator": true, "viewer": true, "sales": true, "support": true, "reseller": true}[r.Role] {
		c.JSON(400, gin.H{"success": false, "msg": "invalid role"})
		return
	}
	claims := c.MustGet("claims").(*auth.Claims)
	var actor models.User
	if h.S.DB.First(&actor, claims.UserID).Error != nil {
		c.JSON(401, gin.H{"success": false, "msg": "actor not found"})
		return
	}
	var target models.User
	if h.S.DB.First(&target, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "user not found"})
		return
	}
	if target.ID == actor.ID {
		c.JSON(400, gin.H{"success": false, "msg": "you cannot change your own role"})
		return
	}
	if r.Role == "owner" && actor.Role != "owner" {
		c.JSON(403, gin.H{"success": false, "msg": "only an owner can grant owner role"})
		return
	}
	if target.Role == "owner" && actor.Role != "owner" {
		c.JSON(403, gin.H{"success": false, "msg": "only an owner can modify an owner"})
		return
	}
	if target.Role == "owner" && r.Role != "owner" {
		var owners int64
		h.S.DB.Model(&models.User{}).Where("role = ?", "owner").Count(&owners)
		if owners <= 1 {
			c.JSON(409, gin.H{"success": false, "msg": "cannot remove the last owner"})
			return
		}
	}
	if err := h.S.DB.Model(&target).Update("role", r.Role).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "user.role", c.Param("id"), r.Role)
	c.JSON(200, gin.H{"success": true})
}

// Users handles the Users API operation.
func (h *ProHandler) Users(c *gin.Context) {
	var rows []models.User
	h.S.DB.Select("id,username,role,must_change_password,last_login_at,last_login_ip,created_at,updated_at").Order("id asc").Find(&rows)
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// TestNotification handles the TestNotification API operation.
func (h *ProHandler) TestNotification(c *gin.Context) {
	if err := notify.Telegram("ELECTRUVPN BETA1 notification test: the Telegram integration is working."); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	h.audit(c, "notification.telegram.test", "telegram", nil)
	c.JSON(200, gin.H{"success": true})
}

// HashAPISecret handles the HashAPISecret API operation.
func (h *ProHandler) HashAPISecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
