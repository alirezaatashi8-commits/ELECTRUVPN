// Package core holds the Server type shared by internal/api (routing) and
// internal/api/handlers (request handling), so neither of those two
// packages needs to import the other -- avoiding an import cycle while
// still giving every handler a single shared db/config/xray handle.
package core

import (
	"encoding/json"
	"fmt"
	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/singbox"
	"github.com/electruvpn/electruvpn/internal/xray"
	"gorm.io/gorm"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Server bundles everything a handler needs. It's passed by reference into
// every handlers.* constructor so there's exactly one place that owns the
// db connection, config, and the running xray.Manager.
type Server struct {
	DB           *gorm.DB
	Config       *config.Config
	Xray         *xray.Manager
	SingBox      *singbox.Manager
	recoveryMu   sync.RWMutex
	autoRecovery bool
	regenerateMu sync.Mutex

	xrayMetaMu        sync.Mutex
	lastXrayPid       int
	lastXrayStartedAt time.Time
	trafficSyncMu     sync.Mutex // single-flight: one traffic sync at a time

	live liveRefresher // shared live-stats refresh (see live.go)
}

func NewServer(db *gorm.DB, cfg *config.Config) *Server {
	// Loopback Clash API for live sing-box stats; the secret is derived from the
	// panel JWT secret so it survives panel restarts.
	singbox.ConfigureLiveAPI(cfg.SingBoxAPIPort, cfg.JWTSecret)
	return &Server{
		DB:           db,
		Config:       cfg,
		Xray:         xray.NewManager(cfg.XrayBinaryPath, cfg.XrayConfigPath),
		SingBox:      singbox.NewManager(cfg.SingBoxBinaryPath, cfg.SingBoxConfigPath),
		autoRecovery: true,
	}
}

func (s *Server) SetAutoRecovery(enabled bool) {
	s.recoveryMu.Lock()
	s.autoRecovery = enabled
	s.recoveryMu.Unlock()
}
func (s *Server) AutoRecoveryEnabled() bool {
	s.recoveryMu.RLock()
	defer s.recoveryMu.RUnlock()
	return s.autoRecovery
}

// EffectiveDNSResolvers returns the operator-selected DNS resolver list.
// Panel setting "dns_provider" (cloudflare|google|quad9|system|custom|auto)
// overrides env ELECTRU_DNS_RESOLVERS when set. Custom list comes from
// "dns_custom_resolvers". Traffic/DNS accounting remains independent of this.
func (s *Server) EffectiveDNSResolvers() []string {
	if s == nil || s.DB == nil {
		if s != nil && s.Config != nil && len(s.Config.DNSResolvers) > 0 {
			return s.Config.DNSResolvers
		}
		return config.DNSPresetResolvers(config.DNSProviderAuto)
	}
	var row models.PanelSetting
	provider := config.DNSProviderAuto
	if s.DB.Where("key = ?", config.SettingDNSProvider).First(&row).Error == nil && strings.TrimSpace(row.Value) != "" {
		provider = config.NormalizeDNSProvider(row.Value)
	}
	if provider == config.DNSProviderCustom {
		var custom models.PanelSetting
		if s.DB.Where("key = ?", config.SettingDNSCustomResolvers).First(&custom).Error == nil {
			if list := config.ParseCustomResolvers(custom.Value); len(list) > 0 {
				return list
			}
		}
		// fallback if custom empty
		if s.Config != nil && len(s.Config.DNSResolvers) > 0 {
			return s.Config.DNSResolvers
		}
		return config.DNSPresetResolvers(config.DNSProviderAuto)
	}
	if provider == config.DNSProviderAuto && s.Config != nil && len(s.Config.DNSResolvers) > 0 {
		// Keep env list as primary when operator left provider on auto
		return s.Config.DNSResolvers
	}
	return config.DNSPresetResolvers(provider)
}

// RegenerateAndRestartXray rebuilds config.json from the current database
// state and restarts xray-core so the change takes effect. Every handler
// that adds/edits/deletes an inbound or client calls this at the end, so
// the running proxy server is always in sync with what the panel shows.
func (s *Server) RegenerateAndRestartXray() error {
	// Config generation/restart is expensive and must be serialized. Without
	// this guard, two rapid admin changes or an auto-recovery tick can validate
	// and restart the engines concurrently.
	s.regenerateMu.Lock()
	defer s.regenerateMu.Unlock()

	var inbounds []models.Inbound
	if err := s.DB.Preload("Clients").Find(&inbounds).Error; err != nil {
		return err
	}
	// Inject enabled VLESS config profiles into their target inbounds. A profile
	// reuses the original client's UUID/quota/expiry and never creates a second
	// billing identity.
	var profiles []models.ClientConfigProfile
	if err := s.DB.Where("enabled = ?", true).Order("priority asc, id asc").Find(&profiles).Error; err != nil {
		return err
	}
	clientsByID := make(map[uint]models.Client)
	for _, ib := range inbounds {
		for _, cl := range ib.Clients {
			clientsByID[cl.ID] = cl
		}
	}
	for _, pr := range profiles {
		cl, ok := clientsByID[pr.ClientID]
		if !ok || !cl.Enable || cl.IsExpired() || cl.IsOverQuota() {
			continue
		}
		for i := range inbounds {
			if inbounds[i].ID != pr.InboundID || inbounds[i].Protocol != models.ProtocolVLESS {
				continue
			}
			clone := cl
			if pr.Name != "" {
				clone.Email = fmt.Sprintf("%s#profile-%d", cl.Email, pr.ID)
			}
			inbounds[i].Clients = append(inbounds[i].Clients, clone)
			break
		}
	}

	var xrayInbounds []models.Inbound
	var sbInbounds []models.Inbound
	for _, ib := range inbounds {
		switch ib.Protocol {
		case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
			sbInbounds = append(sbInbounds, ib)
		default:
			xrayInbounds = append(xrayInbounds, ib)
		}
	}

	xcfg, err := xray.GenerateWithDNS(xrayInbounds, s.Config.XrayAPIPort, s.EffectiveDNSResolvers())
	if err != nil {
		return err
	}
	xray.ApplyEnabledChain(xcfg, s.DB)
	// Build and validate every active engine config before replacing or
	// restarting anything. This makes a bad edit fail safely instead of
	// leaving the running service with a broken configuration.
	xrayBytes, err := json.MarshalIndent(xcfg, "", "  ")
	if err != nil {
		return err
	}
	if err := validateExternalConfig(s.Config.XrayBinaryPath, "xray", xrayBytes); err != nil {
		return err
	}

	var sbBytes []byte
	if len(sbInbounds) > 0 {
		if !s.SingBox.Installed() {
			return fmt.Errorf("sing-box is required for %d inbound(s) but is not installed", len(sbInbounds))
		}
		sbBytes, err = s.generateValidatedSingBox(sbInbounds)
		if err != nil {
			return err
		}
	}

	// Keep the previous runtime files so a failure in either engine can be
	// rolled back together. Config files are written atomically by the managers.
	oldXray, oldXrayExists, err := readFileState(s.Config.XrayConfigPath)
	if err != nil {
		return err
	}
	oldSB, oldSBExists, err := readFileState(s.Config.SingBoxConfigPath)
	if err != nil {
		return err
	}
	oldSBRunning := s.SingBox.Status().Running

	if err := s.Xray.WriteConfig(xcfg); err != nil {
		return err
	}
	if len(sbInbounds) > 0 {
		if err := s.SingBox.WriteConfig(json.RawMessage(sbBytes)); err != nil {
			_ = restoreFileState(s.Config.XrayConfigPath, oldXray, oldXrayExists)
			return err
		}
	}

	if err := s.Xray.Restart(); err != nil {
		_ = restoreFileState(s.Config.XrayConfigPath, oldXray, oldXrayExists)
		_ = s.Xray.Restart()
		if len(sbInbounds) > 0 {
			_ = restoreFileState(s.Config.SingBoxConfigPath, oldSB, oldSBExists)
		}
		return fmt.Errorf("xray restart failed; runtime rolled back: %w", err)
	}

	if len(sbInbounds) > 0 {
		if err := s.SingBox.Restart(); err != nil {
			_ = restoreFileState(s.Config.XrayConfigPath, oldXray, oldXrayExists)
			_ = restoreFileState(s.Config.SingBoxConfigPath, oldSB, oldSBExists)
			_ = s.Xray.Restart()
			if oldSBRunning {
				_ = s.SingBox.Restart()
			} else {
				_ = s.SingBox.Stop()
			}
			return fmt.Errorf("sing-box restart failed; runtime rolled back: %w", err)
		}
	} else {
		if s.SingBox.Status().Running {
			if err := s.SingBox.Stop(); err != nil {
				_ = restoreFileState(s.Config.XrayConfigPath, oldXray, oldXrayExists)
				_ = s.Xray.Restart()
				return fmt.Errorf("sing-box stop failed; runtime rolled back: %w", err)
			}
		}
		if err := os.Remove(s.Config.SingBoxConfigPath); err != nil && !os.IsNotExist(err) {
			_ = restoreFileState(s.Config.XrayConfigPath, oldXray, oldXrayExists)
			_ = s.Xray.Restart()
			if oldSBRunning {
				_ = restoreFileState(s.Config.SingBoxConfigPath, oldSB, oldSBExists)
				_ = s.SingBox.Restart()
			}
			return fmt.Errorf("could not remove stale sing-box config; runtime rolled back: %w", err)
		}
	}
	return nil
}

// ValidateInboundConfig builds and validates only the supplied inbound. It never
// writes a live config or restarts an engine, making it safe for the Inbounds
// wizard's "test before apply" and live preview actions.
func (s *Server) ValidateInboundConfig(ib models.Inbound) (string, []byte, error) {
	// Validate the configuration itself: an inbound that is currently over its
	// own volume/time limit must still be fully checked instead of being
	// silently skipped by the config builders.
	ib.TotalGB, ib.ExpiryTime = 0, 0
	if ib.Protocol == models.ProtocolHysteria2 || ib.Protocol == models.ProtocolTUIC || ib.Protocol == models.ProtocolNaive || ib.Protocol == models.ProtocolWireGuard || ib.Protocol == models.ProtocolAnyTLS || ib.Protocol == models.ProtocolShadowTLS {
		if !s.SingBox.Installed() {
			return "sing-box", nil, fmt.Errorf("sing-box is required for protocol %q but is not installed", ib.Protocol)
		}
		data, err := s.generateValidatedSingBox([]models.Inbound{ib})
		if err != nil {
			return "sing-box", data, err
		}
		return "sing-box", data, nil
	}
	cfg, err := xray.GenerateWithDNS([]models.Inbound{ib}, s.Config.XrayAPIPort, s.EffectiveDNSResolvers())
	if err != nil {
		return "xray", nil, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "xray", nil, err
	}
	if err := validateExternalConfig(s.Config.XrayBinaryPath, "xray", data); err != nil {
		return "xray", data, err
	}
	return "xray", data, nil
}

func validateExternalConfig(binary, engine string, data []byte) error {
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("%s binary unavailable: %w", engine, err)
	}
	tmp, err := os.CreateTemp("", "electruvpn-validate-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if engine == "sing-box" {
		cmd = exec.Command(binary, "check", "-c", name)
	} else {
		cmd = exec.Command(binary, "run", "-test", "-config", name)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s config validation failed: %s", engine, string(out))
	}
	return nil
}

