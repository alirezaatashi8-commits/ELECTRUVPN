// Package routing — Outbound health probing & semi-automatic failover (2026).
//
// Periodically probes every exit host belonging to enabled TunnelChain rows
// (primary ExitAddress + RotateAddresses). Unhealthy hosts are skipped by
// ApplyEnabledChain. When the current ActiveExitHost fails:
//
//   - If ConfirmBeforeFailover=true → Telegram interactive prompt is sent;
//     traffic is moved only after admin confirmation (best healthy exit chosen).
//   - If ConfirmBeforeFailover=false → classic automatic rotation (RotateOnFail).
//
// Pending confirmations expire after 5 minutes.
package routing

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"gorm.io/gorm"
)

const (
	defaultProbeTimeout   = 4 * time.Second
	defaultMaxRTTMs       = 800.0 // above this → degraded / candidate for removal
	defaultFailThreshold  = 2     // consecutive failures before marking bad
	defaultRecoverSuccess = 2     // consecutive successes before recovery
	pendingFailoverTTL    = 5 * time.Minute
)

// HostHealth tracks probe results for one exit address.
type HostHealth struct {
	Host            string
	Healthy         bool
	RTTMs           float64
	ConsecutiveFail int
	ConsecutiveOK   int
	LastError       string
	LastProbeAt     time.Time
}

// PendingFailover holds a confirm-before-switch request waiting for admin.
type PendingFailover struct {
	ChainID    uint
	ChainName  string
	FromHost   string
	Candidates []CandidateHost // sorted by RTT ascending (best first)
	CreatedAt  time.Time
}

// CandidateHost is a healthy alternative exit with measured RTT.
type CandidateHost struct {
	Host  string
	RTTMs float64
}

var (
	healthMu   sync.RWMutex
	hostHealth = map[string]*HostHealth{} // key = host:port or host

	pendingMu        sync.Mutex
	pendingFailovers = map[uint]*PendingFailover{} // key = chain ID
)

// GetHostHealth returns a snapshot of the last probe for host (may be nil).
func GetHostHealth(host string) *HostHealth {
	host = normalizeHost(host)
	healthMu.RLock()
	defer healthMu.RUnlock()
	h := hostHealth[host]
	if h == nil {
		return nil
	}
	cp := *h
	return &cp
}

// IsHostHealthy reports whether host is currently considered usable.
// Unknown hosts are treated as healthy (optimistic) so first boot works.
func IsHostHealthy(host string) bool {
	h := GetHostHealth(host)
	if h == nil {
		return true
	}
	return h.Healthy
}

func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	return h
}

// ProbeHost performs a TCP connect to host:port and records RTT.
// Returns healthy flag, RTT in ms, and error message.
func ProbeHost(host string, port int, timeout time.Duration) (bool, float64, string) {
	host = normalizeHost(host)
	if host == "" || port <= 0 {
		return false, 0, "empty host or port"
	}
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	rtt := float64(time.Since(start).Milliseconds())
	if err != nil {
		return false, rtt, err.Error()
	}
	_ = conn.Close()
	if rtt > defaultMaxRTTMs {
		return false, rtt, fmt.Sprintf("RTT %.0fms exceeds limit %.0fms", rtt, defaultMaxRTTMs)
	}
	return true, rtt, ""
}

func recordProbe(host string, ok bool, rtt float64, errMsg string) *HostHealth {
	host = normalizeHost(host)
	healthMu.Lock()
	defer healthMu.Unlock()
	h, exists := hostHealth[host]
	if !exists {
		h = &HostHealth{Host: host, Healthy: true}
		hostHealth[host] = h
	}
	h.LastProbeAt = time.Now()
	h.RTTMs = rtt
	h.LastError = errMsg
	if ok {
		h.ConsecutiveOK++
		h.ConsecutiveFail = 0
		if h.ConsecutiveOK >= defaultRecoverSuccess {
			h.Healthy = true
		}
	} else {
		h.ConsecutiveFail++
		h.ConsecutiveOK = 0
		if h.ConsecutiveFail >= defaultFailThreshold {
			h.Healthy = false
		}
	}
	cp := *h
	return &cp
}

