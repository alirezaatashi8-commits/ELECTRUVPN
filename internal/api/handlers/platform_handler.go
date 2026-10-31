package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/syncbus"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PlatformHandler struct{ S *core.Server }

// NewPlatformHandler creates or exposes the NewPlatformHandler handler.
func NewPlatformHandler(s *core.Server) *PlatformHandler { return &PlatformHandler{S: s} }

func auditUsername(c *gin.Context) string {
	if v, ok := c.Get("claims"); ok {
		if cl, ok := v.(*auth.Claims); ok && strings.TrimSpace(cl.Username) != "" {
			return cl.Username
		}
	}
	return "unknown"
}

func platformAudit(s *core.Server, c *gin.Context, action, target string, details any) {
	b, _ := json.Marshal(details)
	username := "unknown"
	if v, ok := c.Get("claims"); ok {
		if cl, ok := v.(*auth.Claims); ok {
			username = cl.Username
		}
	}
	_ = s.DB.Create(&models.AuditLog{Username: username, Action: action, Target: target, IP: c.ClientIP(), Details: string(b)}).Error
	go dispatchWebhookEvent(s, action, gin.H{"action": action, "target": target, "username": username, "ip": c.ClientIP(), "details": details, "time": time.Now().UTC()})
}

func dispatchWebhookEvent(s *core.Server, event string, payload any) {
	var hooks []models.WebhookEndpoint
	if s.DB.Where("enabled = ?", true).Find(&hooks).Error != nil {
		return
	}
	body, _ := json.Marshal(payload)
	for _, hook := range hooks {
		events := strings.TrimSpace(hook.Events)
		if events != "" && events != "*" {
			ok := false
			for _, item := range strings.Split(events, ",") {
				if strings.TrimSpace(item) == event {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		u, err := url.Parse(strings.TrimSpace(hook.URL))
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Electru-Event", event)
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		req.Header.Set("X-Electru-Signature", hex.EncodeToString(mac.Sum(nil)))
		client := safeWebhookClient()
		res, err := client.Do(req)
		if err == nil {
			_ = res.Body.Close()
		}
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hashToken(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }

func safeWebhookClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout:       6 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("webhook redirects are disabled") },
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				ips, err := net.LookupIP(host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if isPrivateOrLocalIP(ip) {
						continue
					}
					conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
					if err == nil {
						return conn, nil
					}
				}
				return nil, fmt.Errorf("webhook host resolves only to private/local addresses")
			},
		},
	}
}

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// RFC 6598 shared address space plus link-local.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
	}
	return false
}

// Jobs handles the Jobs API operation.
func (h *PlatformHandler) Jobs(c *gin.Context) {
	var rows []models.PlatformJob
	q := h.S.DB.Order("id desc").Limit(200)
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateJob handles the CreateJob API operation.
func (h *PlatformHandler) CreateJob(c *gin.Context) {
	var r struct{ Kind, Target, Payload string }
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Kind) == "" || strings.TrimSpace(r.Target) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "kind and target required"})
		return
	}
	allowed := map[string]bool{"config-validate": true, "xray-restart": true, "xray-status": true, "singbox-restart": true, "singbox-status": true, "bbr-apply": true, "bbr-status": true, "network-status": true, "agent-diagnostics": true, "xray-config-apply": true, "singbox-config-apply": true, "xray-config-rollback": true, "singbox-config-rollback": true}
	kind := strings.TrimSpace(r.Kind)
	if !allowed[kind] {
		c.JSON(400, gin.H{"success": false, "msg": "job kind not allowlisted"})
		return
	}
	if _, err := strconv.ParseUint(strings.TrimSpace(r.Target), 10, 64); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "target must be a node id"})
		return
	}
	var node models.ServerNode
	if h.S.DB.First(&node, strings.TrimSpace(r.Target)).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "target node not found"})
		return
	}
	opID := uuid.NewString()
	j := models.PlatformJob{Kind: kind, Target: strings.TrimSpace(r.Target), Payload: r.Payload, Status: "queued", Progress: 0, OperationID: opID}
	op := models.OperationRecord{OperationID: opID, Type: "job." + kind, Target: strings.TrimSpace(r.Target), Actor: auditUsername(c), Status: "queued", Phase: "queued", RollbackOnFail: true, Payload: r.Payload}
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		return tx.Create(&j).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "job.create", fmt.Sprint(j.ID), r)
	c.JSON(200, gin.H{"success": true, "obj": j})
}

