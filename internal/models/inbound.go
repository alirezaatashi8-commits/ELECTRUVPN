package models

import "time"

// Protocol enumerates the inbound protocols ElectruVPN can generate xray
// configuration for. Adding WireGuard/Hysteria2 later just means adding a
// case here plus a branch in internal/xray/config.go.
type Protocol string

const (
	ProtocolVLESS       Protocol = "vless"
	ProtocolVMess       Protocol = "vmess"
	ProtocolTrojan      Protocol = "trojan"
	ProtocolShadowsocks Protocol = "shadowsocks"
	ProtocolHysteria2   Protocol = "hysteria2"
	ProtocolTUIC        Protocol = "tuic"
	ProtocolWireGuard   Protocol = "wireguard"
	ProtocolAnyTLS      Protocol = "anytls"
	ProtocolShadowTLS   Protocol = "shadowtls"
	ProtocolNaive       Protocol = "naive"
)

// Network is the transport carrying the protocol.
type Network string

const (
	NetworkTCP         Network = "tcp"
	NetworkWS          Network = "ws"
	NetworkGRPC        Network = "grpc"
	NetworkXHTTP       Network = "xhttp"
	NetworkHTTPUpgrade Network = "httpupgrade"
	NetworkMKCP        Network = "mkcp"
	NetworkQUIC        Network = "quic"
	NetworkUDP         Network = "udp"
)

// Security describes how (or whether) the transport is encrypted.
type Security string

const (
	SecurityNone    Security = "none"
	SecurityTLS     Security = "tls"
	SecurityReality Security = "reality"
)

// Inbound is one listening port on the server, e.g. "VLESS+Reality on 443".
// Settings and StreamSettings are stored as raw JSON so the schema can grow
// without a migration every time a new transport option is added; typed
// helpers below give the API and the xray config builder a safe view.
type Inbound struct {
	ID                         uint      `gorm:"primaryKey" json:"id"`
	Remark                     string    `gorm:"size:128;index" json:"remark"`
	Enable                     bool      `gorm:"default:true" json:"enable"`
	Listen                     string    `gorm:"size:64" json:"listen"`
	IPMode                     string    `gorm:"size:16;default:ipv4" json:"ipMode"` // ipv4, ipv6, dual
	SubscriptionEnabled        bool      `gorm:"default:true" json:"subscriptionEnabled"`
	SubscriptionName           string    `gorm:"size:128" json:"subscriptionName,omitempty"`
	SubscriptionDefaultAddress string    `gorm:"size:16;default:ipv4" json:"subscriptionDefaultAddress"` // ipv4, ipv6, domain, dual, all
	SubscriptionPreset         string    `gorm:"size:64;default:standard" json:"subscriptionPreset"`
	SubscriptionSmartEnabled   bool      `gorm:"default:false" json:"subscriptionSmartEnabled"`
	Port                       int       `gorm:"uniqueIndex" json:"port"`
	Protocol                   Protocol  `gorm:"size:32" json:"protocol"`
	Network                    Network   `gorm:"size:16" json:"network"`
	Security                   Security  `gorm:"size:16" json:"security"`
	Settings                   string    `gorm:"type:text" json:"settings"`       // protocol-specific inbound settings (JSON)
	StreamSettings             string    `gorm:"type:text" json:"streamSettings"` // transport/TLS/REALITY settings (JSON)
	Sniffing                   bool      `gorm:"default:true" json:"sniffing"`
	Tag                        string    `gorm:"size:64;uniqueIndex" json:"tag"`
	Labels                     string    `gorm:"size:512" json:"labels"` // comma-separated admin labels/tags
	ExpiryTime                 int64     `json:"expiryTime"`             // unix ms, 0 = never
	TotalGB                    int64     `json:"totalGB"`                // 0 = unlimited
	Up                         int64     `json:"up"`                     // cached bytes uploaded, refreshed from xray stats
	Down                       int64     `json:"down"`
	Maintenance                bool      `gorm:"default:false" json:"maintenance"`  // cached bytes downloaded, refreshed from xray stats
	LimitReached               bool      `gorm:"default:false" json:"limitReached"` // cached: inbound volume or time limit reached (listener is not served)
	CreatedAt                  time.Time `json:"createdAt"`
	UpdatedAt                  time.Time `json:"updatedAt"`

	Clients []Client `gorm:"foreignKey:InboundID" json:"clients,omitempty"`
}

