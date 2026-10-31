package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/electruvpn/electruvpn/internal/config"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/crypto"
	"github.com/electruvpn/electruvpn/internal/devices"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/privacy"
	"github.com/electruvpn/electruvpn/internal/routing"
	"github.com/electruvpn/electruvpn/internal/shop"
	"github.com/electruvpn/electruvpn/internal/telegram"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
	"gorm.io/gorm"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type SubscriptionHandler struct{ S *core.Server }

// NewSubscriptionHandler creates or exposes the NewSubscriptionHandler handler.
func NewSubscriptionHandler(s *core.Server) *SubscriptionHandler { return &SubscriptionHandler{S: s} }

func observedDeviceID(clientID uint, userAgent, platform string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", clientID, strings.TrimSpace(userAgent), strings.TrimSpace(platform))))
	return "subua_" + hex.EncodeToString(sum[:])[:32]
}

func subscriptionExpiredWithGrace(client models.Client) bool {
	if client.ExpiryTime == 0 {
		return false
	}
	now := time.Now().UnixMilli()
	if now < client.ExpiryTime {
		return false
	}
	if client.GracePeriodHours <= 0 {
		return true
	}
	return now >= client.ExpiryTime+int64(client.GracePeriodHours)*int64(time.Hour/time.Millisecond)
}

func subscriptionDeviceKey(client models.Client, c *gin.Context) string {
	ua := strings.TrimSpace(c.GetHeader("User-Agent"))
	platform := strings.TrimSpace(c.GetHeader("Sec-CH-UA-Platform"))
	if ua == "" {
		return ""
	}
	return observedDeviceID(client.ID, ua, platform)
}

func subscriptionAccessAllowed(h *SubscriptionHandler, c *gin.Context, client models.Client, overrideDeviceKey ...string) bool {
	if client.LinkLeakThreshold > 0 {
		var hits int64
		if err := h.S.DB.Model(&models.SubscriptionAccessLog{}).Where("client_id = ? AND created_at >= ?", client.ID, time.Now().Add(-time.Hour)).Count(&hits).Error; err != nil {
			return false
		}
		if hits >= int64(client.LinkLeakThreshold) {
			return false
		}
	}
	key := subscriptionDeviceKey(client, c)
	if len(overrideDeviceKey) > 0 && strings.TrimSpace(overrideDeviceKey[0]) != "" {
		key = strings.TrimSpace(overrideDeviceKey[0])
	}
	if err := devices.AllowAccess(h.S.DB, client, key); err != nil {
		return false
	}
	return true
}

func observeSubscriptionDevice(h *SubscriptionHandler, c *gin.Context, client models.Client) {
	ua := strings.TrimSpace(c.GetHeader("User-Agent"))
	platform := strings.TrimSpace(c.GetHeader("Sec-CH-UA-Platform"))
	if ua == "" {
		return
	}
	manufacturer, model, os, osVersion := inferDeviceInfo(ua, strings.TrimSpace(c.GetHeader("Sec-CH-UA-Model")), platform)
	if model == "" && os == "" {
		return
	}
	modelSource := "user-agent"
	if c.GetHeader("Sec-CH-UA-Model") != "" {
		modelSource = "client-hint"
	}
	if model == "" {
		modelSource = "unknown"
	}
	deviceID := observedDeviceID(client.ID, ua, platform)
	var d models.ClientDevice
	if err := h.S.DB.Where("client_id = ? AND device_id = ?", client.ID, deviceID).First(&d).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		d = models.ClientDevice{ClientID: client.ID, DeviceID: deviceID, Source: "subscription_refresh", ModelSource: modelSource, Active: !client.NewDeviceApproval}
	}
	if d.Name == "" {
		d.Name = model
	}
	if manufacturer != "" {
		d.Manufacturer = manufacturer
	}
	if model != "" {
		d.Model = model
	}
	if os != "" {
		d.OS = os
	}
	if osVersion != "" {
		d.OSVersion = osVersion
	}
	d.Source = "subscription_refresh"
	d.ModelSource = modelSource
	// Never revive a revoked device from passive observation.
	if d.RevokedAt != nil || (d.ID > 0 && !d.Active) {
		return
	}
	// New device: with manual approval enabled, record additional devices as
	// pending so an administrator can approve them. Pending devices never pass
	// subscriptionAccessAllowed and therefore cannot fetch configuration yet.
	if d.ID == 0 {
		if client.NewDeviceApproval {
			var known int64
			if err := h.S.DB.Model(&models.ClientDevice{}).Where("client_id = ?", client.ID).Count(&known).Error; err != nil {
				return
			}
			d.Active = known == 0
			if !d.Active {
				d.LastSeenAt = time.Now()
				_ = h.S.DB.Save(&d).Error
				return
			}
		} else {
			if err := devices.AllowAccess(h.S.DB, client, deviceID); err != nil {
				return
			}
			d.Active = true
		}
	}
	if !d.Active {
		// Pending approval — record last-seen metadata without activating.
		d.LastSeenAt = time.Now()
		_ = h.S.DB.Save(&d).Error
		return
	}
	d.RevokedAt = nil
	d.IP = privacy.IdentifierHash(privacy.NormalizeIP(c.ClientIP()), h.S.Config.JWTSecret)
	d.UserAgent = ""
	d.LastSeenAt = time.Now()
	_ = h.S.DB.Save(&d).Error
}