func readFileState(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return b, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func restoreFileState(path string, data []byte, exists bool) error {
	if !exists {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, data, 0640)
}

// ObserveXrayStatus tracks process identity; returns true when pid/start time changed
// (i.e. xray was restarted since the previous observation).
func (s *Server) ObserveXrayStatus(st xray.Status) bool {
	s.xrayMetaMu.Lock()
	defer s.xrayMetaMu.Unlock()
	if !st.Running {
		return false
	}
	restarted := false
	if s.lastXrayPid != 0 {
		if st.Pid != s.lastXrayPid || (!s.lastXrayStartedAt.IsZero() && !st.StartedAt.Equal(s.lastXrayStartedAt)) {
			restarted = true
		}
	}
	s.lastXrayPid = st.Pid
	s.lastXrayStartedAt = st.StartedAt
	return restarted
}

// SettingString reads a PanelSetting value or returns def when missing.
func (s *Server) SettingString(key, def string) string {
	var r models.PanelSetting
	if s.DB.Where("key = ?", key).First(&r).Error != nil || r.Value == "" {
		return def
	}
	return r.Value
}

// LiveUserApplyEnabled is true when the operator enabled flag_liveUserApply
// in Settings Center. When enabled, pure client add/remove tries Xray
// HandlerService first and skips process restart on success.
func (s *Server) LiveUserApplyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(s.SettingString("settings.flag_liveUserApply", "false")))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

