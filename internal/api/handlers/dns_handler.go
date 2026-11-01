package handlers

import (
	"strings"

	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/gin-gonic/gin"
)

// DNSHandler manages panel-wide DNS provider selection (Cloudflare / Google / …).
// Changing the provider regenerates Xray + sing-box so tunnel DNS uses the new resolvers.
type DNSHandler struct{ S *core.Server }

func NewDNSHandler(s *core.Server) *DNSHandler { return &DNSHandler{S: s} }

// Get returns current DNS provider, resolvers in use, and available presets.
func (h *DNSHandler) Get(c *gin.Context) {
	provider := config.DNSProviderAuto
	if v := strings.TrimSpace(telegram.GetSetting(h.S.DB, config.SettingDNSProvider)); v != "" {
		provider = config.NormalizeDNSProvider(v)
	}
	custom := strings.TrimSpace(telegram.GetSetting(h.S.DB, config.SettingDNSCustomResolvers))
	effective := h.S.EffectiveDNSResolvers()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"provider":        provider,
		"customResolvers": custom,
		"effective":       effective,
		"applyToEngines":  telegram.GetSetting(h.S.DB, config.SettingDNSApplyToEngines) != "0",
		"presets": []gin.H{
			{"id": "auto", "label": "Auto (Cloudflare + Google + Quad9)", "resolvers": config.DNSPresetResolvers(config.DNSProviderAuto)},
			{"id": "cloudflare", "label": "Cloudflare (1.1.1.1)", "resolvers": config.DNSPresetResolvers(config.DNSProviderCloudflare)},
			{"id": "google", "label": "Google (8.8.8.8)", "resolvers": config.DNSPresetResolvers(config.DNSProviderGoogle)},
			{"id": "quad9", "label": "Quad9 (9.9.9.9)", "resolvers": config.DNSPresetResolvers(config.DNSProviderQuad9)},
			{"id": "system", "label": "System / Localhost", "resolvers": config.DNSPresetResolvers(config.DNSProviderSystem)},
			{"id": "custom", "label": "Custom DoH/DoT list", "resolvers": config.ParseCustomResolvers(custom)},
		},
		"note": "Server-side DNS for Xray/sing-box. Iranian domains still prefer local resolution.",
	}})
}

// Save sets dns_provider (+ optional custom list) and optionally regenerates engines.
func (h *DNSHandler) Save(c *gin.Context) {
	var req struct {
		Provider        string `json:"provider"`
		CustomResolvers string `json:"customResolvers"`
		ApplyNow        *bool  `json:"applyNow"` // default true
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid body"})
		return
	}
	provider := config.NormalizeDNSProvider(req.Provider)
	if provider == config.DNSProviderCustom {
		list := config.ParseCustomResolvers(req.CustomResolvers)
		if len(list) == 0 {
			c.JSON(400, gin.H{"success": false, "msg": "customResolvers required when provider=custom"})
			return
		}
		_ = telegram.SetSetting(h.S.DB, config.SettingDNSCustomResolvers, strings.Join(list, ","))
	}
	_ = telegram.SetSetting(h.S.DB, config.SettingDNSProvider, provider)

	apply := true
	if req.ApplyNow != nil {
		apply = *req.ApplyNow
	}
	var regenErr string
	if apply {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			regenErr = err.Error()
		}
	}
	auditAction(h.S, c, "dns.provider", provider, gin.H{
		"custom": req.CustomResolvers, "apply": apply, "regenError": regenErr,
	})
	obj := gin.H{
		"provider":  provider,
		"effective": h.S.EffectiveDNSResolvers(),
		"applied":   apply && regenErr == "",
	}
	if regenErr != "" {
		c.JSON(200, gin.H{"success": true, "msg": "saved but engine reload failed: " + regenErr, "obj": obj})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": obj})
}
