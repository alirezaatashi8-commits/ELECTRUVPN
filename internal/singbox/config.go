package singbox

import (
	"encoding/json"
	"fmt"
	"github.com/electruvpn/electruvpn/internal/models"
	"strings"
)

type Config struct {
	Log       map[string]any   `json:"log"`
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
	DNS       map[string]any   `json:"dns,omitempty"`
	// Experimental carries the loopback-only Clash API used for live stats.
	Experimental map[string]any `json:"experimental,omitempty"`
}

func Generate(inbounds []models.Inbound) (*Config, error) {
	return GenerateWithDNS(inbounds, []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"})
}

func GenerateWithDNS(inbounds []models.Inbound, dnsServers []string) (*Config, error) {
	servers := make([]map[string]any, 0, 8)
	// Local/system for IR-friendly resolution
	servers = append(servers, map[string]any{"type": "local", "tag": "local-dns"})
	// DoH multi-query
	dohList := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	for i, raw := range dnsServers {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "https+local://")
		host = strings.TrimSuffix(host, "/dns-query")
		if host == "" {
			continue
		}
		servers = append(servers, map[string]any{"type": "https", "tag": fmt.Sprintf("secure-dns-%d", i+1), "server": host, "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true}})
	}
	if len(servers) <= 1 {
		for i, h := range dohList {
			servers = append(servers, map[string]any{"type": "https", "tag": fmt.Sprintf("secure-dns-%d", i+1), "server": h, "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true}})
		}
	}
	// DoT fallbacks
	servers = append(servers, map[string]any{"type": "tls", "tag": "dot-cf", "server": "1.1.1.1", "server_port": 853, "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}})
	servers = append(servers, map[string]any{"type": "tls", "tag": "dot-google", "server": "8.8.8.8", "server_port": 853, "tls": map[string]any{"enabled": true, "server_name": "dns.google"}})
	cfg := &Config{Log: map[string]any{"level": "info"}, Outbounds: []map[string]any{{"type": "direct", "tag": "direct"}, {"type": "block", "tag": "block"}}, DNS: map[string]any{"servers": servers, "final": "secure-dns-1", "strategy": "prefer_ipv4"}}
	cfg.Experimental = experimentalBlock()
	for _, ib := range inbounds {
		if !ib.Enable || ib.IsLimited() {
			continue
		}
		x, e := build(ib)
		if e != nil {
			return nil, fmt.Errorf("inbound %q: %w", ib.Remark, e)
		}
		cfg.Inbounds = append(cfg.Inbounds, x)
	}
	return cfg, nil
}
func clients(ib models.Inbound) []models.Client {
	out := []models.Client{}
	for _, c := range ib.Clients {
		if c.Enable && !c.IsExpired() && !c.IsOverQuota() {
			out = append(out, c)
		}
	}
	return out
}
func build(ib models.Inbound) (map[string]any, error) {
	tag := strings.TrimSpace(ib.Tag)
	if tag == "" {
		tag = fmt.Sprintf("inbound-%d", ib.Port) // same fallback as the xray config, keeps live stats keyed consistently
	}
	base := map[string]any{"listen": ib.Listen, "listen_port": ib.Port, "tag": tag}
	var stream models.StreamSettings
	_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	if stream.Security == models.SecurityTLS || stream.Security == models.SecurityReality {
		tls := map[string]any{"enabled": true}
		if stream.TLSSettings != nil {
			tls["server_name"] = stream.TLSSettings.SNI
			tls["certificate_path"] = stream.TLSSettings.CertFile
			tls["key_path"] = stream.TLSSettings.KeyFile
		}
		base["tls"] = tls
	}

	switch ib.Protocol {
	case models.ProtocolHysteria2:
		var st models.Hysteria2Settings
		_ = json.Unmarshal([]byte(ib.Settings), &st)
		users := []map[string]any{}
		for _, c := range clients(ib) {
			users = append(users, map[string]any{"password": c.Password, "name": c.Email})
		}
		base["type"] = "hysteria2"
		base["users"] = users
		if st.UpMbps > 0 {
			base["up_mbps"] = st.UpMbps
		}
		if st.DownMbps > 0 {
			base["down_mbps"] = st.DownMbps
		}
		if strings.EqualFold(strings.TrimSpace(st.ObfsType), "salamander") && st.ObfsPassword != "" {
			base["obfs"] = map[string]any{"type": "salamander", "password": st.ObfsPassword}
		}
		if st.IgnoreClientBandwidth {
			base["ignore_client_bandwidth"] = true
		}
		// HTTP masquerade: probe traffic looks like a normal website (sing-box hysteria2).
		// Each type needs its own required field or sing-box refuses to start.
		switch mt := strings.ToLower(strings.TrimSpace(st.MasqueradeType)); mt {
		case "proxy":
			if u := strings.TrimSpace(st.MasqueradeURL); u != "" {
				base["masquerade"] = map[string]any{"type": "proxy", "url": u, "rewrite_host": true}
			}
		case "file":
			if d := strings.TrimSpace(st.MasqueradeDir); d != "" {
				base["masquerade"] = map[string]any{"type": "file", "directory": d}
			}
		case "string":
			status := st.MasqueradeStatus
			if status < 100 || status > 599 {
				status = 200
			}
			base["masquerade"] = map[string]any{"type": "string", "content": st.MasqueradeContent, "status_code": status}
		}
	case models.ProtocolTUIC:
		// TUIC v5 with HTTP/3 (QUIC) camouflage — looks like Chrome talking to a normal site.
		var st models.TUICSettings
		_ = json.Unmarshal([]byte(ib.Settings), &st)
		users := []map[string]any{}
		for _, c := range clients(ib) {
			// name makes sing-box log "[email]" so live stats can attribute traffic.
			users = append(users, map[string]any{"name": c.Email, "uuid": c.UUID, "password": c.Password})
		}
		base["type"] = "tuic"
		base["users"] = users
		cc := st.CongestionControl
		if cc == "" {
			cc = "bbr"
		}
		base["congestion_control"] = cc
		if st.UDPRelayMode != "" {
			base["udp_relay_mode"] = st.UDPRelayMode
		} else {
			base["udp_relay_mode"] = "quic" // native QUIC relay = stronger HTTP/3 fingerprint match
		}
		if st.ZeroRTTHandshake {
			base["zero_rtt_handshake"] = true
		}
		if st.Heartbeat != "" {
			base["heartbeat"] = st.Heartbeat
		}
		if st.AuthTimeout != "" {
			base["auth_timeout"] = st.AuthTimeout
		}
		// Ensure TLS is present with ALPN h3 for HTTP/3 camouflage
		tls, _ := base["tls"].(map[string]any)
		if tls == nil {
			tls = map[string]any{"enabled": true}
		}
		tls["enabled"] = true
		alpn := st.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h3"}
		}
		tls["alpn"] = alpn
		sni := st.SNI
		if sni == "" && stream.TLSSettings != nil {
			sni = stream.TLSSettings.SNI
		}
		if sni != "" {
			tls["server_name"] = sni
		}
		if stream.TLSSettings != nil {
			if stream.TLSSettings.CertFile != "" {
				tls["certificate_path"] = stream.TLSSettings.CertFile
			}
			if stream.TLSSettings.KeyFile != "" {
				tls["key_path"] = stream.TLSSettings.KeyFile
			}
		}
		base["tls"] = tls

	case models.ProtocolNaive:
		// NaiveProxy-style: HTTP CONNECT proxy over TLS that mimics Chrome (HTTP/2 or HTTP/3).
		// Implemented as sing-box "http" inbound with TLS + strong ALPN so DPI sees browser traffic.
		var st models.NaiveSettings
		_ = json.Unmarshal([]byte(ib.Settings), &st)
		users := []map[string]any{}
		for _, c := range clients(ib) {
			user := c.Email
			if user == "" {
				user = c.UUID
			}
			if st.Username != "" {
				user = st.Username
			}
			pass := c.Password
			if pass == "" {
				pass = c.UUID
			}
			users = append(users, map[string]any{"username": user, "password": pass})
		}
		base["type"] = "http"
		base["users"] = users
		base["set_system_proxy"] = false
		tls, _ := base["tls"].(map[string]any)
		if tls == nil {
			tls = map[string]any{"enabled": true}
		}
		tls["enabled"] = true
		alpn := st.ALPN
		if len(alpn) == 0 {
			if st.HTTP3 {
				alpn = []string{"h3"}
			} else {
				alpn = []string{"h2", "http/1.1"}
			}
		}
		tls["alpn"] = alpn
		sni := st.SNI
		if sni == "" && stream.TLSSettings != nil {
			sni = stream.TLSSettings.SNI
		}
		if sni != "" {
			tls["server_name"] = sni
		}
		cert := st.CertFile
		key := st.KeyFile
		if stream.TLSSettings != nil {
			if cert == "" {
				cert = stream.TLSSettings.CertFile
			}
			if key == "" {
				key = stream.TLSSettings.KeyFile
			}
		}
		if cert != "" {
			tls["certificate_path"] = cert
		}
		if key != "" {
			tls["key_path"] = key
		}
		base["tls"] = tls
		// Optional padding hint stored for client share links (server is TLS HTTP CONNECT)
		if st.Padding {
			base["padding"] = true
		}
		if st.Concurrency > 0 {
			base["max_connections"] = st.Concurrency
		}

	case models.ProtocolAnyTLS:
		base["type"] = "anytls"
		users := []map[string]any{}
		for _, c := range clients(ib) {
			users = append(users, map[string]any{"password": c.Password, "name": c.Email})
		}
		base["users"] = users
	case models.ProtocolShadowTLS:
		base["type"] = "shadowtls"
		users := []map[string]any{}
		for _, c := range clients(ib) {
			users = append(users, map[string]any{"password": c.Password, "name": c.Email})
		}
		base["users"] = users
	case models.ProtocolWireGuard:
		var st models.WireGuardSettings
		_ = json.Unmarshal([]byte(ib.Settings), &st)
		base["type"] = "wireguard"
		base["private_key"] = st.PrivateKey
		base["address"] = []string{st.Address}
		if st.MTU > 0 {
			base["mtu"] = st.MTU
		}
		peers := []map[string]any{}
		for _, c := range clients(ib) {
			peers = append(peers, map[string]any{"public_key": c.PublicKey, "allowed_ips": []string{c.Address}})
		}
		base["peers"] = peers
	default:
		return nil, fmt.Errorf("protocol %q is managed by xray-core", ib.Protocol)
	}
	return base, nil
}
