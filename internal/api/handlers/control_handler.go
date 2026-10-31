package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ControlHandler struct{ S *core.Server }

// NewControlHandler creates or exposes the NewControlHandler handler.
func NewControlHandler(s *core.Server) *ControlHandler { return &ControlHandler{S: s} }

// Performance handles the Performance API operation.
func (h *ControlHandler) Performance(c *gin.Context) {
	var nodes []models.ServerNode
	if err := h.S.DB.Where("enabled = ?", true).Order("id asc").Find(&nodes).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if len(nodes) == 0 {
		c.JSON(200, gin.H{"success": true, "obj": []gin.H{}})
		return
	}

	// Batch-load agents and latest performance rows. The old implementation
	// issued two extra queries per node, which became noticeable with a larger
	// node fabric.
	var agents []models.AgentNode
	_ = h.S.DB.Where("node_id IN ?", nodeIDs(nodes)).Find(&agents).Error
	agentByNode := make(map[uint]models.AgentNode, len(agents))
	for _, a := range agents {
		agentByNode[a.NodeID] = a
	}

	latestByNode := make(map[uint]models.NodePerformanceSnapshot, len(nodes))
	var snaps []models.NodePerformanceSnapshot
	if h.S.DB.Raw(`SELECT p.* FROM node_performance_snapshots p JOIN (SELECT node_id, MAX(created_at) AS created_at FROM node_performance_snapshots WHERE node_id IN ? GROUP BY node_id) x ON x.node_id = p.node_id AND x.created_at = p.created_at`, nodeIDs(nodes)).Scan(&snaps).Error == nil {
		for _, snap := range snaps {
			latestByNode[snap.NodeID] = snap
		}
	}

	out := make([]gin.H, 0, len(nodes))
	for _, n := range nodes {
		a := agentByNode[n.ID]
		healthy := a.LastSeenAt != nil && time.Since(*a.LastSeenAt) < 30*time.Second && a.LastError == ""
		latest := latestByNode[n.ID]
		out = append(out, gin.H{"node": n, "agent": a, "healthy": healthy, "performance": latest})
	}
	c.JSON(200, gin.H{"success": true, "obj": out})
}

