package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
)

// BuildShareLink returns the vless://, vmess://, trojan://, or ss:// URI a
// client app (v2rayN, NekoBox, Streisand, etc.) can import directly. host is
// the public address clients should connect to (usually the server's
// domain or IP, not "0.0.0.0").
func BuildShareLink(ib models.Inbound, c models.Client, host string) (string, error) {
	host, err := normalizePublicHost(host)
	if err != nil {
		return "", err
	}
	var stream models.StreamSettings
	if ib.StreamSettings != "" {
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	}

	switch ib.Protocol {
	case models.ProtocolVLESS:
		return buildVLESSLink(ib, c, host, stream), nil
	case models.ProtocolTrojan:
		return buildTrojanLink(ib, c, host, stream), nil
	case models.ProtocolShadowsocks:
		return buildShadowsocksLink(ib, c, host)
	case models.ProtocolVMess:
		return buildVMessLink(ib, c, host, stream)
	case models.ProtocolHysteria2:
		return buildHysteria2Link(ib, c, host)
	case models.ProtocolTUIC:
		return buildTUICLink(ib, c, host)
	case models.ProtocolNaive:
		return buildNaiveLink(ib, c, host)
	case models.ProtocolWireGuard:
		return buildWireGuardLink(ib, c, host)
	case models.ProtocolAnyTLS:
		return buildAnyTLSLink(ib, c, host)
	case models.ProtocolShadowTLS:
		return buildShadowTLSLink(ib, c, host)
	default:
		return "", fmt.Errorf("unsupported protocol %q", ib.Protocol)
	}
}

func normalizePublicHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "/?#") {
		return "", fmt.Errorf("public host must be a hostname or IP address")
	}
	if h, p, err := net.SplitHostPort(raw); err == nil && p != "" {
		raw = h
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.Trim(raw, "[]")
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String(), nil
	}
	if len(raw) > 253 || strings.Contains(raw, " ") {
		return "", fmt.Errorf("invalid public host")
	}
	for _, label := range strings.Split(strings.TrimSuffix(raw, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid public host")
		}
		for _, r := range label {
			if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return "", fmt.Errorf("invalid public host")
			}
		}
	}
	return strings.TrimSuffix(raw, "."), nil
}