// DeployCurrentConfig handles the DeployCurrentConfig API operation.
func (h *PlatformHandler) DeployCurrentConfig(c *gin.Context) {
	var r struct {
		Engine string `json:"engine"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "engine required"})
		return
	}
	engine := strings.ToLower(strings.TrimSpace(r.Engine))
	var path, kind string
	switch engine {
	case "xray":
		path, kind = h.S.Config.XrayConfigPath, "xray-config-apply"
	case "singbox":
		path, kind = h.S.Config.SingBoxConfigPath, "singbox-config-apply"
	default:
		c.JSON(400, gin.H{"success": false, "msg": "engine must be xray or singbox"})
		return
	}
	target := strings.TrimSpace(c.Param("id"))
	var node models.ServerNode
	if h.S.DB.First(&node, target).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "local config unavailable"})
		return
	}
	if len(data) == 0 || len(data) > 4*1024*1024 {
		c.JSON(400, gin.H{"success": false, "msg": "config size is invalid"})
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	versionFrom := ""
	if current, readErr := os.ReadFile(path); readErr == nil && len(current) > 0 {
		previous := sha256.Sum256(current)
		versionFrom = hex.EncodeToString(previous[:])[:16]
	}
	payloadBytes, _ := json.Marshal(gin.H{"engine": engine, "content": string(data), "sha256": hash})
	var job models.PlatformJob
	var deployment models.Deployment
	opID := uuid.NewString()
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		op := models.OperationRecord{OperationID: opID, Type: "deployment." + engine, Target: target, Actor: auditUsername(c), Status: "queued", Phase: "preflight", RollbackOnFail: true, Payload: string(payloadBytes)}
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		job = models.PlatformJob{Kind: kind, Target: target, Payload: string(payloadBytes), Status: "queued", Progress: 0, OperationID: opID}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		deployment = models.Deployment{Name: "Deploy " + engine + " config to " + node.Name, Target: target, Kind: kind, Status: "queued", VersionFrom: versionFrom, VersionTo: hash[:16], ArtifactHash: hash, Progress: 0, RollbackOnFail: true, JobID: job.ID}
		return tx.Create(&deployment).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "agent.config.deploy", target, gin.H{"engine": engine, "jobId": job.ID, "deploymentId": deployment.ID, "sha256": hash})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"job": job, "deployment": deployment}})
}

// BillingPlans handles the BillingPlans API operation.
func (h *PlatformHandler) BillingPlans(c *gin.Context) {
	var rows []models.BillingPlan
	if err := h.S.DB.Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateBillingPlan handles the CreateBillingPlan API operation.
func (h *PlatformHandler) CreateBillingPlan(c *gin.Context) {
	var r models.BillingPlan
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || r.AmountCents < 0 || r.PeriodDays < 0 || r.TrafficGB < 0 || r.DeviceLimit < 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid billing plan"})
		return
	}
	if r.Currency == "" {
		r.Currency = "EUR"
	}
	r.Enabled = true
	if err := h.S.DB.Create(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "billing.plan.create", fmt.Sprint(r.ID), r.Name)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteBillingPlan handles the DeleteBillingPlan API operation.
func (h *PlatformHandler) DeleteBillingPlan(c *gin.Context) {
	if err := h.S.DB.Delete(&models.BillingPlan{}, c.Param("id")).Error; err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "billing.plan.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// Orders handles the Orders API operation.
func (h *PlatformHandler) Orders(c *gin.Context) {
	var rows []models.Order
	q := h.S.DB.Order("id desc").Limit(500)
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateOrder handles the CreateOrder API operation.
func (h *PlatformHandler) CreateOrder(c *gin.Context) {
	var r struct {
		ClientID, PlanID uint
		Provider         string
	}
	if c.ShouldBindJSON(&r) != nil || r.ClientID == 0 || r.PlanID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "clientId and planId required"})
		return
	}
	var p models.BillingPlan
	if h.S.DB.First(&p, r.PlanID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "billing plan not found"})
		return
	}
	if h.S.DB.First(&models.Client{}, r.ClientID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "client not found"})
		return
	}
	o := models.Order{OrderNo: "EV-" + strings.ToUpper(uuid.NewString()[:12]), ClientID: r.ClientID, PlanID: p.ID, AmountCents: p.AmountCents, Currency: p.Currency, Status: "pending", Provider: r.Provider}
	if err := h.S.DB.Create(&o).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "billing.order.create", o.OrderNo, r)
	c.JSON(200, gin.H{"success": true, "obj": o})
}

// UpdateOrder handles the UpdateOrder API operation.
func (h *PlatformHandler) UpdateOrder(c *gin.Context) {
	var r struct{ Status, ProviderRef string }
	if c.ShouldBindJSON(&r) != nil || r.Status == "" {
		c.JSON(400, gin.H{"success": false, "msg": "status required"})
		return
	}
	if err := h.S.DB.Model(&models.Order{}).Where("id = ?", c.Param("id")).Updates(map[string]any{"status": r.Status, "provider_ref": r.ProviderRef}).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "billing.order.update", c.Param("id"), r)
	c.JSON(200, gin.H{"success": true})
}

// Webhooks handles the Webhooks API operation.
func (h *PlatformHandler) Webhooks(c *gin.Context) {
	var rows []models.WebhookEndpoint
	if err := h.S.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateWebhook handles the CreateWebhook API operation.
func (h *PlatformHandler) CreateWebhook(c *gin.Context) {
	var r struct {
		Name, URL, Secret, Events string
		Enabled                   *bool
	}
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.URL)), "https://") {
		c.JSON(400, gin.H{"success": false, "msg": "name and HTTPS URL required"})
		return
	}
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || u.Hostname() == "" {
		c.JSON(400, gin.H{"success": false, "msg": "invalid webhook URL"})
		return
	}
	ips, err := net.LookupIP(u.Hostname())
	if err != nil || len(ips) == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "webhook hostname does not resolve"})
		return
	}
	for _, ip := range ips {
		if isPrivateOrLocalIP(ip) {
			c.JSON(400, gin.H{"success": false, "msg": "webhook URL must resolve to public addresses"})
			return
		}
	}
	en := true
	if r.Enabled != nil {
		en = *r.Enabled
	}
	if r.Secret == "" {
		r.Secret, _ = randomToken(24)
	}
	w := models.WebhookEndpoint{Name: r.Name, URL: strings.TrimSpace(r.URL), Secret: r.Secret, Events: r.Events, Enabled: en}
	if err := h.S.DB.Create(&w).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "webhook.create", fmt.Sprint(w.ID), w.Name)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": w.ID, "name": w.Name, "url": w.URL, "events": w.Events, "secret": r.Secret}})
}

// DeleteWebhook handles the DeleteWebhook API operation.
func (h *PlatformHandler) DeleteWebhook(c *gin.Context) {
	h.S.DB.Delete(&models.WebhookEndpoint{}, c.Param("id"))
	platformAudit(h.S, c, "webhook.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// Certificates handles the Certificates API operation.
func (h *PlatformHandler) Certificates(c *gin.Context) {
	var rows []models.CertificateRecord
	if err := h.S.DB.Order("expires_at asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// AddCertificate handles the AddCertificate API operation.
func (h *PlatformHandler) AddCertificate(c *gin.Context) {
	var r models.CertificateRecord
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Domain) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name and domain required"})
		return
	}
	if r.Status == "" {
		r.Status = "managed"
	}
	if err := h.S.DB.Create(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "certificate.create", fmt.Sprint(r.ID), r.Domain)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeleteCertificate handles the DeleteCertificate API operation.
func (h *PlatformHandler) DeleteCertificate(c *gin.Context) {
	h.S.DB.Delete(&models.CertificateRecord{}, c.Param("id"))
	platformAudit(h.S, c, "certificate.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// Plugins handles the Plugins API operation.
func (h *PlatformHandler) Plugins(c *gin.Context) {
	var rows []models.PluginRecord
	if err := h.S.DB.Order("name asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// UpsertPlugin handles the UpsertPlugin API operation.
func (h *PlatformHandler) UpsertPlugin(c *gin.Context) {
	var r models.PluginRecord
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name required"})
		return
	}
	var old models.PluginRecord
	if h.S.DB.Where("name = ?", r.Name).First(&old).Error == nil {
		r.ID = old.ID
		r.CreatedAt = old.CreatedAt
	}
	if err := h.S.DB.Save(&r).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "plugin.upsert", r.Name, r)
	c.JSON(200, gin.H{"success": true, "obj": r})
}

// DeletePlugin handles the DeletePlugin API operation.
func (h *PlatformHandler) DeletePlugin(c *gin.Context) {
	h.S.DB.Delete(&models.PluginRecord{}, c.Param("id"))
	platformAudit(h.S, c, "plugin.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// RegisterAgent handles the RegisterAgent API operation.
func (h *PlatformHandler) RegisterAgent(c *gin.Context) {
	var r struct{ NodeID uint }
	if c.ShouldBindJSON(&r) != nil || r.NodeID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "nodeId required"})
		return
	}
	var n models.ServerNode
	if h.S.DB.First(&n, r.NodeID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	token, err := randomToken(32)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	hash := hashToken(token)
	endpoint := requestPanelBase(c, h.S.Config.PanelBasePath) + "/api/agent"
	if !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		c.JSON(http.StatusPreconditionFailed, gin.H{"success": false, "msg": "agent enrollment requires an HTTPS panel endpoint"})
		return
	}
	n.AgentTokenHash, n.AgentEndpoint = hash, endpoint
	var a models.AgentNode
	if h.S.DB.Where("node_id = ?", n.ID).First(&a).Error != nil {
		a = models.AgentNode{NodeID: n.ID}
	}
	a.TokenHash, a.Enabled = hash, true
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&n).Error; err != nil {
			return err
		}
		return tx.Save(&a).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "agent.register", fmt.Sprint(n.ID), n.Name)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"nodeId": n.ID, "token": token, "endpoint": endpoint, "warning": "This token is shown once. Store it securely."}})
}
func agentAuth(h *PlatformHandler, c *gin.Context) (*models.ServerNode, bool) {
	t := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if t == "" {
		c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "agent token required"})
		return nil, false
	}
	hash := hashToken(t)
	var n models.ServerNode
	if h.S.DB.Where("agent_token_hash = ? AND enabled = ?", hash, true).First(&n).Error != nil {
		c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "invalid agent token"})
		return nil, false
	}
	var a models.AgentNode
	if err := h.S.DB.Where("node_id = ?", n.ID).First(&a).Error; err != nil {
		c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "agent is not enrolled"})
		return nil, false
	}
	if !a.Enabled {
		c.AbortWithStatusJSON(403, gin.H{"success": false, "msg": "agent disabled"})
		return nil, false
	}
	if !hmac.Equal([]byte(a.TokenHash), []byte(hash)) {
		c.AbortWithStatusJSON(401, gin.H{"success": false, "msg": "agent token mismatch"})
		return nil, false
	}
	return &n, true
}

// AgentHeartbeat handles the AgentHeartbeat API operation.
func (h *PlatformHandler) AgentHeartbeat(c *gin.Context) {
	n, ok := agentAuth(h, c)
	if !ok {
		return
	}
	var r struct {
		Version, OS, Arch, Hostname, AgentID                                       string
		CPUPercent                                                                 float64
		MemoryBytes, MemoryTotalBytes, DiskBytes, DiskTotalBytes, RxBytes, TxBytes int64
		RTTMs                                                                      float64
		Error                                                                      string
		Capabilities                                                               map[string]any `json:"capabilities"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid heartbeat"})
		return
	}
	now := time.Now()
	remoteIP := c.ClientIP()
	h.S.DB.Model(n).Updates(map[string]any{"last_seen_at": now, "last_error": r.Error})
	var a models.AgentNode
	if h.S.DB.Where("node_id = ?", n.ID).First(&a).Error != nil {
		a = models.AgentNode{NodeID: n.ID}
	}
	capJSON, _ := json.Marshal(r.Capabilities)
	a.Version, a.OS, a.Arch, a.Hostname, a.AgentID, a.RemoteIP = r.Version, r.OS, r.Arch, r.Hostname, r.AgentID, remoteIP
	a.Capabilities, a.HealthState = string(capJSON), "healthy"
	if r.Error != "" {
		a.HealthState = "degraded"
	}
	a.CPUPercent, a.MemoryBytes, a.MemoryTotalBytes, a.DiskBytes, a.DiskTotalBytes, a.RxBytes, a.TxBytes, a.LastSeenAt, a.LastError = r.CPUPercent, r.MemoryBytes, r.MemoryTotalBytes, r.DiskBytes, r.DiskTotalBytes, r.RxBytes, r.TxBytes, &now, r.Error
	if a.Enabled == false && a.ID == 0 {
		a.Enabled = true
	}
	h.S.DB.Save(&a)
	h.S.DB.Create(&models.NodePerformanceSnapshot{NodeID: n.ID, CPUPercent: r.CPUPercent, MemoryBytes: r.MemoryBytes, MemoryTotalBytes: r.MemoryTotalBytes, DiskBytes: r.DiskBytes, DiskTotalBytes: r.DiskTotalBytes, RxBytes: r.RxBytes, TxBytes: r.TxBytes, RTTMs: r.RTTMs, Healthy: r.Error == "", CreatedAt: now})
	// Keep the SQLite history bounded; recent samples are sufficient for routing and charts.
	h.S.DB.Where("node_id = ? AND created_at < ?", n.ID, now.Add(-7*24*time.Hour)).Delete(&models.NodePerformanceSnapshot{})
	// In-memory node registry: routing / dashboards can read without hitting DB every time.
	syncbus.Default.PutNode(syncbus.NodeSnapshot{
		NodeID: n.ID, AgentID: r.AgentID, Health: a.HealthState,
		CPUPercent: r.CPUPercent, MemoryBytes: r.MemoryBytes, MemoryTotalBytes: r.MemoryTotalBytes,
		DiskBytes: r.DiskBytes, DiskTotalBytes: r.DiskTotalBytes,
		RxBytes: r.RxBytes, TxBytes: r.TxBytes, RTTMs: r.RTTMs, RemoteIP: remoteIP, UpdatedAt: now,
	})
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"nodeId": n.ID, "serverTime": now, "pollAfter": 5, "reportTraffic": true}})
}

