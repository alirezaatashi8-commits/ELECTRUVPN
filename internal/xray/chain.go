package xray

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/routing"
	"gorm.io/gorm"
)

// ApplyEnabledChain builds advanced multi-hop + multi-outbound load-balanced exits
// (2026 style):
//
//	[optional chain-mid] → [optional chain-mid2] → chain-exit / balancer of rotate addresses
//
// Smart Geo rules that target proxy-ai / proxy-payment / proxy-gaming are
// redirected onto the same exit balancer so traffic never hits a missing tag.
func ApplyEnabledChain(cfg *Config, db *gorm.DB) {
	if cfg == nil || db == nil {
		return
	}
	var chain models.TunnelChain
	if err := db.Where("enabled = ?", true).Order("id asc").First(&chain).Error; err != nil {
		return
	}
	resolveChainHosts(db, &chain)

	exitHost := strings.TrimSpace(chain.ActiveExitHost)
	if exitHost == "" {
		exitHost = strings.TrimSpace(chain.ExitAddress)
	}
	if exitHost == "" || chain.ExitPort <= 0 {
		return
	}

	midHost := strings.TrimSpace(chain.ActiveMidHost)
	if midHost == "" {
		midHost = strings.TrimSpace(chain.MidAddress)
	}
	hasMid := midHost != "" && chain.MidPort > 0

	mid2Host := strings.TrimSpace(chain.ActiveMid2Host)
	if mid2Host == "" {
		mid2Host = strings.TrimSpace(chain.Mid2Address)
	}
	hasMid2 := mid2Host != "" && chain.Mid2Port > 0

	// Build chain: mid → mid2 → exit (each hop dials through previous)
	if hasMid {
		midOut := buildHopOutbound("chain-mid", hopSpec{
			Host: midHost, Port: chain.MidPort, Protocol: chain.MidProtocol,
			UUID: chain.MidUUID, Password: chain.MidPassword, Flow: chain.MidFlow,
			SNI: chain.MidSNI, PublicKey: chain.MidPublicKey, ShortID: chain.MidShortID,
			Fingerprint: chain.MidFingerprint, Network: chain.MidNetwork,
		}, "")
		if midOut != nil {
			cfg.Outbounds = append(cfg.Outbounds, *midOut)
		} else {
			hasMid = false
		}
	}

	dialer := ""
	if hasMid {
		dialer = "chain-mid"
	}

	if hasMid2 {
		mid2Out := buildHopOutbound("chain-mid2", hopSpec{
			Host: mid2Host, Port: chain.Mid2Port, Protocol: chain.Mid2Protocol,
			UUID: chain.Mid2UUID, Password: chain.Mid2Password, Flow: chain.Mid2Flow,
			SNI: chain.Mid2SNI, PublicKey: chain.Mid2PublicKey, ShortID: chain.Mid2ShortID,
			Fingerprint: chain.Mid2Fingerprint, Network: chain.Mid2Network,
		}, dialer)
		if mid2Out != nil {
			cfg.Outbounds = append(cfg.Outbounds, *mid2Out)
			dialer = "chain-mid2"
		} else {
			hasMid2 = false
		}
	}

	// Only healthy exits participate in the balancer (health probes run in background).
	// Falls back optimistically to configured addresses when no probes have run yet.
	exitHosts := routing.HealthyExitHosts(&chain)
	if len(exitHosts) == 0 {
		exitHosts = []string{exitHost}
	}

	exitTags := make([]string, 0, len(exitHosts))
	for i, host := range exitHosts {
		tag := "chain-exit"
		if i > 0 {
			tag = fmt.Sprintf("chain-exit-%d", i)
		}
		exitOut := buildHopOutbound(tag, hopSpec{
			Host: host, Port: chain.ExitPort, Protocol: chain.ExitProtocol,
			UUID: chain.ExitUUID, Password: chain.ExitPassword, Flow: chain.ExitFlow,
			SNI: chain.ExitSNI, PublicKey: chain.ExitPublicKey, ShortID: chain.ExitShortID,
			Fingerprint: chain.ExitFingerprint, Network: chain.ExitNetwork,
		}, dialer)
		if exitOut == nil {
			continue
		}
		cfg.Outbounds = append(cfg.Outbounds, *exitOut)
		exitTags = append(exitTags, tag)
	}
	if len(exitTags) == 0 {
		return
	}

	finalTag := exitTags[0]
	if len(exitTags) > 1 {
		// Multi-outbound load balancing: round-robin with automatic fallback.
		// Strategy can be switched later via panel setting; roundRobin is the
		// most predictable for multi-exit failover without sticky sessions.
		balTag := "chain-balancer"
		cfg.Routing.Balancers = append(cfg.Routing.Balancers, BalancerJSON{
			Tag:      balTag,
			Selector: exitTags,
			Strategy: &BalancerStrategy{Type: "roundRobin"},
			Fallback: exitTags[0],
		})
		finalTag = balTag
	}

	// Smart Geo categories + catch-all → same exit balancer
	cfg.Routing.Rules = append(cfg.Routing.Rules,
		RoutingRule{Type: "field", OutboundTag: finalTag, Domain: []string{
			"geosite:openai", "domain:openai.com", "domain:chatgpt.com", "domain:ai.com",
			"domain:anthropic.com", "domain:claude.ai", "domain:x.ai", "domain:grok.x.ai",
			"domain:gemini.google.com", "domain:bard.google.com", "domain:perplexity.ai",
		}},
		RoutingRule{Type: "field", OutboundTag: finalTag, Domain: []string{
			"domain:paypal.com", "domain:stripe.com", "domain:wise.com",
			"domain:revolut.com", "domain:binance.com", "domain:coinbase.com",
		}},
		RoutingRule{Type: "field", OutboundTag: finalTag, Domain: []string{
			"geosite:category-games", "domain:steamcommunity.com", "domain:steampowered.com",
			"domain:epicgames.com", "domain:riotgames.com", "domain:ea.com", "domain:origin.com",
		}},
		RoutingRule{Type: "field", Network: "tcp,udp", OutboundTag: finalTag},
	)
}