func nodeIDs(nodes []models.ServerNode) []uint {
	ids := make([]uint, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

// PerformanceHistory handles the PerformanceHistory API operation.
func (h *ControlHandler) PerformanceHistory(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Query("nodeId"), 10, 64)
	limit := 120
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	q := h.S.DB.Order("id desc").Limit(limit)
	if id > 0 {
		q = q.Where("node_id = ?", id)
	}
	var rows []models.NodePerformanceSnapshot
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Routing handles the Routing API operation.
func (h *ControlHandler) Routing(c *gin.Context) {
	var rows []models.RoutingPolicy
	if err := h.S.DB.Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateRouting handles the CreateRouting API operation.
func (h *ControlHandler) CreateRouting(c *gin.Context) {
	var r struct {
		Name, Strategy string
		NodeIDs        []uint
		Failover       *bool
		Enabled        *bool
	}
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name required"})
		return
	}
	strategy := strings.ToLower(strings.TrimSpace(r.Strategy))
	if strategy == "" {
		strategy = "latency"
	}
	allowed := map[string]bool{"latency": true, "load": true, "capacity": true, "weighted": true}
	if !allowed[strategy] {
		c.JSON(400, gin.H{"success": false, "msg": "invalid strategy"})
		return
	}
	b, _ := json.Marshal(r.NodeIDs)
	fail := true
	en := true
	if r.Failover != nil {
		fail = *r.Failover
	}
	if r.Enabled != nil {
		en = *r.Enabled
	}
	p := models.RoutingPolicy{Name: r.Name, Strategy: strategy, NodeIDs: string(b), Failover: fail, Enabled: en}
	if err := h.S.DB.Create(&p).Error; err != nil {
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "routing.create", fmt.Sprint(p.ID), p)
	c.JSON(200, gin.H{"success": true, "obj": p})
}

// DeleteRouting handles the DeleteRouting API operation.
func (h *ControlHandler) DeleteRouting(c *gin.Context) {
	h.S.DB.Delete(&models.RoutingPolicy{}, c.Param("id"))
	platformAudit(h.S, c, "routing.delete", c.Param("id"), nil)
	c.JSON(200, gin.H{"success": true})
}

// SelectNode is deterministic and transparent: it only scores current health,
// latency and load. It does not infer a user's location or preference.
func (h *ControlHandler) SelectNode(c *gin.Context) {
	strategy := strings.ToLower(c.DefaultQuery("strategy", "latency"))
	var nodes []models.ServerNode
	if err := h.S.DB.Where("enabled = ? AND drain_state <> ?", true, "draining").Find(&nodes).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if gid := strings.TrimSpace(c.Query("groupId")); gid != "" {
		var members []models.NodeGroupMember
		if h.S.DB.Where("group_id = ? AND enabled = ?", gid, true).Find(&members).Error == nil {
			allowed := make(map[uint]bool, len(members))
			for _, m := range members {
				allowed[m.NodeID] = true
			}
			filtered := nodes[:0]
			for _, n := range nodes {
				if allowed[n.ID] {
					filtered = append(filtered, n)
				}
			}
			nodes = filtered
		}
	}
	ids := nodeIDs(nodes)
	var agents []models.AgentNode
	_ = h.S.DB.Where("node_id IN ?", ids).Find(&agents).Error
	agentByNode := make(map[uint]models.AgentNode, len(agents))
	for _, a := range agents {
		agentByNode[a.NodeID] = a
	}
	latestByNode := make(map[uint]models.NodePerformanceSnapshot, len(nodes))
	var snaps []models.NodePerformanceSnapshot
	if h.S.DB.Raw(`SELECT p.* FROM node_performance_snapshots p JOIN (SELECT node_id, MAX(created_at) AS created_at FROM node_performance_snapshots WHERE node_id IN ? GROUP BY node_id) x ON x.node_id = p.node_id AND x.created_at = p.created_at`, ids).Scan(&snaps).Error == nil {
		for _, snap := range snaps {
			latestByNode[snap.NodeID] = snap
		}
	}
	type candidate struct {
		Node  models.ServerNode
		Agent models.AgentNode
		Snap  models.NodePerformanceSnapshot
		Score float64
	}
	candidates := make([]candidate, 0, len(nodes))
	for _, n := range nodes {
		a, ok := agentByNode[n.ID]
		if !ok || a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > 45*time.Second || a.LastError != "" {
			continue
		}
		s := latestByNode[n.ID]
		score := 0.0
		switch strategy {
		case "load":
			score = 100 - math.Min(100, a.CPUPercent)
			score -= math.Min(50, s.PacketLoss*10)
		case "capacity":
			score = 100 - math.Min(100, a.CPUPercent)
			if s.MemoryBytes > 0 && a.MemoryBytes > 0 {
				used := float64(a.MemoryBytes) / float64(s.MemoryBytes) * 100
				score += math.Max(0, 100-math.Min(100, used))
			} else {
				score += 25
			}
		case "weighted":
			score = (100-math.Min(100, a.CPUPercent))*0.45 + math.Max(0, 100-math.Min(100, s.RTTMs))*0.35 + math.Max(0, 100-math.Min(100, s.PacketLoss*10))*0.20
		default:
			score = 1000 - math.Max(0, s.RTTMs*10) - math.Min(500, s.PacketLoss*100) - math.Min(100, a.CPUPercent)
		}
		if !s.CreatedAt.IsZero() && !s.Healthy {
			score -= 250
		}
		candidates = append(candidates, candidate{n, a, s, score})
	}
	if len(candidates) == 0 {
		c.JSON(503, gin.H{"success": false, "msg": "no healthy node available"})
		return
	}
	best := candidates[0]
	for _, x := range candidates[1:] {
		if x.Score > best.Score {
			best = x
		}
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"node": best.Node, "agent": best.Agent, "performance": best.Snap, "strategy": strategy, "score": best.Score}})
}

