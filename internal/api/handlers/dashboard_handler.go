package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/version"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// overviewCacheTTL caps how often Overview actually recomputes system
// stats. The dashboard polls every ~5 seconds while visible; a short TTL keeps
// the live network speed and online counter fresh while still coalescing
// requests from several open browser tabs.
const overviewCacheTTL = 4 * time.Second

type DashboardHandler struct {
	S *core.Server

	cacheMu   sync.Mutex
	refreshMu sync.Mutex // coalesce concurrent cache misses into one system/DB sample
	cached    gin.H
	cachedAt  time.Time

	// Previous host network counters, used to derive bytes/second.
	netPrevAt   time.Time
	netPrevSent uint64
	netPrevRecv uint64
}

// NewDashboardHandler creates or exposes the NewDashboardHandler handler.
func NewDashboardHandler(s *core.Server) *DashboardHandler { return &DashboardHandler{S: s} }

// Overview powers the dashboard's top cards: CPU/RAM/disk, uptime, xray
// status, and the sum of all traffic across every client -- everything the
// landing page needs in a single request. Results are cached briefly (see
// overviewCacheTTL) since this is the single most-polled endpoint in the
// panel.
// Overview returns the dashboard summary used by the control-plane landing page.
func (h *DashboardHandler) Overview(c *gin.Context) {
	h.cacheMu.Lock()
	if h.cached != nil && time.Since(h.cachedAt) < overviewCacheTTL {
		obj := h.cached
		h.cacheMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": obj})
		return
	}
	h.cacheMu.Unlock()

	// Multiple browser tabs/users can miss the cache simultaneously. Serialize
	// refreshes and re-check after acquiring the lock so only one request samples
	// gopsutil and queries aggregate DB statistics per cache window.
	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()
	h.cacheMu.Lock()
	if h.cached != nil && time.Since(h.cachedAt) < overviewCacheTTL {
		obj := h.cached
		h.cacheMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": obj})
		return
	}
	h.cacheMu.Unlock()

	cpuPercent, _ := cpu.Percent(0, false)
	cpuCoresLogical, _ := cpu.Counts(true)
	cpuCoresPhysical, _ := cpu.Counts(false)
	vm, _ := mem.VirtualMemory()
	du, _ := disk.Usage("/")
	hi, _ := host.Info()
	netIO, _ := net.IOCounters(false)

	var cpuPct float64
	if len(cpuPercent) > 0 {
		cpuPct = cpuPercent[0]
	}
	if cpuCoresLogical <= 0 {
		cpuCoresLogical = 1
	}
	if cpuCoresPhysical <= 0 {
		cpuCoresPhysical = cpuCoresLogical
	}

	var netUp, netDown uint64
	if len(netIO) > 0 {
		netUp = netIO[0].BytesSent
		netDown = netIO[0].BytesRecv
	}

	// Live network speed (bytes/second) from the delta of the host counters.
	now := time.Now()
	var netUpSpeed, netDownSpeed float64
	h.cacheMu.Lock()
	if !h.netPrevAt.IsZero() {
		if dt := now.Sub(h.netPrevAt).Seconds(); dt > 0 {
			if netUp >= h.netPrevSent {
				netUpSpeed = float64(netUp-h.netPrevSent) / dt
			}
			if netDown >= h.netPrevRecv {
				netDownSpeed = float64(netDown-h.netPrevRecv) / dt
			}
		}
	}
	h.netPrevAt, h.netPrevSent, h.netPrevRecv = now, netUp, netDown
	h.cacheMu.Unlock()

	// Online users: clients whose counters moved recently or that have a device
	// connected right now (Xray and sing-box protocols).
	h.S.RefreshLive()
	var liveClients []models.Client
	_ = h.S.DB.Select("id", "email").Find(&liveClients).Error
	subjectsByClient := clientsTrafficSubjects(h.S.DB, liveClients)
	onlineCount := 0
	for _, cl := range liveClients {
		if live, ok := xray.LiveSampleForSubjects(subjectsByClient[cl.ID]...); ok && live.Online {
			onlineCount++
		}
	}

	// Aggregate traffic in SQLite instead of loading every client row into
	// Go on every dashboard refresh. This stays effectively O(1) as the
	// customer count grows.
	var traffic struct {
		TotalUp   int64 `gorm:"column:total_up"`
		TotalDown int64 `gorm:"column:total_down"`
	}
	_ = h.S.DB.Model(&models.Client{}).Select("COALESCE(SUM(up), 0) AS total_up, COALESCE(SUM(down), 0) AS total_down").Scan(&traffic).Error

	var counts struct {
		Inbounds int64 `gorm:"column:inbounds"`
		Clients  int64 `gorm:"column:clients"`
	}
	_ = h.S.DB.Raw("SELECT (SELECT COUNT(*) FROM inbounds) AS inbounds, (SELECT COUNT(*) FROM clients) AS clients").Scan(&counts).Error

	obj := gin.H{
		"cpuPercent":       cpuPct,
		"cpuCores":         cpuCoresLogical,
		"cpuCoresPhysical": cpuCoresPhysical,
		"memUsedBytes":     vm.Used,
		"memTotalBytes":    vm.Total,
		"memPercent":       vm.UsedPercent,
		"diskUsedBytes":    du.Used,
		"diskTotalBytes":   du.Total,
		"diskPercent":      du.UsedPercent,
		"uptimeSeconds":    hi.Uptime,
		"netSentBytes":     netUp,
		"netRecvBytes":     netDown,
		"netUpSpeed":       netUpSpeed,
		"netDownSpeed":     netDownSpeed,
		"onlineCount":      onlineCount,
		"xray":             h.S.Xray.Status(),
		"inboundCount":     counts.Inbounds,
		"clientCount":      counts.Clients,
		"trafficUp":        traffic.TotalUp,
		"trafficDown":      traffic.TotalDown,
		"panelVersion":     version.Version,
	}

	h.cacheMu.Lock()
	h.cached = obj
	h.cachedAt = time.Now()
	h.cacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"success": true, "obj": obj})
}