// Serve returns the base64 subscription body for one client's SubID. This
// route is intentionally NOT behind auth.RequireAuth -- it's the URL a
// client app polls directly, the same way it works in 3x-ui -- but it only
// ever exposes the one client matching the unguessable SubID in the path,
// never a listing.
// Serve renders a subscription configuration for the requested public subscription token.
func (h *SubscriptionHandler) Serve(c *gin.Context) {
	subID := c.Param("subId")

	var client models.Client
	if err := h.S.DB.Where("sub_id = ?", subID).First(&client).Error; err != nil {
		c.String(http.StatusNotFound, "")
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, client.InboundID).Error; err != nil {
		c.String(http.StatusNotFound, "")
		return
	}

	if !client.SubscriptionEnabled || !ib.SubscriptionEnabled || strings.EqualFold(client.SubscriptionStatus, "revoked") {
		subscriptionAccessLog(h.S, c, client.ID, 0, "", http.StatusForbidden, 0)
		c.String(http.StatusForbidden, "")
		return
	}
	if subscriptionExpiredWithGrace(client) {
		subscriptionAccessLog(h.S, c, client.ID, 0, "", http.StatusForbidden, 0)
		c.String(http.StatusForbidden, "")
		return
	}
	// Observe before enforcing access so a device requiring manual approval is
	// registered as pending. The access check below still blocks its config.
	observeSubscriptionDevice(h, c, client)
	if !subscriptionAccessAllowed(h, c, client) {
		subscriptionAccessLog(h.S, c, client.ID, 0, "", http.StatusTooManyRequests, 0)
		c.String(http.StatusTooManyRequests, "subscription access policy blocked this request")
		return
	}
	// Refresh the panel's configured IP host immediately before generating links.
	// This makes the next successful subscription refresh emit the currently detected
	// IPv4/IPv6 instead of an old literal address, without changing domain-based hosts.
	// Note: the subscription URL itself must still be reachable; use a stable domain
	// (recommended) or keep the old endpoint forwarding during an IP migration.
	if h.S != nil {
		h.S.AdaptPublicIPIfNeeded()
	}

	defaultAddress := strings.ToLower(strings.TrimSpace(client.PrimaryAddressType))
	if defaultAddress == "" {
		defaultAddress = strings.ToLower(strings.TrimSpace(client.SubscriptionAddressType))
	}
	if defaultAddress == "" || defaultAddress == "all" {
		defaultAddress = strings.ToLower(strings.TrimSpace(ib.SubscriptionDefaultAddress))
	}
	if defaultAddress == "" {
		defaultAddress = "ipv4"
	}
	// If a user opened the subscription endpoint through a literal IPv4 and
	// didn't explicitly request a family, default to IPv4. This avoids emitting
	// domain/dual links from an IP-based subscription URL.
	if _, _, splitErr := net.SplitHostPort(c.Request.Host); splitErr == nil {
		if reqHost, _, err := net.SplitHostPort(c.Request.Host); err == nil {
			if ip := net.ParseIP(strings.Trim(reqHost, "[]")); ip != nil && ip.To4() != nil && c.Query("family") == "" {
				defaultAddress = "ipv4"
			}
		}
	} else if ip := net.ParseIP(strings.Trim(c.Request.Host, "[]")); ip != nil && ip.To4() != nil && c.Query("family") == "" {
		defaultAddress = "ipv4"
	}
	addressType := strings.ToLower(strings.TrimSpace(c.DefaultQuery("family", defaultAddress)))
	if addressType != "ipv4" && addressType != "ipv6" && addressType != "domain" && addressType != "dual" && addressType != "all" {
		c.String(http.StatusBadRequest, "")
		return
	}
	// "all"/"dual" on a single-stack inbound falls back to that inbound's own mode
	// instead of returning an empty 400 (older shared links use ?family=all).
	addressType = effectiveLinkFamily(ib, addressType)
	if err := validateInboundFamily(ib, addressType); err != nil {
		subscriptionAccessLog(h.S, c, client.ID, 0, addressType, http.StatusBadRequest, 0)
		c.String(http.StatusBadRequest, "")
		return
	}
	if client.SubscriptionSmart && (ib.Maintenance || !ib.Enable || ib.IsLimited()) {
		slog.Warn("subscription unavailable: smart inbound disabled or in maintenance", "request_id", c.GetString("request_id"), "client_id", client.ID, "inbound_id", ib.ID, "enabled", ib.Enable, "maintenance", ib.Maintenance)
		subscriptionAccessLog(h.S, c, client.ID, 0, addressType, http.StatusServiceUnavailable, 0)
		c.String(http.StatusServiceUnavailable, "")
		return
	}
	requested := requestedAddressTypes(addressType)
	links := make([]string, 0, len(requested))
	linkMap := make(map[string]string, len(requested))
	for _, family := range requested {
		host := publicHostForFamily(h.S.Config, family)
		if host == "" && !strings.EqualFold(h.S.Config.SecurityMode, "strict") {
			host = c.Query("host")
			if host == "" {
				host = c.Request.Host
			}
			if parsedHost, _, err := net.SplitHostPort(host); err == nil {
				host = parsedHost
			}
		}
		if host == "" {
			slog.Error("subscription link generation failed: public host is empty", "request_id", c.GetString("request_id"), "client_id", client.ID, "inbound_id", ib.ID, "family", family)
			c.String(http.StatusServiceUnavailable, "")
			return
		}
		if err := validateHostFamily(host, family); err != nil {
			slog.Error("subscription link generation failed: host family mismatch", "request_id", c.GetString("request_id"), "client_id", client.ID, "inbound_id", ib.ID, "family", family, "error", err)
			c.String(http.StatusServiceUnavailable, "")
			return
		}
		link, err := xray.BuildShareLink(ib, client, host)
		if err != nil {
			slog.Error("subscription link generation failed: BuildShareLink", "request_id", c.GetString("request_id"), "client_id", client.ID, "inbound_id", ib.ID, "family", family, "error", err)
			c.String(http.StatusServiceUnavailable, "")
			return
		}
		links = append(links, link)
		linkMap[family] = link
		// For IPv4 subscriptions, include the immediately previous IPv4 as a
		// second entry. Apps that import all subscription entries will show the
		// new endpoint and the old one side by side; old IP reachability is not
		// guaranteed after a provider migration.
		if family == "ipv4" && h.S.Config.PreviousPublicHostV4 != "" && h.S.Config.PreviousPublicHostV4 != host && validateHostFamily(h.S.Config.PreviousPublicHostV4, "ipv4") == nil {
			if previousLink, err := xray.BuildShareLink(ib, client, h.S.Config.PreviousPublicHostV4); err == nil && previousLink != "" {
				if u, parseErr := url.Parse(previousLink); parseErr == nil {
					u.Fragment = strings.TrimSpace(u.Fragment + " (previous IPv4)")
					previousLink = u.String()
				}
				links = append(links, previousLink)
				linkMap["previous:ipv4"] = previousLink
			}
		}
	}
	// Smart multi-node: when subscription_smart is on, append links for healthy
	// ranked nodes (failover / load-balance). Unhealthy nodes are skipped.
	if client.SubscriptionSmart {
		strategy := "least_load"
		maxNodes := 8
		if h.S.ExperimentalLoadBalancerEnabled() {
			// Experimental path: prefer weighted ranking and expose more candidates.
			strategy = "weighted"
			maxNodes = 12
		}
		cands, _ := routing.SelectNodes(h.S.DB, routing.SelectOptions{
			Strategy: strategy, Failover: true, HealthFilter: "healthy", MaxNodes: maxNodes,
		})
		for _, cand := range cands {
			host := routing.HostForCandidate(cand)
			if host == "" {
				continue
			}
			if err := validateHostFamily(host, addressType); err != nil && addressType != "all" && addressType != "dual" {
				// node address may be any family; still try to build link
			}
			link, err := xray.BuildShareLink(ib, client, host)
			if err == nil && link != "" {
				// de-dup
				dup := false
				for _, existing := range links {
					if existing == link {
						dup = true
						break
					}
				}
				if !dup {
					links = append(links, link)
					linkMap["node:"+cand.Name] = link
				}
			}
		}
	}

	// Add enabled VLESS config profiles. Each profile is a real inbound and
	// reuses this client's UUID/quota/expiry; it is not a duplicate account.
	var profiles []models.ClientConfigProfile
	if err := h.S.DB.Where("client_id = ? AND enabled = ?", client.ID, true).Order("priority asc, id asc").Find(&profiles).Error; err != nil {
		slog.Error("subscription profile query failed", "request_id", c.GetString("request_id"), "client_id", client.ID, "error", err)
		c.String(http.StatusServiceUnavailable, "")
		return
	}
	for _, pr := range profiles {
		var pib models.Inbound
		if err := h.S.DB.First(&pib, pr.InboundID).Error; err != nil || !pib.Enable || pib.IsLimited() || pib.Protocol != models.ProtocolVLESS {
			continue
		}
		for _, family := range requested {
			if err := validateInboundFamily(pib, family); err != nil {
				continue
			}
			host := publicHostForFamily(h.S.Config, family)
			if host == "" && !strings.EqualFold(h.S.Config.SecurityMode, "strict") {
				host = c.Query("host")
				if host == "" {
					host = c.Request.Host
				}
				if parsed, _, err := net.SplitHostPort(host); err == nil {
					host = parsed
				}
			}
			if host == "" || validateHostFamily(host, family) != nil {
				continue
			}
			link, err := xray.BuildShareLink(pib, client, host)
			if err != nil {
				continue
			}
			links = append(links, link)
			key := fmt.Sprintf("profile:%d:%s", pr.ID, family)
			linkMap[key] = link
			if family == "ipv4" && h.S.Config.PreviousPublicHostV4 != "" && h.S.Config.PreviousPublicHostV4 != host && validateHostFamily(h.S.Config.PreviousPublicHostV4, "ipv4") == nil {
				if previousLink, prevErr := xray.BuildShareLink(pib, client, h.S.Config.PreviousPublicHostV4); prevErr == nil && previousLink != "" {
					if u, parseErr := url.Parse(previousLink); parseErr == nil {
						u.Fragment = strings.TrimSpace(u.Fragment + " (previous IPv4)")
						previousLink = u.String()
					}
					links = append(links, previousLink)
					linkMap[fmt.Sprintf("profile:%d:previous:ipv4", pr.ID)] = previousLink
				}
			}
		}
	}
	_ = h.S.DB.Model(&models.Client{}).Where("id = ?", client.ID).Updates(map[string]any{
		"subscription_hits":         gorm.Expr("subscription_hits + 1"),
		"subscription_last_seen_at": time.Now(),
	}).Error
	subscriptionAccessLog(h.S, c, client.ID, 0, addressType, http.StatusOK, len(links))
	dispatchWebhookEvent(h.S, "subscription.refresh", gin.H{"clientId": client.ID, "addressType": addressType, "links": len(links), "time": time.Now().UTC()})
	subBase := subscriptionBaseForFamily(h.S.Config, addressType)
	if subBase == "" {
		subBase = publicSubscriptionBase(h.S.Config, c, addressType)
	}
	subURL := subscriptionURLForFamily(subBase, c, h.S.Config.PanelBasePath, client.SubID, addressType)

	format := strings.ToLower(c.DefaultQuery("format", "base64"))
	body := xray.BuildSubscriptionBody(links)
	// Optional password-encrypted subscription (Argon2id + AES-256-GCM).
	// Standard clients use format=base64 (default). Encrypted format is for
	// secure hand-off / custom portals; decrypt with the same password.
	if format == "encrypted" || format == "enc" {
		password := strings.TrimSpace(c.Query("password"))
		if len(password) < 8 {
			c.String(http.StatusBadRequest, "encrypted subscription requires ?password= (min 8 chars)")
			return
		}
		env, err := crypto.EncryptWithPassword(password, []byte(body), client.Email)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		master := crypto.MasterKeyFromEnv(h.S.Config.ConfigEncryptionKey)
		if sig := crypto.SignPayload(master, env); sig != "" {
			c.Header("X-Electru-Config-Sig", sig)
		}
		c.Header("X-Electru-Config-Alg", "argon2id-aes-256-gcm")
		c.Data(http.StatusOK, "application/json; charset=utf-8", env)
		return
	}
	if wantsSubscriptionHTML(c, format) {
		domainHost, ipHost := resolveDomainAndIPHosts(h.S.Config, c)
		var domainLinks, ipLinks []string
		// Primary inbound dual hosts
		if domainHost != "" {
			if link, err := xray.BuildShareLink(ib, client, domainHost); err == nil && link != "" {
				domainLinks = appendUniqueLink(domainLinks, link)
			}
		}
		if ipHost != "" {
			if link, err := xray.BuildShareLink(ib, client, ipHost); err == nil && link != "" {
				ipLinks = appendUniqueLink(ipLinks, link)
			}
		}
		// Profiles with dual hosts
		for _, pr := range profiles {
			var pib models.Inbound
			if err := h.S.DB.First(&pib, pr.InboundID).Error; err != nil || !pib.Enable || pib.IsLimited() {
				continue
			}
			if domainHost != "" {
				if link, err := xray.BuildShareLink(pib, client, domainHost); err == nil && link != "" {
					domainLinks = appendUniqueLink(domainLinks, link)
				}
			}
			if ipHost != "" {
				if link, err := xray.BuildShareLink(pib, client, ipHost); err == nil && link != "" {
					ipLinks = appendUniqueLink(ipLinks, link)
				}
			}
		}
		// Fallback: if neither domain nor IP set, show whatever was built for the client apps
		if len(domainLinks) == 0 && len(ipLinks) == 0 {
			for _, l := range links {
				ipLinks = appendUniqueLink(ipLinks, l)
			}
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, subscriptionLandingHTML(subLandingData{
			SubURL: subURL, Email: client.Email, SubName: client.SubscriptionName,
			DomainHost: domainHost, IPHost: ipHost,
			DomainLinks: domainLinks, IPLinks: ipLinks,
			Client: client, RenewURL: resolveSubRenewURL(h.S, c), Daily: subDailyUsage(h.S.DB, client, 30),
		}))
		return
	}
	if len(links) == 0 {
		slog.Error("subscription produced no usable configuration links", "request_id", c.GetString("request_id"), "client_id", client.ID, "inbound_id", ib.ID, "family", addressType)
		c.String(http.StatusServiceUnavailable, "no usable configuration links")
		return
	}
	switch format {
	case "link", "uri":
		writeSubscriptionUserinfo(c, client)
		c.String(http.StatusOK, links[0])
	case "json":
		writeSubscriptionUserinfo(c, client)
		c.JSON(http.StatusOK, gin.H{"success": true, "protocol": ib.Protocol, "network": ib.Network, "security": ib.Security, "family": addressType, "link": links[0], "links": linkMap, "subscription": subURL,
			"upload": client.Up, "download": client.Down, "totalGB": client.TotalGB, "expiryTime": client.ExpiryTime,
			"recommendedApps": gin.H{"android": napsternetAndroidURL, "ios": napsternetIOSURL, "name": "NapsternetV"}})
	case "apps":
		c.String(http.StatusOK, "NapsternetV Android: "+napsternetAndroidURL+"\nNapsternetV iOS: "+napsternetIOSURL+"\n")
	default:
		writeSubscriptionUserinfo(c, client)
		master := crypto.MasterKeyFromEnv(h.S.Config.ConfigEncryptionKey)
		if sig := crypto.SignPayload(master, []byte(body)); sig != "" {
			c.Header("X-Electru-Config-Sig", sig)
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, body)
	}
}


// writeSubscriptionUserinfo sets the standard Subscription-Userinfo header so
// clients (v2rayN, Streisand, Hiddify, etc.) can show used/total traffic and expiry.
func writeSubscriptionUserinfo(c *gin.Context, cl models.Client) {
	upload := cl.Up
	if upload < 0 {
		upload = 0
	}
	download := cl.Down
	if download < 0 {
		download = 0
	}
	total := int64(0)
	if cl.TrafficLimitBytes > 0 {
		// Trial accounts are limited in MB via TrafficLimitBytes (TotalGB stays 0);
		// without this the app showed them as "unlimited".
		total = cl.TrafficLimitBytes
	} else if cl.TotalGB > 0 {
		total = cl.TotalGB * 1024 * 1024 * 1024
	}
	expire := int64(0)
	if cl.ExpiryTime > 0 {
		expire = cl.ExpiryTime / 1000 // header uses unix seconds
	}
	val := fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", upload, download, total, expire)
	c.Header("Subscription-Userinfo", val)
	c.Header("profile-update-interval", "1")
	title := strings.TrimSpace(cl.SubscriptionName)
	if title == "" {
		title = strings.TrimSpace(cl.Email)
	}
	if title != "" {
		c.Header("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte(title)))
	}
}

func publicSubscriptionBase(cfg *config.Config, c *gin.Context, family string) string {
	if cfg == nil || c == nil {
		return ""
	}
	host := ""
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "ipv4", "dual", "all":
		// The subscription endpoint itself must use one reachable origin. When
		// the base URL is not explicitly configured, use the current IPv4 even
		// when the payload asks for dual/all config families.
		host = strings.TrimSpace(cfg.PublicHostV4)
		if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
			host = strings.TrimSpace(cfg.PublicHost)
			if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
				host = ""
			}
		}
	case "ipv6":
		host = strings.Trim(strings.TrimSpace(cfg.PublicHostV6), "[]")
	case "domain":
		host = strings.TrimSpace(cfg.PublicHostDomain)
	}
	if host == "" {
		return ""
	}
	hostAuthority := host
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.To4() == nil {
		hostAuthority = "[" + ip.String() + "]"
	}
	scheme := "http"
	if cfg.PanelTLSEnabled || c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	port := cfg.PanelPort
	if port <= 0 {
		port = 443
		if scheme == "http" {
			port = 80
		}
	}
	if (scheme == "https" && port == 443) || (scheme == "http" && port == 80) {
		return scheme + "://" + hostAuthority + strings.TrimRight("/"+strings.Trim(cfg.PanelBasePath, "/"), "/")
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, hostAuthority, port, strings.TrimRight("/"+strings.Trim(cfg.PanelBasePath, "/"), "/"))
}

func subscriptionBaseForFamily(s *config.Config, family string) string {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "ipv4":
		if s.SubscriptionBaseURLV4 != "" {
			return s.SubscriptionBaseURLV4
		}
	case "ipv6":
		if s.SubscriptionBaseURLV6 != "" {
			return s.SubscriptionBaseURLV6
		}
	case "domain":
		if s.SubscriptionBaseURLDomain != "" {
			return s.SubscriptionBaseURLDomain
		}
	}
	return s.SubscriptionBaseURL
}

func subscriptionURL(base string, c *gin.Context, basePath, subID string) string {
	return subscriptionURLForFamily(base, c, basePath, subID, "ipv4")
}

func subscriptionURLForFamily(base string, c *gin.Context, basePath, subID, family string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = fmt.Sprintf("%s/sub/%s", requestPanelBase(c, basePath), subID)
	} else {
		base = base + "/sub/" + subID
	}
	if family == "" || family == "ipv4" {
		return base
	}
	return base + "?family=" + url.QueryEscape(family)
}

func requestPanelBase(c *gin.Context, basePath string) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	base := strings.Trim(strings.TrimSpace(basePath), "/")
	if base == "" {
		return scheme + "://" + c.Request.Host
	}
	return scheme + "://" + c.Request.Host + "/" + base
}

var androidModelRE = regexp.MustCompile(`(?i);\s*([^;()]+?)\s+Build/[^;)]*`)
var androidBuildRE = regexp.MustCompile(`(?i);\s*([A-Za-z0-9._-]+)\s+Build/`)
var androidVersionRE = regexp.MustCompile(`(?i)Android[ /-]([0-9][0-9A-Za-z._-]*)`)
var iOSVersionRE = regexp.MustCompile(`(?i)(?:CPU (?:iPhone )?OS|iPhone OS)[ _/]([0-9_]+)`)

