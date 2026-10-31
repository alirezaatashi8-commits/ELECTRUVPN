// Package xray turns ElectruVPN's database rows into a real xray-core
// config.json, and manages the xray-core process itself. Keeping this as
// its own package means the API handlers never touch xray's config format
// directly -- they just call Generate/Apply after any inbound or client
// change.
package xray

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
)

// Config mirrors just the subset of xray-core's config.json that
// ElectruVPN generates. Routing rules and outbounds are intentionally
// minimal (direct + block); advanced routing can be layered on top later.
type Config struct {
	Log       LogConfig      `json:"log"`
	DNS       DNSConfig      `json:"dns,omitempty"`
	Routing   RoutingConfig  `json:"routing,omitempty"`
	API       APIConfig      `json:"api"`
	Stats     struct{}       `json:"stats"`
	Policy    PolicyConfig   `json:"policy"`
	Inbounds  []InboundJSON  `json:"inbounds"`
	Outbounds []OutboundJSON `json:"outbounds"`
}

type LogConfig struct {
	Loglevel string `json:"loglevel"`
}

type APIConfig struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

type PolicyConfig struct {
	Levels map[string]LevelPolicy `json:"levels"`
	System SystemPolicy           `json:"system"`
}

type LevelPolicy struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
	// StatsUserOnline enables the per-user online IP list used by the live
	// "devices connected" counters.
	StatsUserOnline bool `json:"statsUserOnline"`
}

type SystemPolicy struct {
	StatsInboundUplink   bool `json:"statsInboundUplink"`
	StatsInboundDownlink bool `json:"statsInboundDownlink"`
}