type hopSpec struct {
	Host, Protocol, UUID, Password, Flow, SNI, PublicKey, ShortID, Fingerprint, Network string
	Port                                                                                int
}

func buildHopOutbound(tag string, h hopSpec, dialerProxy string) *OutboundJSON {
	if h.Host == "" || h.Port <= 0 {
		return nil
	}
	proto := strings.ToLower(h.Protocol)
	if proto == "" {
		proto = "vless"
	}
	var settings json.RawMessage
	var stream map[string]any
	out := &OutboundJSON{Tag: tag}

	switch proto {
	case "shadowsocks", "ss", "ss2022":
		settings, _ = json.Marshal(map[string]any{
			"servers": []map[string]any{{
				"address": h.Host, "port": h.Port,
				"method": "2022-blake3-aes-256-gcm", "password": h.Password,
			}},
		})
		out.Protocol = "shadowsocks"
		out.Settings = settings
	case "trojan":
		settings, _ = json.Marshal(map[string]any{
			"servers": []map[string]any{{
				"address": h.Host, "port": h.Port, "password": h.Password,
			}},
		})
		out.Protocol = "trojan"
		out.Settings = settings
		stream = map[string]any{"network": orDefault(h.Network, "tcp"), "security": "tls"}
		if h.SNI != "" {
			stream["tlsSettings"] = map[string]any{"serverName": h.SNI, "fingerprint": orDefault(h.Fingerprint, "chrome")}
		}
	case "wireguard", "wg":
		// Minimal WireGuard outbound (keys expected in Password field as "privateKey|peerPublicKey")
		parts := strings.SplitN(h.Password, "|", 2)
		priv, peerPub := h.Password, ""
		if len(parts) == 2 {
			priv, peerPub = parts[0], parts[1]
		}
		settings, _ = json.Marshal(map[string]any{
			"secretKey": priv,
			"address":   []string{"10.0.0.2/32"},
			"peers": []map[string]any{{
				"publicKey":  peerPub,
				"endpoint":   fmt.Sprintf("%s:%d", h.Host, h.Port),
				"allowedIPs": []string{"0.0.0.0/0", "::/0"},
			}},
		})
		out.Protocol = "wireguard"
		out.Settings = settings
	default: // vless / vmess fallback to vless
		user := map[string]any{"id": h.UUID, "encryption": "none"}
		if h.Flow != "" {
			user["flow"] = h.Flow
		}
		settings, _ = json.Marshal(map[string]any{
			"vnext": []map[string]any{{
				"address": h.Host, "port": h.Port, "users": []map[string]any{user},
			}},
		})
		out.Protocol = "vless"
		out.Settings = settings
		stream = map[string]any{"network": orDefault(h.Network, "tcp"), "security": "none"}
		if h.PublicKey != "" {
			stream["security"] = "reality"
			stream["realitySettings"] = map[string]any{
				"serverName": orDefault(h.SNI, h.Host), "fingerprint": orDefault(h.Fingerprint, "chrome"),
				"publicKey": h.PublicKey, "shortId": h.ShortID, "spiderX": "/",
			}
		} else if h.SNI != "" {
			stream["security"] = "tls"
			stream["tlsSettings"] = map[string]any{"serverName": h.SNI, "fingerprint": orDefault(h.Fingerprint, "chrome")}
		}
	}

	if dialerProxy != "" {
		out.ProxySettings = &ProxySettings{Tag: dialerProxy}
		if stream == nil {
			stream = map[string]any{}
		}
		// also set sockopt for older xray compatibility
		stream["sockopt"] = map[string]any{"dialerProxy": dialerProxy}
	}
	if stream != nil {
		raw, _ := json.Marshal(stream)
		out.StreamSettings = raw
	}
	return out
}

