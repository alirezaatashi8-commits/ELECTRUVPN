package core

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/syncbus"
	"github.com/electruvpn/electruvpn/internal/xray"
	"gorm.io/gorm"
)

// SyncTrafficResult summarizes one background (or on-demand) traffic pass.
type SyncTrafficResult struct {
	ClientsTouched int
	Disabled       int
	Extended       int
	NeedRegen      bool
	SkippedStats   bool
	Error          string
}

// SyncTrafficFromXray polls local xray stats and applies accounting.
func (s *Server) SyncTrafficFromXray() SyncTrafficResult {
	out := SyncTrafficResult{}
	if s == nil || s.DB == nil || s.Config == nil {
		out.Error = "server not ready"
		return out
	}
	stats, err := xray.PollStats(s.Config.XrayBinaryPath, s.Config.XrayAPIPort, false)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	xrayRestarted := s.ObserveXrayStatus(s.Xray.Status())
	xray.UpdateLiveSamples(stats, time.Now())
	return s.SyncTrafficWithStats(stats, xrayRestarted)
}

// FlushRemoteTrafficDeltas drains the multi-node syncbus and applies true
// per-node delta accounting.
//
// Each Agent reports absolute Xray counters that are independent per node.
// We therefore keep a durable last-seen absolute (NodeClientTraffic) keyed by
// (node_id, email). For every sample we compute only the positive increment
// (or the full value after a counter reset) and add that increment to the
// global Client.Up / Client.Down. This prevents both double-counting across
// nodes and silent loss when a node restarts its Xray process.
func (s *Server) FlushRemoteTrafficDeltas() SyncTrafficResult {
	out := SyncTrafficResult{}
	if s == nil || s.DB == nil {
		out.Error = "server not ready"
		return out
	}
	deltas := syncbus.Default.DrainTraffic(2000, 50*time.Millisecond)
	if len(deltas) == 0 {
		out.SkippedStats = true
		return out
	}

	// Collapse to the newest absolute sample per (node, email). Reordering
	// within the same flush window is possible; the highest absolute is the
	// safest single representative for that node.
	type key struct {
		node  uint
		email string
	}
	latest := make(map[key]syncbus.TrafficDelta, len(deltas))
	for _, d := range deltas {
		if d.Email == "" || d.NodeID == 0 {
			continue
		}
		k := key{node: d.NodeID, email: d.Email}
		prev, ok := latest[k]
		if !ok || d.Uplink > prev.Uplink || d.Downlink > prev.Downlink || d.ReportedAt.After(prev.ReportedAt) {
			latest[k] = d
		}
	}
	if len(latest) == 0 {
		out.SkippedStats = true
		return out
	}

	// Load all clients we might touch (email → client) in one query.
	emails := make([]string, 0, len(latest))
	seenEmail := make(map[string]struct{}, len(latest))
	for k := range latest {
		if _, ok := seenEmail[k.email]; !ok {
			seenEmail[k.email] = struct{}{}
			emails = append(emails, k.email)
		}
	}
	var clients []models.Client
	if err := s.DB.Select("id,email,enable,total_gb,traffic_limit_bytes,expiry_time,up,down,traffic_base_up,traffic_base_down,auto_extend_gb,auto_extend_days,subscription_status").
		Where("email IN ?", emails).Find(&clients).Error; err != nil {
		out.Error = err.Error()
		// Park everything so we do not lose the samples.
		for _, d := range latest {
			syncbus.Default.PushDeadLetter(d, out.Error, 1)
		}
		return out
	}
	clientByEmail := make(map[string]*models.Client, len(clients))
	for i := range clients {
		clientByEmail[clients[i].Email] = &clients[i]
	}

	// Load existing last-seen counters for these (node, email) pairs.
	nodeIDs := make([]uint, 0, 8)
	nodeSeen := make(map[uint]struct{})
	for k := range latest {
		if _, ok := nodeSeen[k.node]; !ok {
			nodeSeen[k.node] = struct{}{}
			nodeIDs = append(nodeIDs, k.node)
		}
	}
	var lastRows []models.NodeClientTraffic
	_ = s.DB.Where("node_id IN ? AND email IN ?", nodeIDs, emails).Find(&lastRows).Error
	lastMap := make(map[key]models.NodeClientTraffic, len(lastRows))
	for _, row := range lastRows {
		lastMap[key{node: row.NodeID, email: row.Email}] = row
	}

	s.trafficSyncMu.Lock()
	defer s.trafficSyncMu.Unlock()

	tx := s.DB.Begin()
	if tx.Error != nil {
		out.Error = tx.Error.Error()
		for _, d := range latest {
			syncbus.Default.PushDeadLetter(d, out.Error, 1)
		}
		return out
	}

	now := time.Now()
	snapshots := make([]models.UsageSnapshot, 0, 32)
	disableIDs := make([]uint, 0, 8)
	touched := make(map[uint]struct{})

	for k, d := range latest {
		cl := clientByEmail[k.email]
		if cl == nil {
			// Unknown email — still advance the last-seen so we do not
			// later attribute a huge jump if the client is created later.
			// We simply ignore the contribution.
			continue
		}

		prev, hasPrev := lastMap[k]
		var contribUp, contribDown int64
		if !hasPrev {
			// First sample from this node for this client: establish the baseline
			// without billing historical traffic that occurred before the agent
			// was connected. Subsequent samples will only add positive deltas.
			contribUp = 0
			contribDown = 0
		} else if d.Uplink < prev.LastUp || d.Downlink < prev.LastDown {
			// Counter reset on the node (Xray restart). Treat the new absolute
			// as a fresh contribution.
			contribUp = d.Uplink
			contribDown = d.Downlink
		} else {
			contribUp = d.Uplink - prev.LastUp
			contribDown = d.Downlink - prev.LastDown
		}
		if contribUp < 0 {
			contribUp = 0
		}
		if contribDown < 0 {
			contribDown = 0
		}

		// Always persist the new absolute so subsequent samples are correct
		// even when contribution is zero.
		row := models.NodeClientTraffic{
			NodeID: k.node, Email: k.email,
			LastUp: d.Uplink, LastDown: d.Downlink, UpdatedAt: now,
		}
		if hasPrev {
			row.ID = prev.ID
			if err := tx.Model(&models.NodeClientTraffic{}).Where("id = ?", prev.ID).
				Updates(map[string]any{"last_up": d.Uplink, "last_down": d.Downlink, "updated_at": now}).Error; err != nil {
				tx.Rollback()
				out.Error = err.Error()
				for _, dd := range latest {
					syncbus.Default.PushDeadLetter(dd, out.Error, 1)
				}
				return out
			}
		} else {
			if err := tx.Create(&row).Error; err != nil {
				// Race: another flush may have inserted; retry as update.
				var existing models.NodeClientTraffic
				if e2 := tx.Where("node_id = ? AND email = ?", k.node, k.email).First(&existing).Error; e2 == nil {
					_ = tx.Model(&existing).Updates(map[string]any{"last_up": d.Uplink, "last_down": d.Downlink, "updated_at": now}).Error
				} else {
					tx.Rollback()
					out.Error = err.Error()
					for _, dd := range latest {
						syncbus.Default.PushDeadLetter(dd, out.Error, 1)
					}
					return out
				}
			}
		}

		if contribUp == 0 && contribDown == 0 {
			continue
		}

		newUp := cl.Up + contribUp
		newDown := cl.Down + contribDown
		if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).
			Updates(map[string]any{"up": newUp, "down": newDown}).Error; err != nil {
			tx.Rollback()
			out.Error = err.Error()
			for _, dd := range latest {
				syncbus.Default.PushDeadLetter(dd, out.Error, 1)
			}
			return out
		}
		cl.Up, cl.Down = newUp, newDown
		out.ClientsTouched++
		touched[cl.ID] = struct{}{}

		// Snapshot at most once per hour (same policy as local path).
		snapshots = append(snapshots, models.UsageSnapshot{ClientID: cl.ID, Up: cl.Up, Down: cl.Down})

		// Quota / expiry checks (same rules as local SyncTrafficWithStats).
		expired, overQuota := cl.IsExpired(), cl.IsOverQuota()
		if expired || overQuota {
			renewBoth := cl.AutoExtendGB > 0 && cl.AutoExtendDays > 0
			doGB := cl.AutoExtendGB > 0 && (overQuota || renewBoth)
			doDays := cl.AutoExtendDays > 0 && (expired || renewBoth)
			if (doGB || doDays) && (cl.Enable || cl.SubscriptionStatus == "expired") {
				updates := map[string]any{"enable": true}
				if doGB {
					updates["total_gb"] = cl.TotalGB + cl.AutoExtendGB
					if cl.TrafficLimitBytes > 0 {
						updates["traffic_limit_bytes"] = cl.TrafficLimitBytes + cl.AutoExtendGB*1024*1024*1024
					}
				}
				if doDays {
					base := now
					if cl.ExpiryTime > base.UnixMilli() {
						base = time.UnixMilli(cl.ExpiryTime)
					}
					updates["expiry_time"] = base.Add(time.Duration(cl.AutoExtendDays) * 24 * time.Hour).UnixMilli()
				}
				if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(updates).Error; err != nil {
					tx.Rollback()
					out.Error = err.Error()
					for _, dd := range latest {
						syncbus.Default.PushDeadLetter(dd, out.Error, 1)
					}
					return out
				}
				out.Extended++
				out.NeedRegen = true
			} else if cl.Enable {
				disableIDs = append(disableIDs, cl.ID)
			}
		}
	}

	if len(disableIDs) > 0 {
		if err := tx.Model(&models.Client{}).Where("id IN ? AND (subscription_status IS NULL OR subscription_status <> ?)", disableIDs, "revoked").
			Updates(map[string]any{"enable": false, "subscription_status": "expired"}).Error; err != nil {
			tx.Rollback()
			out.Error = err.Error()
			for _, dd := range latest {
				syncbus.Default.PushDeadLetter(dd, out.Error, 1)
			}
			return out
		}
		out.Disabled += len(disableIDs)
		out.NeedRegen = true
	}

	// Deduplicate snapshots (same client may appear multiple times in one flush).
	if len(snapshots) > 0 {
		seenSnap := make(map[uint]struct{}, len(snapshots))
		unique := snapshots[:0]
		for _, sn := range snapshots {
			if _, ok := seenSnap[sn.ClientID]; ok {
				continue
			}
			seenSnap[sn.ClientID] = struct{}{}
			unique = append(unique, sn)
		}
		// Enforce the documented "at most once per hour per client" policy.
		// Without this every 30s flush wrote a row per active client.
		ids := make([]uint, 0, len(unique))
		for _, sn := range unique {
			ids = append(ids, sn.ClientID)
		}
		recentSet := make(map[uint]struct{}, len(ids))
		var recentIDs []uint
		if err := tx.Model(&models.UsageSnapshot{}).
			Where("client_id IN ? AND created_at >= ?", ids, now.Add(-time.Hour)).
			Pluck("client_id", &recentIDs).Error; err == nil {
			for _, rid := range recentIDs {
				recentSet[rid] = struct{}{}
			}
		}
		fresh := make([]models.UsageSnapshot, 0, len(unique))
		for _, sn := range unique {
			if _, recent := recentSet[sn.ClientID]; !recent {
				fresh = append(fresh, sn)
			}
		}
		if len(fresh) > 0 {
			if err := tx.CreateInBatches(fresh, 100).Error; err != nil {
				slog.Debug("usage snapshot batch (remote)", "error", err)
			}
		}
	}

	// Keep inbound aggregates in sync.
	if err := tx.Exec(`UPDATE inbounds SET up = COALESCE((SELECT SUM(up) FROM clients WHERE clients.inbound_id=inbounds.id),0), down = COALESCE((SELECT SUM(down) FROM clients WHERE clients.inbound_id=inbounds.id),0)`).Error; err != nil {
		tx.Rollback()
		out.Error = err.Error()
		for _, dd := range latest {
			syncbus.Default.PushDeadLetter(dd, out.Error, 1)
		}
		return out
	}

	// Inbound-level volume / expiry: flag state changes so the runtime config
	// is regenerated once when an inbound reaches (or leaves) its limit.
	if n, err := syncInboundLimitStates(tx); err != nil {
		tx.Rollback()
		out.Error = err.Error()
		for _, dd := range latest {
			syncbus.Default.PushDeadLetter(dd, out.Error, 1)
		}
		return out
	} else if n > 0 {
		out.NeedRegen = true
	}

	if err := tx.Commit().Error; err != nil {
		out.Error = err.Error()
		for _, dd := range latest {
			syncbus.Default.PushDeadLetter(dd, out.Error, 1)
		}
		return out
	}

	syncbus.Default.MarkFlush()
	return out
}

