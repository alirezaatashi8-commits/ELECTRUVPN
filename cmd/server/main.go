// Command electruvpn-server is the entrypoint for the ElectruVPN panel.
// It loads config from the environment, opens the database (PostgreSQL preferred), wires up
// the gin router, and serves the panel + subscription endpoints on one port.
package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/electruvpn/electruvpn/internal/api"
	"github.com/electruvpn/electruvpn/internal/automation"
	"github.com/electruvpn/electruvpn/internal/backup"
	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/database"
	"github.com/electruvpn/electruvpn/internal/honeypot"
	"github.com/electruvpn/electruvpn/internal/logging"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"github.com/electruvpn/electruvpn/internal/obfuscation"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/electruvpn/electruvpn/internal/syncbus"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/electruvpn/electruvpn/internal/xray"
	"gorm.io/gorm"
)

func main() {
	slog.SetDefault(logging.New())
	cfg := config.Load()
	if cfg.JWTSecret == "change-me-in-production-please" && os.Getenv("ELECTRU_ALLOW_DEFAULT_SECRET") != "1" {
		slog.Error("refusing to start with the default JWT secret; set ELECTRU_JWT_SECRET or ELECTRU_ALLOW_DEFAULT_SECRET=1 for development")
		os.Exit(1)
	}

	db, err := database.Init(cfg.DBPath)
	if err != nil {
		slog.Error("database init failed", "error", err)
		os.Exit(1)
	}

	server := core.NewServer(db, cfg)
	honeypot.EnsureSettings(db)
	hpSrv := &honeypot.Server{DB: db}
	honeypot.Default = hpSrv
	xray.FallbackEnricher = func(fbs []map[string]any) []map[string]any {
		return honeypot.InjectFallbacks(db, fbs)
	}
	// Auto-adapt public IP when the VPS address changed (domains never touched).
	if st := server.AdaptPublicIPIfNeeded(); st.Adapted {
		slog.Info("public host auto-adapted on startup", "host", st.PublicHost, "v4", st.PublicHostV4, "v6", st.PublicHostV6)
	}
	if n, err := telegram.MigrateSecretSettings(db); err != nil {
		slog.Warn("secret settings migrate", "error", err)
	} else if n > 0 {
		slog.Info("encrypted plaintext bot tokens at rest", "count", n)
	}
	if os.Getenv("ELECTRU_CONFIG_ENCRYPTION_KEY") == "" {
		slog.Warn("ELECTRU_CONFIG_ENCRYPTION_KEY is empty — bot tokens stored in plaintext")
	}
	notify.SetTelegramOverride(telegram.GetSetting(db, telegram.KeyAdminToken), telegram.GetSetting(db, telegram.KeyAdminIDs))

	// Bring xray-core up with whatever inbounds already exist in the
	// database (empty on a fresh install, which is fine -- xray just runs
	// with only the internal stats API inbound until the admin adds one).
	if err := server.RegenerateAndRestartXray(); err != nil {
		slog.Warn("services did not start cleanly", "error", err)
		slog.Warn("check Xray/sing-box binaries and TLS settings")
	}

	nodeAlertState := make(map[uint]bool)

	// Background reliability workers share one cancellation context so shutdown
	// never leaves tickers or database work running after the HTTP server exits.
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	var workerWG sync.WaitGroup
	startWorker := func(interval time.Duration, fn func()) {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			runWorker(workerCtx, interval, fn)
		}()
	}

	startWorker(time.Duration(maxInt(cfg.BackupIntervalMin, 15))*time.Minute, func() {
		if row, err := backup.Create(db, cfg.DBPath, cfg.XrayConfigPath, cfg.SingBoxConfigPath, filepath.Join(cfg.DataDir(), "backups"), cfg.BackupRemoteDir); err != nil {
			slog.Error("scheduled backup failed", "error", err)
		} else {
			_ = backup.Prune(db, filepath.Join(cfg.DataDir(), "backups"), cfg.BackupRetention)
			slog.Info("scheduled backup created", "path", row.Path)
		}
	})

	// Node/agent health is operational telemetry, not a latency-critical path.
	// A 90s cadence avoids repeatedly scanning the node registry on small VPSs.
	startWorker(90*time.Second, func() {
		var nodes []models.ServerNode
		if err := db.Select("id", "name").Where("enabled = ?", true).Find(&nodes).Error; err != nil {
			return
		}
		var agents []models.AgentNode
		if err := db.Select("node_id", "last_seen_at", "last_error").Find(&agents).Error; err != nil {
			return
		}
		agentByNode := make(map[uint]*models.AgentNode, len(agents))
		for i := range agents {
			agent := &agents[i]
			agentByNode[agent.NodeID] = agent
		}

		// Keep alert state in the worker closure rather than querying the DB
		// once per node. This changes the hot path from N+1 queries to 2 queries.
		for _, n := range nodes {
			a := agentByNode[n.ID]
			if a == nil {
				continue // preserve existing behavior for nodes not yet enrolled
			}
			bad := a.LastSeenAt == nil || time.Since(*a.LastSeenAt) > 45*time.Second || a.LastError != ""
			if bad && !nodeAlertState[n.ID] {
				_ = notify.Telegram(fmt.Sprintf("ElectruVPN alert: node %s is unhealthy", n.Name))
			}
			if !bad && nodeAlertState[n.ID] {
				_ = notify.Telegram(fmt.Sprintf("ElectruVPN recovery: node %s is healthy again", n.Name))
			}
			nodeAlertState[n.ID] = bad
		}
	})

	// Outbound (exit-node) health probe + automatic failover.
	// Every ~90s TCP-probes all TunnelChain exits; unhealthy hosts are dropped
	// from the balancer and ActiveExitHost is rotated without client disconnect.
	// Telegram alerts are sent on fail / recovery / failover events.
	startWorker(180*time.Second, func() {
		if routing.CheckAllEnabledChains(db) {
			if err := server.RegenerateAndRestartXray(); err != nil {
				slog.Warn("outbound health failover regen", "error", err)
			} else {
				slog.Info("outbound health failover applied — xray config regenerated")
			}
		}
	})

	// Scheduled automation only needs to run near due times; avoid querying the
	// scheduler table twice per minute on small installations.
	startWorker(60*time.Second, func() {
		automation.TickContext(workerCtx, db)
	})

	// Background traffic accounting — must not depend on an open admin browser tab.
	// Traffic accounting currently examines client policy/quota state as well as
	// querying Xray's full stats stream. Run it every two minutes to bound its
	// recurring DB/process cost; live traffic endpoints remain independently polled.
	startWorker(120*time.Second, func() {
		res := server.SyncTrafficFromXray()
		if res.Error != "" {
			slog.Debug("traffic sync", "error", res.Error)
		}
		if res.NeedRegen {
			if err := server.RegenerateAndRestartXray(); err != nil {
				slog.Warn("traffic sync regen", "error", err)
			}
		}
		if res.Disabled > 0 || res.Extended > 0 {
			slog.Info("traffic sync applied", "disabled", res.Disabled, "extended", res.Extended, "touched", res.ClientsTouched)
		}
	})

	// Multi-node traffic delta flush: Agents push samples onto the sync bus;
	// this worker batches them into the same accounting path as local stats.
	// Remote deltas are batched; a 30s flush keeps accounting responsive without
	// waking the DB worker four times per minute when the queue is usually empty.
	startWorker(30*time.Second, func() {
		// Hard deadline so a stuck DB cannot pin this worker forever.
		err := syncbus.WithTimeout(workerCtx, syncbus.Default.FlushTimeout(), func(ctx context.Context) error {
			res := server.FlushRemoteTrafficDeltas()
			if res.Error != "" {
				slog.Debug("remote traffic flush", "error", res.Error)
				return fmt.Errorf("%s", res.Error)
			}
			if res.NeedRegen {
				if err := server.RegenerateAndRestartXray(); err != nil {
					slog.Warn("remote traffic regen", "error", err)
				}
			}
			if res.ClientsTouched > 0 {
				slog.Debug("remote traffic applied", "touched", res.ClientsTouched, "disabled", res.Disabled)
			}
			return nil
		})
		if err != nil {
			slog.Warn("remote traffic flush timed out or failed", "error", err)
		}
	})

	// usage_snapshots gets one row per client per hour; without retention the
	// table grows forever on large deployments. Prunes in small batches.
	startWorker(6*time.Hour, func() {
		if _, err := database.PruneUsageSnapshots(db, database.SnapshotRetentionDays()); err != nil {
			slog.Warn("usage snapshot retention failed", "error", err)
		}
	})

	// Prune stale in-memory node registry entries (no DB).
	startWorker(2*time.Minute, func() {
		if n := syncbus.Default.PruneStaleNodes(10 * time.Minute); n > 0 {
			slog.Debug("syncbus pruned stale nodes", "count", n)
		}
	})

	// Preserve the original startup behavior: perform one domain/certificate
	// check immediately, then keep subsequent maintenance off the request path.
	automation.CheckDomains(db)
	automation.RenewCertificates(db)
	// Domain health checks perform network probes and database writes. Five
	// minutes is sufficient for failover detection and avoids repeatedly
	// probing every configured domain on small VPS instances.
	startWorker(5*time.Minute, func() {
		automation.CheckDomains(db)
	})

	// Certbot already checks certificate renewal eligibility itself. Running
	// it every two minutes when a certificate is within 30 days of expiry
	// needlessly spawns subprocesses and can contend on certbot's lock. Check
	// on startup above, then periodically at a conservative six-hour interval.
	startWorker(6*time.Hour, func() {
		automation.RenewCertificates(db)
	})

	// Recovery is intentionally rate-limited. If Xray is misconfigured or a
	// binary is missing, retrying a full config generation/validation/restart
	// every 30 seconds creates a persistent CPU/process storm. Back off up to
	// five minutes after failures and reset the delay after a successful repair.
	startWorker(30*time.Second, func() {
		if !server.AutoRecoveryEnabled() {
			return
		}
		xrayStatus := server.Xray.Status()
		singboxStatus := server.SingBox.Status()
		needsRecovery := !xrayStatus.Running ||
			(singboxStatus.Installed && !singboxStatus.Running && fileExists(cfg.SingBoxConfigPath))
		if !needsRecovery || !recoveryRetryDue() {
			return
		}
		if err := server.RegenerateAndRestartXray(); err != nil {
			nextRecoveryDelay(err)
			slog.Error("auto-recovery failed; retry delayed", "error", err)
			return
		}

		// A nil return only means the apply operation completed; it does not
		// prove the child processes stayed alive after their startup grace period.
		// Verify both configured engines before resetting backoff, otherwise a
		// rapidly exiting engine can trigger config generation/restart every 30s.
		xrayNow := server.Xray.Status()
		singboxNow := server.SingBox.Status()
		singboxRequired := singboxNow.Installed && fileExists(cfg.SingBoxConfigPath)
		if !xrayNow.Running || (singboxRequired && !singboxNow.Running) {
			err := fmt.Errorf("proxy process still not running after recovery (xray=%t, sing-box=%t)", xrayNow.Running, singboxNow.Running)
			nextRecoveryDelay(err)
			slog.Error("auto-recovery incomplete; retry delayed", "error", err)
			return
		}
		resetRecoveryDelay()
		slog.Info("auto-recovery restored proxy engines")
	})

	// Interactive Telegram bots (admin + user). Tokens/IDs from panel settings DB.
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		(&telegram.AdminBot{DB: db, Cfg: cfg, AfterClientChange: func() { _ = server.RegenerateAndRestartXray() }}).Run(workerCtx)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		(&telegram.UserBot{DB: db, AfterClientChange: func() { _ = server.RegenerateAndRestartXray() }}).Run(workerCtx)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunExpiryNotices(workerCtx, db)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunPeriodicReports(workerCtx, db)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunSecurityMonitor(workerCtx, db)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunSatisfactionSurveys(workerCtx, db)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunAutoRenew(workerCtx, db, func() { _ = server.RegenerateAndRestartXray() })
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		telegram.RunWinbackCampaigns(workerCtx, db)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		hpSrv.Start(workerCtx)
	}()
	// Reality dest pool: health probe only (never auto-rotate keys or force-apply dest).
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		obfuscation.SeedDefaults(db)
		obfuscation.HealthCheckAll(db)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				c, h := obfuscation.HealthCheckAll(db)
				slog.Info("reality dest pool health", "checked", c, "healthy", h)
			}
		}
	}()

	bootstrapPanelCertificate(db, cfg)
	router := api.NewRouter(server, "./web")

	addr := fmt.Sprintf("%s:%d", cfg.PanelBindAddr, cfg.PanelPort)
	slog.Info("panel listening", "addr", addr, "base_path", cfg.PanelBasePath, "tls", cfg.PanelTLSEnabled)
	// Explicit timeouts prevent slow clients from holding HTTP connections
	// forever while keeping normal dashboard/API requests comfortable.
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErr := make(chan error, 1)
	go func() {
		if cfg.PanelTLSEnabled {
			if cfg.PanelTLSCertPath == "" || cfg.PanelTLSKeyPath == "" {
				serverErr <- fmt.Errorf("panel TLS enabled but certificate/key path is missing")
				return
			}
			serverErr <- httpServer.ListenAndServeTLS(cfg.PanelTLSCertPath, cfg.PanelTLSKeyPath)
			return
		}
		serverErr <- httpServer.ListenAndServe()
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	var serverExitErr error
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverExitErr = err
			slog.Error("server exited", "error", err)
		}
	case received := <-sig:
		slog.Info("shutting down gracefully", "signal", received)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("HTTP shutdown failed", "error", err)
		}
		cancel()
	}

	// Stop all background work before the process exits. Cancellation alone is
	// not sufficient for a production shutdown: wait for every worker to leave
	// its ticker loop, then terminate child proxy processes so systemd never
	// observes orphaned xray/sing-box children during a restart.
	stopWorkers()
	workersDone := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
		slog.Info("background workers stopped")
	case <-time.After(10 * time.Second):
		slog.Warn("timed out waiting for background workers to stop")
	}
	if err := server.Xray.Stop(); err != nil && !strings.Contains(err.Error(), "not running") {
		slog.Warn("xray shutdown failed", "error", err)
	}
	if err := server.SingBox.Stop(); err != nil && !strings.Contains(err.Error(), "not running") {
		slog.Warn("sing-box shutdown failed", "error", err)
	}
	if serverExitErr != nil {
		os.Exit(1)
	}
}