func streamQuery(ib models.Inbound, stream models.StreamSettings) url.Values {
	q := url.Values{}
	linkType := string(ib.Network)
	if ib.Network == models.NetworkMKCP {
		linkType = "kcp" // share-link spelling understood by v2rayN/NekoBox/Hiddify
	}
	q.Set("type", linkType)
	q.Set("security", string(ib.Security))

	switch ib.Network {
	case models.NetworkXHTTP:
		path := strings.TrimSpace(stream.XHTTPPath)
		if path == "" {
			path = "/xhttp"
		}
		q.Set("path", path)
		if stream.XHTTPHost != "" {
			q.Set("host", stream.XHTTPHost)
		}
		mode := strings.TrimSpace(stream.XHTTPMode)
		if mode == "" {
			if ib.Security == models.SecurityReality {
				mode = "stream-one"
			} else {
				mode = "auto"
			}
		}
		q.Set("mode", mode)
	case models.NetworkHTTPUpgrade:
		path := strings.TrimSpace(stream.HTTPUpgradePath)
		if path == "" {
			path = "/"
		}
		q.Set("path", path)
		if stream.HTTPUpgradeHost != "" {
			q.Set("host", stream.HTTPUpgradeHost)
		}
	case models.NetworkWS:
		if stream.WSPath != "" {
			q.Set("path", stream.WSPath)
		}
		if stream.WSHost != "" {
			q.Set("host", stream.WSHost)
		}
	case models.NetworkGRPC:
		if stream.GRPCService != "" {
			q.Set("serviceName", stream.GRPCService)
		}
	}

	switch ib.Security {
	case models.SecurityTLS:
		if stream.TLSSettings != nil && stream.TLSSettings.SNI != "" {
			q.Set("sni", stream.TLSSettings.SNI)
		}
		if stream.TLSSettings != nil && strings.TrimSpace(stream.TLSSettings.ALPN) != "" {
			q.Set("alpn", strings.TrimSpace(stream.TLSSettings.ALPN))
		}
	case models.SecurityReality:
		if stream.Reality != nil {
			if len(stream.Reality.ServerNames) > 0 {
				q.Set("sni", stream.Reality.ServerNames[0])
			}
			q.Set("pbk", stream.Reality.PublicKey)
			if len(stream.Reality.ShortIDs) > 0 {
				q.Set("sid", stream.Reality.ShortIDs[0])
			}
			q.Set("fp", "chrome")
			if sx := strings.TrimSpace(stream.Reality.SpiderX); sx != "" {
				q.Set("spx", sx)
			}
		}
	}

	// Xray dialer hints for modern clients (Hiddify / Xray / Streisand).
	// Standard fragment: packets,length,interval[,maxSplit]
	if stream.Fragment != nil && stream.Fragment.Enabled {
		packets := orDefault(stream.Fragment.Packets, orDefault(stream.Fragment.Type, "tlshello"))
		length := orDefault(stream.Fragment.Length, "100-200")
		interval := orDefault(stream.Fragment.Interval, "10-20")
		frag := packets + "," + length + "," + interval
		if ms := strings.TrimSpace(stream.Fragment.MaxSplit); ms != "" {
			frag += "," + ms
		}
		q.Set("fragment", frag)
	}
	// TCP Mask 1 (optional second-stage / Hiddify-style mask)
	if len(stream.TCPMasks) > 0 {
		m0 := stream.TCPMasks[0]
		pk := orDefault(m0.Packets, "1-3")
		ln := orDefault(m0.Lengths, "100-200")
		dl := orDefault(m0.Delays, "10-20")
		mask := pk + "," + ln + "," + dl
		if ms := strings.TrimSpace(m0.MaxSplit); ms != "" {
			mask += "," + ms
		}
		q.Set("tcpMask", mask)
		if m0.Type != "" {
			q.Set("tcpMaskType", m0.Type)
		}
	}
	if stream.Sockopt != nil && stream.Sockopt.Enabled {
		if stream.Sockopt.TCPKeepAlive > 0 {
			q.Set("tcpKeepAliveIdle", strconv.Itoa(stream.Sockopt.TCPKeepAlive))
		}
		if stream.Sockopt.TCPNoDelay != nil && *stream.Sockopt.TCPNoDelay {
			q.Set("tcpNoDelay", "1")
		}
		if stream.Sockopt.TCPFastOpen != nil && *stream.Sockopt.TCPFastOpen {
			q.Set("tcpFastOpen", "1")
		}
	}
	if stream.Mux != nil && stream.Mux.Enabled {
		q.Set("mux", "true")
		if stream.Mux.Concurrency > 0 {
			q.Set("muxConcurrency", strconv.Itoa(stream.Mux.Concurrency))
		}
		if stream.Mux.SessionTTL > 0 {
			q.Set("muxSessionTTL", strconv.Itoa(stream.Mux.SessionTTL))
		}
		if stream.Mux.XudpConcurrency > 0 {
			q.Set("muxXudpConcurrency", strconv.Itoa(stream.Mux.XudpConcurrency))
		}
		if stream.Mux.XudpProxyUDP443 != "" {
			q.Set("muxXudpProxyUDP443", stream.Mux.XudpProxyUDP443)
		}
		if stream.Mux.Padding {
			q.Set("muxPadding", "true")
		}
	}
	return q
}

func buildVLESSLink(ib models.Inbound, c models.Client, host string, stream models.StreamSettings) string {
	q := streamQuery(ib, stream)
	q.Set("encryption", "none") // part of the standard VLESS URI; some clients refuse links without it
	if c.Flow != "" {
		q.Set("flow", c.Flow)
	}
	u := url.URL{
		Scheme:   "vless",
		User:     url.User(c.UUID),
		Host:     net.JoinHostPort(host, strconv.Itoa(ib.Port)),
		RawQuery: q.Encode(),
		Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email),
	}
	return u.String()
}

func buildTrojanLink(ib models.Inbound, c models.Client, host string, stream models.StreamSettings) string {
	q := streamQuery(ib, stream)
	u := url.URL{
		Scheme:   "trojan",
		User:     url.User(c.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(ib.Port)),
		RawQuery: q.Encode(),
		Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email),
	}
	return u.String()
}

func buildShadowsocksLink(ib models.Inbound, c models.Client, host string) (string, error) {
	var ss models.ShadowsocksSettings
	if ib.Settings != "" {
		_ = json.Unmarshal([]byte(ib.Settings), &ss)
	}
	if ss.Method == "" {
		ss.Method = "2022-blake3-aes-128-gcm"
	}
	password := c.Password
	if ShadowsocksKeyLength(ss.Method) > 0 && strings.TrimSpace(ss.Password) != "" {
		// SIP022 multi-user: the client presents "serverKey:userKey".
		password = ss.Password + ":" + c.Password
	}
	userInfo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", ss.Method, password)))
	return (&url.URL{Scheme: "ss", Host: net.JoinHostPort(host, strconv.Itoa(ib.Port)), User: url.User(userInfo), Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email)}).String(), nil
}

