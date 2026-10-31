// Package routing selects healthy nodes for subscription delivery with
// failover and load-aware ordering (2026 multi-node / multi-outbound best practice).
// Strategies: health | latency | least_load | least_connections | weighted | priority | round_robin
package routing

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/syncbus"
	"gorm.io/gorm"
)

// Candidate is a ranked node endpoint for a share/subscription link.
type Candidate struct {
	NodeID   uint    `json:"nodeId"`
	Name     string  `json:"name"`
	Address  string  `json:"address"`
	Port     int     `json:"port"`
	Priority int     `json:"priority"`
	Weight   int     `json:"weight"`
	Score    float64 `json:"score"`
	Healthy  bool    `json:"healthy"`
	Latency  float64 `json:"latency"`
	Load     float64 `json:"load"`
}

// SelectOptions controls selection behaviour.
type SelectOptions struct {
	GroupID      uint
	Strategy     string // health | weighted | latency | least_load | least_connections | priority | round_robin
	Failover     bool
	HealthFilter string // healthy | any | degraded
	MaxNodes     int
}

// SelectNodes returns ordered candidates for subscription link generation.
// Unhealthy / drained nodes are excluded when Failover is true (default).
func SelectNodes(db *gorm.DB, opt SelectOptions) ([]Candidate, error) {
	if opt.Strategy == "" {
		opt.Strategy = "least_load"
	}
	if opt.HealthFilter == "" {
		opt.HealthFilter = "healthy"
	}
	// Failover is the default for multi-node safety.
	if !opt.Failover && opt.HealthFilter == "healthy" {
		opt.Failover = true
	}
	if opt.MaxNodes <= 0 {
		opt.MaxNodes = 32
	}

	var members []models.NodeGroupMember
	q := db.Where("enabled = ?", true)
	if opt.GroupID > 0 {
		q = q.Where("group_id = ?", opt.GroupID)
	}
	if err := q.Order("priority asc, id asc").Find(&members).Error; err != nil {
		return nil, err
	}
	var out []Candidate
	if len(members) == 0 {
		var nodes []models.ServerNode
		if err := db.Where("enabled = ?", true).Find(&nodes).Error; err != nil {
			return nil, err
		}
		out = make([]Candidate, 0, len(nodes))
		for _, n := range nodes {
			if isDraining(n) {
				continue
			}
			c := Candidate{
				NodeID: n.ID, Name: n.Name, Address: n.Address, Port: n.Port,
				Priority: 100, Weight: 100, Healthy: nodeLooksHealthy(db, n), Score: 1,
			}
			if shouldSkipUnhealthy(opt, c) {
				continue
			}
			out = append(out, enrichScore(db, c, opt.Strategy))
		}
	} else {
		out = make([]Candidate, 0, len(members))
		for _, m := range members {
			var n models.ServerNode
			if err := db.First(&n, m.NodeID).Error; err != nil || !n.Enabled {
				continue
			}
			if isDraining(n) {
				continue
			}
			w := m.Weight
			if w <= 0 {
				w = 100
			}
			c := Candidate{
				NodeID: n.ID, Name: n.Name, Address: n.Address, Port: n.Port,
				Priority: m.Priority, Weight: w, Healthy: nodeLooksHealthy(db, n),
			}
			if shouldSkipUnhealthy(opt, c) {
				continue
			}
			out = append(out, enrichScore(db, c, opt.Strategy))
		}
	}

	sortCandidates(out, opt.Strategy)

	// Failover guarantee: if healthy filter removed everyone, return best-effort enabled nodes.
	if len(out) == 0 && opt.Failover {
		var nodes []models.ServerNode
		_ = db.Where("enabled = ?", true).Find(&nodes)
		for _, n := range nodes {
			if isDraining(n) {
				continue
			}
			c := Candidate{
				NodeID: n.ID, Name: n.Name, Address: n.Address, Port: n.Port,
				Priority: 999, Weight: 1, Healthy: false, Score: 0.01,
			}
			out = append(out, enrichScore(db, c, opt.Strategy))
		}
		sortCandidates(out, opt.Strategy)
	}

	if len(out) > opt.MaxNodes {
		out = out[:opt.MaxNodes]
	}
	return out, nil
}

func isDraining(n models.ServerNode) bool {
	d := strings.ToLower(strings.TrimSpace(n.DrainState))
	return d == "drain" || d == "draining" || d == "disabled"
}