// CollectExitHosts returns unique exit addresses for a chain (primary + rotate list).
func CollectExitHosts(chain *models.TunnelChain) []string {
	if chain == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(a string) {
		a = normalizeHost(a)
		if a == "" {
			return
		}
		if _, ok := seen[a]; ok {
			return
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	add(chain.ExitAddress)
	add(chain.ActiveExitHost)
	for _, p := range strings.Split(chain.RotateAddresses, ",") {
		add(p)
	}
	return out
}

// rankHealthyCandidates returns healthy hosts sorted by ascending RTT (best first).
func rankHealthyCandidates(hosts []string, exclude string) []CandidateHost {
	exclude = normalizeHost(exclude)
	var cands []CandidateHost
	for _, h := range hosts {
		h = normalizeHost(h)
		if h == "" || h == exclude {
			continue
		}
		hh := GetHostHealth(h)
		if hh == nil || !hh.Healthy {
			continue
		}
		cands = append(cands, CandidateHost{Host: h, RTTMs: hh.RTTMs})
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].RTTMs < cands[j].RTTMs
	})
	return cands
}

// CheckChainOutbounds probes every exit of the given chain, updates in-memory
// health, and either rotates ActiveExitHost automatically or queues a
// confirm-before-failover request for the admin Telegram bot.
// Returns true if ActiveExitHost changed (caller should regenerate xray config).
func CheckChainOutbounds(db *gorm.DB, chain *models.TunnelChain) bool {
	if db == nil || chain == nil || !chain.Enabled {
		return false
	}
	hosts := CollectExitHosts(chain)
	if len(hosts) == 0 {
		return false
	}
	port := chain.ExitPort
	if port <= 0 {
		port = 443
	}

	changed := false
	var alerts []string

	for _, host := range hosts {
		ok, rtt, errMsg := ProbeHost(host, port, defaultProbeTimeout)
		prev := GetHostHealth(host)
		wasHealthy := prev == nil || prev.Healthy
		hh := recordProbe(host, ok, rtt, errMsg)

		if wasHealthy && !hh.Healthy {
			alerts = append(alerts, fmt.Sprintf("⚠️ Outbound exit %s is UNHEALTHY (RTT=%.0fms): %s", host, rtt, errMsg))
		}
		if !wasHealthy && hh.Healthy {
			alerts = append(alerts, fmt.Sprintf("✅ Outbound exit %s recovered (RTT=%.0fms)", host, rtt))
		}
	}

	// Failover decision
	active := normalizeHost(chain.ActiveExitHost)
	if active == "" {
		active = normalizeHost(chain.ExitAddress)
	}
	needRotate := false
	if active != "" {
		if !IsHostHealthy(active) {
			needRotate = true
		}
	} else {
		needRotate = true
	}

	if needRotate && chain.RotateOnFail {
		cands := rankHealthyCandidates(hosts, active)

		if chain.ConfirmBeforeFailover && len(cands) > 0 {
			// Semi-automatic path: ask admin, do NOT rotate yet.
			storePendingFailover(chain, active, cands)
			sendConfirmPrompt(chain, active, cands)
			// Still notify the raw health alerts so admin sees the probe results.
			for _, msg := range alerts {
				_ = notify.Telegram("ElectruVPN " + msg)
			}
			return false
		}

		// Classic automatic path (or no healthy alternative).
		if len(cands) > 0 {
			best := cands[0].Host
			if best != active {
				now := time.Now()
				chain.ActiveExitHost = best
				chain.LastFailAt = &now
				chain.LastError = fmt.Sprintf("auto-failover from %s to %s (RTT=%.0fms)", active, best, cands[0].RTTMs)
				_ = db.Model(chain).Updates(map[string]any{
					"active_exit_host": chain.ActiveExitHost,
					"last_fail_at":     chain.LastFailAt,
					"last_error":       chain.LastError,
				}).Error
				changed = true
				alerts = append(alerts, fmt.Sprintf("🔄 Auto-Failover: exit %s → %s (RTT=%.0fms, no client disconnect)", active, best, cands[0].RTTMs))
			}
		} else {
			now := time.Now()
			chain.LastFailAt = &now
			chain.LastError = "all exit hosts unhealthy"
			_ = db.Model(chain).Updates(map[string]any{
				"last_fail_at": chain.LastFailAt,
				"last_error":   chain.LastError,
			}).Error
			alerts = append(alerts, "🚨 All outbound exits unhealthy — traffic may be disrupted")
		}
	}

	for _, msg := range alerts {
		_ = notify.Telegram("ElectruVPN " + msg)
	}
	return changed
}