func normalizeDeviceOS(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	case "ipados":
		return "iPadOS"
	case "windows":
		return "Windows"
	case "macos", "os x":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return strings.TrimSpace(v)
	}
}

func inferDeviceInfo(userAgent, modelHint, platformHint string) (manufacturer, model, os, osVersion string) {
	ua := strings.TrimSpace(userAgent)
	model = strings.TrimSpace(modelHint)
	platform := strings.TrimSpace(platformHint)
	lower := strings.ToLower(ua + " " + platform)
	switch {
	case strings.Contains(lower, "android"):
		os = "Android"
		if m := androidVersionRE.FindStringSubmatch(ua); len(m) > 1 {
			osVersion = strings.ReplaceAll(m[1], "_", ".")
		}
		if model == "" {
			if m := androidBuildRE.FindStringSubmatch(ua); len(m) > 1 {
				model = strings.TrimSpace(m[1])
			} else if m := androidModelRE.FindStringSubmatch(ua); len(m) > 1 {
				model = strings.TrimSpace(m[1])
			}
		}
	case strings.Contains(lower, "iphone"):
		os = "iOS"
		model = "iPhone"
		if m := iOSVersionRE.FindStringSubmatch(ua); len(m) > 1 {
			osVersion = strings.ReplaceAll(m[1], "_", ".")
		}
	case strings.Contains(lower, "ipad"):
		os = "iPadOS"
		model = "iPad"
		if m := iOSVersionRE.FindStringSubmatch(ua); len(m) > 1 {
			osVersion = strings.ReplaceAll(m[1], "_", ".")
		}
	case strings.Contains(lower, "windows"):
		os = "Windows"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macintosh"):
		os = "macOS"
	case strings.Contains(lower, "linux"):
		os = "Linux"
	}
	if strings.EqualFold(os, "Android") && manufacturer == "" {
		for _, prefix := range []struct{ key, name string }{{"pixel", "Google"}, {"sm-", "Samsung"}, {"samsung", "Samsung"}, {"redmi", "Xiaomi"}, {"mi ", "Xiaomi"}, {"m\u2011", "Xiaomi"}, {"oneplus", "OnePlus"}, {"huawei", "Huawei"}, {"honor", "Honor"}, {"motorola", "Motorola"}, {"moto ", "Motorola"}, {"nokia", "Nokia"}, {"asus", "ASUS"}, {"sony", "Sony"}} {
			if strings.Contains(strings.ToLower(model), prefix.key) {
				manufacturer = prefix.name
				break
			}
		}
	}
	return strings.TrimSpace(manufacturer), strings.TrimSpace(model), strings.TrimSpace(os), strings.TrimSpace(osVersion)
}

// RegisterDevice lets a client portal/app announce a device without exposing
// the admin API. The subscription token is the credential; only the matching
// ClientDevice row is touched.
func (h *SubscriptionHandler) RegisterDevice(c *gin.Context) {
	subID := strings.TrimSpace(c.Param("subId"))
	var client models.Client
	if err := h.S.DB.Where("sub_id = ?", subID).First(&client).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "subscription not found"})
		return
	}
	if !client.SubscriptionEnabled || strings.EqualFold(client.SubscriptionStatus, "revoked") || client.IsOverQuota() || !client.Enable || subscriptionExpiredWithGrace(client) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "subscription inactive"})
		return
	}
	var req struct {
		DeviceID     string `json:"deviceId"`
		Name         string `json:"name"`
		Manufacturer string `json:"manufacturer"`
		Model        string `json:"model"`
		OS           string `json:"os"`
		OSVersion    string `json:"osVersion"`
		AppName      string `json:"appName"`
		AppVersion   string `json:"appVersion"`
		Architecture string `json:"architecture"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.DeviceID) == "" || len(req.DeviceID) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "deviceId required"})
		return
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if !subscriptionAccessAllowed(h, c, client, deviceID) {
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "msg": "subscription access policy blocked this request"})
		return
	}
	manufacturer, model, os, osVersion := inferDeviceInfo(c.GetHeader("User-Agent"), req.Model, c.GetHeader("Sec-CH-UA-Platform"))
	deviceModelSource := "unknown"
	if strings.TrimSpace(req.Model) != "" {
		deviceModelSource = "reported"
	} else if strings.TrimSpace(c.GetHeader("Sec-CH-UA-Model")) != "" {
		deviceModelSource = "client-hint"
	} else if model != "" {
		deviceModelSource = "user-agent"
	}
	if req.Manufacturer != "" {
		manufacturer = strings.TrimSpace(req.Manufacturer)
	}
	if req.OS != "" {
		os = normalizeDeviceOS(req.OS)
	}
	if req.OSVersion != "" {
		osVersion = strings.TrimSpace(req.OSVersion)
	}
	if req.AppName == "" {
		req.AppName = strings.TrimSpace(c.GetHeader("X-Electru-App"))
	}
	if req.AppVersion == "" {
		req.AppVersion = strings.TrimSpace(c.GetHeader("X-Electru-App-Version"))
	}
	if req.Architecture == "" {
		req.Architecture = strings.TrimSpace(c.GetHeader("X-Electru-Arch"))
	}
	var d models.ClientDevice
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("client_id = ? AND device_id = ?", client.ID, deviceID).First(&d).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := devices.AllowAccess(tx, client, deviceID); err != nil {
				return err
			}
			limit := devices.EffectiveLimit(client)
			if limit > 0 {
				n, err := devices.CountActive(tx, client.ID, true)
				if err != nil {
					return err
				}
				if n >= int64(limit) {
					return devices.ErrLimitReached
				}
			}
			d = models.ClientDevice{ClientID: client.ID, DeviceID: deviceID, Name: strings.TrimSpace(req.Name), Active: !client.NewDeviceApproval}
		} else if !d.Active || d.RevokedAt != nil {
			return devices.ErrRevoked
		}
		if strings.TrimSpace(req.Name) != "" {
			d.Name = strings.TrimSpace(req.Name)
		}
		if manufacturer != "" {
			d.Manufacturer = manufacturer
		}
		if model != "" {
			d.Model = model
		}
		if os != "" {
			d.OS = os
		}
		if osVersion != "" {
			d.OSVersion = osVersion
		}
		if req.AppName != "" {
			d.AppName = strings.TrimSpace(req.AppName)
		}
		if req.AppVersion != "" {
			d.AppVersion = strings.TrimSpace(req.AppVersion)
		}
		if req.Architecture != "" {
			d.Architecture = strings.TrimSpace(req.Architecture)
		}
		d.Source = "subscription_refresh"
		d.ModelSource = deviceModelSource
		d.IP = privacy.IdentifierHash(privacy.NormalizeIP(c.ClientIP()), h.S.Config.JWTSecret)
		d.UserAgent = ""
		d.LastSeenAt = time.Now()
		return tx.Save(&d).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, devices.ErrLimitReached):
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "msg": "سقف تعداد دستگاه پر است"})
		case errors.Is(err, devices.ErrRevoked):
			c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": "این دستگاه قطع شده است"})
		case errors.Is(err, devices.ErrBindingLocked), errors.Is(err, devices.ErrNeedsApproval):
			c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": err.Error()})
		default:
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"deviceId": d.DeviceID, "clientId": client.ID, "name": d.Name, "manufacturer": d.Manufacturer, "model": d.Model, "os": d.OS, "osVersion": d.OSVersion, "appName": d.AppName, "appVersion": d.AppVersion, "architecture": d.Architecture, "source": d.Source, "modelSource": d.ModelSource, "lastSeenAt": d.LastSeenAt}})
}

const (
	napsternetAndroidURL = "https://play.google.com/store/apps/details?id=com.napsternetlabs.napsternetv"
	napsternetIOSURL     = "https://apps.apple.com/us/app/npv-tunnel/id1629465476"
)

func wantsSubscriptionHTML(c *gin.Context, format string) bool {
	if format == "html" || format == "page" || c.Query("page") == "1" {
		return true
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	ua := strings.ToLower(c.GetHeader("User-Agent"))
	if strings.Contains(accept, "text/html") && (strings.Contains(ua, "mozilla") || strings.Contains(ua, "chrome") || strings.Contains(ua, "safari") || strings.Contains(ua, "mobile")) {
		// VPN apps usually send empty or */* without browser UA tokens
		if !strings.Contains(ua, "clash") && !strings.Contains(ua, "v2ray") && !strings.Contains(ua, "sing-box") && !strings.Contains(ua, "shadowrocket") && !strings.Contains(ua, "streisand") {
			return true
		}
	}
	return false
}

func isLiteralIPHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host) != nil
}

func appendUniqueLink(dst []string, link string) []string {
	link = strings.TrimSpace(link)
	if link == "" {
		return dst
	}
	for _, x := range dst {
		if x == link {
			return dst
		}
	}
	return append(dst, link)
}

// resolveDomainAndIPHosts returns panel domain (if registered) and server IP host.
func resolveDomainAndIPHosts(cfg *config.Config, c *gin.Context) (domainHost, ipHost string) {
	if cfg != nil {
		domainHost = strings.TrimSpace(cfg.PublicHostDomain)
		ipHost = strings.TrimSpace(cfg.PublicHostV4)
		ph := strings.TrimSpace(cfg.PublicHost)
		if ipHost == "" && isLiteralIPHost(ph) {
			ipHost = ph
		}
		if domainHost == "" && ph != "" && !isLiteralIPHost(ph) {
			domainHost = ph
		}
	}
	// Browser host as domain fallback when not an IP
	reqHost := c.Request.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	reqHost = strings.TrimSpace(reqHost)
	if domainHost == "" && reqHost != "" && !isLiteralIPHost(reqHost) {
		domainHost = reqHost
	}
	if ipHost == "" && isLiteralIPHost(reqHost) {
		ipHost = reqHost
	}
	return domainHost, ipHost
}

func htmlLinkList(links []string, emptyMsg string, indexOffset int, qrDataURIs []string) string {
	if len(links) == 0 {
		if emptyMsg == "" {
			return ""
		}
		return `<div class="empty">` + emptyMsg + `</div>`
	}
	var b strings.Builder
	for i, l := range links {
		esc := html.EscapeString(l)
		b.WriteString(`<div class="link-row">`)
		b.WriteString(fmt.Sprintf(`<div class="link-idx">#%d</div>`, i+1))
		b.WriteString(`<div class="link-body">`)
		b.WriteString(`<div class="link-txt" dir="ltr">` + esc + `</div>`)
		b.WriteString(`<div class="link-actions">`)
		b.WriteString(fmt.Sprintf(`<button type="button" class="btn-mini" onclick="copyOne(%d)">کپی</button>`, indexOffset+i))
		if i < len(qrDataURIs) && qrDataURIs[i] != "" {
			b.WriteString(fmt.Sprintf(`<button type="button" class="btn-mini" onclick="showQR(%d)">QR</button>`, indexOffset+i))
		}
		b.WriteString(`</div></div>`)
		if i < len(qrDataURIs) && qrDataURIs[i] != "" {
			b.WriteString(`<img class="qr-thumb" src="` + qrDataURIs[i] + `" alt="QR" width="96" height="96" loading="lazy"/>`)
		}
		b.WriteString(`</div>`)
	}
	return b.String()
}

func qrDataURI(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil || len(png) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func qrDataURIsFor(links []string) []string {
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = qrDataURI(l)
	}
	return out
}

type subLandingData struct {
	SubURL, Email, SubName string
	DomainHost, IPHost     string
	DomainLinks, IPLinks   []string
	Client                 models.Client
	Daily                  []subDayUsage // last 30 days of usage (oldest first) for the usage chart
	RenewURL               string
}

// subDayUsage is one day of traffic for the usage chart (local calendar day).
type subDayUsage struct {
	Date string `json:"d"` // YYYY-MM-DD (server local time)
	Up   int64  `json:"u"`
	Down int64  `json:"w"`
}

