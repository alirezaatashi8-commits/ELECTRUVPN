package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/syncbus"
	"github.com/electruvpn/electruvpn/internal/version"
	"github.com/gin-gonic/gin"
)

var processStart = time.Now()

// metricsHandler serves a Prometheus text-format exposition at /metrics.
// It is disabled (404) unless ELECTRU_METRICS_TOKEN is set, and requires the
// token as "Authorization: Bearer <token>". Only aggregate counters are
// exported: no client names, emails, IPs or links ever appear here.
func metricsHandler(s *core.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(os.Getenv("ELECTRU_METRICS_TOKEN"))
		if token == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		given := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		var inboundsTotal, inboundsEnabled, clientsTotal, clientsEnabled, clientsExpired int64
		s.DB.Model(&models.Inbound{}).Count(&inboundsTotal)
		s.DB.Model(&models.Inbound{}).Where("enable = ?", true).Count(&inboundsEnabled)
		s.DB.Model(&models.Client{}).Count(&clientsTotal)
		s.DB.Model(&models.Client{}).Where("enable = ?", true).Count(&clientsEnabled)
		s.DB.Model(&models.Client{}).Where("expiry_time > 0 AND expiry_time <= ?", time.Now().UnixMilli()).Count(&clientsExpired)
		var traffic struct {
			Up   int64
			Down int64
		}
		s.DB.Model(&models.Client{}).Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").Scan(&traffic)

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		var b strings.Builder
		metric := func(name, kind, help string, value float64) {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", name, help, name, kind, name, value)
		}
		boolValue := func(v bool) float64 {
			if v {
				return 1
			}
			return 0
		}

		fmt.Fprintf(&b, "# HELP electruvpn_build_info Build information.\n# TYPE electruvpn_build_info gauge\nelectruvpn_build_info{version=%q} 1\n", version.Version)
		metric("electruvpn_uptime_seconds", "gauge", "Seconds since the panel process started.", time.Since(processStart).Seconds())
		metric("electruvpn_xray_running", "gauge", "1 when the Xray core process is running.", boolValue(s.Xray.Status().Running))
		singbox := s.SingBox.Status()
		metric("electruvpn_singbox_installed", "gauge", "1 when the sing-box binary is installed.", boolValue(singbox.Installed))
		metric("electruvpn_singbox_running", "gauge", "1 when the sing-box process is running.", boolValue(singbox.Running))
		metric("electruvpn_inbounds_total", "gauge", "Number of configured inbounds.", float64(inboundsTotal))
		metric("electruvpn_inbounds_enabled", "gauge", "Number of enabled inbounds.", float64(inboundsEnabled))
		metric("electruvpn_clients_total", "gauge", "Number of configured clients.", float64(clientsTotal))
		metric("electruvpn_clients_enabled", "gauge", "Number of enabled clients.", float64(clientsEnabled))
		metric("electruvpn_clients_expired", "gauge", "Number of clients whose expiry date has passed.", float64(clientsExpired))
		metric("electruvpn_traffic_upload_bytes", "gauge", "Total upload bytes across all clients.", float64(traffic.Up))
		metric("electruvpn_traffic_download_bytes", "gauge", "Total download bytes across all clients.", float64(traffic.Down))
		bus := syncbus.Default.Stats()
		metric("electruvpn_syncbus_nodes", "gauge", "Nodes currently registered in the in-memory multi-node bus.", float64(bus["nodes"]))
		metric("electruvpn_syncbus_queue_len", "gauge", "Pending remote traffic deltas in the sync bus.", float64(bus["queueLen"]))
		metric("electruvpn_syncbus_deltas_queued", "counter", "Total remote traffic deltas accepted.", float64(bus["deltasQueued"]))
		metric("electruvpn_syncbus_deltas_dropped", "counter", "Total remote traffic deltas dropped under back-pressure.", float64(bus["deltasDropped"]))
		metric("electruvpn_syncbus_flushes", "counter", "Successful remote traffic flush cycles.", float64(bus["flushes"]))
		metric("electruvpn_syncbus_dlq_len", "gauge", "Dead-letter traffic deltas awaiting inspection.", float64(bus["dlqLen"]))
		metric("electruvpn_syncbus_dlq_dropped", "counter", "Dead-letter entries dropped under capacity pressure.", float64(bus["dlqDropped"]))
		metric("electruvpn_go_goroutines", "gauge", "Number of goroutines.", float64(runtime.NumGoroutine()))
		metric("electruvpn_go_heap_alloc_bytes", "gauge", "Bytes of allocated heap objects.", float64(mem.HeapAlloc))
		metric("electruvpn_go_heap_objects", "gauge", "Number of allocated heap objects.", float64(mem.HeapObjects))

		metric("electruvpn_go_goroutines", "gauge", "Number of goroutines.", float64(runtime.NumGoroutine()))
		metric("electruvpn_go_memory_alloc_bytes", "gauge", "Bytes of allocated heap objects.", float64(mem.Alloc))

		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(b.String()))
	}
}
