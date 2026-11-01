package core

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/netinfo"
)

var adaptMu sync.Mutex

// PublicHostStatus is returned to the panel UI / API.
type PublicHostStatus struct {
	DetectedV4           string `json:"detectedV4"`
	DetectedV6           string `json:"detectedV6"`
	PublicHost           string `json:"publicHost"`
	PublicHostV4         string `json:"publicHostV4"`
	PreviousPublicHostV4 string `json:"previousPublicHostV4,omitempty"`
	PublicHostV6         string `json:"publicHostV6"`
	PublicHostDomain     string `json:"publicHostDomain"`
	AutoIPEnabled        bool   `json:"autoIPEnabled"`
	LastAdaptedAt        string `json:"lastAdaptedAt,omitempty"`
	Adapted              bool   `json:"adapted"`
	Message              string `json:"message,omitempty"`
}

// AdaptPublicIPIfNeeded detects the current outbound IP and, when the
// configured public host fields are empty or still hold a *previous IP*,
// rewrites them to the new IP. Domain fields are never modified.
//
// Typical case: VPS IP was filtered → operator assigns a new IP → reboot
// panel → share links and subscription hosts pick up the new IP automatically.
func (s *Server) AdaptPublicIPIfNeeded() PublicHostStatus {
	st := PublicHostStatus{
		DetectedV4:    netinfo.DetectOutboundIPv4(),
		DetectedV6:    netinfo.DetectOutboundIPv6(),
		AutoIPEnabled: netinfo.AutoPublicIPEnabled(),
	}
	if s == nil || s.Config == nil {
		st.Message = "server not ready"
		return st
	}
	st.PublicHost = s.Config.PublicHost
	st.PublicHostV4 = s.Config.PublicHostV4
	st.PreviousPublicHostV4 = s.Config.PreviousPublicHostV4
	st.PublicHostV6 = s.Config.PublicHostV6
	st.PublicHostDomain = s.Config.PublicHostDomain

	if !st.AutoIPEnabled {
		st.Message = "auto public IP disabled (ELECTRU_AUTO_PUBLIC_IP=0)"
		return st
	}

	adaptMu.Lock()
	defer adaptMu.Unlock()

	changed := false
	oldV4ForURLs := strings.TrimSpace(s.Config.PublicHostV4)
	if ip := net.ParseIP(oldV4ForURLs); ip == nil || ip.To4() == nil {
		oldV4ForURLs = ""
		if ip := net.ParseIP(strings.TrimSpace(s.Config.PublicHost)); ip != nil && ip.To4() != nil {
			oldV4ForURLs = ip.To4().String()
		}
	}
	// Keep the immediately previous IPv4 so refreshed subscriptions can expose
	// both the new endpoint and the previous endpoint as separate entries.
	// This is useful during migration, although the old endpoint only works if
	// it remains routed/reachable.
	if st.DetectedV4 != "" {
		oldV4 := strings.TrimSpace(s.Config.PublicHostV4)
		if ip := net.ParseIP(oldV4); ip == nil || ip.To4() == nil {
			oldV4 = ""
			if ip := net.ParseIP(strings.TrimSpace(s.Config.PublicHost)); ip != nil && ip.To4() != nil {
				oldV4 = strings.TrimSpace(s.Config.PublicHost)
			}
		}
		if oldV4 != "" && oldV4 != st.DetectedV4 && oldV4 != s.Config.PreviousPublicHostV4 {
			s.Config.PreviousPublicHostV4 = oldV4
			changed = true
		}
	}
	// Primary host: only auto-update when empty or currently an IP literal.
	if st.DetectedV4 != "" {
		if s.Config.PublicHost == "" || netinfo.IsIP(s.Config.PublicHost) {
			if s.Config.PublicHost != st.DetectedV4 {
				s.Config.PublicHost = st.DetectedV4
				changed = true
			}
		}
		v4 := net.ParseIP(strings.TrimSpace(s.Config.PublicHostV4))
		if s.Config.PublicHostV4 == "" || (v4 != nil && v4.To4() != nil) {
			if s.Config.PublicHostV4 != st.DetectedV4 {
				s.Config.PublicHostV4 = st.DetectedV4
				changed = true
			}
		}
	}
	if st.DetectedV6 != "" {
		if s.Config.PublicHostV6 == "" || netinfo.IsIP(s.Config.PublicHostV6) {
			if s.Config.PublicHostV6 != st.DetectedV6 {
				s.Config.PublicHostV6 = st.DetectedV6
				changed = true
			}
		}
	}

	// If an explicitly configured subscription base URL embeds the old IPv4,
	// migrate only that literal-IP URL. Domain-based bases are intentionally
	// preserved because they are the stable option for IP migrations.
	urlUpdates := updateSubscriptionBaseURLs(s.Config, oldV4ForURLs, s.Config.PublicHostV4)
	if len(urlUpdates) > 0 {
		changed = true
	}

	st.PublicHost = s.Config.PublicHost
	st.PublicHostV4 = s.Config.PublicHostV4
	st.PreviousPublicHostV4 = s.Config.PreviousPublicHostV4
	st.PublicHostV6 = s.Config.PublicHostV6
	st.Adapted = changed

	if !changed {
		st.Message = "public host already matches detected IP (or is a domain — left untouched)"
		return st
	}

	// Persist to env file so reboot keeps the new IP.
	envUpdates := map[string]string{
		"ELECTRU_PUBLIC_HOST":             s.Config.PublicHost,
		"ELECTRU_PUBLIC_HOST_V4":          s.Config.PublicHostV4,
		"ELECTRU_PREVIOUS_PUBLIC_HOST_V4": s.Config.PreviousPublicHostV4,
		"ELECTRU_PUBLIC_HOST_V6":          s.Config.PublicHostV6,
	}
	for k, v := range urlUpdates {
		envUpdates[k] = v
	}
	if err := upsertEnvKeys(s.Config.EnvFilePath, envUpdates); err != nil {
		slog.Warn("auto public IP: env update failed", "error", err)
		st.Message = "updated in-memory; env file write failed: " + err.Error()
	} else {
		st.Message = "auto-adapted public IP to " + s.Config.PublicHost
	}
	// Mirror into panel settings for UI consumers.
	if s.DB != nil {
		_ = s.DB.Where("key = ?", "settings.publicHost").Assign(models.PanelSetting{Key: "settings.publicHost", Value: s.Config.PublicHost}).FirstOrCreate(&models.PanelSetting{})
		_ = s.DB.Where("key = ?", "public_host").Assign(models.PanelSetting{Key: "public_host", Value: s.Config.PublicHost}).FirstOrCreate(&models.PanelSetting{})
	}
	st.LastAdaptedAt = time.Now().UTC().Format(time.RFC3339)
	slog.Info("public host auto-adapted", "v4", s.Config.PublicHostV4, "v6", s.Config.PublicHostV6, "host", s.Config.PublicHost)
	return st
}