type InboundJSON struct {
	Tag            string          `json:"tag"`
	Listen         string          `json:"listen"`
	Port           int             `json:"port"`
	Protocol       string          `json:"protocol"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings json.RawMessage `json:"streamSettings,omitempty"`
	Sniffing       *SniffingJSON   `json:"sniffing,omitempty"`
}

type SniffingJSON struct {
	Enabled         bool     `json:"enabled"`
	DestOverride    []string `json:"destOverride,omitempty"`
	MetadataOnly    bool     `json:"metadataOnly,omitempty"`
	RouteOnly       bool     `json:"routeOnly,omitempty"`
	DomainsExcluded []string `json:"domainsExcluded,omitempty"`
}

type DNSConfig struct {
	Servers       []any  `json:"servers"`
	QueryStrategy string `json:"queryStrategy,omitempty"`
	DisableCache  bool   `json:"disableCache,omitempty"`
	Tag           string `json:"tag,omitempty"`
}

type RoutingConfig struct {
	DomainStrategy string         `json:"domainStrategy"`
	Rules          []RoutingRule  `json:"rules"`
	Balancers      []BalancerJSON `json:"balancers,omitempty"`
}

type RoutingRule struct {
	Type        string   `json:"type,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	Network     string   `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	Source      []string `json:"source,omitempty"`
	User        []string `json:"user,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
}

// BalancerJSON implements Xray load-balancing selectors (2026 multi-outbound).
// Strategy: random | roundRobin | leastLoad | leastPing
type BalancerJSON struct {
	Tag      string            `json:"tag"`
	Selector []string          `json:"selector"`
	Strategy *BalancerStrategy `json:"strategy,omitempty"`
	Fallback string            `json:"fallbackTag,omitempty"`
}

type BalancerStrategy struct {
	Type string `json:"type"` // random | roundRobin | leastLoad | leastPing
}

type OutboundJSON struct {
	Tag            string          `json:"tag"`
	Protocol       string          `json:"protocol"`
	Settings       json.RawMessage `json:"settings,omitempty"`
	StreamSettings json.RawMessage `json:"streamSettings,omitempty"`
	ProxySettings  *ProxySettings  `json:"proxySettings,omitempty"` // for dialerProxy / chaining
	Mux            *MuxSettings    `json:"mux,omitempty"`
}

type ProxySettings struct {
	Tag string `json:"tag"` // dialerProxy tag for multi-hop
}

type MuxSettings struct {
	Enabled     bool `json:"enabled"`
	Concurrency int  `json:"concurrency,omitempty"`
}

// apiInboundPort is a fixed local-only inbound that exposes xray's stats and handler
// gRPC services (StatsService + HandlerService), queried later via the `xray api`
// CLI for traffic and optional live user add/remove. Local-only; not stored in DB.
const apiTag = "api"

// Generate builds a full xray Config from the given inbounds (each with its
// Clients preloaded). apiPort is the local gRPC port used for live stats.
// Default DNS uses multiple encrypted resolvers (Cloudflare + Google + Quad9).
func Generate(inbounds []models.Inbound, apiPort int) (*Config, error) {
	return GenerateWithDNS(inbounds, apiPort, nil)
}

// apiRoutingRule routes the loopback API inbound to Xray's built-in API handler.
func apiRoutingRule() RoutingRule {
	return RoutingRule{Type: "field", InboundTag: []string{apiTag}, OutboundTag: apiTag}
}

// DefaultIranAwareRules returns advanced 2026 GeoIP/GeoSite smart routing rules
// optimised for restricted networks (Iran focus):
// - private ranges blocked
// - Iranian IPs/domains go direct (no hairpin through exit nodes)
// - remaining traffic falls through to freedom / chain-exit / balancer
// Additional smart category rules (AI, payment, gaming) are injected by
// ApplyEnabledChain when a multi-hop / multi-outbound chain is active.
func DefaultIranAwareRules() []RoutingRule {
	return []RoutingRule{
		// Block private / bogon
		{Type: "field", IP: []string{"geoip:private"}, OutboundTag: "block"},
		{Type: "field", Domain: []string{"geosite:private"}, OutboundTag: "block"},

		// Iranian domestic traffic must never hairpin through exit nodes
		{Type: "field", IP: []string{"geoip:ir"}, OutboundTag: "direct"},
		{Type: "field", Domain: []string{
			"geosite:category-ir",
			"regexp:.*\\.ir$",
			"domain:ir",
			"full:digikala.com", "domain:digikala.com",
			"full:snapp.ir", "domain:snapp.ir",
			"full:divar.ir", "domain:divar.ir",
			"full:bankmellat.ir", "domain:bankmellat.ir",
			"full:bmi.ir", "domain:bmi.ir",
			"domain:shaparak.ir", "domain:iran.ir",
		}, OutboundTag: "direct"},
	}
}

// GenerateWithDNS creates a privacy-hardened server config. DNS is resolved by
// Xray over encrypted DNS instead of inheriting the host resolver. Private/local
// destinations are blocked; Iranian destinations are forced direct so traffic
// does not hairpin through the tunnel when the destination is already local.
func GenerateWithDNS(inbounds []models.Inbound, apiPort int, dnsServers []string) (*Config, error) {
	smart := BuildSmartDNSServers(dnsServers)
	cfg := &Config{
		Log: LogConfig{Loglevel: "warning"},
		API: APIConfig{Tag: apiTag, Services: []string{"StatsService", "HandlerService"}},
		Policy: PolicyConfig{
			Levels: map[string]LevelPolicy{"0": {StatsUserUplink: true, StatsUserDownlink: true, StatsUserOnline: true}},
			System: SystemPolicy{StatsInboundUplink: true, StatsInboundDownlink: true},
		},
		DNS: DNSConfig{Servers: smart, QueryStrategy: "UseIP", DisableCache: false, Tag: "electru-dns"},
		Routing: RoutingConfig{
			DomainStrategy: "IPIfNonMatch",
			// The local API inbound MUST be routed to the "api" outbound, and this
			// rule must come first: otherwise the gRPC stats/handler calls made by
			// `xray api ...` are treated as ordinary traffic to 127.0.0.1 and hit
			// the private-range block rule, so every stats query fails and live
			// speeds / online users / traffic counters stay at zero.
			Rules: append([]RoutingRule{apiRoutingRule()}, DefaultIranAwareRules()...),
		},
		Outbounds: []OutboundJSON{
			{Tag: "direct", Protocol: "freedom"},
			{Tag: "block", Protocol: "blackhole"},
		},
	}

	// The local API inbound, dokodemo-door style, only reachable from
	// localhost so the stats port is never exposed to the internet.
	apiSettings, _ := json.Marshal(map[string]any{"address": "127.0.0.1"})
	cfg.Inbounds = append(cfg.Inbounds, InboundJSON{
		Tag:      apiTag,
		Listen:   "127.0.0.1",
		Port:     apiPort,
		Protocol: "dokodemo-door",
		Settings: apiSettings,
	})

	for _, ib := range inbounds {
		if !ib.Enable || ib.IsLimited() {
			continue
		}
		built, err := buildInbound(ib)
		if err != nil {
			return nil, fmt.Errorf("inbound %q (port %d): %w", ib.Remark, ib.Port, err)
		}
		cfg.Inbounds = append(cfg.Inbounds, *built)
	}

	return cfg, nil
}

func buildInbound(ib models.Inbound) (*InboundJSON, error) {
	var settings json.RawMessage
	var err error

	switch ib.Protocol {
	case models.ProtocolVLESS:
		settings, err = buildVLESSSettings(ib)
	case models.ProtocolVMess:
		settings, err = buildVMessSettings(ib)
	case models.ProtocolTrojan:
		settings, err = buildTrojanSettings(ib)
	case models.ProtocolShadowsocks:
		settings, err = buildShadowsocksSettings(ib)
	case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
		return nil, fmt.Errorf("protocol %q is managed by sing-box", ib.Protocol)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", ib.Protocol)
	}
	if err != nil {
		return nil, err
	}

	stream, err := buildStreamSettings(ib)
	if err != nil {
		return nil, err
	}

	tag := strings.TrimSpace(ib.Tag)
	if tag == "" {
		if ib.Port > 0 {
			tag = fmt.Sprintf("inbound-%d", ib.Port)
		} else {
			tag = fmt.Sprintf("inbound-%d", ib.ID)
		}
	}
	return &InboundJSON{
		Tag:            tag,
		Listen:         ib.Listen,
		Port:           ib.Port,
		Protocol:       string(ib.Protocol),
		Settings:       settings,
		StreamSettings: stream,
		Sniffing:       buildSniffing(ib),
	}, nil
}

// buildSniffing merges inbound.Sniffing flag with advanced options from StreamSettings.
func buildSniffing(ib models.Inbound) *SniffingJSON {
	if !ib.Sniffing {
		return &SniffingJSON{Enabled: false}
	}
	out := &SniffingJSON{
		Enabled:      true,
		DestOverride: []string{"http", "tls", "quic"},
		RouteOnly:    true, // 2026 default: sniff for routing without breaking applications
	}
	var stream models.StreamSettings
	if ib.StreamSettings != "" {
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	}
	if stream.Sniffing != nil {
		if len(stream.Sniffing.DestOverride) > 0 {
			out.DestOverride = stream.Sniffing.DestOverride
		}
		out.MetadataOnly = stream.Sniffing.MetadataOnly
		// explicit false in JSON still means use default true only when nil struct;
		// here we honor whatever is set when advanced block present
		out.RouteOnly = stream.Sniffing.RouteOnly
		if stream.Sniffing.DomainsExcluded != nil {
			out.DomainsExcluded = stream.Sniffing.DomainsExcluded
		}
		// if advanced block exists but RouteOnly left false intentionally — OK
		// Re-enable sensible default when destOverride set but neither flag chosen:
		if !stream.Sniffing.MetadataOnly && !stream.Sniffing.RouteOnly && len(stream.Sniffing.DestOverride) == 0 {
			out.RouteOnly = true
		}
	}
	return out
}

func buildVLESSSettings(ib models.Inbound) (json.RawMessage, error) {
	type client struct {
		ID    string `json:"id"`
		Flow  string `json:"flow,omitempty"`
		Email string `json:"email"`
		Level int    `json:"level"`
	}
	var clients []client
	for _, c := range ib.Clients {
		if !c.Enable || c.IsExpired() || c.IsOverQuota() {
			continue
		}
		clients = append(clients, client{ID: c.UUID, Flow: c.Flow, Email: c.Email})
	}
	settings := map[string]any{
		"clients":    clients,
		"decryption": "none",
	}
	if fbs := extractFallbacks(ib); len(fbs) > 0 {
		settings["fallbacks"] = fbs
	}
	return json.Marshal(settings)
}

func buildVMessSettings(ib models.Inbound) (json.RawMessage, error) {
	type client struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Level int    `json:"level"`
	}
	var clients []client
	for _, c := range ib.Clients {
		if !c.Enable || c.IsExpired() || c.IsOverQuota() {
			continue
		}
		clients = append(clients, client{ID: c.UUID, Email: c.Email})
	}
	return json.Marshal(map[string]any{"clients": clients})
}

func buildTrojanSettings(ib models.Inbound) (json.RawMessage, error) {
	type client struct {
		Password string `json:"password"`
		Email    string `json:"email"`
		Level    int    `json:"level"`
	}
	var clients []client
	for _, c := range ib.Clients {
		if !c.Enable || c.IsExpired() || c.IsOverQuota() {
			continue
		}
		clients = append(clients, client{Password: c.Password, Email: c.Email})
	}
	settings := map[string]any{"clients": clients}
	if fbs := extractFallbacks(ib); len(fbs) > 0 {
		settings["fallbacks"] = fbs
	}
	return json.Marshal(settings)
}

func buildShadowsocksSettings(ib models.Inbound) (json.RawMessage, error) {
	var ss models.ShadowsocksSettings
	if ib.Settings != "" {
		_ = json.Unmarshal([]byte(ib.Settings), &ss)
	}
	if ss.Method == "" {
		ss.Method = "2022-blake3-aes-128-gcm"
	}

	type client struct {
		Password string `json:"password"`
		Email    string `json:"email"`
		Method   string `json:"method,omitempty"`
	}
	is2022 := ShadowsocksKeyLength(ss.Method) > 0
	if is2022 && strings.TrimSpace(ss.Password) == "" {
		return nil, fmt.Errorf("Shadowsocks-2022 inbound %q has no server key; open and save the inbound once to generate it", ib.Remark)
	}
	var clients []client
	for _, c := range ib.Clients {
		if !c.Enable || c.IsExpired() || c.IsOverQuota() {
			continue
		}
		cl := client{Password: c.Password, Email: c.Email}
		if !is2022 {
			// Legacy multi-user Shadowsocks: Xray reads the cipher from each client
			// and rejects clients without one ("unknown cipher method").
			cl.Method = ss.Method
		}
		clients = append(clients, cl)
	}
	out := map[string]any{
		"method":  ss.Method,
		"network": "tcp,udp",
		"clients": clients,
	}
	if is2022 {
		out["password"] = ss.Password
	}
	return json.Marshal(out)
}

func buildStreamSettings(ib models.Inbound) (json.RawMessage, error) {
	var stream models.StreamSettings
	if ib.StreamSettings != "" {
		if err := json.Unmarshal([]byte(ib.StreamSettings), &stream); err != nil {
			return nil, fmt.Errorf("invalid streamSettings JSON: %w", err)
		}
	}
	stream.Network = ib.Network
	stream.Security = ib.Security

	out := map[string]any{
		"network":  string(stream.Network),
		"security": string(stream.Security),
	}

	switch stream.Network {
	case models.NetworkXHTTP:
		// 2026 best practice: stream-one under REALITY (direct), auto otherwise (CDN-friendly).
		defaultMode := "auto"
		if stream.Security == models.SecurityReality {
			defaultMode = "stream-one"
		}
		xs := map[string]any{"path": orDefault(stream.XHTTPPath, "/"), "host": stream.XHTTPHost, "mode": orDefault(stream.XHTTPMode, defaultMode)}
		if strings.TrimSpace(stream.XHTTPExtraJSON) != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(stream.XHTTPExtraJSON), &extra); err != nil {
				return nil, fmt.Errorf("invalid XHTTP advanced JSON: %w", err)
			}
			for k, v := range extra {
				xs[k] = v
			}
		}
		out["xhttpSettings"] = xs
	case models.NetworkHTTPUpgrade:
		hu := map[string]any{"path": orDefault(stream.HTTPUpgradePath, "/"), "host": stream.HTTPUpgradeHost, "acceptProxyProtocol": stream.AcceptProxyProto}
		if strings.TrimSpace(stream.HTTPUpgradeHeadersJSON) != "" {
			var headers map[string]string
			if err := json.Unmarshal([]byte(stream.HTTPUpgradeHeadersJSON), &headers); err != nil {
				return nil, fmt.Errorf("invalid HTTPUpgrade headers JSON: %w", err)
			}
			hu["headers"] = headers
		}
		out["httpupgradeSettings"] = hu
	case models.NetworkMKCP:
		out["kcpSettings"] = map[string]any{}
	case models.NetworkWS:
		out["wsSettings"] = map[string]any{"path": orDefault(stream.WSPath, "/"), "host": stream.WSHost}
	case models.NetworkGRPC:
		out["grpcSettings"] = map[string]any{"serviceName": orDefault(stream.GRPCService, "electruvpn")}
	}

	if stream.Security == models.SecurityReality && stream.Network != models.NetworkTCP && stream.Network != models.NetworkXHTTP && stream.Network != models.NetworkGRPC {
		return nil, fmt.Errorf("REALITY is only supported with RAW/TCP, XHTTP, or gRPC transport")
	}

	switch stream.Security {
	case models.SecurityTLS:
		if stream.TLSSettings != nil {
			tls := map[string]any{
				"certificates": []map[string]any{{
					"certificateFile": stream.TLSSettings.CertFile,
					"keyFile":         stream.TLSSettings.KeyFile,
				}},
				"serverName": stream.TLSSettings.SNI,
			}
			if alpn := strings.TrimSpace(stream.TLSSettings.ALPN); alpn != "" {
				parts := strings.Split(alpn, ",")
				clean := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						clean = append(clean, p)
					}
				}
				if len(clean) > 0 {
					tls["alpn"] = clean
				}
			}
			out["tlsSettings"] = tls
		}
	case models.SecurityReality:
		if stream.Reality != nil {
			rs := map[string]any{
				"show":        false,
				"dest":        stream.Reality.Dest,
				"serverNames": stream.Reality.ServerNames,
				"privateKey":  stream.Reality.PrivateKey,
				"shortIds":    stream.Reality.ShortIDs,
			}
			if sx := strings.TrimSpace(stream.Reality.SpiderX); sx != "" {
				rs["spiderX"] = sx
			}
			// Post-quantum hybrid key share (Xray 26.9+). Off by default for client compatibility.
			if stream.Reality.RequireMlkem {
				rs["requireMlkem"] = true
			}
			out["realitySettings"] = rs
		}
	}

	// Optional performance sockopt (TCPFastOpen / keepalive / nodelay) when configured on the inbound.
	if stream.Sockopt != nil && stream.Sockopt.Enabled {
		so := map[string]any{}
		if stream.Sockopt.TCPKeepAlive > 0 {
			so["tcpKeepAliveIdle"] = stream.Sockopt.TCPKeepAlive
		}
		if stream.Sockopt.TCPNoDelay != nil {
			so["tcpNoDelay"] = *stream.Sockopt.TCPNoDelay
		}
		if stream.Sockopt.TCPFastOpen != nil {
			so["tcpFastOpen"] = *stream.Sockopt.TCPFastOpen
		}
		if stream.Sockopt.Mark > 0 {
			so["mark"] = stream.Sockopt.Mark
		}
		if strings.TrimSpace(stream.Sockopt.Interface) != "" {
			so["interface"] = stream.Sockopt.Interface
		}
		if len(so) > 0 {
			out["sockopt"] = so
		}
	}

	return json.Marshal(out)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// extractFallbacks reads deceptive fallbacks from StreamSettings (preferred) or
// legacy Settings JSON key "fallbacks".
func extractFallbacks(ib models.Inbound) []map[string]any {
	var entries []models.FallbackEntry
	var stream models.StreamSettings
	if ib.StreamSettings != "" {
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
		if len(stream.Fallbacks) > 0 {
			entries = stream.Fallbacks
		}
	}
	if len(entries) == 0 && ib.Settings != "" {
		var wrap struct {
			Fallbacks []models.FallbackEntry `json:"fallbacks"`
		}
		_ = json.Unmarshal([]byte(ib.Settings), &wrap)
		entries = wrap.Fallbacks
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		dest := strings.TrimSpace(e.Dest)
		if dest == "" {
			continue
		}
		m := map[string]any{"dest": dest}
		if n := strings.TrimSpace(e.Name); n != "" {
			m["name"] = n
		}
		if a := strings.TrimSpace(e.Alpn); a != "" {
			m["alpn"] = a
		}
		if p := strings.TrimSpace(e.Path); p != "" {
			m["path"] = p
		}
		if e.Xver > 0 {
			m["xver"] = e.Xver
		}
		out = append(out, m)
	}
	if FallbackEnricher != nil {
		out = FallbackEnricher(out)
	}
	return out
}

// FallbackEnricher optionally injects honeypot / extra fallbacks (set from main).
var FallbackEnricher func([]map[string]any) []map[string]any

// BuildSmartDNSServers builds multi-resolver DoH/DoT list with Iran-aware split DNS.
// When extra is non-empty (from panel dns_provider), those resolvers are preferred
// for global traffic; Iranian domains still resolve via localhost first.
func BuildSmartDNSServers(extra []string) []any {
	primary := make([]string, 0, 4)
	for _, e := range extra {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		primary = append(primary, e)
	}
	if len(primary) == 0 {
		primary = []string{
			"https+local://1.1.1.1/dns-query",
			"https+local://8.8.8.8/dns-query",
			"https+local://9.9.9.9/dns-query",
		}
	}
	servers := []any{
		map[string]any{
			"address":   "localhost",
			"domains":   []string{"geosite:category-ir", "regexp:.*\\.ir$"},
			"expectIPs": []string{"geoip:ir"},
		},
	}
	for _, addr := range primary {
		// No "domains" filter here: the tag geosite:geolocation-!ir does not exist in the
		// installed geosite.dat and makes xray fail to start. Resolvers without a domain
		// list are the default for everything the Iranian localhost rule above does not match.
		entry := map[string]any{"address": addr}
		servers = append(servers, entry)
	}
	fallbacks := []string{
		"https+local://1.1.1.1/dns-query",
		"https+local://8.8.8.8/dns-query",
		"tls+local://1.1.1.1:853",
		"tls+local://8.8.8.8:853",
	}
	seen := map[string]bool{}
	for _, p := range primary {
		seen[p] = true
	}
	for _, f := range fallbacks {
		if seen[f] {
			continue
		}
		servers = append(servers, map[string]any{"address": f})
		seen[f] = true
	}
	servers = append(servers, "localhost")
	return servers
}
