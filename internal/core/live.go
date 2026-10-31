package core

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/singbox"
	"github.com/electruvpn/electruvpn/internal/xray"
)

const (
	// liveRefreshMinGap matches the Xray stats cache TTL: sampling faster only
	// re-reads the same counters.
	liveRefreshMinGap = 1500 * time.Millisecond
	liveIndexTTL      = 15 * time.Second
)

// liveRefresher serialises live sampling for all protocols. Every handler that
// shows speeds / online state calls Server.RefreshLive instead of polling the
// engines itself, so a burst of browser polls costs one sample.
type liveRefresher struct {
	mu       sync.Mutex
	at       time.Time
	idxAt    time.Time
	emails   map[string]struct{}
	uuids    map[string]string
	onlineIP atomic.Bool
}

// RefreshLive samples Xray (counters + online IPs) and sing-box (Clash API
// connections) into the shared live tracker. It is cheap to call repeatedly:
// calls closer than liveRefreshMinGap return immediately.
func (s *Server) RefreshLive() {
	if s == nil || s.Config == nil {
		return
	}
	r := &s.live
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !r.at.IsZero() && now.Sub(r.at) < liveRefreshMinGap {
		return
	}
	r.at = now
	r.loadIndex(s, now)

	stats, err := xray.PollStats(s.Config.XrayBinaryPath, s.Config.XrayAPIPort, false)
	if err != nil || stats == nil {
		stats = make(map[string]*xray.Traffic)
	}

	// sing-box protocols: per-inbound and per-user counters from the Clash API.
	sbSubjects := make(map[string]struct{})
	if singbox.LiveAPIEnabled() && s.SingBox != nil && s.SingBox.Status().Running {
		if snap, err := singbox.PollLive(now); err == nil {
			merged := make(map[string]*xray.Traffic, len(snap))
			conns := make(map[string]int, len(snap))
			ips := make(map[string][]string, len(snap))
			for key, sub := range snap {
				subject := key
				if strings.HasPrefix(key, "inbound:") {
					subject = strings.TrimPrefix(key, "inbound:")
				} else {
					subject = r.resolveUser(key)
				}
				t := merged[subject]
				if t == nil {
					t = &xray.Traffic{}
					merged[subject] = t
				}
				t.Uplink += sub.Up
				t.Downlink += sub.Down
				conns[subject] += sub.Conns
				ips[subject] = append(ips[subject], sub.IPs...)
			}
			for subject, t := range merged {
				stats[subject] = t
				sbSubjects[subject] = struct{}{}
				xray.RecordDeviceIPs(subject, ips[subject], now)
				xray.RecordConnections(subject, conns[subject], now)
			}
		}
	}

	xray.UpdateLiveSamples(stats, now)

	// Xray protocols: ask which source IPs are connected for users that are
	// active right now. Runs in the background so the HTTP request that
	// triggered the refresh is never blocked by CLI calls.
	if s.Xray != nil && s.Xray.Status().Running && r.onlineIP.CompareAndSwap(false, true) {
		candidates := make([]string, 0, 16)
		for key := range stats {
			if _, isSB := sbSubjects[key]; isSB {
				continue
			}
			base := key
			if i := strings.Index(key, "#profile-"); i > 0 {
				base = key[:i]
			}
			if _, ok := r.emails[base]; !ok {
				continue
			}
			if sample, ok := xray.LiveSampleFor(key); ok && sample.Online {
				candidates = append(candidates, key)
			}
		}
		bin, port := s.Config.XrayBinaryPath, s.Config.XrayAPIPort
		go func() {
			defer r.onlineIP.Store(false)
			xray.PollOnlineIPs(bin, port, candidates, time.Now())
		}()
	}
}

// loadIndex refreshes the email / uuid lookup used to name sing-box users.
func (r *liveRefresher) loadIndex(s *Server, now time.Time) {
	if r.emails != nil && now.Sub(r.idxAt) < liveIndexTTL {
		return
	}
	if s.DB == nil {
		return
	}
	var rows []models.Client
	if err := s.DB.Select("id", "email", "uuid").Find(&rows).Error; err != nil {
		return
	}
	emails := make(map[string]struct{}, len(rows))
	uuids := make(map[string]string, len(rows))
	for _, c := range rows {
		emails[c.Email] = struct{}{}
		if c.UUID != "" {
			uuids[c.UUID] = c.Email
		}
	}
	r.emails, r.uuids, r.idxAt = emails, uuids, now
}

// resolveUser maps the user name sing-box reports (email, or the UUID for
// protocols that identify users by UUID) to the client's email.
func (r *liveRefresher) resolveUser(user string) string {
	if _, ok := r.emails[user]; ok {
		return user
	}
	if email, ok := r.uuids[user]; ok {
		return email
	}
	return user
}

// InboundLiveSubject returns the live-stats key of an inbound (its engine tag).
func InboundLiveSubject(ib models.Inbound) string {
	if tag := strings.TrimSpace(ib.Tag); tag != "" {
		return tag
	}
	if ib.Port > 0 {
		return fmt.Sprintf("inbound-%d", ib.Port)
	}
	return fmt.Sprintf("inbound-%d", ib.ID)
}

// LiveEngineNote explains why live data may be missing for a protocol; empty
// when the engine is able to report live stats.
func (s *Server) LiveEngineNote(p models.Protocol) string {
	switch p {
	case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
		if !singbox.LiveAPIEnabled() {
			return "sing-box live API is disabled (ELECTRU_SINGBOX_API_PORT=0 or this sing-box build has no Clash API)"
		}
		if s.SingBox == nil || !s.SingBox.Status().Running {
			return "sing-box is not running"
		}
	}
	return ""
}