// ExperimentalLoadBalancerEnabled tightens default node selection strategy
// when the experimental load-balancer feature flag is on.
func (s *Server) ExperimentalLoadBalancerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(s.SettingString("settings.flag_experimentalLoadBalancer", "false")))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

// RegenerateConfigOnly rebuilds and writes engine configs from the database
// without restarting processes. Used after a successful live user apply so
// the on-disk config stays consistent for the next cold start.
func (s *Server) RegenerateConfigOnly() error {
	s.regenerateMu.Lock()
	defer s.regenerateMu.Unlock()

	var inbounds []models.Inbound
	if err := s.DB.Preload("Clients").Find(&inbounds).Error; err != nil {
		return err
	}
	var profiles []models.ClientConfigProfile
	if err := s.DB.Where("enabled = ?", true).Order("priority asc, id asc").Find(&profiles).Error; err != nil {
		return err
	}
	clientsByID := make(map[uint]models.Client)
	for _, ib := range inbounds {
		for _, cl := range ib.Clients {
			clientsByID[cl.ID] = cl
		}
	}
	for _, pr := range profiles {
		cl, ok := clientsByID[pr.ClientID]
		if !ok || !cl.Enable || cl.IsExpired() || cl.IsOverQuota() {
			continue
		}
		for i := range inbounds {
			if inbounds[i].ID != pr.InboundID || inbounds[i].Protocol != models.ProtocolVLESS {
				continue
			}
			clone := cl
			if pr.Name != "" {
				clone.Email = fmt.Sprintf("%s#profile-%d", cl.Email, pr.ID)
			}
			inbounds[i].Clients = append(inbounds[i].Clients, clone)
			break
		}
	}

	var xrayInbounds []models.Inbound
	var sbInbounds []models.Inbound
	for _, ib := range inbounds {
		switch ib.Protocol {
		case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
			sbInbounds = append(sbInbounds, ib)
		default:
			xrayInbounds = append(xrayInbounds, ib)
		}
	}

	xcfg, err := xray.GenerateWithDNS(xrayInbounds, s.Config.XrayAPIPort, s.EffectiveDNSResolvers())
	if err != nil {
		return err
	}
	xray.ApplyEnabledChain(xcfg, s.DB)
	if err := s.Xray.WriteConfig(xcfg); err != nil {
		return err
	}
	if len(sbInbounds) > 0 {
		sbcfg, err := singbox.GenerateWithDNS(sbInbounds, s.EffectiveDNSResolvers())
		if err != nil {
			return err
		}
		sbBytes, err := json.MarshalIndent(sbcfg, "", "  ")
		if err != nil {
			return err
		}
		if err := s.SingBox.WriteConfig(json.RawMessage(sbBytes)); err != nil {
			return err
		}
	}
	return nil
}