func storePendingFailover(chain *models.TunnelChain, from string, cands []CandidateHost) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	// Avoid spamming: if a pending request for this chain already exists and is fresh, skip.
	if p, ok := pendingFailovers[chain.ID]; ok {
		if time.Since(p.CreatedAt) < pendingFailoverTTL {
			return
		}
	}
	pendingFailovers[chain.ID] = &PendingFailover{
		ChainID:    chain.ID,
		ChainName:  chain.Name,
		FromHost:   from,
		Candidates: cands,
		CreatedAt:  time.Now(),
	}
}

func sendConfirmPrompt(chain *models.TunnelChain, from string, cands []CandidateHost) {
	best := cands[0]
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🚨 <b>Outbound Failover — نیاز به تأیید</b>\n\n"))
	sb.WriteString(fmt.Sprintf("Chain: <code>%s</code> (ID %d)\n", chain.Name, chain.ID))
	sb.WriteString(fmt.Sprintf("خروجی فعلی: <code>%s</code> — <b>قطع / پینگ بالا</b>\n\n", from))
	sb.WriteString("بهترین جایگزین پیشنهادی:\n")
	sb.WriteString(fmt.Sprintf("• <code>%s</code>  (RTT ≈ %.0fms)\n", best.Host, best.RTTMs))
	if len(cands) > 1 {
		sb.WriteString("\nسایر گزینه‌های سالم:\n")
		for i, c := range cands {
			if i == 0 {
				continue
			}
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("… و %d مورد دیگر\n", len(cands)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("• <code>%s</code>  (%.0fms)\n", c.Host, c.RTTMs))
		}
	}
	sb.WriteString("\n⏱ این درخواست تا ۵ دقیقه معتبر است.\n")
	sb.WriteString("اگر تأیید نکنید، هیچ جابه‌جایی انجام نمی‌شود.")

	// Inline keyboard
	rows := [][]map[string]string{
		{
			{"text": "✅ بله — بهترین سرور را انتخاب کن و منتقل کن", "callback_data": fmt.Sprintf("fo:yes:%d", chain.ID)},
		},
		{
			{"text": "❌ نه — فعلاً دست نزن", "callback_data": fmt.Sprintf("fo:no:%d", chain.ID)},
		},
	}
	// Optional direct-pick buttons for top 3 alternatives
	for i, c := range cands {
		if i >= 3 {
			break
		}
		label := fmt.Sprintf("➡️ %s (%.0fms)", c.Host, c.RTTMs)
		if len(label) > 60 {
			label = label[:57] + "…"
		}
		rows = append(rows, []map[string]string{
			{"text": label, "callback_data": fmt.Sprintf("fo:pick:%d:%s", chain.ID, c.Host)},
		})
	}

	markup := map[string]any{"inline_keyboard": rows}
	_ = notify.TelegramWithMarkup(sb.String(), markup)
}

// GetPendingFailover returns a copy of the pending request for a chain, or nil.
func GetPendingFailover(chainID uint) *PendingFailover {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pendingFailovers[chainID]
	if !ok {
		return nil
	}
	if time.Since(p.CreatedAt) > pendingFailoverTTL {
		delete(pendingFailovers, chainID)
		return nil
	}
	cp := *p
	return &cp
}

// ClearPendingFailover removes a pending request (after confirm or deny).
func ClearPendingFailover(chainID uint) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	delete(pendingFailovers, chainID)
}