// resolveChainHosts fills Active* from ServerNode IDs and optional auto selection.
func resolveChainHosts(db *gorm.DB, chain *models.TunnelChain) {
	if chain == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(chain.SelectionMode))
	if mode == "auto" {
		autoPickNodes(db, chain)
		return
	}
	// Manual: prefer linked ServerNode address when IDs set
	if chain.ExitNodeID > 0 {
		var n models.ServerNode
		if db.First(&n, chain.ExitNodeID).Error == nil && n.Enabled {
			if chain.ExitAddress == "" {
				chain.ExitAddress = n.Address
			}
			if chain.ExitPort == 0 && n.Port > 0 {
				chain.ExitPort = n.Port
			}
			if chain.ActiveExitHost == "" {
				chain.ActiveExitHost = n.Address
			}
		}
	}
	if chain.MidNodeID > 0 {
		var n models.ServerNode
		if db.First(&n, chain.MidNodeID).Error == nil && n.Enabled {
			if chain.MidAddress == "" {
				chain.MidAddress = n.Address
			}
			if chain.MidPort == 0 && n.Port > 0 {
				chain.MidPort = n.Port
			}
			if chain.ActiveMidHost == "" {
				chain.ActiveMidHost = n.Address
			}
		}
	}
	if chain.ActiveExitHost == "" {
		chain.ActiveExitHost = chain.ExitAddress
	}
	if chain.ActiveMidHost == "" {
		chain.ActiveMidHost = chain.MidAddress
	}
}

func autoPickNodes(db *gorm.DB, chain *models.TunnelChain) {
	strategy := chain.AutoStrategy
	if strategy == "" {
		strategy = "least_load"
	}
	cands, err := routing.SelectNodes(db, routing.SelectOptions{
		Strategy: strategy, Failover: true, HealthFilter: "healthy", MaxNodes: 16,
	})
	if err != nil || len(cands) == 0 {
		// fallback to configured addresses
		if chain.ActiveExitHost == "" {
			chain.ActiveExitHost = chain.ExitAddress
		}
		if chain.ActiveMidHost == "" {
			chain.ActiveMidHost = chain.MidAddress
		}
		return
	}
	// Exit = best candidate; Mid = second best (if different)
	exit := cands[0]
	chain.ActiveExitHost = exit.Address
	if chain.ExitPort == 0 && exit.Port > 0 {
		chain.ExitPort = exit.Port
	}
	chain.ExitNodeID = exit.NodeID
	if len(cands) > 1 {
		mid := cands[1]
		if mid.Address != exit.Address {
			chain.ActiveMidHost = mid.Address
			if chain.MidPort == 0 && mid.Port > 0 {
				chain.MidPort = mid.Port
			}
			chain.MidNodeID = mid.NodeID
		}
	} else if chain.MidAddress != "" {
		chain.ActiveMidHost = chain.MidAddress
	}
	_ = db.Model(chain).Updates(map[string]any{
		"active_exit_host": chain.ActiveExitHost,
		"active_mid_host":  chain.ActiveMidHost,
		"exit_node_id":     chain.ExitNodeID,
		"mid_node_id":      chain.MidNodeID,
	}).Error
}

// RotateChainHost cycles exit among ExitAddress + RotateAddresses.
func RotateChainHost(db *gorm.DB, chain *models.TunnelChain) string {
	if chain == nil {
		return ""
	}
	addrs := []string{}
	add := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" {
			return
		}
		for _, x := range addrs {
			if x == a {
				return
			}
		}
		addrs = append(addrs, a)
	}
	add(chain.ExitAddress)
	for _, p := range strings.Split(chain.RotateAddresses, ",") {
		add(p)
	}
	if chain.ExitNodeID > 0 {
		var n models.ServerNode
		if db.First(&n, chain.ExitNodeID).Error == nil {
			add(n.Address)
		}
	}
	// Also include healthy nodes when auto
	if strings.EqualFold(chain.SelectionMode, "auto") {
		cands, _ := routing.SelectNodes(db, routing.SelectOptions{Strategy: "least_load", Failover: true, MaxNodes: 8})
		for _, c := range cands {
			add(c.Address)
		}
	}
	if len(addrs) == 0 {
		return ""
	}
	cur := strings.TrimSpace(chain.ActiveExitHost)
	idx := 0
	for i, a := range addrs {
		if a == cur {
			idx = (i + 1) % len(addrs)
			break
		}
	}
	next := addrs[idx]
	_ = db.Model(chain).Update("active_exit_host", next).Error
	return next
}