// subDailyUsage builds per-day traffic deltas for one client from the periodic
// usage_snapshots table (cumulative counters). Counter resets (negative delta)
// are treated as "traffic since reset". The newest point uses the live counters.
func subDailyUsage(db *gorm.DB, cl models.Client, days int) []subDayUsage {
	if days < 1 {
		days = 30
	}
	loc := time.Local
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	out := make([]subDayUsage, days)
	idx := make(map[string]int, days)
	for i := range out {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out[i].Date = d
		idx[d] = i
	}
	if db == nil || cl.ID == 0 {
		return out
	}
	var prevUp, prevDown int64
	havePrev := false
	var base models.UsageSnapshot
	if res := db.Where("client_id = ? AND created_at < ?", cl.ID, start.UTC()).Order("created_at desc").Limit(1).Find(&base); res.Error == nil && res.RowsAffected > 0 {
		prevUp, prevDown, havePrev = base.Up, base.Down, true
	}
	var snaps []models.UsageSnapshot
	if err := db.Where("client_id = ? AND created_at >= ?", cl.ID, start.UTC()).Order("created_at asc").Find(&snaps).Error; err != nil {
		return out
	}
	add := func(up, down int64, at time.Time) {
		if !havePrev {
			prevUp, prevDown, havePrev = up, down, true
			return
		}
		du := up - prevUp
		if du < 0 {
			du = up
		}
		dd := down - prevDown
		if dd < 0 {
			dd = down
		}
		prevUp, prevDown = up, down
		if i, ok := idx[at.In(loc).Format("2006-01-02")]; ok {
			out[i].Up += du
			out[i].Down += dd
		}
	}
	for _, sn := range snaps {
		add(sn.Up, sn.Down, sn.CreatedAt)
	}
	add(cl.Up, cl.Down, now)
	return out
}

func resolveSubRenewURL(s *core.Server, c *gin.Context) string {
	if s == nil {
		return ""
	}
	// Preferred: Telegram user bot deep link
	cfg := shop.LoadReferralConfig(s.DB)
	uname := strings.TrimPrefix(strings.TrimSpace(cfg.BotUsername), "@")
	if uname == "" {
		uname = strings.TrimPrefix(strings.TrimSpace(telegram.GetSetting(s.DB, "telegram_user_bot_username")), "@")
	}
	if uname != "" {
		return "https://t.me/" + uname + "?start=shop"
	}
	// Fallback: web portal if served
	base := requestPanelBase(c, s.Config.PanelBasePath)
	return strings.TrimRight(base, "/") + "/portal/"
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
}

func fmtBytesGB(n int64) string {
	if n <= 0 {
		return "0"
	}
	gb := float64(n) / (1024 * 1024 * 1024)
	if gb < 0.01 {
		mb := float64(n) / (1024 * 1024)
		if mb < 1 {
			return fmt.Sprintf("%.0f KB", float64(n)/1024)
		}
		return fmt.Sprintf("%.1f MB", mb)
	}
	return fmt.Sprintf("%.2f", gb)
}

type subStatusView struct {
	Status, Expiry, Traffic string
	UpLabel, DownLabel      string
	UsedGB, LimitGB         string
	Pct                     float64
	Warn                    bool
	// Added for the redesigned page.
	Remain    string // remaining traffic, human readable
	DaysLeft  int    // -1 when the subscription has no expiry
	HasExpiry bool
	Level     string // ok | warn | bad
}

func subStatusViewOf(cl models.Client) subStatusView {
	v := subStatusView{Status: "فعال", Expiry: "نامحدود", Warn: false, DaysLeft: -1, Level: "ok"}
	bad := false
	if !cl.Enable || strings.EqualFold(cl.SubscriptionStatus, "revoked") || !cl.SubscriptionEnabled {
		v.Status = "غیرفعال"
		v.Warn = true
		bad = true
	}
	if cl.ExpiryTime > 0 {
		exp := time.UnixMilli(cl.ExpiryTime).Local()
		v.Expiry = exp.Format("2006/01/02 15:04")
		v.HasExpiry = true
		if time.Now().After(exp) {
			v.Status = "منقضی"
			v.Warn = true
			v.DaysLeft = 0
			bad = true
		} else {
			left := time.Until(exp)
			days := int(left.Hours() / 24)
			v.DaysLeft = days
			if left%(24*time.Hour) > 0 {
				v.DaysLeft = days + 1
			}
			if days <= 3 {
				v.Status = fmt.Sprintf("به‌زودی (%d روز)", days)
				v.Warn = true
			}
		}
	}
	used := cl.Up + cl.Down
	v.UpLabel = humanBytes(cl.Up)
	v.DownLabel = humanBytes(cl.Down)
	v.UsedGB = humanBytes(used)
	if cl.TotalGB > 0 {
		limit := cl.TotalGB * 1024 * 1024 * 1024
		v.LimitGB = fmt.Sprintf("%d", cl.TotalGB)
		if limit > 0 {
			v.Pct = float64(used) / float64(limit) * 100
			if v.Pct > 100 {
				v.Pct = 100
			}
		}
		v.Traffic = fmt.Sprintf("%s / %s GB", v.UsedGB, v.LimitGB)
		rem := limit - used
		if rem < 0 {
			rem = 0
		}
		v.Remain = humanBytes(rem)
		if used >= limit {
			v.Status = "اتمام حجم"
			v.Warn = true
			bad = true
		} else if v.Pct >= 90 {
			v.Warn = true
		}
	} else {
		v.LimitGB = "∞"
		v.Traffic = v.UsedGB + " · نامحدود"
		v.Remain = "نامحدود"
		v.Pct = 0
	}
	if bad {
		v.Level = "bad"
	} else if v.Warn {
		v.Level = "warn"
	}
	return v
}

// jsJSON marshals v for safe embedding inside an inline <script> (json.Marshal
// escapes <, > and & so a hostile remark cannot close the script tag).
func jsJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// subLinkMeta derives a protocol badge, a display name and host:port from a share link.
func subLinkMeta(link string, idx int) (proto, name, host string) {
	link = strings.TrimSpace(link)
	scheme := ""
	if i := strings.Index(link, "://"); i > 0 {
		scheme = strings.ToLower(link[:i])
	}
	switch scheme {
	case "hysteria2", "hy2":
		proto = "HY2"
	case "":
		proto = "LINK"
	default:
		proto = strings.ToUpper(scheme)
	}
	if i := strings.LastIndex(link, "#"); i >= 0 && i+1 < len(link) {
		if n, err := url.PathUnescape(link[i+1:]); err == nil {
			name = strings.TrimSpace(n)
		} else {
			name = strings.TrimSpace(link[i+1:])
		}
	}
	if scheme == "vmess" {
		raw := strings.TrimPrefix(link, "vmess://")
		if i := strings.IndexAny(raw, "#?"); i >= 0 {
			raw = raw[:i]
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(raw)
		}
		if err == nil {
			var vm struct {
				PS   string      `json:"ps"`
				Add  string      `json:"add"`
				Port interface{} `json:"port"`
			}
			if json.Unmarshal(b, &vm) == nil {
				if name == "" {
					name = strings.TrimSpace(vm.PS)
				}
				if vm.Add != "" {
					host = fmt.Sprintf("%s:%v", vm.Add, vm.Port)
				}
			}
		}
	} else if u, err := url.Parse(link); err == nil {
		host = u.Host
	}
	if name == "" {
		name = fmt.Sprintf("کانفیگ %d", idx+1)
	}
	return proto, name, host
}

// subConfigRows renders the config rows (QR thumbnail, protocol badge, name, actions).
// Index offsets keep copy/QR aligned with ALL_LINKS / ALL_QR (QR images are filled in by JS).
func subConfigRows(links []string, offset int, group, groupLabel string, qrs []string) string {
	var b strings.Builder
	for i, l := range links {
		proto, name, host := subLinkMeta(l, offset+i)
		hasQR := i < len(qrs) && qrs[i] != ""
		idx := offset + i
		b.WriteString(fmt.Sprintf(`<article class="cfg" data-group="%s">`, group))
		if hasQR {
			b.WriteString(fmt.Sprintf(`<button type="button" class="thumb" onclick="showQR(%d)" title="بزرگ‌نمایی QR" aria-label="بزرگ‌نمایی QR"><img data-qr="%d" width="92" height="92" alt="QR" loading="lazy"/></button>`, idx, idx))
		}
		b.WriteString(`<div class="cfg-main"><div class="cfg-top"><span class="proto" data-p="` + html.EscapeString(strings.ToLower(proto)) + `">` + html.EscapeString(proto) + `</span>`)
		b.WriteString(`<span class="tag">` + html.EscapeString(groupLabel) + `</span></div>`)
		b.WriteString(`<div class="cfg-name">` + html.EscapeString(name) + `</div>`)
		if host != "" {
			b.WriteString(`<div class="cfg-host" dir="ltr">` + html.EscapeString(host) + `</div>`)
		}
		b.WriteString(`</div><div class="cfg-act">`)
		b.WriteString(fmt.Sprintf(`<button type="button" class="ib primary" onclick="copyOne(%d)"><svg class="i"><use href="#i-copy"/></svg><span>کپی کانفیگ</span></button>`, idx))
		if hasQR {
			b.WriteString(fmt.Sprintf(`<button type="button" class="ib" onclick="showQR(%d)"><svg class="i"><use href="#i-qr"/></svg><span>QR</span></button>`, idx))
		}
		b.WriteString(`</div>`)
		b.WriteString(`<details class="cfg-raw"><summary>نمایش لینک کامل</summary><div class="raw" dir="ltr">` + html.EscapeString(l) + `</div></details>`)
		b.WriteString(`</article>`)
	}
	return b.String()
}

// subscriptionLandingHTML is shown when a human opens the sub URL in a browser.
func subscriptionLandingHTML(d subLandingData) string {
	title := strings.TrimSpace(d.SubName)
	if title == "" {
		title = strings.TrimSpace(d.Email)
	}
	if title == "" {
		title = "ElectruVPN"
	}
	st := subStatusViewOf(d.Client)

	// Domain links first, then IP links: indexes must stay aligned with ALL_LINKS / ALL_QR.
	all := make([]string, 0, len(d.DomainLinks)+len(d.IPLinks))
	all = append(all, d.DomainLinks...)
	all = append(all, d.IPLinks...)
	allQR := qrDataURIsFor(all)
	domainQR := allQR[:len(d.DomainLinks)]
	ipQR := allQR[len(d.DomainLinks):]

	// Progress ring (pathLength=100): offset = 100 - percent.
	ringOffset := 100.0 - st.Pct
	ringLabel := fmt.Sprintf("%.0f%%", st.Pct)
	if st.LimitGB == "∞" {
		ringOffset = 0
		ringLabel = "∞"
	}

	days := "∞"
	daysLbl := "بدون انقضا"
	if st.HasExpiry {
		days = fmt.Sprintf("%d", st.DaysLeft)
		daysLbl = "روز تا پایان اشتراک"
		if st.DaysLeft <= 0 {
			daysLbl = "اشتراک پایان یافته"
		}
	}

	renewBtn := ""
	topRenew := ""
	if d.RenewURL != "" {
		u := html.EscapeString(d.RenewURL)
		renewBtn = `<a class="btn btn-renew" href="` + u + `" target="_blank" rel="noopener"><svg class="i"><use href="#i-refresh"/></svg><span>تمدید / شارژ / خرید</span></a>`
		topRenew = `<a class="top-link" href="` + u + `" target="_blank" rel="noopener"><svg class="i"><use href="#i-bolt"/></svg><span>تمدید</span></a>`
	}

	subQR := qrDataURI(d.SubURL)
	subQRBlock := ""
	if subQR != "" {
		subQRBlock = `<div class="sub-qr"><img src="` + subQR + `" width="132" height="132" alt="QR لینک اشتراک"/><span>اسکن با گوشی</span></div>`
	}

	// Config tabs + rows
	nD, nI := len(d.DomainLinks), len(d.IPLinks)
	var tabs strings.Builder
	tabs.WriteString(fmt.Sprintf(`<button type="button" class="tab on" data-g="all">همه <b>%d</b></button>`, len(all)))
	if nD > 0 {
		tabs.WriteString(fmt.Sprintf(`<button type="button" class="tab" data-g="domain"><svg class="i"><use href="#i-globe"/></svg>دامنه <b>%d</b></button>`, nD))
	}
	if nI > 0 {
		tabs.WriteString(fmt.Sprintf(`<button type="button" class="tab" data-g="ip"><svg class="i"><use href="#i-server"/></svg>IP سرور <b>%d</b></button>`, nI))
	}
	rows := subConfigRows(d.DomainLinks, 0, "domain", "دامنه", domainQR) + subConfigRows(d.IPLinks, nD, "ip", "IP", ipQR)
	if rows == "" {
		rows = `<div class="empty">هنوز کانفیگی برای این اشتراک ساخته نشده است. با پشتیبانی تماس بگیرید.</div>`
	}
	hosts := ""
	if d.DomainHost != "" {
		hosts += `<span class="chip" dir="ltr"><svg class="i"><use href="#i-globe"/></svg>` + html.EscapeString(d.DomainHost) + `</span>`
	}
	if d.IPHost != "" {
		hosts += `<span class="chip" dir="ltr"><svg class="i"><use href="#i-server"/></svg>` + html.EscapeString(d.IPHost) + `</span>`
	}

	initial := "E"
	if rn := []rune(strings.TrimSpace(title)); len(rn) > 0 {
		initial = strings.ToUpper(string(rn[0]))
	}
	emailLine := strings.TrimSpace(d.Email)
	if emailLine == "" || emailLine == title {
		emailLine = "اشتراک اختصاصی شما"
	}
	pill := ""
	if st.Level == "warn" {
		pill = " warn"
	} else if st.Level == "bad" {
		pill = " bad"
	}

	pctW := fmt.Sprintf("%.1f", st.Pct)
	if st.LimitGB == "∞" {
		pctW = "8"
	}

	dailyData := d.Daily
	if dailyData == nil {
		dailyData = []subDayUsage{}
	}

	r := strings.NewReplacer(
		"__CSS__", subPageCSS+subSpeedCSS,
		"__PCTW__", pctW,
		"__CFGCOUNT__", fmt.Sprintf("%d", len(all)),
		"__TITLE__", html.EscapeString(title),
		"__INITIAL__", html.EscapeString(initial),
		"__EMAIL__", html.EscapeString(emailLine),
		"__STATUS__", html.EscapeString(st.Status),
		"__PILL__", pill,
		"__LEVEL__", st.Level,
		"__RINGOFF__", fmt.Sprintf("%.1f", ringOffset),
		"__RINGLBL__", ringLabel,
		"__USED__", html.EscapeString(st.UsedGB),
		"__REMAIN__", html.EscapeString(st.Remain),
		"__TOTAL__", html.EscapeString(st.Traffic),
		"__UP__", html.EscapeString(st.UpLabel),
		"__DOWN__", html.EscapeString(st.DownLabel),
		"__EXPIRY__", html.EscapeString(st.Expiry),
		"__DAYS__", days,
		"__DAYSLBL__", daysLbl,
		"__EXPMS__", fmt.Sprintf("%d", d.Client.ExpiryTime),
		"__RENEWTOP__", topRenew,
		"__RENEW__", renewBtn,
		"__SUBQR__", subQRBlock,
		"__SUBURL__", html.EscapeString(d.SubURL),
		"__TABS__", tabs.String(),
		"__HOSTS__", hosts,
		"__ROWS__", rows,
		"__SPEED__", subSpeedHTML,
		"__ANDROID__", html.EscapeString(napsternetAndroidURL),
		"__IOS__", html.EscapeString(napsternetIOSURL),
		"__DATA_LINKS__", jsJSON(all),
		"__DATA_QR__", jsJSON(allQR),
		"__DATA_SUB__", jsJSON(d.SubURL),
		"__DATA_DAILY__", jsJSON(dailyData),
		"__DATA_GROUPS__", jsJSON(subGroupsOf(nD, nI)),
		"__JS__", subPageJS+subSpeedJS,
	)
	return r.Replace(subPageTpl)
}

