// Package obfuscation applies protocol-aware anti-DPI profiles.
// Profiles only strengthen existing transports; they do not inject dummy
// application-layer traffic (that belongs in the client/core, not the panel).
package obfuscation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
)

// Profile names exposed to API/UI.
const (
	ProfileOff        = "off"
	ProfileLight      = "light"
	ProfileBalanced   = "balanced"
	ProfileAggressive = "aggressive"
)

// NormalizeProfile maps free-form input to a known profile (default off).
func NormalizeProfile(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ProfileLight, "l", "1":
		return ProfileLight
	case ProfileBalanced, "b", "2", "medium", "default":
		return ProfileBalanced
	case ProfileAggressive, "a", "3", "strong", "max":
		return ProfileAggressive
	default:
		return ProfileOff
	}
}

// LabelToken returns the labels fragment stored on inbound (e.g. antidpi:balanced).
func LabelToken(profile string) string {
	p := NormalizeProfile(profile)
	if p == ProfileOff {
		return ""
	}
	return "antidpi:" + p
}

// ParseLabel extracts profile from inbound labels field.
func ParseLabel(labels string) string {
	for _, part := range strings.Split(labels, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if strings.HasPrefix(part, "antidpi:") {
			return NormalizeProfile(strings.TrimPrefix(part, "antidpi:"))
		}
	}
	return ProfileOff
}

// MergeLabel sets/replaces antidpi:* token in labels.
func MergeLabel(labels, profile string) string {
	parts := []string{}
	for _, part := range strings.Split(labels, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(strings.ToLower(part), "antidpi:") {
			continue
		}
		parts = append(parts, part)
	}
	if tok := LabelToken(profile); tok != "" {
		parts = append(parts, tok)
	}
	return strings.Join(parts, ", ")
}

func boolPtr(v bool) *bool { return &v }

// ApplyToStream mutates stream settings for VLESS/Trojan/VMess Reality/TLS paths.
// Existing Reality keys/dest are preserved when already set.
func ApplyToStream(stream *models.StreamSettings, profile string) {
	if stream == nil {
		return
	}
	p := NormalizeProfile(profile)
	if p == ProfileOff {
		return
	}

	// Fragment: client-side TLS hello split (subscription exports these).
	fragLen, fragInt, maxSplit := "50-120", "10-20", ""
	muxConc, muxPad := 4, false
	tcpMasks := false
	sockopt := false

	switch p {
	case ProfileLight:
		fragLen, fragInt = "50-100", "10-20"
		muxConc, muxPad = 4, false
	case ProfileBalanced:
		fragLen, fragInt, maxSplit = "100-200", "10-20", "100-200"
		muxConc, muxPad = 8, true
		tcpMasks = true
		sockopt = true
	case ProfileAggressive:
		fragLen, fragInt, maxSplit = "100-200", "8-15", "80-160"
		muxConc, muxPad = 8, true
		tcpMasks = true
		sockopt = true
	}

	if stream.Fragment == nil {
		stream.Fragment = &models.FragmentSettings{}
	}
	stream.Fragment.Enabled = true
	if stream.Fragment.Type == "" {
		stream.Fragment.Type = "tlshello"
	}
	if stream.Fragment.Packets == "" {
		stream.Fragment.Packets = "tlshello"
	}
	stream.Fragment.Length = fragLen
	stream.Fragment.Interval = fragInt
	stream.Fragment.MaxSplit = maxSplit

	if stream.Mux == nil {
		stream.Mux = &models.MuxSettings{}
	}
	stream.Mux.Enabled = true
	if stream.Mux.Concurrency <= 0 {
		stream.Mux.Concurrency = muxConc
	}
	stream.Mux.Padding = muxPad
	if stream.Mux.SessionTTL <= 0 {
		stream.Mux.SessionTTL = 300
	}
	if stream.Mux.XudpConcurrency <= 0 {
		stream.Mux.XudpConcurrency = 16
	}
	if stream.Mux.XudpProxyUDP443 == "" {
		stream.Mux.XudpProxyUDP443 = "reject"
	}

	if tcpMasks && len(stream.TCPMasks) == 0 {
		stream.TCPMasks = []models.TCPMaskRule{{
			Type: "fragment", Packets: "1-3", Lengths: fragLen, Delays: fragInt, MaxSplit: maxSplit,
		}}
	}

	if sockopt {
		if stream.Sockopt == nil {
			stream.Sockopt = &models.SockoptSettings{}
		}
		stream.Sockopt.Enabled = true
		if stream.Sockopt.TCPKeepAlive <= 0 {
			stream.Sockopt.TCPKeepAlive = 30
		}
		if stream.Sockopt.TCPNoDelay == nil {
			stream.Sockopt.TCPNoDelay = boolPtr(true)
		}
	}

	// Reality cover sites: only fill empties — never overwrite operator keys/dest.
	if stream.Security == models.SecurityReality || (stream.Reality != nil) {
		if stream.Reality == nil {
			stream.Reality = &models.RealitySettings{}
		}
		if strings.TrimSpace(stream.Reality.Dest) == "" {
			stream.Reality.Dest = "www.microsoft.com:443"
		}
		if len(stream.Reality.ServerNames) == 0 {
			stream.Reality.ServerNames = []string{"www.microsoft.com", "microsoft.com", "www.cloudflare.com"}
			if p == ProfileAggressive {
				stream.Reality.ServerNames = append(stream.Reality.ServerNames, "www.apple.com", "dl.google.com")
			}
		}
		if stream.Reality.SpiderX == "" {
			stream.Reality.SpiderX = "/"
		}
		if p == ProfileAggressive && len(stream.Fallbacks) == 0 {
			dest := stream.Reality.Dest
			stream.Fallbacks = []models.FallbackEntry{
				{Dest: dest},
				{Alpn: "h2", Dest: dest},
			}
		}
	}

	if stream.Sniffing == nil && (p == ProfileBalanced || p == ProfileAggressive) {
		stream.Sniffing = &models.SniffingOptions{
			DestOverride: []string{"http", "tls", "quic"},
			RouteOnly:    true,
		}
	}
}