// IsExpired reports whether the inbound-level expiry date has passed.
// ExpiryTime == 0 means the inbound never expires.
func (i Inbound) IsExpired() bool {
	if i.ExpiryTime <= 0 {
		return false
	}
	return time.Now().UnixMilli() >= i.ExpiryTime
}

// IsOverQuota reports whether the whole inbound (sum of all its clients) has
// used up the inbound-level volume. TotalGB == 0 means unlimited. Usage is the
// plain Up+Down byte sum, counted 1:1.
func (i Inbound) IsOverQuota() bool {
	return quotaReached(i.Up, i.Down, i.TotalGB, 0)
}

// IsLimited reports whether the inbound reached its own volume or time limit.
// A limited inbound is removed from the generated runtime config until the
// limit is raised or the traffic counter is reset.
func (i Inbound) IsLimited() bool {
	return i.IsExpired() || i.IsOverQuota()
}

// ShadowsocksSettings is the JSON shape stored in Inbound.Settings when
// Protocol == shadowsocks.
type Hysteria2Settings struct {
	UpMbps         int    `json:"upMbps"`
	DownMbps       int    `json:"downMbps"`
	ObfsType       string `json:"obfsType"`
	ObfsPassword   string `json:"obfsPassword"`
	MasqueradeType string `json:"masqueradeType,omitempty"` // proxy | file | string (sing-box)
	MasqueradeURL  string `json:"masqueradeURL,omitempty"`  // e.g. https://www.microsoft.com
	// Hysteria 2 (apernet/hysteria v2.x, sing-box 1.12+) options.
	MasqueradeDir         string `json:"masqueradeDir,omitempty"`         // type=file: directory served to probes
	MasqueradeContent     string `json:"masqueradeContent,omitempty"`     // type=string: fixed response body
	MasqueradeStatus      int    `json:"masqueradeStatus,omitempty"`      // type=string: HTTP status (default 200)
	IgnoreClientBandwidth bool   `json:"ignoreClientBandwidth,omitempty"` // server ignores client-declared speed (use BBR)
	AllowInsecure         bool   `json:"allowInsecure,omitempty"`         // client hint: skip certificate verification (self-signed)
	PinSHA256             string `json:"pinSHA256,omitempty"`             // client hint: pin the server certificate (hex, colons optional)
	PortHopping           string `json:"portHopping,omitempty"`           // client hint "20000-30000": server must redirect that UDP range to the inbound port
}

// TUICSettings — TUIC v5 (QUIC / HTTP/3 camouflage). Stored in Inbound.Settings.
// When TLS is enabled with ALPN including "h3", traffic is nearly indistinguishable
// from a normal Chrome QUIC session to a large site.
type TUICSettings struct {
	CongestionControl string   `json:"congestionControl"`          // bbr | cubic | new_reno (default bbr)
	UDPRelayMode      string   `json:"udpRelayMode,omitempty"`     // native | quic
	ZeroRTTHandshake  bool     `json:"zeroRTTHandshake,omitempty"` // 0-RTT (faster, slight replay risk)
	Heartbeat         string   `json:"heartbeat,omitempty"`        // e.g. "10s"
	ALPN              []string `json:"alpn,omitempty"`             // default ["h3"] for HTTP/3 camouflage
	SNI               string   `json:"sni,omitempty"`              // TLS server name (camouflage domain)
	DisableSNI        bool     `json:"disableSNI,omitempty"`
	AllowInsecure     bool     `json:"allowInsecure,omitempty"` // client-side only hint
	AuthTimeout       string   `json:"authTimeout,omitempty"`   // e.g. "3s"
}