// subGroupsOf returns the group name of every entry in ALL_LINKS.
func subGroupsOf(nDomain, nIP int) []string {
	g := make([]string, 0, nDomain+nIP)
	for i := 0; i < nDomain; i++ {
		g = append(g, "domain")
	}
	for i := 0; i < nIP; i++ {
		g = append(g, "ip")
	}
	return g
}

// subPageTpl is the landing page (dark only). Tokens like __TITLE__ are filled by
// subscriptionLandingHTML (values are escaped there). It contains no backticks.
const subPageTpl = `<!DOCTYPE html>
<html lang="fa" dir="rtl">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"/>
<meta name="color-scheme" content="dark"/>
<meta name="theme-color" content="#070a14"/>
<meta name="robots" content="noindex,nofollow"/>
<title>__TITLE__ — ElectruVPN</title>
<script>try{var dk=localStorage.getItem('ev-design:'+location.pathname)||localStorage.getItem('ev-design');if(dk==='classic'||dk==='modern')document.documentElement.setAttribute('data-design',dk)}catch(e){}</script>
<style>
__CSS__
</style>
</head>
<body>
<svg width="0" height="0" style="position:absolute" aria-hidden="true" focusable="false">
  <defs>
    <linearGradient id="rg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#22d3ee"/><stop offset=".55" stop-color="#8b5cf6"/><stop offset="1" stop-color="#f472b6"/></linearGradient>
  </defs>
  <symbol id="i-copy" viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></symbol>
  <symbol id="i-qr" viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3zM20 14v.01M14 20h3M20 17v4"/></symbol>
  <symbol id="i-down" viewBox="0 0 24 24"><path d="M12 3v12m0 0l-4-4m4 4l4-4"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></symbol>
  <symbol id="i-up" viewBox="0 0 24 24"><path d="M12 15V3m0 0L8 7m4-4l4 4"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></symbol>
  <symbol id="i-clock" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></symbol>
  <symbol id="i-data" viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/></symbol>
  <symbol id="i-link" viewBox="0 0 24 24"><path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7"/><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7"/></symbol>
  <symbol id="i-globe" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.3 3 14.7 0 18M12 3c-3 3.3-3 14.7 0 18"/></symbol>
  <symbol id="i-server" viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/></symbol>
  <symbol id="i-bolt" viewBox="0 0 24 24"><path d="M13 2L4 14h7l-1 8 9-12h-7z"/></symbol>
  <symbol id="i-phone" viewBox="0 0 24 24"><rect x="7" y="2" width="10" height="20" rx="2"/><path d="M11 18h2"/></symbol>
  <symbol id="i-shield" viewBox="0 0 24 24"><path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/><path d="M9 12l2 2 4-4"/></symbol>
  <symbol id="i-refresh" viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/></symbol>
  <symbol id="i-check" viewBox="0 0 24 24"><path d="M4 12l5 5L20 6"/></symbol>
  <symbol id="i-layout" viewBox="0 0 24 24"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></symbol>
  <symbol id="i-chev" viewBox="0 0 24 24"><path d="M6 9l6 6 6-6"/></symbol>
  <symbol id="i-chart" viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M8 17v-6M13 17V7M18 17v-4"/></symbol>
  <symbol id="i-x" viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18"/></symbol>
  <symbol id="i-devices" viewBox="0 0 24 24"><rect x="2" y="5" width="14" height="10" rx="2"/><path d="M6 19h6M9 15v4"/><rect x="18" y="8" width="4" height="9" rx="1"/></symbol>
</svg>

<header class="topbar">
  <div class="topbar-in">
    <div class="brand">
      <div class="logo" aria-hidden="true"><svg class="i"><use href="#i-bolt"/></svg></div>
      <div class="brand-txt"><b>ElectruVPN</b><span>صفحه‌ی امن اشتراک</span></div>
    </div>
    <div class="top-actions">
      __RENEWTOP__
      <div class="design-wrap">
        <button type="button" class="top-link" id="designBtn" aria-haspopup="true" aria-expanded="false"><svg class="i"><use href="#i-layout"/></svg><span>طرح‌ها</span><svg class="i chev"><use href="#i-chev"/></svg></button>
        <div class="design-menu" id="designMenu" hidden role="menu">
          <button type="button" class="d-opt" data-design="modern" role="menuitemradio"><span class="d-prev m"></span><div><b>مدرن</b><span>کانفیگ‌ها بالا، آمار کنار صفحه</span></div><svg class="i ck"><use href="#i-check"/></svg></button>
          <button type="button" class="d-opt" data-design="classic" role="menuitemradio"><span class="d-prev c"></span><div><b>کلاسیک (اولیه)</b><span>همان طرح قبلی، بهینه برای ویندوز</span></div><svg class="i ck"><use href="#i-check"/></svg></button>
        </div>
      </div>
    </div>
  </div>
</header>

<div class="page">
  <section class="hero lv-__LEVEL__ reveal">
    <div class="avatar" aria-hidden="true">__INITIAL__</div>
    <div class="hero-txt"><h1>__TITLE__</h1><p class="muted">__EMAIL__</p></div>
    <div class="hero-chips">
      <span class="pill__PILL__">__STATUS__</span>
      <span class="hchip"><svg class="i"><use href="#i-data"/></svg>باقی‌مانده: <b>__REMAIN__</b></span>
      <span class="hchip"><svg class="i"><use href="#i-clock"/></svg><b>__DAYS__</b> __DAYSLBL__</span>
    </div>
    <div class="classic-only c-hero">
      <p class="muted">وضعیت ترافیک، کانفیگ‌ها، QR و تمدید — همه در یک صفحه.</p>
      <button type="button" class="btn btn-copy" onclick="copyAll()"><svg class="i"><use href="#i-copy"/></svg><span>کپی همه کانفیگ‌ها (__CFGCOUNT__)</span></button>
    </div>
  </section>

  <div class="layout">
    <div class="main">
      <section class="card reveal" id="configs">
        <div class="card-head">
          <span class="ico"><svg class="i"><use href="#i-shield"/></svg></span>
          <div><h2>کانفیگ‌های شما</h2><p class="muted">کپی کنید یا QR را با اپ VPN اسکن کنید</p></div>
          <button type="button" class="ib primary" onclick="copyAll()"><svg class="i"><use href="#i-copy"/></svg><span>کپی همه</span></button>
        </div>
        <div class="tabs" role="tablist">__TABS__</div>
        <div class="hosts">__HOSTS__</div>
        <div class="cfg-list" id="cfgList">__ROWS__</div>
      </section>

      <section class="card reveal c-usage" id="usage">
        <div class="card-head">
          <span class="ico"><svg class="i"><use href="#i-chart"/></svg></span>
          <div><h2>مصرف روزانه</h2><p class="muted">دانلود و آپلود هر روز؛ روی هر ستون بزنید یا موس ببرید</p></div>
          <div class="range" role="tablist" aria-label="بازه نمودار">
            <button type="button" class="rg" data-n="7">۷ روز</button>
            <button type="button" class="rg on" data-n="14">۱۴ روز</button>
            <button type="button" class="rg" data-n="30">۳۰ روز</button>
          </div>
        </div>
        <div class="u-sum">
          <div><span>مجموع بازه</span><b id="uTotal">—</b></div>
          <div><span>میانگین روزانه</span><b id="uAvg">—</b></div>
          <div><span>پرمصرف‌ترین روز</span><b id="uPeak">—</b><small id="uPeakD"></small></div>
          <div><span>امروز</span><b id="uToday">—</b></div>
        </div>
        <div class="u-tip" id="uTip" aria-live="polite"></div>
        <div class="u-chart" id="uChart"></div>
        <div class="u-legend"><span><i style="background:#8b5cf6"></i>دانلود</span><span><i style="background:#22d3ee"></i>آپلود</span></div>
      </section>

      <section class="card reveal c-sub">
        <div class="card-head">
          <span class="ico"><svg class="i"><use href="#i-link"/></svg></span>
          <div><h2>لینک سابسکریپشن</h2><p class="muted">این لینک را در اپ VPN وارد کنید؛ کانفیگ‌ها با هر به‌روزرسانی خودکار تازه می‌شوند.</p></div>
        </div>
        <div class="sub-row">
          <div class="sub-main">
            <div class="sub-box" dir="ltr" id="subBox">__SUBURL__</div>
            <button type="button" class="ib primary" onclick="copyText(SUB_URL)"><svg class="i"><use href="#i-copy"/></svg><span>کپی لینک ساب</span></button>
          </div>
          __SUBQR__
        </div>
      </section>

      <section class="card reveal c-apps">
        <div class="card-head">
          <span class="ico"><svg class="i"><use href="#i-phone"/></svg></span>
          <div><h2>دانلود اپ</h2><p class="muted">NapsternetV / Npv Tunnel</p></div>
        </div>
        <div class="apps">
          <a class="app android" href="__ANDROID__" target="_blank" rel="noopener"><svg class="i"><use href="#i-down"/></svg><span>دانلود برای اندروید</span></a>
          <a class="app ios" href="__IOS__" target="_blank" rel="noopener"><svg class="i"><use href="#i-down"/></svg><span>دانلود برای آیفون (iOS)</span></a>
        </div>
      </section>
    </div>

    <aside class="side">
      <section class="card traffic lv-__LEVEL__ reveal">
        <div class="card-head">
          <span class="ico"><svg class="i"><use href="#i-data"/></svg></span>
          <div><h2>مصرف ترافیک</h2><p class="muted">آپلود و دانلود جدا</p></div>
        </div>
        <div class="usage-bar classic-only"><i style="width:__PCTW__%"></i></div>
        <div class="ring-wrap modern-only">
          <svg class="ring" viewBox="0 0 120 120" role="img" aria-label="درصد مصرف ترافیک">
            <circle class="ring-bg" cx="60" cy="60" r="52" pathLength="100"/>
            <circle class="ring-p" cx="60" cy="60" r="52" pathLength="100" style="--off:__RINGOFF__"/>
          </svg>
          <div class="ring-c"><b>__RINGLBL__</b><span>مصرف شده</span></div>
        </div>
        <div class="kv">
          <div class="k-down"><span><svg class="i"><use href="#i-down"/></svg>دانلود</span><b>__DOWN__</b></div>
          <div class="k-up"><span><svg class="i"><use href="#i-up"/></svg>آپلود</span><b>__UP__</b></div>
          <div class="wide"><span>مجموع / سقف</span><b dir="ltr">__TOTAL__</b></div>
          <div class="wide"><span>انقضا</span><b dir="ltr">__EXPIRY__</b><small id="cdown" data-exp="__EXPMS__"></small></div>
        </div>
        __RENEW__
      </section>

      __SPEED__
    </aside>
  </div>
</div>

<div class="qr-modal" id="qrModal" role="dialog" aria-modal="true" aria-label="QR کانفیگ">
  <div class="box">
    <button type="button" class="icon-btn close" onclick="closeQR()" aria-label="بستن"><svg class="i"><use href="#i-x"/></svg></button>
    <img id="qrModalImg" alt="QR" width="256" height="256"/>
    <p class="muted" id="qrModalName"></p>
  </div>
</div>
<div class="toast" id="toast" role="status" aria-live="polite">کپی شد</div>
<script>
var ALL_LINKS = __DATA_LINKS__;
var ALL_QR = __DATA_QR__;
var ALL_GROUPS = __DATA_GROUPS__;
var SUB_URL = __DATA_SUB__;
var DAILY = __DATA_DAILY__;
__JS__
</script>
</body></html>`

