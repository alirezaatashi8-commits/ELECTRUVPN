package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/automation"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PriorityHandler struct{ S *core.Server }

// NewPriorityHandler creates or exposes the NewPriorityHandler handler.
func NewPriorityHandler(s *core.Server) *PriorityHandler { return &PriorityHandler{S: s} }

func actor(c *gin.Context) string {
	if v, ok := c.Get("claims"); ok {
		if cl, ok := v.(*auth.Claims); ok {
			return cl.Username
		}
	}
	return "system"
}

// Operations handles the Operations API operation.
func (h *PriorityHandler) Operations(c *gin.Context) {
	var rows []models.OperationRecord
	q := h.S.DB.Order("id desc").Limit(200)
	if s := strings.TrimSpace(c.Query("status")); s != "" {
		q = q.Where("status = ?", s)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Operation handles the Operation API operation.
func (h *PriorityHandler) Operation(c *gin.Context) {
	var row models.OperationRecord
	if h.S.DB.Where("operation_id = ?", c.Param("id")).First(&row).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "operation not found"})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// CreateOperation handles the CreateOperation API operation.
func (h *PriorityHandler) CreateOperation(c *gin.Context) {
	var r struct {
		Type, Target, Payload, IdempotencyKey string
		RollbackOnFail                        *bool
	}
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Type) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "type required"})
		return
	}
	key := strings.TrimSpace(r.IdempotencyKey)
	if key != "" {
		var old models.OperationRecord
		if h.S.DB.Where("idempotency_key = ?", key).First(&old).Error == nil {
			c.JSON(200, gin.H{"success": true, "obj": old, "idempotent": true})
			return
		}
	}
	rollback := true
	if r.RollbackOnFail != nil {
		rollback = *r.RollbackOnFail
	}
	id := uuid.NewString()
	if key == "" {
		key = "__none__" + id
	}
	row := models.OperationRecord{OperationID: id, IdempotencyKey: key, Type: strings.TrimSpace(r.Type), Target: strings.TrimSpace(r.Target), Actor: actor(c), Phase: "queued", Status: "queued", Progress: 0, RollbackOnFail: rollback, Payload: r.Payload}
	if err := h.S.DB.Create(&row).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "operation.create", id, gin.H{"type": row.Type, "target": row.Target})
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// CancelOperation handles the CancelOperation API operation.
func (h *PriorityHandler) CancelOperation(c *gin.Context) {
	var row models.OperationRecord
	if h.S.DB.Where("operation_id = ?", c.Param("id")).First(&row).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "operation not found"})
		return
	}
	if row.Status == "succeeded" || row.Status == "failed" {
		c.JSON(409, gin.H{"success": false, "msg": "operation already finished"})
		return
	}
	now := time.Now()
	row.Status = "cancelled"
	row.Phase = "cancelled"
	row.FinishedAt = &now
	row.UpdatedAt = now
	if err := h.S.DB.Save(&row).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "operation.cancel", row.OperationID, nil)
	c.JSON(200, gin.H{"success": true, "obj": row})
}

// NodeGroups handles the NodeGroups API operation.
func (h *PriorityHandler) NodeGroups(c *gin.Context) {
	var groups []models.NodeGroup
	if err := h.S.DB.Order("name asc").Find(&groups).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	type out struct {
		models.NodeGroup
		Members []models.NodeGroupMember `json:"members"`
	}
	result := make([]out, 0, len(groups))
	for _, g := range groups {
		var m []models.NodeGroupMember
		_ = h.S.DB.Where("group_id = ?", g.ID).Order("priority asc").Find(&m).Error
		result = append(result, out{g, m})
	}
	c.JSON(200, gin.H{"success": true, "obj": result})
}