func buildVMessLink(ib models.Inbound, c models.Client, host string, stream models.StreamSettings) (string, error) {
	// vmess:// links are base64(json), not a query-string URI.
	payload := map[string]any{
		"v":    "2",
		"ps":   fmt.Sprintf("%s-%s", ib.Remark, c.Email),
		"add":  host,
		"port": ib.Port,
		"id":   c.UUID,
		"aid":  0,
		"net":  string(ib.Network),
		"type": "none",
		"tls":  string(ib.Security),
	}
	if ib.Security == models.SecurityTLS {
		payload["tls"] = "tls"
		if stream.TLSSettings != nil && stream.TLSSettings.SNI != "" {
			payload["sni"] = stream.TLSSettings.SNI
		}
		if stream.TLSSettings != nil && strings.TrimSpace(stream.TLSSettings.ALPN) != "" {
			payload["alpn"] = strings.TrimSpace(stream.TLSSettings.ALPN)
		}
	} else {
		payload["tls"] = ""
	}
	if ib.Network == models.NetworkMKCP {
		payload["net"] = "kcp"
	}
	if ib.Network == models.NetworkGRPC {
		// vmess grpc carries the service name in "path"; without it the client
		// connects to an empty service and the handshake fails.
		payload["path"] = stream.GRPCService
		payload["type"] = "gun"
	} else if ib.Network == models.NetworkWS {
		payload["path"] = stream.WSPath
		payload["host"] = stream.WSHost
	} else if ib.Network == models.NetworkXHTTP {
		payload["path"] = stream.XHTTPPath
		payload["host"] = stream.XHTTPHost
		payload["mode"] = stream.XHTTPMode
	} else if ib.Network == models.NetworkHTTPUpgrade {
		payload["path"] = stream.HTTPUpgradePath
		payload["host"] = stream.HTTPUpgradeHost
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
}

// BuildSubscriptionBody returns the base64 blob served at
// /sub/<subId> -- one share link per line, then base64-encoded as a whole,
// which is the format every major client app expects for a subscription URL.
func BuildSubscriptionBody(links []string) string {
	joined := strings.Join(links, "\n")
	return base64.StdEncoding.EncodeToString([]byte(joined))
}

func buildHysteria2Link(ib models.Inbound, c models.Client, host string) (string, error) {
	var st models.Hysteria2Settings
	_ = json.Unmarshal([]byte(ib.Settings), &st)
	q := url.Values{}
	if strings.EqualFold(strings.TrimSpace(st.ObfsType), "salamander") && st.ObfsPassword != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", st.ObfsPassword)
	}
	if st.AllowInsecure {
		q.Set("insecure", "1")
	}
	if pin := strings.TrimSpace(st.PinSHA256); pin != "" {
		q.Set("pinSHA256", pin)
	}
	if hop := strings.TrimSpace(st.PortHopping); hop != "" {
		q.Set("mport", hop) // client-side port hopping range (apernet/hysteria v2.x URI)
	}
	var h2stream models.StreamSettings
	_ = json.Unmarshal([]byte(ib.StreamSettings), &h2stream)
	if h2stream.TLSSettings != nil && h2stream.TLSSettings.SNI != "" {
		q.Set("sni", h2stream.TLSSettings.SNI) // needed when connecting by IP or to a different name than the certificate
	}
	return (&url.URL{Scheme: "hysteria2", User: url.User(c.Password), Host: net.JoinHostPort(host, strconv.Itoa(ib.Port)), RawQuery: q.Encode(), Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email)}).String(), nil
}
func buildTUICLink(ib models.Inbound, c models.Client, host string) (string, error) {
	var st models.TUICSettings
	_ = json.Unmarshal([]byte(ib.Settings), &st)
	q := url.Values{}
	cc := st.CongestionControl
	if cc == "" {
		cc = "bbr"
	}
	q.Set("congestion_control", cc)
	relay := st.UDPRelayMode
	if relay == "" {
		relay = "quic"
	}
	q.Set("udp_relay_mode", relay)
	if st.ZeroRTTHandshake {
		q.Set("zero_rtt_handshake", "1")
	}
	alpn := st.ALPN
	if len(alpn) == 0 {
		alpn = []string{"h3"}
	}
	q.Set("alpn", strings.Join(alpn, ","))
	sni := st.SNI
	if sni == "" {
		var stream models.StreamSettings
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
		if stream.TLSSettings != nil {
			sni = stream.TLSSettings.SNI
		}
	}
	if sni != "" {
		q.Set("sni", sni)
	}
	if st.AllowInsecure {
		q.Set("allow_insecure", "1")
	}
	q.Set("version", "5") // TUIC v5
	return (&url.URL{
		Scheme:   "tuic",
		User:     url.UserPassword(c.UUID, c.Password),
		Host:     net.JoinHostPort(host, strconv.Itoa(ib.Port)),
		RawQuery: q.Encode(),
		Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email),
	}).String(), nil
}