// SyncTrafficWithStats applies an already-fetched stats map (local or remote).
// Optimisations vs the original loop:
//   - single bulk profile load (no N+1)
//   - skip UPDATE when counters unchanged
//   - usage snapshots inserted in batches
//   - expiry/quota applied with bulk ID lists where possible
func (s *Server) SyncTrafficWithStats(stats map[string]*xray.Traffic, xrayRestarted bool) SyncTrafficResult {
	out := SyncTrafficResult{}
	if s == nil || s.DB == nil {
		out.Error = "server not ready"
		return out
	}
	s.trafficSyncMu.Lock()
	defer s.trafficSyncMu.Unlock()
	if stats == nil {
		stats = map[string]*xray.Traffic{}
	}
	statsEmpty := len(stats) == 0
	if statsEmpty {
		out.SkippedStats = true
	}

	var clients []models.Client
	// Only columns required for accounting / policy — keeps memory low at scale.
	if err := s.DB.Select("id,email,enable,total_gb,traffic_limit_bytes,expiry_time,up,down,traffic_base_up,traffic_base_down,auto_extend_gb,auto_extend_days,subscription_status").
		Find(&clients).Error; err != nil {
		out.Error = err.Error()
		return out
	}
	if len(clients) == 0 {
		return out
	}

	// Bulk-load profile aliases once (was N+1 per client).
	profileSubjects := loadProfileSubjects(s.DB, clients)

	type latestSnapshot struct {
		ClientID  uint
		CreatedAt time.Time
	}
	latest := make(map[uint]latestSnapshot, len(clients))
	ids := make([]uint, 0, len(clients))
	for _, cl := range clients {
		ids = append(ids, cl.ID)
	}
	// Chunk the IN query for very large fleets (SQLite/Postgres parameter limits).
	const idChunk = 800
	for i := 0; i < len(ids); i += idChunk {
		end := i + idChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		var rows []latestSnapshot
		_ = s.DB.Raw(`
			SELECT u.client_id, u.created_at
			FROM usage_snapshots u
			JOIN (SELECT client_id, MAX(created_at) AS created_at FROM usage_snapshots WHERE client_id IN ? GROUP BY client_id) x
			  ON x.client_id = u.client_id AND x.created_at = u.created_at
			WHERE u.client_id IN ?`, chunk, chunk).Scan(&rows).Error
		for _, row := range rows {
			latest[row.ClientID] = row
		}
	}

	tx := s.DB.Begin()
	if tx.Error != nil {
		out.Error = tx.Error.Error()
		return out
	}

	snapshots := make([]models.UsageSnapshot, 0, 64)
	disableIDs := make([]uint, 0, 16)
	now := time.Now()

	for i := range clients {
		cl := &clients[i]
		up, down := cl.Up, cl.Down
		subjects := []string{cl.Email}
		if extra := profileSubjects[cl.ID]; len(extra) > 0 {
			subjects = append(subjects, extra...)
		}

		if len(subjects) > 0 && !statsEmpty {
			aggregate := xray.SumTraffic(stats, subjects...)
			res := xray.ComputeEffectiveTraffic(xray.EffectiveTrafficInput{
				Up: cl.Up, Down: cl.Down,
				TrafficBaseUp: cl.TrafficBaseUp, TrafficBaseDown: cl.TrafficBaseDown,
				AggregateUp: aggregate.Uplink, AggregateDown: aggregate.Downlink,
				SubjectPresent: xray.SubjectPresentInStats(stats, subjects...),
				StatsEmpty:     statsEmpty,
				XrayRestarted:  xrayRestarted,
			})
			if !res.Skip {
				changed := res.Up != up || res.Down != down ||
					res.TrafficBaseUp != cl.TrafficBaseUp || res.TrafficBaseDown != cl.TrafficBaseDown
				if changed {
					if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(map[string]any{
						"up": res.Up, "down": res.Down,
						"traffic_base_up": res.TrafficBaseUp, "traffic_base_down": res.TrafficBaseDown,
					}).Error; err != nil {
						tx.Rollback()
						out.Error = err.Error()
						return out
					}
					cl.Up, cl.Down = res.Up, res.Down
					cl.TrafficBaseUp, cl.TrafficBaseDown = res.TrafficBaseUp, res.TrafficBaseDown
					out.ClientsTouched++
				}
				last, exists := latest[cl.ID]
				if !exists || now.Sub(last.CreatedAt) >= time.Hour {
					snapshots = append(snapshots, models.UsageSnapshot{ClientID: cl.ID, Up: cl.Up, Down: cl.Down})
				}
			}
		}

		expired, overQuota := cl.IsExpired(), cl.IsOverQuota()
		if expired || overQuota {
			// Auto-extend only fixes the condition it can actually fix: GB extension cures
			// quota exhaustion, day extension cures expiry. When both are configured the
			// package is renewed as a whole. This prevents an endless GB top-up loop for a
			// client that is expired by time but has only GB auto-extend configured.
			renewBoth := cl.AutoExtendGB > 0 && cl.AutoExtendDays > 0
			doGB := cl.AutoExtendGB > 0 && (overQuota || renewBoth)
			doDays := cl.AutoExtendDays > 0 && (expired || renewBoth)
			if (doGB || doDays) && (cl.Enable || cl.SubscriptionStatus == "expired") {
				updates := map[string]any{"enable": true}
				if doGB {
					updates["total_gb"] = cl.TotalGB + cl.AutoExtendGB
					// Trials use TrafficLimitBytes as the real quota; extend that too so
					// auto-extend actually clears over-quota instead of looping forever.
					if cl.TrafficLimitBytes > 0 {
						updates["traffic_limit_bytes"] = cl.TrafficLimitBytes + cl.AutoExtendGB*1024*1024*1024
					}
				}
				if doDays {
					base := now
					if cl.ExpiryTime > base.UnixMilli() {
						base = time.UnixMilli(cl.ExpiryTime)
					}
					updates["expiry_time"] = base.Add(time.Duration(cl.AutoExtendDays) * 24 * time.Hour).UnixMilli()
				}
				if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(updates).Error; err != nil {
					tx.Rollback()
					out.Error = err.Error()
					return out
				}
				out.Extended++
				out.NeedRegen = true
			} else if cl.Enable {
				disableIDs = append(disableIDs, cl.ID)
			}
		}
	}

	if len(disableIDs) > 0 {
		if err := tx.Model(&models.Client{}).Where("id IN ? AND (subscription_status IS NULL OR subscription_status <> ?)", disableIDs, "revoked").Updates(map[string]any{
			"enable": false, "subscription_status": "expired",
		}).Error; err != nil {
			tx.Rollback()
			out.Error = err.Error()
			return out
		}
		out.Disabled += len(disableIDs)
		out.NeedRegen = true
	}

	if len(snapshots) > 0 {
		if err := tx.CreateInBatches(snapshots, 100).Error; err != nil {
			// Snapshot failure must not abort traffic accounting.
			slog.Debug("usage snapshot batch", "error", err)
		}
	}

	if err := tx.Exec(`UPDATE inbounds SET up = COALESCE((SELECT SUM(up) FROM clients WHERE clients.inbound_id=inbounds.id),0), down = COALESCE((SELECT SUM(down) FROM clients WHERE clients.inbound_id=inbounds.id),0)`).Error; err != nil {
		tx.Rollback()
		out.Error = err.Error()
		return out
	}
	// Inbound-level volume / expiry: flag state changes so the runtime config
	// is regenerated once when an inbound reaches (or leaves) its limit.
	if n, err := syncInboundLimitStates(tx); err != nil {
		tx.Rollback()
		out.Error = err.Error()
		return out
	} else if n > 0 {
		out.NeedRegen = true
	}
	if err := tx.Commit().Error; err != nil {
		out.Error = err.Error()
		return out
	}

	if n := shop.DisableExpiredTrials(s.DB); n > 0 {
		out.Disabled += n
		out.NeedRegen = true
		slog.Info("disabled expired trials", "count", n)
	}
	if n := shop.RecoverStuckOrders(s.DB); n > 0 {
		slog.Info("recovered stuck orders", "count", n)
	}
	return out
}

