// Package netinfo detects the server's primary outbound IP addresses so the
// panel can auto-adapt share links when the host IP changes (e.g. after an
// IP filter / VPS rebuild). Domains are never auto-changed — operators set
// ELECTRU_PUBLIC_HOST_DOMAIN (or panel setting) manually.
package netinfo

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	cacheMu  sync.RWMutex
	cacheV4  string
	cacheV6  string
	cacheAt  time.Time
	cacheTTL = 60 * time.Second
)

// DetectOutboundIPv4 returns the public IPv4 the server appears to use. It first
// uses the kernel's UDP route address, then falls back to validated HTTPS
// identity endpoints for VPS networks where the local interface is behind NAT.
func DetectOutboundIPv4() string {
	cacheMu.RLock()
	if cacheV4 != "" && time.Since(cacheAt) < cacheTTL {
		v := cacheV4
		cacheMu.RUnlock()
		return v
	}
	cacheMu.RUnlock()

	ip := dialUDP("udp4", "1.1.1.1:53")
	if ip == "" {
		ip = dialUDP("udp4", "8.8.8.8:53")
	}
	if ip == "" {
		// On NAT-based VPS networks the UDP socket local address can be private.
		// Ask a small set of plain-text IP services as a fallback, validating the
		// result strictly before allowing it to affect generated client links.
		ip = detectExternalIP("https://api.ipify.org", "https://ipv4.icanhazip.com")
	}
	if ip == "" {
		ip = firstNonLoopback(true)
	}
	cacheMu.Lock()
	cacheV4, cacheAt = ip, time.Now()
	cacheMu.Unlock()
	return ip
}

// DetectOutboundIPv6 is the IPv6 counterpart (may be empty on IPv4-only hosts).

func detectExternalIP(urls ...string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	for _, endpoint := range urls {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "ElectruVPN-IP-Discovery/1.0")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(string(body)))
		if ip != nil && ip.To4() != nil && isUsablePublicIP(ip) {
			return ip.To4().String()
		}
	}
	return ""
}

func DetectOutboundIPv6() string {
	cacheMu.RLock()
	if cacheV6 != "" && time.Since(cacheAt) < cacheTTL {
		v := cacheV6
		cacheMu.RUnlock()
		return v
	}
	cacheMu.RUnlock()

	ip := dialUDP("udp6", "[2606:4700:4700::1111]:53")
	if ip == "" {
		ip = firstNonLoopback(false)
	}
	cacheMu.Lock()
	cacheV6, cacheAt = ip, time.Now()
	cacheMu.Unlock()
	return ip
}

func dialUDP(network, target string) string {
	c, err := net.DialTimeout(network, target, 2*time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	addr, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return ""
	}
	ip := addr.IP
	if !isUsablePublicIP(ip) {
		return ""
	}
	if ip4 := ip.To4(); network == "udp4" {
		if ip4 == nil {
			return ""
		}
		return ip4.String()
	}
	if ip.To4() != nil {
		return ""
	}
	return ip.String()
}

func firstNonLoopback(v4 bool) string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifs {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if !isUsablePublicIP(ip) {
				continue
			}
			if v4 {
				if ip4 := ip.To4(); ip4 != nil {
					return ip4.String()
				}
			} else if ip.To4() == nil {
				return ip.String()
			}
		}
	}
	return ""
}

// isUsablePublicIP excludes local, private, link-local and shared CGNAT
// addresses. Detecting a private address behind NAT must never overwrite a
// configured public host in generated client links.
func isUsablePublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	// RFC 6598 shared address space (100.64.0.0/10) is not a public endpoint.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// IsIP reports whether s is a literal IP (v4 or v6), not a domain name.
func IsIP(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	return net.ParseIP(s) != nil
}

// AutoPublicIPEnabled reads env ELECTRU_AUTO_PUBLIC_IP (default true).
func AutoPublicIPEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ELECTRU_AUTO_PUBLIC_IP")))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