// TryLiveAddClient attempts HandlerService user add for a single Xray-backed
// inbound. Returns true when live apply succeeded (caller should skip restart).
func (s *Server) TryLiveAddClient(ib models.Inbound, cl models.Client) bool {
	if !s.LiveUserApplyEnabled() || !s.Xray.Status().Running {
		return false
	}
	switch ib.Protocol {
	case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
		return false // sing-box paths still require full restart
	}
	tag := strings.TrimSpace(ib.Tag)
	if tag == "" {
		// Match stable tags produced by inbound create / config generation.
		if ib.Port > 0 {
			tag = fmt.Sprintf("inbound-%d", ib.Port)
		} else {
			tag = fmt.Sprintf("inbound-%d", ib.ID)
		}
	}
	user := xray.LiveUser{Email: cl.Email, UUID: cl.UUID, Password: cl.Password, Flow: cl.Flow}
	if err := xray.LiveAddUser(s.Config.XrayBinaryPath, s.Config.XrayAPIPort, tag, user); err != nil {
		return false
	}
	// Disk config must stay in sync; if write fails, force full restart path.
	if err := s.RegenerateConfigOnly(); err != nil {
		return false
	}
	return true
}

// TryLiveRemoveClient attempts HandlerService user remove. Returns true on success.
func (s *Server) TryLiveRemoveClient(email string) bool {
	if !s.LiveUserApplyEnabled() || !s.Xray.Status().Running {
		return false
	}
	if err := xray.LiveRemoveUser(s.Config.XrayBinaryPath, s.Config.XrayAPIPort, email); err != nil {
		return false
	}
	if err := s.RegenerateConfigOnly(); err != nil {
		return false
	}
	return true
}

// generateValidatedSingBox builds and validates the sing-box config. The live
// stats Clash API block is optional: if the config only fails because the
// installed sing-box build rejects that block, the API is switched off (live
// stats for sing-box protocols become unavailable) and the config is accepted
// without it, so a missing build feature can never stop the VPN from starting.
func (s *Server) generateValidatedSingBox(inbounds []models.Inbound) ([]byte, error) {
	build := func() ([]byte, error) {
		cfg, err := singbox.GenerateWithDNS(inbounds, s.EffectiveDNSResolvers())
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(cfg, "", "  ")
	}
	data, err := build()
	if err != nil {
		return nil, err
	}
	verr := validateExternalConfig(s.Config.SingBoxBinaryPath, "sing-box", data)
	if verr == nil {
		return data, nil
	}
	if !singbox.LiveAPIEnabled() {
		return data, verr
	}
	singbox.SetLiveAPIEnabled(false)
	plain, err := build()
	if err == nil && validateExternalConfig(s.Config.SingBoxBinaryPath, "sing-box", plain) == nil {
		slog.Warn("sing-box rejected the live-stats Clash API block; live stats for sing-box protocols are disabled", "error", verr)
		return plain, nil
	}
	singbox.SetLiveAPIEnabled(true) // the failure is unrelated to the live API
	return data, verr
}