// CreateNodeGroup handles the CreateNodeGroup API operation.
func (h *PriorityHandler) CreateNodeGroup(c *gin.Context) {
	var g models.NodeGroup
	if c.ShouldBindJSON(&g) != nil || strings.TrimSpace(g.Name) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name required"})
		return
	}
	if g.Strategy == "" {
		g.Strategy = "health"
	}
	if g.Strategy != "health" && g.Strategy != "latency" && g.Strategy != "capacity" && g.Strategy != "weighted" && g.Strategy != "least_load" && g.Strategy != "priority" {
		c.JSON(400, gin.H{"success": false, "msg": "invalid strategy"})
		return
	}
	if err := h.S.DB.Create(&g).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "nodegroup.create", fmt.Sprint(g.ID), g.Name)
	c.JSON(200, gin.H{"success": true, "obj": g})
}

// PreviewNodeSelection ranks nodes for a group (or all nodes) using the same
// logic as smart subscription delivery (failover + load balancing).
func (h *PriorityHandler) PreviewNodeSelection(c *gin.Context) {
	gid, _ := parseUint(c.DefaultQuery("groupId", "0"))
	strategy := strings.TrimSpace(c.DefaultQuery("strategy", "health"))
	filter := strings.TrimSpace(c.DefaultQuery("healthFilter", "healthy"))
	failover := !strings.EqualFold(c.Query("failover"), "0")
	cands, err := routing.SelectNodes(h.S.DB, routing.SelectOptions{
		GroupID: gid, Strategy: strategy, Failover: failover, HealthFilter: filter, MaxNodes: 32,
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(cands))
	for _, x := range cands {
		items = append(items, gin.H{
			"nodeId": x.NodeID, "name": x.Name, "address": x.Address, "port": x.Port,
			"priority": x.Priority, "weight": x.Weight, "score": x.Score,
			"healthy": x.Healthy, "latencyMs": x.Latency, "load": x.Load,
			"host": routing.HostForCandidate(x),
		})
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"strategy": strategy, "failover": failover, "healthFilter": filter, "nodes": items}})
}

// AddNodeGroupMember handles the AddNodeGroupMember API operation.
func (h *PriorityHandler) AddNodeGroupMember(c *gin.Context) {
	gid, err := parseUint(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid group"})
		return
	}
	var r struct {
		NodeID           uint
		Priority, Weight *int
	}
	if c.ShouldBindJSON(&r) != nil || r.NodeID == 0 {
		c.JSON(400, gin.H{"success": false, "msg": "nodeId required"})
		return
	}
	var n models.ServerNode
	if h.S.DB.First(&n, r.NodeID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	p, w := 100, 100
	if r.Priority != nil {
		p = *r.Priority
	}
	if r.Weight != nil {
		w = *r.Weight
	}
	if p < 0 || w < 1 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid priority/weight"})
		return
	}
	m := models.NodeGroupMember{GroupID: gid, NodeID: r.NodeID, Priority: p, Weight: w, Enabled: true}
	if err := h.S.DB.Save(&m).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "nodegroup.member.add", fmt.Sprintf("%d/%d", gid, r.NodeID), nil)
	c.JSON(200, gin.H{"success": true, "obj": m})
}

// SetNodeDrain handles the SetNodeDrain API operation.
func (h *PriorityHandler) SetNodeDrain(c *gin.Context) {
	var r struct {
		Drain bool `json:"drain"`
	}
	if c.ShouldBindJSON(&r) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "drain required"})
		return
	}
	var n models.ServerNode
	if h.S.DB.First(&n, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	state := "active"
	if r.Drain {
		state = "draining"
	}
	if err := h.S.DB.Model(&n).Update("drain_state", state).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "node.drain", fmt.Sprint(n.ID), state)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"nodeId": n.ID, "drainState": state}})
}