// XrayLogs returns the most recent captured stdout/stderr lines from the
// running (or last-run) xray-core process, for the panel's log viewer.
func (h *DashboardHandler) XrayLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": h.S.Xray.RecentLogs()})
}

// XrayAction starts, stops, or restarts xray-core on demand from the UI.
func (h *DashboardHandler) XrayAction(c *gin.Context) {
	action := c.Param("action")

	var err error
	switch action {
	case "start":
		h.S.SetAutoRecovery(true)
		err = h.S.RegenerateAndRestartXray()
	case "stop":
		h.S.SetAutoRecovery(false)
		err = h.S.Xray.Stop()
	case "restart":
		h.S.SetAutoRecovery(true)
		err = h.S.RegenerateAndRestartXray()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "unknown action"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": h.S.Xray.Status()})
}

// SyncTraffic is the on-demand endpoint; the same logic runs every ~45s in a
// background worker so quota/expiry still apply when no admin tab is open.
func (h *DashboardHandler) SyncTraffic(c *gin.Context) {
	res := h.S.SyncTrafficFromXray()
	if res.Error != "" && res.ClientsTouched == 0 && res.Disabled == 0 {
		// soft fail when xray stats unavailable (panel still usable)
		c.JSON(http.StatusOK, gin.H{"success": false, "msg": res.Error})
		return
	}
	if res.NeedRegen {
		_ = h.S.RegenerateAndRestartXray()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"touched": res.ClientsTouched, "disabled": res.Disabled,
		"extended": res.Extended, "skippedStats": res.SkippedStats,
	}})
}

// ResetAllTraffic
// ResetAllTraffic zeroes every client's cached counters and resets xray's
// own live counters in one shot -- the "start the month fresh" button.
func (h *DashboardHandler) ResetAllTraffic(c *gin.Context) {
	if _, err := xray.PollStats(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, true); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": "could not reset Xray counters: " + err.Error()})
		return
	}
	if err := h.S.DB.Model(&models.Client{}).Where("1 = 1").Updates(map[string]any{"up": 0, "down": 0, "traffic_base_up": 0, "traffic_base_down": 0}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = h.S.DB.Model(&models.Inbound{}).Where("1 = 1").Updates(map[string]any{"up": 0, "down": 0}).Error
	xray.ResetAllLiveSamples()
	// Keep historical snapshots intact; reset only affects the current accounting window.
	auditAction(h.S, c, "traffic.reset_all", "all", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"scope": "all", "resetAt": time.Now()}})
}