// ApplyToHysteria2 sets salamander obfuscation + optional HTTP masquerade for sing-box.
func ApplyToHysteria2(st *models.Hysteria2Settings, profile string) {
	if st == nil {
		return
	}
	p := NormalizeProfile(profile)
	if p == ProfileOff {
		return
	}
	if st.ObfsType == "" {
		st.ObfsType = "salamander"
	}
	if strings.TrimSpace(st.ObfsPassword) == "" {
		st.ObfsPassword = randomHex(16)
	}
	// Masquerade: scanners hitting the port see a real HTTPS site (sing-box).
	if p == ProfileBalanced || p == ProfileAggressive {
		if st.MasqueradeType == "" {
			st.MasqueradeType = "proxy"
		}
		if strings.TrimSpace(st.MasqueradeURL) == "" {
			st.MasqueradeURL = "https://www.microsoft.com"
		}
	}
}

// ApplyToInboundJSON applies profile to inbound Settings + StreamSettings JSON blobs.
// Returns updated settings, streamSettings, labels.
func ApplyToInboundJSON(protocol models.Protocol, settingsJSON, streamJSON, labels, profile string) (settingsOut, streamOut, labelsOut string, err error) {
	p := NormalizeProfile(profile)
	labelsOut = MergeLabel(labels, p)
	settingsOut, streamOut = settingsJSON, streamJSON
	if p == ProfileOff {
		return settingsOut, streamOut, labelsOut, nil
	}

	switch protocol {
	case models.ProtocolHysteria2:
		var st models.Hysteria2Settings
		if strings.TrimSpace(settingsJSON) != "" {
			_ = json.Unmarshal([]byte(settingsJSON), &st)
		}
		ApplyToHysteria2(&st, p)
		b, e := json.Marshal(st)
		if e != nil {
			return settingsJSON, streamJSON, labelsOut, e
		}
		settingsOut = string(b)

	case models.ProtocolVLESS, models.ProtocolVMess, models.ProtocolTrojan:
		var stream models.StreamSettings
		if strings.TrimSpace(streamJSON) != "" {
			if e := json.Unmarshal([]byte(streamJSON), &stream); e != nil {
				return settingsJSON, streamJSON, labelsOut, e
			}
		}
		ApplyToStream(&stream, p)
		b, e := json.Marshal(stream)
		if e != nil {
			return settingsJSON, streamJSON, labelsOut, e
		}
		streamOut = string(b)

	case models.ProtocolTUIC:
		// Mild hardening only: ensure congestion + quic relay defaults stay in settings.
		var st models.TUICSettings
		if strings.TrimSpace(settingsJSON) != "" {
			_ = json.Unmarshal([]byte(settingsJSON), &st)
		}
		if st.CongestionControl == "" {
			st.CongestionControl = "bbr"
		}
		if st.UDPRelayMode == "" {
			st.UDPRelayMode = "quic"
		}
		if len(st.ALPN) == 0 {
			st.ALPN = []string{"h3"}
		}
		b, e := json.Marshal(st)
		if e != nil {
			return settingsJSON, streamJSON, labelsOut, e
		}
		settingsOut = string(b)

	case models.ProtocolNaive:
		var st models.NaiveSettings
		if strings.TrimSpace(settingsJSON) != "" {
			_ = json.Unmarshal([]byte(settingsJSON), &st)
		}
		st.Padding = true
		if len(st.ALPN) == 0 {
			st.ALPN = []string{"h2", "http/1.1"}
		}
		b, e := json.Marshal(st)
		if e != nil {
			return settingsJSON, streamJSON, labelsOut, e
		}
		settingsOut = string(b)
	}
	return settingsOut, streamOut, labelsOut, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("electruvpn-obfs-fallback-key"))[:n*2]
	}
	return hex.EncodeToString(b)
}