const subPageCSS = `
:root{
  color-scheme:dark;
  --bg0:#060914;--bg1:#0c1224;--surface:rgba(255,255,255,.055);--surface2:rgba(255,255,255,.085);
  --stroke:rgba(255,255,255,.10);--stroke2:rgba(255,255,255,.18);
  --text:#f1f5ff;--muted:#93a0bf;--soft:rgba(255,255,255,.05);
  --violet:#8b5cf6;--cyan:#22d3ee;--pink:#f472b6;--amber:#fbbf24;--green:#34d399;--rose:#fb7185;
  --grad:linear-gradient(135deg,#7c3aed,#06b6d4);
  --shadow:0 24px 60px -26px rgba(0,0,0,.75);
  --hub:#0c1224;--tick:rgba(255,255,255,.55);--tick2:rgba(255,255,255,.25);
  --ink-cyan:#67e8f9;--ink-rose:#fda4af;--ink-green:#86efac;--ink-amber:#fcd34d;--ink-violet:#c4b5fd;--accent:#22d3ee;
  --radius:22px;
}
*{box-sizing:border-box}
html{-webkit-text-size-adjust:100%;scroll-behavior:smooth}
html,body{margin:0;padding:0;background:var(--bg0);color:var(--text)}
body{
  font-family:"Vazirmatn","Vazir","IRANSansX","IRANSans","Segoe UI Variable Text","Segoe UI",Tahoma,system-ui,-apple-system,sans-serif;
  -webkit-font-smoothing:antialiased;line-height:1.6;min-height:100vh;
  background:
    radial-gradient(1000px 560px at 100% -8%,rgba(139,92,246,.28),transparent 60%),
    radial-gradient(800px 480px at -8% 20%,rgba(34,211,238,.14),transparent 55%),
    radial-gradient(700px 440px at 50% 112%,rgba(244,114,182,.10),transparent 50%),
    linear-gradient(180deg,var(--bg1),var(--bg0));
  background-attachment:fixed;
}
h1,h2,h3,p{margin:0}
a{color:inherit}
.muted{color:var(--muted);font-size:.82rem}
.i{width:1.15em;height:1.15em;flex:none;fill:none;stroke:currentColor;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
:focus-visible{outline:2px solid var(--cyan);outline-offset:2px;border-radius:10px}

.topbar{position:sticky;top:0;z-index:20;backdrop-filter:blur(16px);-webkit-backdrop-filter:blur(16px);
  background:rgba(6,9,20,.72);border-bottom:1px solid var(--stroke);padding-top:env(safe-area-inset-top,0px)}
.topbar-in{max-width:1400px;margin:0 auto;padding:10px clamp(14px,3vw,32px);display:flex;align-items:center;justify-content:space-between;gap:12px}
.brand{display:flex;align-items:center;gap:12px}
.logo{width:40px;height:40px;border-radius:13px;display:grid;place-items:center;background:var(--grad);box-shadow:0 10px 26px -8px rgba(124,58,237,.7)}
.logo .i{width:22px;height:22px;fill:#fff;stroke:#fff}
.brand-txt{display:flex;flex-direction:column;line-height:1.25}
.brand-txt b{font-size:1.02rem;font-weight:800}
.brand-txt span{font-size:.72rem;color:var(--muted)}
.top-link{display:inline-flex;align-items:center;gap:6px;text-decoration:none;font-weight:700;font-size:.84rem;padding:9px 15px;border-radius:12px;
  background:var(--surface2);border:1px solid var(--stroke2);color:var(--text);transition:background .15s}
.top-link:hover{background:rgba(255,255,255,.14)}
.top-link .i{color:var(--amber)}
.icon-btn{width:38px;height:38px;display:grid;place-items:center;border-radius:12px;border:1px solid var(--stroke);background:var(--surface);color:var(--text);cursor:pointer}

.page{max-width:1400px;margin:0 auto;padding:20px clamp(14px,3vw,32px) calc(56px + env(safe-area-inset-bottom,0px))}

/* hero strip */
.hero{display:flex;align-items:center;gap:14px;flex-wrap:wrap;padding:16px 20px;margin-bottom:20px;border-radius:24px;border:1px solid var(--stroke);box-shadow:var(--shadow);
  background:linear-gradient(135deg,rgba(139,92,246,.20),rgba(34,211,238,.07) 55%,rgba(255,255,255,.03))}
.avatar{width:52px;height:52px;border-radius:17px;display:grid;place-items:center;font-weight:900;font-size:1.3rem;color:#fff;background:var(--grad);box-shadow:0 12px 28px -10px rgba(124,58,237,.8)}
.hero-txt{flex:1;min-width:150px}
.hero-txt h1{font-size:1.2rem;font-weight:800;word-break:break-word}
.hero-chips{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.hchip{display:inline-flex;align-items:center;gap:7px;font-size:.8rem;padding:7px 13px;border-radius:999px;background:var(--soft);border:1px solid var(--stroke);color:var(--muted)}
.hchip b{color:var(--text);font-weight:800}
.hchip .i{color:var(--accent)}
.pill{font-size:.76rem;font-weight:800;padding:7px 14px;border-radius:999px;background:rgba(52,211,153,.14);color:var(--green);border:1px solid rgba(52,211,153,.28);white-space:nowrap}
.pill.warn{background:rgba(251,191,36,.14);color:var(--amber);border-color:rgba(251,191,36,.32)}
.pill.bad{background:rgba(251,113,133,.14);color:var(--rose);border-color:rgba(251,113,133,.32)}
.pill.off{background:rgba(147,160,191,.14);color:var(--muted);border-color:rgba(147,160,191,.25)}

/* layout: configs first (right in RTL), traffic + speed on the side */
.layout{display:grid;gap:18px;grid-template-columns:minmax(0,1fr)}
.main,.side{display:grid;gap:18px;align-content:start;min-width:0}
@media (min-width:1020px){
  .layout{grid-template-columns:minmax(0,1fr) 390px;gap:22px;align-items:start}
  .side{position:sticky;top:84px}
}
@media (min-width:1500px){.layout{grid-template-columns:minmax(0,1fr) 430px}}

.card{background:var(--surface);backdrop-filter:blur(18px);-webkit-backdrop-filter:blur(18px);border:1px solid var(--stroke);border-radius:var(--radius);
  padding:clamp(16px,2vw,22px);box-shadow:var(--shadow);min-width:0}
.card-head{display:flex;gap:12px;align-items:center;margin-bottom:16px;flex-wrap:wrap}
.card-head>div{flex:1;min-width:170px}
.card-head h2{font-size:1.06rem;font-weight:800}
.card-head .ico{width:42px;height:42px;border-radius:14px;display:grid;place-items:center;background:var(--soft);border:1px solid var(--stroke);color:var(--accent)}
.card-head .ico .i{width:21px;height:21px}

/* tabs + configs */
.tabs{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px}
.tab{display:inline-flex;align-items:center;gap:7px;font:inherit;font-size:.84rem;font-weight:700;padding:8px 15px;border-radius:999px;cursor:pointer;color:var(--muted);background:var(--soft);border:1px solid var(--stroke);transition:all .15s}
.tab b{font-size:.72rem;background:var(--soft);border-radius:999px;padding:1px 8px}
.tab:hover{color:var(--text)}
.tab.on{color:#fff;background:rgba(139,92,246,.26);border-color:rgba(139,92,246,.55)}
.hosts{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:14px}
.hosts:empty{display:none}
.chip{display:inline-flex;align-items:center;gap:7px;font-size:.76rem;padding:6px 12px;border-radius:999px;background:var(--soft);border:1px solid var(--stroke);color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
.cfg-list{display:grid;gap:12px}
.cfg{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:8px 16px;align-items:center;padding:14px;border-radius:18px;background:rgba(0,0,0,.26);border:1px solid var(--stroke);transition:border-color .2s}
.cfg:hover{border-color:var(--stroke2)}
.cfg[hidden]{display:none}
.thumb{padding:5px;border:0;border-radius:14px;background:#fff;cursor:pointer;box-shadow:0 8px 20px -10px rgba(0,0,0,.6);display:block;transition:transform .15s}
.thumb:hover{transform:scale(1.04)}
.thumb img{display:block;border-radius:8px}
.cfg-top{display:flex;gap:8px;align-items:center;margin-bottom:5px}
.proto{font-size:.68rem;font-weight:900;letter-spacing:.06em;padding:3px 10px;border-radius:9px;color:var(--ink-violet);background:rgba(139,92,246,.16);border:1px solid rgba(139,92,246,.30);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
.proto[data-p="vless"]{color:var(--ink-cyan);background:rgba(34,211,238,.12);border-color:rgba(34,211,238,.28)}
.proto[data-p="trojan"]{color:var(--ink-rose);background:rgba(251,113,133,.12);border-color:rgba(251,113,133,.28)}
.proto[data-p="ss"]{color:var(--ink-green);background:rgba(52,211,153,.12);border-color:rgba(52,211,153,.28)}
.proto[data-p="hy2"],.proto[data-p="tuic"]{color:var(--ink-amber);background:rgba(251,191,36,.12);border-color:rgba(251,191,36,.28)}
.tag{font-size:.68rem;font-weight:700;padding:2px 9px;border-radius:999px;background:var(--soft);border:1px solid var(--stroke);color:var(--muted)}
.cfg-name{font-weight:800;font-size:.95rem;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.cfg-host{font-size:.74rem;color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin-top:2px}
.cfg-act{display:flex;gap:8px;flex-wrap:wrap}
.ib{display:inline-flex;align-items:center;justify-content:center;gap:7px;font:inherit;font-size:.8rem;font-weight:700;padding:9px 14px;border-radius:12px;cursor:pointer;color:var(--ink-cyan);background:rgba(34,211,238,.10);border:1px solid rgba(34,211,238,.24);transition:background .15s,transform .15s}
.ib:hover{background:rgba(34,211,238,.20)}.ib:active{transform:scale(.96)}
.ib.primary{color:#fff;background:linear-gradient(135deg,#7c3aed,#4f46e5);border-color:transparent;box-shadow:0 10px 24px -12px rgba(79,70,229,.9)}
.ib.primary:hover{filter:brightness(1.1)}
.cfg-raw{grid-column:1/-1}
.cfg-raw summary{cursor:pointer;font-size:.74rem;color:var(--muted);list-style:none;width:max-content}
.cfg-raw summary::-webkit-details-marker{display:none}
.cfg-raw summary:hover{color:var(--text)}
.raw,.sub-box{margin-top:8px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.72rem;line-height:1.55;word-break:break-all;text-align:left;padding:11px 13px;border-radius:13px;background:rgba(34,211,238,.07);border:1px dashed rgba(34,211,238,.28);color:#a5f3fc;max-height:160px;overflow:auto}
.sub-box{margin:0 0 12px;font-size:.8rem}
.empty{color:var(--muted);font-size:.88rem;padding:10px 2px}
@media (max-width:640px){
  .cfg{grid-template-columns:auto minmax(0,1fr)}
  .cfg-act{grid-column:1/-1}
  .cfg-act .ib{flex:1}
}

/* sub link */
.sub-row{display:flex;gap:18px;align-items:center;flex-wrap:wrap}
.sub-main{flex:1;min-width:240px}
.sub-qr{display:grid;gap:6px;justify-items:center;font-size:.74rem;color:var(--muted)}
.sub-qr img{background:#fff;padding:7px;border-radius:14px;display:block}

/* apps: neutral buttons */
.apps{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,230px),1fr));gap:10px}
.app{display:flex;align-items:center;justify-content:center;gap:9px;padding:13px 16px;border-radius:14px;text-decoration:none;font-weight:700;font-size:.9rem;
  color:var(--text);background:var(--soft);border:1px solid var(--stroke2);transition:background .15s,border-color .15s}
.app:hover{background:var(--surface2);border-color:rgba(255,255,255,.3)}

/* traffic */
.ring-wrap{position:relative;width:min(200px,62%);aspect-ratio:1;margin:6px auto 18px}
.ring{width:100%;height:100%;transform:rotate(-90deg);overflow:visible}
.ring circle{fill:none;stroke-width:9;stroke-linecap:round}
.ring-bg{stroke:var(--soft)}
.ring-p{stroke:url(#rg);stroke-dasharray:100;stroke-dashoffset:var(--off);animation:ringIn 1.3s cubic-bezier(.22,.9,.25,1) both;filter:drop-shadow(0 0 7px rgba(139,92,246,.55))}
.lv-warn .ring-p{stroke:var(--amber);filter:drop-shadow(0 0 7px rgba(251,191,36,.5))}
.lv-bad .ring-p{stroke:var(--rose);filter:drop-shadow(0 0 7px rgba(251,113,133,.5))}
@keyframes ringIn{from{stroke-dashoffset:100}}
.ring-c{position:absolute;inset:0;display:grid;place-content:center;text-align:center}
.ring-c b{font-size:2.2rem;font-weight:900;letter-spacing:-.02em;line-height:1.1}
.ring-c span{font-size:.78rem;color:var(--muted)}
.kv{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.kv>div{background:var(--soft);border:1px solid var(--stroke);border-radius:14px;padding:11px 13px;min-width:0}
.kv .wide{grid-column:1/-1}
.kv span{display:flex;align-items:center;gap:6px;font-size:.72rem;color:var(--muted)}
.kv b{font-size:.98rem;font-weight:800;overflow-wrap:anywhere}
.k-down .i{color:var(--ink-violet)}.k-up .i{color:var(--ink-cyan)}
.kv small{display:block;font-size:.72rem;color:var(--muted)}
.kv small:empty{display:none}
.btn{display:flex;align-items:center;justify-content:center;gap:9px;width:100%;text-decoration:none;border:0;border-radius:15px;padding:13px 16px;margin-top:12px;
  font:inherit;font-weight:800;font-size:.94rem;cursor:pointer;color:#fff;transition:transform .15s,filter .15s}
.btn:hover{filter:brightness(1.08)}.btn:active{transform:scale(.98)}
.btn-renew{background:linear-gradient(135deg,#f59e0b,#ea580c);box-shadow:0 14px 30px -12px rgba(234,88,12,.8)}

/* toast + modal */
.toast{position:fixed;bottom:calc(24px + env(safe-area-inset-bottom,0px));left:50%;transform:translateX(-50%) translateY(14px);background:#18203a;color:var(--text);
  border:1px solid var(--stroke2);box-shadow:var(--shadow);padding:11px 20px;border-radius:999px;font-size:.84rem;font-weight:700;opacity:0;transition:.22s;pointer-events:none;z-index:60}
.toast.on{opacity:1;transform:translateX(-50%) translateY(0)}
.qr-modal{display:none;position:fixed;inset:0;background:rgba(2,6,23,.78);backdrop-filter:blur(10px);-webkit-backdrop-filter:blur(10px);z-index:50;align-items:center;justify-content:center;padding:20px}
.qr-modal.on{display:flex}
.qr-modal .box{position:relative;background:#111a30;border:1px solid var(--stroke2);border-radius:26px;padding:26px 26px 20px;text-align:center;max-width:340px;width:100%;box-shadow:var(--shadow);animation:pop .22s ease-out}
.qr-modal img{width:100%;max-width:256px;height:auto;aspect-ratio:1;background:#fff;border-radius:18px;padding:11px}
.qr-modal .close{position:absolute;top:10px;inset-inline-start:10px}
.qr-modal .muted{margin-top:10px;word-break:break-word}
@keyframes pop{from{opacity:0;transform:scale(.94)}}


/* ---------- design menu ---------- */
.top-actions{display:flex;align-items:center;gap:8px}
.design-wrap{position:relative}
.top-link{font:inherit;cursor:pointer}
.top-link .chev{width:.95em;height:.95em;opacity:.7;transition:transform .2s}
#designBtn[aria-expanded="true"] .chev{transform:rotate(180deg)}
.design-menu{position:absolute;top:calc(100% + 10px);inset-inline-end:0;min-width:270px;background:#111a30;border:1px solid var(--stroke2);border-radius:18px;padding:8px;box-shadow:var(--shadow);z-index:40;animation:pop .16s ease-out}
.design-menu[hidden]{display:none}
.d-opt{display:flex;gap:12px;align-items:center;width:100%;text-align:start;font:inherit;color:var(--text);background:transparent;border:1px solid transparent;border-radius:13px;padding:10px 12px;cursor:pointer;transition:background .15s}
.d-opt:hover{background:var(--soft)}
.d-opt.on{background:rgba(139,92,246,.18);border-color:rgba(139,92,246,.45)}
.d-opt b{display:block;font-size:.88rem}.d-opt div span{font-size:.72rem;color:var(--muted)}
.d-prev{width:46px;height:36px;border-radius:9px;flex:none;border:1px solid var(--stroke2);background-color:#0c1224}
.d-prev.m{background-image:linear-gradient(90deg,#7c3aed 0 22%,transparent 22% 28%,#2b3658 28% 100%);background-size:100% 100%}
.d-prev.c{background-image:repeating-linear-gradient(180deg,#2b3658 0 7px,transparent 7px 10px);background-clip:content-box;padding:4px}
.d-opt .ck{margin-inline-start:auto;opacity:0;color:var(--green)}
.d-opt.on .ck{opacity:1}
@media (max-width:480px){#designBtn span:not(.chev){display:inline}.top-link{padding:9px 11px}}

/* ---------- classic design (original look), optimized for Windows / wide screens ---------- */
.classic-only{display:none}
html[data-design="classic"] .classic-only{display:block}
html[data-design="classic"] .modern-only{display:none}
html[data-design="classic"] .page{max-width:620px}
html[data-design="classic"] .layout{display:flex;flex-direction:column;gap:14px}
html[data-design="classic"] .main,html[data-design="classic"] .side{display:contents}
html[data-design="classic"] .traffic{order:1}
html[data-design="classic"] .c-speed{order:2}
html[data-design="classic"] #configs{order:4}
html[data-design="classic"] .c-sub{order:5}
html[data-design="classic"] .c-apps{order:6}
@media (min-width:1000px){
  html[data-design="classic"] .page{max-width:1160px}
  html[data-design="classic"] .layout{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;align-items:stretch}
  html[data-design="classic"] #configs{grid-column:1/-1}
  html[data-design="classic"] .cfg-list{grid-template-columns:repeat(2,minmax(0,1fr))}
}
@media (min-width:1600px){html[data-design="classic"] .page{max-width:1320px}}

html[data-design="classic"] .hero{display:grid;grid-template-columns:auto minmax(0,1fr) auto;gap:14px;align-items:center;position:relative;overflow:hidden;
  border-radius:28px;padding:22px 20px 18px;border-color:rgba(255,255,255,.12);
  background:linear-gradient(145deg,rgba(139,92,246,.22),rgba(34,211,238,.08) 45%,rgba(255,255,255,.03))}
html[data-design="classic"] .hero::after{content:"";position:absolute;inset:-40% auto auto -20%;width:200px;height:200px;border-radius:50%;
  background:radial-gradient(circle,rgba(34,211,238,.32),transparent 70%);filter:blur(8px);pointer-events:none}
html[data-design="classic"] .hero>*{position:relative;z-index:1}
html[data-design="classic"] .hchip{display:none}
html[data-design="classic"] .avatar{border-radius:14px;width:46px;height:46px}
html[data-design="classic"] .c-hero{grid-column:1/-1}
html[data-design="classic"] .c-hero p{margin-bottom:2px;line-height:1.7}
.btn-copy{background:linear-gradient(135deg,#7c3aed,#4f46e5);box-shadow:0 12px 28px -12px rgba(79,70,229,.9)}

html[data-design="classic"] .usage-bar{height:10px;border-radius:999px;background:rgba(255,255,255,.08);overflow:hidden;margin:4px 0 16px}
html[data-design="classic"] .usage-bar>i{display:block;height:100%;border-radius:inherit;background:linear-gradient(90deg,var(--cyan),var(--violet),var(--pink));box-shadow:0 0 12px rgba(139,92,246,.5);transition:width 1s ease}
html[data-design="classic"] .lv-warn .usage-bar>i{background:var(--amber)}
html[data-design="classic"] .lv-bad .usage-bar>i{background:var(--rose)}
html[data-design="classic"] .k-down{background:linear-gradient(160deg,rgba(139,92,246,.18),rgba(0,0,0,.15))}
html[data-design="classic"] .k-up{background:linear-gradient(160deg,rgba(34,211,238,.15),rgba(0,0,0,.15))}
html[data-design="classic"] .kv b{font-size:1.1rem}

html[data-design="classic"] .tabs{display:none}
html[data-design="classic"] .cfg{background:rgba(0,0,0,.28);border-radius:16px;padding:12px}
html[data-design="classic"] .cfg-raw summary{display:none}
html[data-design="classic"] .cfg-raw .raw{max-height:none;margin:0;padding:0;border:0;background:transparent;font-size:.64rem;opacity:.85;color:var(--text);line-height:1.5}
html[data-design="classic"] .ib.primary{color:var(--ink-cyan);background:rgba(34,211,238,.10);border:1px solid rgba(34,211,238,.24);box-shadow:none}
html[data-design="classic"] .card{border-radius:22px}
html[data-design="classic"] .apps{grid-template-columns:1fr}
html[data-design="classic"] .app{border:0;color:#fff;padding:14px 16px;border-radius:16px;font-weight:800;font-size:.95rem}
html[data-design="classic"] .app.android{background:linear-gradient(135deg,#22c55e,#15803d)}
html[data-design="classic"] .app.ios{background:linear-gradient(135deg,#3b82f6,#1d4ed8)}
html[data-design="classic"] .app:hover{filter:brightness(1.1)}
@media (hover:hover) and (pointer:fine){html[data-design="classic"] .card{transition:border-color .2s,transform .2s}html[data-design="classic"] .card:hover{border-color:var(--stroke2)}}

.reveal{animation:rise .5s cubic-bezier(.22,.9,.25,1) both}
@keyframes rise{from{opacity:0;transform:translateY(12px)}}
@media (prefers-reduced-motion:reduce){*,*::before,*::after{animation:none!important;transition:none!important;scroll-behavior:auto!important}}
@media (max-width:420px){.brand-txt span{display:none}.kv{grid-template-columns:1fr}}

/* ---------- daily usage chart ---------- */
.range{display:flex;gap:6px;flex-wrap:wrap}
.rg{font:inherit;font-size:.78rem;font-weight:700;padding:7px 13px;border-radius:999px;cursor:pointer;color:var(--muted);background:var(--soft);border:1px solid var(--stroke);transition:all .15s}
.rg:hover{color:var(--text)}
.rg.on{color:#fff;background:rgba(139,92,246,.26);border-color:rgba(139,92,246,.55)}
.u-sum{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;margin-bottom:14px}
.u-sum>div{background:var(--soft);border:1px solid var(--stroke);border-radius:14px;padding:10px 13px;min-width:0}
.u-sum span{display:block;font-size:.72rem;color:var(--muted)}
.u-sum b{font-size:.98rem;font-weight:800;overflow-wrap:anywhere;direction:ltr;unicode-bidi:isolate;display:inline-block}
.u-sum small{display:block;font-size:.68rem;color:var(--muted)}
.u-sum small:empty{display:none}
.u-tip{display:flex;flex-wrap:wrap;gap:4px 16px;align-items:center;min-height:38px;padding:8px 13px;margin-bottom:10px;border-radius:12px;background:rgba(0,0,0,.24);border:1px solid var(--stroke);font-size:.8rem}
.u-tip .d{font-weight:800}
.u-tip .k{color:var(--muted)}
.u-tip .k b{color:var(--text);font-weight:800;direction:ltr;unicode-bidi:isolate;display:inline-block}
.u-chart{direction:ltr;width:100%;min-width:0}
.u-chart svg{display:block;width:100%;height:auto;overflow:visible}
.u-grid{stroke:var(--stroke2);stroke-width:1;stroke-dasharray:3 5;opacity:.7}
.u-ax{fill:var(--muted);font-size:10px;font-family:inherit}
.u-bar-d{fill:url(#gDw)}
.u-bar-u{fill:url(#gUp)}
.u-hit{fill:transparent}
.u-col{cursor:pointer}
.u-col:hover .u-hit,.u-col.on .u-hit{fill:rgba(255,255,255,.07)}
.u-empty{fill:var(--muted);font-size:12px;font-family:inherit}
.u-legend{display:flex;gap:18px;justify-content:center;margin-top:10px;font-size:.76rem;color:var(--muted)}
.u-legend i{display:inline-block;width:10px;height:10px;border-radius:3px;margin-inline-end:6px;vertical-align:-1px}
html[data-design="classic"] .c-usage{order:3}
@media (min-width:1000px){html[data-design="classic"] .c-usage{grid-column:1/-1}}
`

