// Package syncbus provides a lightweight in-process state and traffic-delta
// bus for Multi-Node coordination. It intentionally avoids external brokers
// (NATS/Kafka) so Agents keep the existing outbound-HTTPS model while the
// Master aggregates node state and traffic with minimal database load.
package syncbus

import (
	"sync"
	"sync/atomic"
	"time"
)

// NodeSnapshot is the latest observed health of one Agent/edge node.
type NodeSnapshot struct {
	NodeID           uint
	AgentID          string
	Health           string
	CPUPercent       float64
	MemoryBytes      int64
	MemoryTotalBytes int64
	DiskBytes        int64
	DiskTotalBytes   int64
	RxBytes          int64
	TxBytes          int64
	RTTMs            float64
	RemoteIP         string
	UpdatedAt        time.Time
}

// TrafficDelta is a compact per-user counter sample from a node Agent.
// Counters are absolute Xray stats values (same semantics as local PollStats).
type TrafficDelta struct {
	NodeID     uint
	Email      string
	Uplink     int64
	Downlink   int64
	ReportedAt time.Time
}

// Bus is a process-wide registry + bounded traffic queue.
type Bus struct {
	nodesMu sync.RWMutex
	nodes   map[uint]NodeSnapshot

	// traffic queue: Agents push; a flusher drains in batches.
	trafficCh chan TrafficDelta

	// metrics (atomic for lock-free observability)
	heartbeats   atomic.Uint64
	deltasQueued atomic.Uint64
	deltasDrop   atomic.Uint64
	flushes      atomic.Uint64

	res *Resilience
}

// Default is the shared bus for the panel process.
var Default = New(8192)

// New creates a bus with a bounded traffic channel (drops oldest policy on overflow
// is handled by non-blocking send + drop counter).
func New(trafficQueue int) *Bus {
	if trafficQueue < 256 {
		trafficQueue = 256
	}
	return &Bus{
		nodes:     make(map[uint]NodeSnapshot, 64),
		trafficCh: make(chan TrafficDelta, trafficQueue),
	}
}

// PutNode stores/replaces the latest node snapshot (O(1), no DB).
func (b *Bus) PutNode(s NodeSnapshot) {
	if s.NodeID == 0 {
		return
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now()
	}
	b.nodesMu.Lock()
	b.nodes[s.NodeID] = s
	b.nodesMu.Unlock()
	b.heartbeats.Add(1)
}

// GetNode returns a snapshot if present and fresher than maxAge (0 = any).
func (b *Bus) GetNode(id uint, maxAge time.Duration) (NodeSnapshot, bool) {
	b.nodesMu.RLock()
	s, ok := b.nodes[id]
	b.nodesMu.RUnlock()
	if !ok {
		return NodeSnapshot{}, false
	}
	if maxAge > 0 && time.Since(s.UpdatedAt) > maxAge {
		return s, false
	}
	return s, true
}

// ListNodes returns a copy of all snapshots (for dashboards / routing hints).
func (b *Bus) ListNodes() []NodeSnapshot {
	b.nodesMu.RLock()
	defer b.nodesMu.RUnlock()
	out := make([]NodeSnapshot, 0, len(b.nodes))
	for _, s := range b.nodes {
		out = append(out, s)
	}
	return out
}

// HealthyNodeIDs returns node IDs with healthy state updated within maxAge.
func (b *Bus) HealthyNodeIDs(maxAge time.Duration) map[uint]struct{} {
	b.nodesMu.RLock()
	defer b.nodesMu.RUnlock()
	out := make(map[uint]struct{}, len(b.nodes))
	now := time.Now()
	for id, s := range b.nodes {
		if s.Health != "healthy" {
			continue
		}
		if maxAge > 0 && now.Sub(s.UpdatedAt) > maxAge {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

// EnqueueTraffic non-blocking enqueue. Returns false if the queue is full
// (caller should treat as back-pressure; Master still accepts heartbeat).
func (b *Bus) EnqueueTraffic(d TrafficDelta) bool {
	if d.Email == "" {
		return false
	}
	if d.ReportedAt.IsZero() {
		d.ReportedAt = time.Now()
	}
	select {
	case b.trafficCh <- d:
		b.deltasQueued.Add(1)
		return true
	default:
		b.deltasDrop.Add(1)
		return false
	}
}

// DrainTraffic pulls up to max items without blocking longer than wait.
func (b *Bus) DrainTraffic(max int, wait time.Duration) []TrafficDelta {
	if max < 1 {
		max = 1
	}
	out := make([]TrafficDelta, 0, max)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for len(out) < max {
		select {
		case d := <-b.trafficCh:
			out = append(out, d)
		case <-timer.C:
			return out
		default:
			if len(out) > 0 {
				return out
			}
			// block once for wait, then return whatever we got
			select {
			case d := <-b.trafficCh:
				out = append(out, d)
				timer.Reset(wait)
			case <-timer.C:
				return out
			}
		}
	}
	return out
}

// Stats returns lightweight counters for metrics/debug.
func (b *Bus) Stats() map[string]uint64 {
	b.nodesMu.RLock()
	n := uint64(len(b.nodes))
	b.nodesMu.RUnlock()
	out := map[string]uint64{
		"nodes":         n,
		"heartbeats":    b.heartbeats.Load(),
		"deltasQueued":  b.deltasQueued.Load(),
		"deltasDropped": b.deltasDrop.Load(),
		"flushes":       b.flushes.Load(),
		"queueLen":      uint64(len(b.trafficCh)),
		"queueCap":      uint64(cap(b.trafficCh)),
	}
	for k, v := range b.DLQStats() {
		out[k] = v
	}
	return out
}

// MarkFlush increments the flush counter after a successful DB apply.
func (b *Bus) MarkFlush() { b.flushes.Add(1) }

// PruneStaleNodes drops snapshots older than maxAge (call periodically).
func (b *Bus) PruneStaleNodes(maxAge time.Duration) int {
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	b.nodesMu.Lock()
	for id, s := range b.nodes {
		if s.UpdatedAt.Before(cutoff) {
			delete(b.nodes, id)
			removed++
		}
	}
	b.nodesMu.Unlock()
	return removed
}