// ApplyConfirmedFailover is called from the Telegram admin bot after the
// admin clicks "Yes" or picks a specific host. It rotates ActiveExitHost
// and returns (changed, human message).
func ApplyConfirmedFailover(db *gorm.DB, chainID uint, chosenHost string) (bool, string) {
	if db == nil || chainID == 0 {
		return false, "پارامتر نامعتبر"
	}
	pending := GetPendingFailover(chainID)
	if pending == nil {
		return false, "درخواست منقضی شده یا وجود ندارد (بیش از ۵ دقیقه گذشته)."
	}

	var chain models.TunnelChain
	if err := db.First(&chain, chainID).Error; err != nil {
		ClearPendingFailover(chainID)
		return false, "Chain پیدا نشد"
	}
	if !chain.Enabled {
		ClearPendingFailover(chainID)
		return false, "Chain غیرفعال است"
	}

	chosenHost = normalizeHost(chosenHost)
	if chosenHost == "" {
		// "best" path — use first candidate
		if len(pending.Candidates) == 0 {
			ClearPendingFailover(chainID)
			return false, "هیچ سرور سالمی در لیست نبود"
		}
		chosenHost = pending.Candidates[0].Host
	} else {
		// Validate chosen host is still among candidates (or currently healthy)
		found := false
		for _, c := range pending.Candidates {
			if c.Host == chosenHost {
				found = true
				break
			}
		}
		if !found && !IsHostHealthy(chosenHost) {
			ClearPendingFailover(chainID)
			return false, "سرور انتخاب‌شده دیگر سالم نیست"
		}
	}

	from := pending.FromHost
	now := time.Now()
	chain.ActiveExitHost = chosenHost
	chain.LastFailAt = &now
	chain.LastError = fmt.Sprintf("confirmed-failover from %s to %s (admin)", from, chosenHost)
	if err := db.Model(&chain).Updates(map[string]any{
		"active_exit_host": chain.ActiveExitHost,
		"last_fail_at":     chain.LastFailAt,
		"last_error":       chain.LastError,
	}).Error; err != nil {
		return false, "خطا در ذخیره: " + err.Error()
	}
	ClearPendingFailover(chainID)

	msg := fmt.Sprintf("✅ تأیید شد\nChain <code>%s</code>\nخروجی: <code>%s</code> → <code>%s</code>\n(بدون قطع اتصال کلاینت‌ها)",
		chain.Name, from, chosenHost)
	_ = notify.Telegram("ElectruVPN 🔄 Confirmed failover: " + from + " → " + chosenHost)
	return true, msg
}

// DenyFailover cancels a pending request.
func DenyFailover(chainID uint) string {
	p := GetPendingFailover(chainID)
	if p == nil {
		return "درخواست منقضی شده یا وجود ندارد."
	}
	ClearPendingFailover(chainID)
	return fmt.Sprintf("❌ لغو شد\nChain <code>%s</code>\nخروجی فعلی <code>%s</code> بدون تغییر باقی ماند.", p.ChainName, p.FromHost)
}

// CheckAllEnabledChains runs health probes for every enabled TunnelChain.
// Returns true if any active exit was rotated (config regen recommended).
func CheckAllEnabledChains(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	var chains []models.TunnelChain
	if err := db.Where("enabled = ?", true).Find(&chains).Error; err != nil {
		return false
	}
	anyChanged := false
	for i := range chains {
		if CheckChainOutbounds(db, &chains[i]) {
			anyChanged = true
		}
	}
	// Cleanup expired pending entries
	pendingMu.Lock()
	now := time.Now()
	for id, p := range pendingFailovers {
		if now.Sub(p.CreatedAt) > pendingFailoverTTL {
			delete(pendingFailovers, id)
		}
	}
	pendingMu.Unlock()
	return anyChanged
}

// HealthyExitHosts returns only currently healthy exits for a chain,
// preserving order. If none are known-healthy yet, returns all candidates
// (optimistic) so the panel keeps working on first start.
func HealthyExitHosts(chain *models.TunnelChain) []string {
	all := CollectExitHosts(chain)
	if len(all) == 0 {
		return nil
	}
	var healthy []string
	for _, h := range all {
		if IsHostHealthy(h) {
			healthy = append(healthy, h)
		}
	}
	if len(healthy) == 0 {
		// Optimistic: no probes yet or everything failed — still return primary
		if primary := normalizeHost(chain.ActiveExitHost); primary != "" {
			return []string{primary}
		}
		if primary := normalizeHost(chain.ExitAddress); primary != "" {
			return []string{primary}
		}
		return all
	}
	return healthy
}