// SetPublicIPv4 manually overrides the generated-link IPv4. Manual mode disables
// auto-detection until the operator explicitly re-enables it from the panel.
func (s *Server) SetPublicIPv4(raw string) error {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("a valid IPv4 address is required")
	}
	value := ip.To4().String()
	adaptMu.Lock()
	defer adaptMu.Unlock()
	old := strings.TrimSpace(s.Config.PublicHostV4)
	oldIP := net.ParseIP(old)
	if oldIP == nil || oldIP.To4() == nil {
		old = ""
		if primaryIP := net.ParseIP(strings.TrimSpace(s.Config.PublicHost)); primaryIP != nil && primaryIP.To4() != nil {
			old = strings.TrimSpace(s.Config.PublicHost)
		}
	}
	if old != "" && old != value {
		s.Config.PreviousPublicHostV4 = old
	}
	urlUpdates := updateSubscriptionBaseURLs(s.Config, old, value)
	s.Config.PublicHostV4 = value
	if s.Config.PublicHost == "" || netinfo.IsIP(s.Config.PublicHost) {
		s.Config.PublicHost = value
	}
	envUpdates := map[string]string{
		"ELECTRU_PUBLIC_HOST":             s.Config.PublicHost,
		"ELECTRU_PUBLIC_HOST_V4":          value,
		"ELECTRU_PREVIOUS_PUBLIC_HOST_V4": s.Config.PreviousPublicHostV4,
		"ELECTRU_AUTO_PUBLIC_IP":          "0",
	}
	for k, v := range urlUpdates {
		envUpdates[k] = v
	}
	if err := upsertEnvKeys(s.Config.EnvFilePath, envUpdates); err != nil {
		return err
	}
	_ = os.Setenv("ELECTRU_AUTO_PUBLIC_IP", "0")
	return nil
}