// Deployments handles the Deployments API operation.
func (h *ControlHandler) Deployments(c *gin.Context) {
	var rows []models.Deployment
	q := h.S.DB.Order("id desc").Limit(200)
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// CreateDeployment handles the CreateDeployment API operation.
func (h *ControlHandler) CreateDeployment(c *gin.Context) {
	var r struct {
		Name, Target, Kind, VersionTo string
		RollbackOnFail                *bool
		Payload                       string
	}
	if c.ShouldBindJSON(&r) != nil || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Target) == "" {
		c.JSON(400, gin.H{"success": false, "msg": "name and target required"})
		return
	}
	rb := true
	if r.RollbackOnFail != nil {
		rb = *r.RollbackOnFail
	}
	kind := strings.TrimSpace(r.Kind)
	if kind == "" {
		kind = "config-validate"
	}
	allowed := map[string]bool{"config-validate": true, "xray-restart": true, "xray-status": true, "singbox-restart": true, "singbox-status": true, "bbr-apply": true, "bbr-status": true, "network-status": true, "xray-config-apply": true, "singbox-config-apply": true, "xray-config-rollback": true, "singbox-config-rollback": true}
	if !allowed[kind] {
		c.JSON(400, gin.H{"success": false, "msg": "deployment kind not allowlisted"})
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
	var d models.Deployment
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		job := models.PlatformJob{Kind: kind, Target: strings.TrimSpace(r.Target), Payload: r.Payload, Status: "queued", Progress: 0}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		d = models.Deployment{Name: strings.TrimSpace(r.Name), Target: strings.TrimSpace(r.Target), Kind: kind, Status: "queued", VersionTo: strings.TrimSpace(r.VersionTo), Progress: 0, RollbackOnFail: rb, JobID: job.ID}
		return tx.Create(&d).Error
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "deployment.create", fmt.Sprint(d.ID), d)
	c.JSON(200, gin.H{"success": true, "obj": d})
}

// RollbackDeployment handles the RollbackDeployment API operation.
func (h *ControlHandler) RollbackDeployment(c *gin.Context) {
	var d models.Deployment
	if h.S.DB.First(&d, c.Param("id")).Error != nil {
		c.JSON(404, gin.H{"success": false, "msg": "deployment not found"})
		return
	}
	if d.Status != "succeeded" {
		c.JSON(409, gin.H{"success": false, "msg": "only a successful deployment can be rolled back explicitly"})
		return
	}
	if strings.TrimSpace(d.ArtifactHash) == "" {
		c.JSON(409, gin.H{"success": false, "msg": "deployment has no versioned artifact reference"})
		return
	}
	var kind string
	switch d.Kind {
	case "xray-config-apply":
		kind = "xray-config-rollback"
	case "singbox-config-apply":
		kind = "singbox-config-rollback"
	default:
		c.JSON(409, gin.H{"success": false, "msg": "rollback is available only for versioned Xray/sing-box configuration deployments"})
		return
	}
	var job models.PlatformJob
	var rollback models.Deployment
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		job = models.PlatformJob{Kind: kind, Target: d.Target, Payload: `{}`, Status: "queued", Progress: 0}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		rollback = models.Deployment{Name: "Rollback: " + d.Name, Target: d.Target, Kind: kind, Status: "queued", VersionFrom: d.VersionTo, VersionTo: "previous", ArtifactHash: d.ArtifactHash, Progress: 0, RollbackOnFail: true, JobID: job.ID}
		return tx.Create(&rollback).Error
	})
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	platformAudit(h.S, c, "deployment.rollback", fmt.Sprint(d.ID), gin.H{"rollbackDeploymentId": rollback.ID, "jobId": job.ID, "target": d.Target})
	c.JSON(200, gin.H{"success": true, "obj": rollback})
}

// Advisor handles the Advisor API operation.
func (h *ControlHandler) Advisor(c *gin.Context) {
	var nodes []models.ServerNode
	h.S.DB.Where("enabled = ?", true).Find(&nodes)
	items := []gin.H{}
	for _, n := range nodes {
		var a models.AgentNode
		h.S.DB.Where("node_id = ?", n.ID).First(&a)
		var s models.NodePerformanceSnapshot
		h.S.DB.Where("node_id = ?", n.ID).Order("id desc").First(&s)
		rec := []string{}
		if a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > 45*time.Second {
			rec = append(rec, "agent heartbeat is stale")
		}
		if a.CPUPercent > 85 {
			rec = append(rec, "high CPU load")
		}
		if s.PacketLoss > 2 {
			rec = append(rec, "packet loss above 2%")
		}
		if s.RTTMs > 150 {
			rec = append(rec, "high RTT")
		}
		if a.LastError != "" {
			rec = append(rec, "agent reports an error")
		}
		if len(rec) == 0 {
			rec = append(rec, "no immediate issue detected")
		}
		items = append(items, gin.H{"nodeId": n.ID, "node": n.Name, "recommendations": rec})
	}
	c.JSON(200, gin.H{"success": true, "obj": items})
}

func auditUser(c *gin.Context) string {
	if v, ok := c.Get("claims"); ok {
		if cl, ok := v.(*auth.Claims); ok {
			return cl.Username
		}
	}
	return "unknown"
}