// loadProfileSubjects returns clientID -> alias emails for enabled profiles.
func loadProfileSubjects(db *gorm.DB, clients []models.Client) map[uint][]string {
	out := make(map[uint][]string)
	if len(clients) == 0 {
		return out
	}
	ids := make([]uint, 0, len(clients))
	emailByID := make(map[uint]string, len(clients))
	for _, cl := range clients {
		ids = append(ids, cl.ID)
		emailByID[cl.ID] = cl.Email
	}
	var profiles []models.ClientConfigProfile
	if err := db.Select("id,client_id").Where("client_id IN ? AND enabled = ?", ids, true).Find(&profiles).Error; err != nil {
		return out
	}
	for _, pr := range profiles {
		email := emailByID[pr.ClientID]
		if email == "" {
			continue
		}
		out[pr.ClientID] = append(out[pr.ClientID], fmt.Sprintf("%s#profile-%d", email, pr.ID))
	}
	return out
}

func trafficSubjects(db *gorm.DB, client models.Client) []string {
	subjects := []string{client.Email}
	var profiles []models.ClientConfigProfile
	if err := db.Where("client_id = ? AND enabled = ?", client.ID, true).Order("id asc").Find(&profiles).Error; err != nil {
		return subjects
	}
	for _, pr := range profiles {
		subjects = append(subjects, fmt.Sprintf("%s#profile-%d", client.Email, pr.ID))
	}
	return subjects
}
