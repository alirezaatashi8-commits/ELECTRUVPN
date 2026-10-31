package config

import (
	"strings"
)

// DNS provider presets for panel operators. Values are stored in PanelSetting
// key "dns_provider". Optional key "dns_custom_resolvers" holds a comma-separated
// list used when provider == "custom".
const (
	DNSProviderCloudflare = "cloudflare"
	DNSProviderGoogle     = "google"
	DNSProviderQuad9      = "quad9"
	DNSProviderSystem     = "system"
	DNSProviderCustom     = "custom"
	DNSProviderAuto       = "auto" // Cloudflare + Google + Quad9 (default)
)

// Panel setting keys
const (
	SettingDNSProvider        = "dns_provider"
	SettingDNSCustomResolvers = "dns_custom_resolvers"
	SettingDNSApplyToEngines  = "dns_apply_to_engines" // "1" = regenerate xray/sing-box on change
)

// DNSPresetResolvers returns DoH endpoints for a named provider.
func DNSPresetResolvers(provider string) []string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case DNSProviderCloudflare, "cf":
		return []string{
			"https+local://1.1.1.1/dns-query",
			"https+local://1.0.0.1/dns-query",
			"tls+local://1.1.1.1:853",
		}
	case DNSProviderGoogle, "google-dns":
		return []string{
			"https+local://8.8.8.8/dns-query",
			"https+local://8.8.4.4/dns-query",
			"tls+local://8.8.8.8:853",
		}
	case DNSProviderQuad9:
		return []string{
			"https+local://9.9.9.9/dns-query",
			"https+local://149.112.112.112/dns-query",
			"tls+local://9.9.9.9:853",
		}
	case DNSProviderSystem, "local":
		return []string{"localhost"}
	case DNSProviderCustom:
		return nil // caller supplies custom list
	default: // auto
		return []string{
			"https+local://1.1.1.1/dns-query",
			"https+local://8.8.8.8/dns-query",
			"https+local://9.9.9.9/dns-query",
		}
	}
}

// ParseCustomResolvers splits a comma/semicolon/space list of resolver URLs.
func ParseCustomResolvers(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// NormalizeDNSProvider maps aliases to canonical ids.
func NormalizeDNSProvider(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case "cf", "cloudflare":
		return DNSProviderCloudflare
	case "google", "google-dns", "googledns":
		return DNSProviderGoogle
	case "quad9", "9.9.9.9":
		return DNSProviderQuad9
	case "system", "local", "localhost":
		return DNSProviderSystem
	case "custom":
		return DNSProviderCustom
	case "", "auto", "default":
		return DNSProviderAuto
	default:
		return DNSProviderAuto
	}
}