func shouldSkipUnhealthy(opt SelectOptions, c Candidate) bool {
	if strings.EqualFold(opt.HealthFilter, "any") {
		return false
	}
	if opt.Failover || strings.EqualFold(opt.HealthFilter, "healthy") {
		return !c.Healthy
	}
	return false
}

func nodeLooksHealthy(db *gorm.DB, n models.ServerNode) bool {
	if !n.Enabled || isDraining(n) {
		return false
	}
	// Explicit health field if model has LastError
	if strings.TrimSpace(n.LastError) != "" {
		return false
	}
	// Prefer the in-memory multi-node bus (updated on every Agent heartbeat)
	// to avoid a DB round-trip on every subscription selection under load.
	if snap, ok := syncbus.Default.GetNode(n.ID, 90*time.Second); ok {
		return snap.Health == "healthy"
	}
	// Agent heartbeat is the strongest signal for remote nodes.
	var agent models.AgentNode
	if err := db.Where("node_id = ?", n.ID).First(&agent).Error; err == nil {
		if agent.LastError != "" {
			return false
		}
		if agent.LastSeenAt == nil {
			return false
		}
		// Stale agent → treat node as down so subscription fails over.
		if time.Since(*agent.LastSeenAt) > 45*time.Second {
			return false
		}
		return true
	}
	// Inventory-only nodes: LastSeen within 5 minutes counts as healthy.
	if n.LastSeenAt != nil {
		return time.Since(*n.LastSeenAt) < 5*time.Minute
	}
	// Local / never-seen enabled nodes without agent: optimistic healthy.
	return true
}

func enrichScore(db *gorm.DB, c Candidate, strategy string) Candidate {
	var snap models.NodePerformanceSnapshot
	if err := db.Where("node_id = ?", c.NodeID).Order("created_at desc").First(&snap).Error; err == nil {
		c.Latency = snap.RTTMs
		// Approximate load from CPU/bandwidth if present
		load := snap.CPUPercent / 100.0
		if snap.MemoryTotalBytes > 0 {
			memLoad := float64(snap.MemoryBytes) / float64(snap.MemoryTotalBytes)
			load = math.Max(load, memLoad)
		}
		if !snap.Healthy {
			load = math.Max(load, 0.95)
		}
		if load < 0 {
			load = 0
		}
		if load > 1 {
			load = 1
		}
		c.Load = load
	}
	switch strings.ToLower(strategy) {
	case "latency", "least_ping":
		// lower latency → higher score
		c.Score = 10000.0 / (1.0 + c.Latency)
	case "least_load", "least_connections":
		// Approximate least-connections via observed load (CPU / memory)
		c.Score = (1.0 - c.Load) * float64(c.Weight)
		if c.Healthy {
			c.Score += 1000
		}
	case "weighted", "priority":
		c.Score = float64(c.Weight) / (1.0 + float64(c.Priority))
		if c.Healthy {
			c.Score += 1000
		}
	case "round_robin", "roundrobin":
		// Neutral score; callers cycle via index. Still prefer healthy.
		c.Score = float64(c.Weight)
		if c.Healthy {
			c.Score += 1000
		}
	default: // health
		c.Score = float64(c.Weight)
		if c.Healthy {
			c.Score += 1000
		}
		c.Score -= c.Load * 100
	}
	return c
}

func sortCandidates(out []Candidate, strategy string) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Healthy != out[j].Healthy {
			return out[i].Healthy
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Priority < out[j].Priority
	})
}

// HostForCandidate prefers node Address when it looks like a host/IP.
func HostForCandidate(c Candidate) string {
	h := strings.TrimSpace(c.Address)
	if h == "" {
		return ""
	}
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	if j := strings.Index(h, "/"); j >= 0 {
		h = h[:j]
	}
	// [ipv6]:port
	if strings.HasPrefix(h, "[") {
		if k := strings.Index(h, "]"); k > 0 {
			return h[:k+1]
		}
		return h
	}
	// host:port (not bare ipv6)
	if i := strings.LastIndex(h, ":"); i > 0 && strings.Count(h, ":") == 1 {
		return h[:i]
	}
	return h
}

// PrimaryHost
// PrimaryHost returns the best healthy host or empty.
func PrimaryHost(db *gorm.DB, groupID uint) string {
	cands, err := SelectNodes(db, SelectOptions{GroupID: groupID, Failover: true, Strategy: "least_load", MaxNodes: 1})
	if err != nil || len(cands) == 0 {
		return ""
	}
	return HostForCandidate(cands[0])
}