// updateSubscriptionBaseURLs rewrites only configured URL hosts that are exactly
// the previous IPv4 literal. Domain-based URLs and unrelated hosts are untouched.
func updateSubscriptionBaseURLs(cfg *config.Config, oldIP, newIP string) map[string]string {
	updates := map[string]string{}
	if cfg == nil || oldIP == "" || newIP == "" || oldIP == newIP {
		return updates
	}
	entries := map[string]*string{
		"ELECTRU_SUBSCRIPTION_BASE_URL":    &cfg.SubscriptionBaseURL,
		"ELECTRU_SUBSCRIPTION_BASE_URL_V4": &cfg.SubscriptionBaseURLV4,
	}
	for key, value := range entries {
		parsed, err := url.Parse(strings.TrimSpace(*value))
		if err != nil || parsed.Scheme == "" || parsed.Hostname() != oldIP {
			continue
		}
		port := parsed.Port()
		if port != "" {
			parsed.Host = net.JoinHostPort(newIP, port)
		} else {
			parsed.Host = newIP
		}
		*value = strings.TrimRight(parsed.String(), "/")
		updates[key] = *value
	}
	return updates
}

// SetAutoPublicIP toggles automatic public IPv4/IPv6 adaptation at runtime and
// persists the choice for subsequent service starts.
func (s *Server) SetAutoPublicIP(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	if err := upsertEnvKeys(s.Config.EnvFilePath, map[string]string{"ELECTRU_AUTO_PUBLIC_IP": value}); err != nil {
		return err
	}
	_ = os.Setenv("ELECTRU_AUTO_PUBLIC_IP", value)
	return nil
}

// SetPublicDomain sets only the domain endpoint (manual operator action when
// the previous domain is filtered). IP fields are left alone.
func (s *Server) SetPublicDomain(domain string) error {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Trim(domain, "/")
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	if netinfo.IsIP(domain) {
		return fmt.Errorf("use auto-IP for addresses; domain must be a hostname")
	}
	adaptMu.Lock()
	defer adaptMu.Unlock()
	s.Config.PublicHostDomain = domain
	// If primary host was a domain, update it too; if it was an IP keep IP for dual setups.
	if s.Config.PublicHost != "" && !netinfo.IsIP(s.Config.PublicHost) {
		s.Config.PublicHost = domain
	}
	_ = upsertEnvKeys(s.Config.EnvFilePath, map[string]string{
		"ELECTRU_PUBLIC_HOST_DOMAIN": domain,
		"ELECTRU_PUBLIC_HOST":        s.Config.PublicHost,
	})
	if s.DB != nil {
		_ = s.DB.Where("key = ?", "settings.publicHostDomain").Assign(models.PanelSetting{Key: "settings.publicHostDomain", Value: domain}).FirstOrCreate(&models.PanelSetting{})
	}
	return nil
}

// upsertEnvKeys updates or appends KEY=value lines in an env file (0600).
func upsertEnvKeys(path string, kv map[string]string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("env path empty")
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	}
	seen := map[string]bool{}
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		eq := strings.IndexByte(trim, '=')
		if eq <= 0 {
			continue
		}
		k := trim[:eq]
		if v, ok := kv[k]; ok {
			lines[i] = k + "=" + v
			seen[k] = true
		}
	}
	for k, v := range kv {
		if !seen[k] {
			lines = append(lines, k+"="+v)
		}
	}
	content := strings.Join(lines, "\n") + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
