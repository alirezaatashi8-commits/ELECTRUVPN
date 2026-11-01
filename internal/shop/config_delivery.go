package shop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/xray"
)

// BuildClientConfigs returns share link, plain text config lines, and a minimal
// sing-box client JSON for the given client.
func BuildClientConfigs(ib models.Inbound, cl models.Client, publicHost string) (link string, textBundle string, singboxJSON []byte, err error) {
	host := strings.TrimSpace(publicHost)
	if host == "" {
		host = "vpn.example.com"
	}
	link, err = xray.BuildShareLink(ib, cl, host)
	if err != nil {
		return "", "", nil, err
	}
	textBundle = link + "\n"
	// Minimal sing-box outbound wrapper (importable by many clients)
	sb := map[string]any{
		"outbounds": []any{
			map[string]any{
				"type":      "urltest",
				"tag":       "auto",
				"outbounds": []string{"proxy"},
				"url":       "https://www.gstatic.com/generate_204",
				"interval":  "5m",
			},
			map[string]any{
				"type": "direct",
				"tag":  "direct",
			},
		},
		"route": map[string]any{
			"final": "auto",
		},
		// Keep human-readable note; many apps accept subscription URL better than partial JSON.
		"_electruvpn": map[string]any{
			"shareLink": link,
			"email":     cl.Email,
			"subId":     cl.SubID,
			"protocol":  ib.Protocol,
			"remark":    ib.Remark,
		},
	}
	// Protocol-specific outbound (VLESS-like generic)
	outbound := map[string]any{
		"type":        strings.ToLower(string(ib.Protocol)),
		"tag":         "proxy",
		"server":      host,
		"server_port": ib.Port,
	}
	if cl.UUID != "" {
		outbound["uuid"] = cl.UUID
	}
	if cl.Password != "" {
		outbound["password"] = cl.Password
	}
	sb["outbounds"] = []any{outbound, map[string]any{"type": "direct", "tag": "direct"}}
	sb["route"] = map[string]any{"final": "proxy"}
	raw, _ := json.MarshalIndent(sb, "", "  ")
	return link, textBundle, raw, nil
}

func ConfigFilename(cl models.Client) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, cl.Email)
	if safe == "" {
		safe = "config"
	}
	return fmt.Sprintf("electruvpn-%s.json", safe)
}