func runWorker(ctx context.Context, interval time.Duration, fn func()) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}

var recoveryStateMu sync.Mutex
var recoveryRetryAt time.Time
var recoveryDelay = 30 * time.Second

func recoveryRetryDue() bool {
	recoveryStateMu.Lock()
	defer recoveryStateMu.Unlock()
	return recoveryRetryAt.IsZero() || !time.Now().Before(recoveryRetryAt)
}

func nextRecoveryDelay(_ error) {
	recoveryStateMu.Lock()
	defer recoveryStateMu.Unlock()
	recoveryRetryAt = time.Now().Add(recoveryDelay)
	recoveryDelay *= 2
	if recoveryDelay > 5*time.Minute {
		recoveryDelay = 5 * time.Minute
	}
}

func resetRecoveryDelay() {
	recoveryStateMu.Lock()
	recoveryRetryAt = time.Time{}
	recoveryDelay = 30 * time.Second
	recoveryStateMu.Unlock()
}

func maxInt(v, min int) int {
	if v < min {
		return min
	}
	return v
}

func bootstrapPanelCertificate(db *gorm.DB, cfg *config.Config) {
	if !cfg.PanelTLSEnabled || cfg.PanelTLSCertPath == "" || cfg.PanelTLSKeyPath == "" {
		return
	}
	certBytes, err := os.ReadFile(cfg.PanelTLSCertPath)
	if err != nil {
		slog.Error("cannot read panel certificate", "error", err)
		return
	}
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		// PEM decoding is required for normal Let's Encrypt fullchain.pem files.
		block, _ := pem.Decode(certBytes)
		if block == nil {
			slog.Error("cannot parse panel certificate PEM", "error", err)
			return
		}
		cert, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			slog.Error("cannot parse panel certificate", "error", err)
			return
		}
	}
	domain := cfg.PanelTLSHost
	if domain == "" {
		domain = cert.Subject.CommonName
	}
	issuer := cert.Issuer.CommonName
	row := models.CertificateRecord{}
	q := db.Where("cert_path = ?", cfg.PanelTLSCertPath).First(&row)
	if errors.Is(q.Error, gorm.ErrRecordNotFound) {
		row = models.CertificateRecord{Name: "Panel TLS", Domain: domain, Issuer: issuer, ExpiresAt: &cert.NotAfter, AutoRenew: true, CertPath: cfg.PanelTLSCertPath, KeyPath: cfg.PanelTLSKeyPath, Status: "active"}
		db.Create(&row)
	} else if q.Error == nil {
		row.Domain, row.Issuer, row.ExpiresAt, row.Status = domain, issuer, &cert.NotAfter, "active"
		db.Save(&row)
	}
	_ = automation.SyncCertificateRevision(db, &row)
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