// NaiveSettings — NaiveProxy (Chrome-like HTTP/2 or HTTP/3). Traffic looks like
// a normal browser talking to an HTTPS site. Managed via sing-box http/naive path
// or external naiveproxy binary when installed.
type NaiveSettings struct {
	Username     string            `json:"username,omitempty"`    // optional shared user; clients use own password
	Padding      bool              `json:"padding"`               // pad requests to defeat size fingerprint
	Concurrency  int               `json:"concurrency,omitempty"` // max concurrent streams (0 = default)
	SNI          string            `json:"sni,omitempty"`         // camouflage domain
	ALPN         []string          `json:"alpn,omitempty"`        // ["h2","http/1.1"] or ["h3"]
	CertFile     string            `json:"certFile,omitempty"`
	KeyFile      string            `json:"keyFile,omitempty"`
	HTTP3        bool              `json:"http3,omitempty"` // prefer QUIC/HTTP3 when true
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
}

type WireGuardSettings struct {
	PrivateKey string   `json:"privateKey"`
	PublicKey  string   `json:"publicKey,omitempty"` // server public (for clients)
	Address    string   `json:"address"`
	MTU        int      `json:"mtu"`
	DNS        []string `json:"dns"`
	// Optional TLS cover: when set, share links recommend connecting via a TLS/AnyTLS/ShadowTLS hop first.
	TLSCoverEnabled bool   `json:"tlsCoverEnabled,omitempty"`
	TLSCoverSNI     string `json:"tlsCoverSni,omitempty"`
	TLSCoverPort    int    `json:"tlsCoverPort,omitempty"`
}

type ShadowsocksSettings struct {
	Method string `json:"method"` // 2022-blake3-aes-128-gcm | 2022-blake3-aes-256-gcm | aes-256-gcm
	// Password is the SERVER key of a Shadowsocks-2022 inbound (base64, exact
	// key length). Per-client keys live on the clients; links carry both.
	Password string `json:"password,omitempty"`
}

// RealitySettings is the JSON shape stored in Inbound.StreamSettings.reality
// when Security == reality.
type RealitySettings struct {
	Dest         string   `json:"dest"`        // e.g. "www.microsoft.com:443"
	ServerNames  []string `json:"serverNames"` // multi-SNI for CDN / domain fronting resilience
	PrivateKey   string   `json:"privateKey"`
	PublicKey    string   `json:"publicKey"` // handed out to clients, not written to xray config
	ShortIDs     []string `json:"shortIds"`
	SpiderX      string   `json:"spiderX,omitempty"`
	RequireMlkem bool     `json:"requireMlkem,omitempty"` // post-quantum hybrid key share (Xray 26.9+)
}

// TLSSettings is the JSON shape stored in Inbound.StreamSettings.tls when
// Security == tls.
type TLSSettings struct {
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
	SNI      string `json:"sni"`
	ALPN     string `json:"alpn,omitempty"` // e.g. "h2,http/1.1"
}

// FragmentSettings is the modern TLS/TCP fragment dialer advertised to clients
// (Xray dialer, Hiddify, Streisand, NekoBox, v2rayNG forks).
type FragmentSettings struct {
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type,omitempty"`     // tlshello | tcp | 1-3 style
	Packets  string `json:"packets,omitempty"`  // tlshello | 1-3 | 1-5
	Length   string `json:"length,omitempty"`   // e.g. "100-200"
	Interval string `json:"interval,omitempty"` // delays ms e.g. "10-20"
	MaxSplit string `json:"maxSplit,omitempty"` // e.g. "100-200" (Xray 1.8.24+)
}

// TCPMaskRule is one TCP fragment/mask stage (Hiddify-style multi-mask).
type TCPMaskRule struct {
	Type     string `json:"type,omitempty"`    // fragment | mask label
	Packets  string `json:"packets,omitempty"` // e.g. "1-3"
	Lengths  string `json:"lengths,omitempty"` // e.g. "100-200"
	Delays   string `json:"delays,omitempty"`  // e.g. "10-20"
	MaxSplit string `json:"maxSplit,omitempty"`
}