// CertificateHistory handles the CertificateHistory API operation.
func (h *PriorityHandler) CertificateHistory(c *gin.Context) {
	var rows []models.CertificateRevision
	q := h.S.DB.Order("certificate_id asc, version desc")
	if id := strings.TrimSpace(c.Query("certificateId")); id != "" {
		q = q.Where("certificate_id = ?", id)
	}
	if err := q.Limit(500).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Domains handles the Domains API operation.
func (h *PriorityHandler) Domains(c *gin.Context) {
	var rows []models.DomainRecord
	if err := h.S.DB.Order("priority asc,id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateDomain handles the CreateDomain API operation.
func (h *PriorityHandler) CreateDomain(c *gin.Context) {
	var d models.DomainRecord
	if c.ShouldBindJSON(&d) != nil || strings.TrimSpace(d.Domain) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "domain required"})
		return
	}
	d.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d.Domain), "."))
	if strings.Contains(d.Domain, "/") {
		c.JSON(400, gin.H{"success": false, "msg": "invalid domain"})
		return
	}
	if d.Provider == "" {
		d.Provider = "manual"
	}
	if d.Challenge == "" {
		d.Challenge = "http-01"
	}
	if d.Priority == 0 {
		d.Priority = 100
	}
	if d.HealthStatus == "" {
		d.HealthStatus = "unknown"
	}
	if err := h.S.DB.Create(&d).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "domain.create", fmt.Sprint(d.ID), d.Domain)
	c.JSON(200, gin.H{"success": true, "obj": d})
}

// CheckDomain handles the CheckDomain API operation.
func (h *PriorityHandler) CheckDomain(c *gin.Context) {
	var d models.DomainRecord
	if h.S.DB.First(&d, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "domain not found"})
		return
	}
	status, lat, errMsg, expires := automation.CheckDomain(d.Domain)
	now := time.Now()
	d.HealthStatus = status
	d.LastLatencyMs = lat
	d.LastError = errMsg
	d.LastHealthAt = &now
	if status == "healthy" {
		d.HealthFailures = 0
	} else {
		d.HealthFailures++
	}
	_ = h.S.DB.Save(&d).Error
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"status": status, "latencyMs": lat, "error": errMsg, "certificateExpiresAt": expires}})
}

// FailoverDomain handles the FailoverDomain API operation.
func (h *PriorityHandler) FailoverDomain(c *gin.Context) {
	var d models.DomainRecord
	if h.S.DB.First(&d, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "domain not found"})
		return
	}
	var next models.DomainRecord
	if h.S.DB.Where("enabled = ? AND id <> ? AND health_status = ?", true, d.ID, "healthy").Order("priority asc,id asc").First(&next).Error != nil {
		c.JSON(409, gin.H{"success": false, "msg": "no healthy failover domain available"})
		return
	}
	now := time.Now()
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.DomainRecord{}).Where("primary = ?", true).Update("primary", false).Error; err != nil {
			return err
		}
		return tx.Model(&models.DomainRecord{}).Where("id = ?", next.ID).Updates(map[string]any{"primary": true, "last_failover_at": now}).Error
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "domain.failover", fmt.Sprint(next.ID), gin.H{"from": d.Domain, "to": next.Domain})
	c.JSON(200, gin.H{"success": true, "obj": next})
}

// AgentDiagnostics handles the AgentDiagnostics API operation.
func (h *PriorityHandler) AgentDiagnostics(c *gin.Context) {
	nodeID, err := parseUint(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid node"})
		return
	}
	var n models.ServerNode
	if h.S.DB.First(&n, nodeID).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "node not found"})
		return
	}
	var job models.PlatformJob
	opID := uuid.NewString()
	payload, _ := json.Marshal(gin.H{"operationId": opID})
	job = models.PlatformJob{Kind: "agent-diagnostics", Target: fmt.Sprint(nodeID), Payload: string(payload), Status: "queued", Progress: 0, OperationID: opID}
	op := models.OperationRecord{OperationID: opID, Type: "agent.diagnostics", Target: fmt.Sprint(nodeID), Actor: actor(c), Status: "queued", Phase: "queued", RollbackOnFail: false, Payload: string(payload)}
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		return tx.Create(&job).Error
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "agent.diagnostics", fmt.Sprint(nodeID), opID)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"operationId": opID, "jobId": job.ID, "node": n.Name}})
}

func parseUint(v string) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	return uint(n), err
}