// buildNaiveLink produces a Chrome-like HTTP/2 or HTTP/3 share URI.
// Format: naive+https://user:pass@host:port#remark  (widely used by NekoBox / v2rayN / Streisand)
func buildNaiveLink(ib models.Inbound, c models.Client, host string) (string, error) {
	var st models.NaiveSettings
	_ = json.Unmarshal([]byte(ib.Settings), &st)
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
	q := url.Values{}
	if st.Padding {
		q.Set("padding", "true")
	}
	sni := st.SNI
	if sni == "" {
		var stream models.StreamSettings
		_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
		if stream.TLSSettings != nil {
			sni = stream.TLSSettings.SNI
		}
	}
	if sni != "" {
		q.Set("sni", sni)
	}
	if len(st.ALPN) > 0 {
		q.Set("alpn", strings.Join(st.ALPN, ","))
	} else if st.HTTP3 {
		q.Set("alpn", "h3")
	} else {
		q.Set("alpn", "h2,http/1.1")
	}
	// Manual build: scheme "naive+https" is not a valid net/url Scheme token.
	auth := url.UserPassword(user, pass).String()
	hostPort := net.JoinHostPort(host, strconv.Itoa(ib.Port))
	frag := url.PathEscape(fmt.Sprintf("%s-%s", ib.Remark, c.Email))
	link := fmt.Sprintf("naive+https://%s@%s", auth, hostPort)
	if enc := q.Encode(); enc != "" {
		link += "?" + enc
	}
	if frag != "" {
		link += "#" + frag
	}
	return link, nil
}
func buildWireGuardLink(ib models.Inbound, c models.Client, host string) (string, error) {
	var st models.WireGuardSettings
	if err := json.Unmarshal([]byte(ib.Settings), &st); err != nil {
		return "", fmt.Errorf("invalid WireGuard settings: %w", err)
	}
	if strings.TrimSpace(st.PrivateKey) == "" {
		return "", fmt.Errorf("WireGuard server private key is missing")
	}
	// WireGuardSettings stores the server private key, not a separate public
	// key. The public key is deterministically derived from that private key.
	// wireguard-tools is installed by the ElectruVPN installer, so use the
	// official wg utility instead of inventing a second source of truth.
	cmd := exec.Command("wg", "pubkey")
	cmd.Stdin = strings.NewReader(st.PrivateKey + "\n")
	pub, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("derive WireGuard server public key: %w", err)
	}
	serverPublicKey := strings.TrimSpace(string(pub))
	if serverPublicKey == "" {
		return "", fmt.Errorf("derived WireGuard server public key is empty")
	}
	dns := st.DNS
	if len(dns) == 0 {
		dns = []string{"1.1.1.1", "8.8.8.8"}
	}
	b := map[string]any{
		"private_key": c.PrivateKey,
		"address":     c.Address,
		"dns":         dns,
		"server":      host,
		"port":        ib.Port,
		"public_key":  serverPublicKey,
		// Full-tunnel defaults keep DNS and IPv6-capable traffic inside the
		// WireGuard tunnel instead of relying on the host network.
		"allowed_ips":          []string{"0.0.0.0/0", "::/0"},
		"persistent_keepalive": 25,
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("encode WireGuard share link: %w", err)
	}
	return "wireguard://" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func buildAnyTLSLink(ib models.Inbound, c models.Client, host string) (string, error) {
	return (&url.URL{Scheme: "anytls", User: url.User(c.Password), Host: net.JoinHostPort(host, strconv.Itoa(ib.Port)), Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email)}).String(), nil
}
func buildShadowTLSLink(ib models.Inbound, c models.Client, host string) (string, error) {
	return (&url.URL{Scheme: "shadowtls", User: url.User(c.Password), Host: net.JoinHostPort(host, strconv.Itoa(ib.Port)), Fragment: fmt.Sprintf("%s-%s", ib.Remark, c.Email)}).String(), nil
}