const subPageJS = `
(function(){
  var $=function(s,r){return (r||document).querySelector(s)}, $$=function(s,r){return [].slice.call((r||document).querySelectorAll(s))};
  var tt=0;
  window.toast=function(m){var t=$('#toast');t.textContent=m||'کپی شد';t.classList.add('on');clearTimeout(tt);tt=setTimeout(function(){t.classList.remove('on')},1700)};
  window.copyText=async function(s){
    s=String(s||'');
    try{ if(navigator.clipboard&&navigator.clipboard.writeText&&window.isSecureContext){ await navigator.clipboard.writeText(s); toast('کپی شد ✓'); return; } }catch(e){}
    try{ var ta=document.createElement('textarea'); ta.value=s; ta.setAttribute('readonly',''); ta.style.cssText='position:fixed;left:-9999px;top:0'; document.body.appendChild(ta); ta.select(); ta.setSelectionRange(0,s.length+1); var ok=document.execCommand('copy'); document.body.removeChild(ta); toast(ok?'کپی شد ✓':'کپی نشد'); }catch(e){ toast('کپی نشد'); }
  };
  window.copyOne=function(i){ if(ALL_LINKS[i]) copyText(ALL_LINKS[i]); };
  var curGroup='all';
  window.copyAll=function(){
    var l=ALL_LINKS.filter(function(x,i){return x&&(curGroup==='all'||ALL_GROUPS[i]===curGroup)});
    if(!l.length){toast('لینکی نیست');return}
    copyText(l.filter(function(v,i,a){return a.indexOf(v)===i}).join('\n'));
  };
  window.showQR=function(i){
    var src=ALL_QR[i]; if(!src){toast('QR نیست');return}
    $('#qrModalImg').src=src;
    var n=$$('.cfg')[i]; var nm=n&&$('.cfg-name',n); $('#qrModalName').textContent=nm?nm.textContent:'';
    $('#qrModal').classList.add('on');
  };
  window.closeQR=function(){$('#qrModal').classList.remove('on')};
  $('#qrModal').addEventListener('click',function(e){if(e.target===this)closeQR()});
  document.addEventListener('keydown',function(e){if(e.key==='Escape')closeQR()});

  // QR thumbnails come from ALL_QR (kept out of the HTML to avoid sending every image twice)
  $$('img[data-qr]').forEach(function(im){var s=ALL_QR[Number(im.getAttribute('data-qr'))]; if(s) im.src=s});

  function setGroup(g){
    curGroup=g;
    $$('.tab').forEach(function(x){x.classList.toggle('on',x.getAttribute('data-g')===g)});
    $$('.cfg').forEach(function(c){c.hidden=!(g==='all'||c.getAttribute('data-group')===g)});
  }
  $$('.tab').forEach(function(b){b.addEventListener('click',function(){setGroup(b.getAttribute('data-g'))})});

  // designs: modern (default) / classic (original look); remembered per subscription in this browser
  var root=document.documentElement, dBtn=$('#designBtn'), dMenu=$('#designMenu');
  function applyDesign(d,save){
    if(d!=='classic')d='modern';
    root.setAttribute('data-design',d);
    $$('.d-opt').forEach(function(o){var on=o.getAttribute('data-design')===d;o.classList.toggle('on',on);o.setAttribute('aria-checked',on?'true':'false')});
    $$('details.cfg-raw').forEach(function(x){x.open=(d==='classic')});
    if(d==='classic')setGroup('all');
    if(save){try{localStorage.setItem('ev-design:'+location.pathname,d);localStorage.setItem('ev-design',d)}catch(e){}}
  }
  function menu(open){dMenu.hidden=!open;dBtn.setAttribute('aria-expanded',open?'true':'false')}
  dBtn.addEventListener('click',function(e){e.stopPropagation();menu(dMenu.hidden)});
  $$('.d-opt').forEach(function(o){o.addEventListener('click',function(){applyDesign(o.getAttribute('data-design'),true);menu(false);toast('طرح تغییر کرد')})});
  document.addEventListener('click',function(e){if(!dMenu.hidden&&!dMenu.contains(e.target))menu(false)});
  document.addEventListener('keydown',function(e){if(e.key==='Escape')menu(false)});
  applyDesign(root.getAttribute('data-design')||'modern',false);

  var cd=$('#cdown'), exp=cd?Number(cd.getAttribute('data-exp')):0;
  function tick(){
    if(!(exp>0))return;
    var ms=exp-Date.now();
    if(ms<=0){cd.textContent='';return}
    var d=Math.floor(ms/864e5), h=Math.floor(ms%864e5/36e5), m=Math.floor(ms%36e5/6e4);
    cd.textContent=(d>0?d+' روز و ':'')+h+' ساعت'+(d===0?' و '+m+' دقیقه':'')+' باقی‌مانده';
  }
  tick(); setInterval(tick,30000);
})();

(function(){
  var box=document.getElementById('uChart'); if(!box)return;
  var D=(typeof DAILY!=='undefined'&&DAILY)||[], tip=document.getElementById('uTip');
  var range=14, sel=-1, rows=[];
  function $i(i){return document.getElementById(i)}
  function fmtB(n){n=Number(n)||0;if(n<=0)return '0 B';var u=['B','KB','MB','GB','TB'],i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return (i<2?Math.round(n*10)/10:Math.round(n*100)/100)+' '+u[i]}
  var fL,fS,fW;
  try{fL=new Intl.DateTimeFormat('fa-IR-u-ca-persian',{month:'long',day:'numeric'});fS=new Intl.DateTimeFormat('fa-IR-u-ca-persian',{month:'numeric',day:'numeric'});fW=new Intl.DateTimeFormat('fa-IR',{weekday:'long'})}catch(e){}
  function dt(s){var p=s.split('-');return new Date(+p[0],+p[1]-1,+p[2],12)}
  function lab(s,short){try{return (short?fS:fL).format(dt(s))}catch(e){return s.slice(5)}}
  function wk(s){try{return fW.format(dt(s))}catch(e){return ''}}
  function nice(max){
    if(max<=0)return 1;
    var k=Math.floor(Math.log(max)/Math.log(1024)),p=Math.pow(1024,k),v=max/p,st=[1,2,4,8,12,16,20,40,80,100,200,400,800,1000,1024];
    for(var i=0;i<st.length;i++){if(v<=st[i])return st[i]*p}
    return v*p;
  }
  function tipFor(i){
    var r=rows[i]; if(!r){tip.textContent='';return}
    tip.innerHTML='<span class="d">'+wk(r.d)+' '+lab(r.d)+'</span><span class="k">دانلود: <b>'+fmtB(r.w)+'</b></span><span class="k">آپلود: <b>'+fmtB(r.u)+'</b></span><span class="k">مجموع: <b>'+fmtB(r.u+r.w)+'</b></span>';
  }
  function mark(i){
    sel=i; var cols=box.querySelectorAll('.u-col');
    for(var c=0;c<cols.length;c++){cols[c].setAttribute('class','u-col'+(Number(cols[c].getAttribute('data-i'))===i?' on':''))}
    tipFor(i);
  }
  function summary(){
    var tot=0,pk=-1,pv=0;
    rows.forEach(function(r,i){var t=r.u+r.w;tot+=t;if(t>pv){pv=t;pk=i}});
    $i('uTotal').textContent=fmtB(tot);
    $i('uAvg').textContent=fmtB(rows.length?tot/rows.length:0);
    $i('uPeak').textContent=pk>=0?fmtB(pv):'—';
    $i('uPeakD').textContent=pk>=0?lab(rows[pk].d):'';
    var l=rows[rows.length-1]; $i('uToday').textContent=l?fmtB(l.u+l.w):'—';
  }
  function render(){
    rows=D.slice(-range); var n=rows.length;
    if(!n){box.innerHTML='';tip.textContent='';return}
    var W=Math.max(300,box.clientWidth||640),H=210,pl=48,pr=6,pt=10,pb=26,iw=W-pl-pr,ih=H-pt-pb;
    var mx=0; rows.forEach(function(r){mx=Math.max(mx,r.u+r.w)});
    var empty=mx<=0, top=empty?1048576:nice(mx), bw=iw/n, bar=Math.max(3,Math.min(36,bw*0.62)), yb=pt+ih, s='';
    s+='<svg viewBox="0 0 '+W+' '+H+'" role="img" aria-label="نمودار مصرف روزانه"><defs><linearGradient id="gDw" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#a78bfa"/><stop offset="1" stop-color="#7c3aed"/></linearGradient><linearGradient id="gUp" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#67e8f9"/><stop offset="1" stop-color="#0891b2"/></linearGradient></defs>';
    for(var g=0;g<=4;g++){var y=yb-ih*g/4;s+='<line class="u-grid" x1="'+pl+'" x2="'+(W-pr)+'" y1="'+y+'" y2="'+y+'"/><text class="u-ax" x="'+(pl-7)+'" y="'+(y+3.5)+'" text-anchor="end">'+(g?fmtB(top*g/4):'0')+'</text>'}
    var step=n>20?5:(n>10?2:1);
    rows.forEach(function(r,i){
      var x=pl+bw*i+(bw-bar)/2, hd=ih*r.w/top, hu=ih*r.u/top;
      if(r.w>0&&hd<2)hd=2; if(r.u>0&&hu<2)hu=2;
      s+='<g class="u-col" data-i="'+i+'"><rect class="u-hit" x="'+(pl+bw*i)+'" y="'+pt+'" width="'+bw+'" height="'+ih+'"/>';
      if(r.w>0)s+='<rect class="u-bar-d" x="'+x+'" y="'+(yb-hd)+'" width="'+bar+'" height="'+hd+'" rx="2.5"/>';
      if(r.u>0)s+='<rect class="u-bar-u" x="'+x+'" y="'+(yb-hd-hu)+'" width="'+bar+'" height="'+hu+'" rx="2.5"/>';
      s+='</g>';
      if((n-1-i)%step===0)s+='<text class="u-ax" x="'+(pl+bw*i+bw/2)+'" y="'+(H-8)+'" text-anchor="middle">'+lab(r.d,true)+'</text>';
    });
    if(empty)s+='<text class="u-empty" x="'+(pl+iw/2)+'" y="'+(pt+ih/2)+'" text-anchor="middle">هنوز مصرفی ثبت نشده؛ نمودار با استفاده از VPN پر می‌شود</text>';
    s+='</svg>';
    box.innerHTML=s; summary(); mark(sel>=0&&sel<n?sel:n-1);
  }
  function pick(e){var t=e.target&&e.target.closest?e.target.closest('.u-col'):null; if(t)mark(Number(t.getAttribute('data-i')))}
  box.addEventListener('mousemove',pick); box.addEventListener('click',pick); box.addEventListener('touchstart',pick,{passive:true});
  box.addEventListener('mouseleave',function(){mark(rows.length-1)});
  var rg=document.querySelectorAll('.rg');
  for(var b=0;b<rg.length;b++){(function(btn){btn.addEventListener('click',function(){
    range=Number(btn.getAttribute('data-n'))||14; sel=-1;
    for(var k=0;k<rg.length;k++)rg[k].classList.toggle('on',rg[k]===btn);
    render();
  })})(rg[b])}
  var lastW=0,rt=0;
  function onSize(){var w=box.clientWidth;if(Math.abs(w-lastW)<2)return;lastW=w;clearTimeout(rt);rt=setTimeout(render,80)}
  if(window.ResizeObserver){new ResizeObserver(onSize).observe(box)}else{window.addEventListener('resize',onSize)}
  render();
})();
`

func napsternetAppsHTML(subURL, email string) string {
	return subscriptionLandingHTML(subLandingData{SubURL: subURL, Email: email})
}