// AgentTraffic accepts a compact batch of per-user Xray counters from a node Agent.
// Samples are enqueued on the sync bus and flushed asynchronously to the DB so
// heartbeats stay cheap under high concurrency.
func (h *PlatformHandler) AgentTraffic(c *gin.Context) {
	n, ok := agentAuth(h, c)
	if !ok {
		return
	}
	var r struct {
		// Samples is a map of email -> [uplink, downlink] absolute counters.
		Samples map[string][]int64 `json:"samples"`
		// Optional flat list form for agents that prefer arrays.
		Entries []struct {
			Email    string `json:"email"`
			Uplink   int64  `json:"up"`
			Downlink int64  `json:"down"`
		} `json:"entries"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid traffic payload"})
		return
	}
	now := time.Now()
	accepted, dropped, rejected := 0, 0, 0
	enqueue := func(email string, up, down int64) {
		email = strings.TrimSpace(email)
		// Agent counters are absolute byte counters. Negative values are never
		// valid and must not enter the accounting pipeline.
		if email == "" || up < 0 || down < 0 {
			rejected++
			return
		}
		if up == 0 && down == 0 {
			return
		}
		if syncbus.Default.EnqueueTraffic(syncbus.TrafficDelta{
			NodeID: n.ID, Email: email, Uplink: up, Downlink: down, ReportedAt: now,
		}) {
			accepted++
		} else {
			dropped++
		}
	}
	for email, pair := range r.Samples {
		var up, down int64
		if len(pair) > 0 {
			up = pair[0]
		}
		if len(pair) > 1 {
			down = pair[1]
		}
		enqueue(email, up, down)
	}
	for _, e := range r.Entries {
		enqueue(e.Email, e.Uplink, e.Downlink)
	}
	// Cap acknowledged volume so a misbehaving agent cannot force huge responses.
	if accepted > 5000 {
		accepted = 5000
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"accepted": accepted, "dropped": dropped, "rejected": rejected, "queue": syncbus.Default.Stats(),
	}})
}

// AgentPoll handles the AgentPoll API operation.
func (h *PlatformHandler) AgentPoll(c *gin.Context) {
	n, ok := agentAuth(h, c)
	if !ok {
		return
	}
	now := time.Now()
	leaseUntil := now.Add(10 * time.Minute)
	claimed := make([]models.PlatformJob, 0, 10)
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		// A crashed Agent can leave a job in running forever. Requeue only jobs
		// whose lease expired; this makes the queue self-healing without allowing
		// two healthy agents for the same node to execute the same job.
		var expired []models.PlatformJob
		if err := tx.Where("target = ? AND status = ? AND lease_until IS NOT NULL AND lease_until < ? AND attempts >= ?", fmt.Sprint(n.ID), "running", now, 3).Find(&expired).Error; err != nil {
			return err
		}
		for _, job := range expired {
			if err := tx.Model(&models.PlatformJob{}).Where("id = ? AND status = ?", job.ID, "running").Updates(map[string]any{"status": "failed", "error": "agent job lease expired after maximum attempts", "finished_at": now, "lease_until": nil, "lease_token": "", "progress": 0}).Error; err != nil {
				return err
			}
			_ = tx.Model(&models.Deployment{}).Where("job_id = ?", job.ID).Updates(map[string]any{"status": "failed", "error": "agent job lease expired after maximum attempts", "progress": 0}).Error
			if job.OperationID != "" {
				_ = tx.Model(&models.OperationRecord{}).Where("operation_id = ?", job.OperationID).Updates(map[string]any{"status": "failed", "phase": "lease-expired", "error": "agent job lease expired after maximum attempts", "progress": 0, "finished_at": now}).Error
			}
		}
		if err := tx.Model(&models.PlatformJob{}).Where("target = ? AND status = ? AND lease_until IS NOT NULL AND lease_until < ? AND attempts < ?", fmt.Sprint(n.ID), "running", now, 3).Updates(map[string]any{"status": "queued", "started_at": nil, "lease_until": nil, "lease_token": ""}).Error; err != nil {
			return err
		}
		var jobs []models.PlatformJob
		if err := tx.Where("target = ? AND status = ?", fmt.Sprint(n.ID), "queued").Order("id asc").Limit(10).Find(&jobs).Error; err != nil {
			return err
		}
		for _, job := range jobs {
			leaseToken := uuid.NewString()
			res := tx.Model(&models.PlatformJob{}).Where("id = ? AND target = ? AND status = ?", job.ID, fmt.Sprint(n.ID), "queued").Updates(map[string]any{"status": "running", "started_at": now, "lease_until": leaseUntil, "lease_token": leaseToken, "progress": 1, "attempts": gorm.Expr("attempts + 1")})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				job.Status = "running"
				job.Progress = 1
				job.StartedAt = &now
				job.LeaseUntil = &leaseUntil
				job.LeaseToken = leaseToken
				job.Attempts++
				if job.OperationID != "" {
					_ = tx.Model(&models.OperationRecord{}).Where("operation_id = ?", job.OperationID).Updates(map[string]any{"status": "running", "phase": "executing", "progress": 1, "started_at": now}).Error
				}
				claimed = append(claimed, job)
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": claimed, "serverTime": now, "pollAfter": 5})
}

// AgentJobResult handles the AgentJobResult API operation.
func (h *PlatformHandler) AgentJobResult(c *gin.Context) {
	n, ok := agentAuth(h, c)
	if !ok {
		return
	}
	var r struct {
		LeaseToken string `json:"leaseToken"`
		Status     string `json:"status"`
		Progress   int    `json:"progress"`
		Result     string `json:"result"`
		Error      string `json:"error"`
	}
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.LeaseToken) == "" || r.Status == "" {
		c.JSON(400, gin.H{"success": false, "msg": "leaseToken and status required"})
		return
	}
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	if !map[string]bool{"running": true, "done": true, "failed": true}[r.Status] {
		c.JSON(400, gin.H{"success": false, "msg": "invalid job status"})
		return
	}
	if r.Progress < 0 {
		r.Progress = 0
	}
	if r.Progress > 100 {
		r.Progress = 100
	}
	var job models.PlatformJob
	if h.S.DB.First(&job, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "job not found"})
		return
	}
	if job.Target != fmt.Sprint(n.ID) {
		c.JSON(403, gin.H{"success": false, "msg": "job belongs to another node"})
		return
	}
	if job.LeaseToken == "" || !hmac.Equal([]byte(job.LeaseToken), []byte(strings.TrimSpace(r.LeaseToken))) {
		c.JSON(409, gin.H{"success": false, "msg": "job lease is no longer valid"})
		return
	}
	if job.Status != "running" {
		c.JSON(409, gin.H{"success": false, "msg": "job is no longer active"})
		return
	}
	updates := map[string]any{"status": r.Status, "progress": r.Progress, "result": r.Result, "error": r.Error}
	if r.Status == "running" {
		updates["lease_until"] = time.Now().Add(10 * time.Minute)
	}
	if r.Status == "done" || r.Status == "failed" {
		updates["finished_at"] = time.Now()
		updates["lease_until"] = nil
		updates["lease_token"] = ""
	}
	res := h.S.DB.Model(&models.PlatformJob{}).Where("id = ? AND target = ? AND status = ? AND lease_token = ?", job.ID, fmt.Sprint(n.ID), "running", strings.TrimSpace(r.LeaseToken)).Updates(updates)
	if res.Error != nil {
		c.JSON(500, gin.H{"success": false, "msg": res.Error.Error()})
		return
	}
	if res.RowsAffected != 1 {
		c.JSON(409, gin.H{"success": false, "msg": "job lease expired or was replaced"})
		return
	}
	if job.OperationID != "" {
		op := map[string]any{"progress": r.Progress, "result": r.Result}
		if r.Status == "running" {
			op["status"] = "running"
			op["phase"] = "executing"
		}
		if r.Status == "done" {
			op["status"] = "succeeded"
			op["phase"] = "verified"
			op["finished_at"] = time.Now()
		}
		if r.Status == "failed" {
			op["status"] = "failed"
			op["phase"] = "rollback"
			op["error"] = r.Error
			op["finished_at"] = time.Now()
		}
		_ = h.S.DB.Model(&models.OperationRecord{}).Where("operation_id = ?", job.OperationID).Updates(op).Error
	}
	var d models.Deployment
	if h.S.DB.Where("job_id = ?", job.ID).First(&d).Error == nil {
		if r.Status == "done" {
			d.Status = "succeeded"
			d.Error = ""
		} else if r.Status == "failed" {
			d.Status = "failed"
			d.Error = r.Error
		}
		d.Progress = r.Progress
		_ = h.S.DB.Save(&d).Error
	}
	c.JSON(200, gin.H{"success": true})
}

// SecuritySummary handles the SecuritySummary API operation.
func (h *PlatformHandler) SecuritySummary(c *gin.Context) {
	var users, keys, audit, devices int64
	h.S.DB.Model(&models.User{}).Count(&users)
	h.S.DB.Model(&models.APIKey{}).Where("enabled = ?", true).Count(&keys)
	h.S.DB.Model(&models.AuditLog{}).Count(&audit)
	h.S.DB.Model(&models.ClientDevice{}).Count(&devices)
	_, weakJWT := h.S.Config.JWTSecret, h.S.Config.JWTSecret == "change-me-in-production-please"
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"users": users, "activeApiKeys": keys, "auditEvents": audit, "devices": devices, "weakJwtSecret": weakJWT, "httpsRecommended": true}})
}

// PlatformSummary handles the PlatformSummary API operation.
func (h *PlatformHandler) PlatformSummary(c *gin.Context) {
	var nodes, agents, jobs, orders, certs, plugins int64
	h.S.DB.Model(&models.ServerNode{}).Count(&nodes)
	h.S.DB.Model(&models.AgentNode{}).Where("enabled = ?", true).Count(&agents)
	h.S.DB.Model(&models.PlatformJob{}).Where("status IN ?", []string{"queued", "running"}).Count(&jobs)
	h.S.DB.Model(&models.Order{}).Where("status = ?", "pending").Count(&orders)
	h.S.DB.Model(&models.CertificateRecord{}).Count(&certs)
	h.S.DB.Model(&models.PluginRecord{}).Where("enabled = ?", true).Count(&plugins)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"nodes": nodes, "agents": agents, "activeJobs": jobs, "pendingOrders": orders, "certificates": certs, "plugins": plugins, "time": time.Now()}})
}

// AgentManifest handles the AgentManifest API operation.
func (h *PlatformHandler) AgentManifest(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"name": "electruvpn-agent", "version": "1.3.0", "protocol": 1, "features": []string{"heartbeat", "job-poll", "job-result", "xray", "sing-box", "bbr", "telemetry", "identity", "capabilities", "diagnostics", "config-rollback"}, "generatedAt": time.Now()})
}

// AgentBinaryInfo handles the AgentBinaryInfo API operation.
func (h *PlatformHandler) AgentBinaryInfo(c *gin.Context) {
	_, _ = os.Stat(".")
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "Use the bundled cmd/agent build; remote execution is allowlisted by the agent."})
}