// SockoptSettings are client dialer socket options advertised in share links.
type SockoptSettings struct {
	Enabled      bool   `json:"enabled"`
	TCPKeepAlive int    `json:"tcpKeepAliveIdle,omitempty"` // seconds
	TCPNoDelay   *bool  `json:"tcpNoDelay,omitempty"`
	Mark         int    `json:"mark,omitempty"`
	Interface    string `json:"interface,omitempty"`
	DialerProxy  string `json:"dialerProxy,omitempty"`
	TCPFastOpen  *bool  `json:"tcpFastOpen,omitempty"`
	Penetrate    bool   `json:"penetrate,omitempty"` // some clients: penetrate firewall
}

// MuxSettings enables connection multiplexing advertised to clients.
type MuxSettings struct {
	Enabled         bool   `json:"enabled"`
	Concurrency     int    `json:"concurrency,omitempty"`
	SessionTTL      int    `json:"sessionTTL,omitempty"` // seconds; 0 = client default
	XudpConcurrency int    `json:"xudpConcurrency,omitempty"`
	XudpProxyUDP443 string `json:"xudpProxyUDP443,omitempty"`
	Padding         bool   `json:"padding,omitempty"`
}

// SniffingOptions is the advanced Xray sniffing block (2024–2026 best practice).
// When empty, panel defaults to destOverride http+tls+quic with routeOnly.
type SniffingOptions struct {
	DestOverride    []string `json:"destOverride,omitempty"`    // http, tls, quic, fakedns
	MetadataOnly    bool     `json:"metadataOnly,omitempty"`    // prefer metadata without deep parse
	RouteOnly       bool     `json:"routeOnly,omitempty"`       // use sniffed domain only for routing
	DomainsExcluded []string `json:"domainsExcluded,omitempty"` // never sniff these
}

// FallbackEntry is a deceptive fallback for VLESS/Trojan: non-matching TLS/HTTP
// probes are proxied to a real web destination so scanners see a normal site.
type FallbackEntry struct {
	Name string `json:"name,omitempty"` // SNI match (optional)
	Alpn string `json:"alpn,omitempty"` // h2, http/1.1
	Path string `json:"path,omitempty"` // HTTP path match
	Dest string `json:"dest"`           // host:port or unix:/path or port number
	Xver int    `json:"xver,omitempty"` // PROXY protocol version to dest
}

// StreamSettings is the full transport envelope stored as JSON.
type StreamSettings struct {
	Network                Network           `json:"network"`
	Security               Security          `json:"security"`
	WSPath                 string            `json:"wsPath,omitempty"`
	WSHost                 string            `json:"wsHost,omitempty"`
	GRPCService            string            `json:"grpcServiceName,omitempty"`
	XHTTPPath              string            `json:"xhttpPath,omitempty"`
	XHTTPHost              string            `json:"xhttpHost,omitempty"`
	XHTTPMode              string            `json:"xhttpMode,omitempty"`
	XHTTPExtraJSON         string            `json:"xhttpExtraJson,omitempty"`
	HTTPUpgradePath        string            `json:"httpUpgradePath,omitempty"`
	HTTPUpgradeHost        string            `json:"httpUpgradeHost,omitempty"`
	HTTPUpgradeHeadersJSON string            `json:"httpUpgradeHeadersJson,omitempty"`
	AcceptProxyProto       bool              `json:"acceptProxyProtocol,omitempty"`
	TLSSettings            *TLSSettings      `json:"tlsSettings,omitempty"`
	Reality                *RealitySettings  `json:"realitySettings,omitempty"`
	Fragment               *FragmentSettings `json:"fragment,omitempty"`
	TCPMasks               []TCPMaskRule     `json:"tcpMasks,omitempty"` // multi-stage TCP masks
	Sockopt                *SockoptSettings  `json:"sockopt,omitempty"`
	Mux                    *MuxSettings      `json:"mux,omitempty"`
	Sniffing               *SniffingOptions  `json:"sniffing,omitempty"`
	Fallbacks              []FallbackEntry   `json:"fallbacks,omitempty"`
}

// InboundRevision stores a complete point-in-time inbound+client snapshot so
// an operator can roll back a bad edit or deletion without relying on an
// external database backup.
type InboundRevision struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	InboundID uint      `gorm:"index" json:"inboundId"`
	Snapshot  string    `gorm:"type:text" json:"-"`
	Reason    string    `gorm:"size:64" json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}
