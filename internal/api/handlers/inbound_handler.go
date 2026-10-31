package handlers

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/auth"
	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/obfuscation"
	"github.com/electruvpn/electruvpn/internal/xray"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"gorm.io/gorm"
)

type InboundHandler struct{ S *core.Server }

// NewInboundHandler creates or exposes the NewInboundHandler handler.
func NewInboundHandler(s *core.Server) *InboundHandler { return &InboundHandler{S: s} }

// parseX25519Output understands both legacy ("Private key:" / "Public key:")
// and modern Xray ("PrivateKey:" / "Password:" or "PublicKey:") x25519 CLI output.
func parseX25519Output(out string) (priv, pub string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		var val string
		if i := strings.Index(line, ":"); i >= 0 {
			val = strings.TrimSpace(line[i+1:])
		}
		if val == "" {
			continue
		}
		switch {
		case strings.HasPrefix(low, "private key"), strings.HasPrefix(low, "privatekey"):
			priv = val
		case strings.HasPrefix(low, "public key"), strings.HasPrefix(low, "publickey"):
			pub = val
		case strings.HasPrefix(low, "password"):
			// Modern xray labels the public half as Password
			if pub == "" {
				pub = val
			}
		}
	}
	return priv, pub
}

// generateX25519KeypairPure produces an Xray-compatible REALITY keypair without
// depending on the xray binary (crypto/ecdh + RawURL base64).
func generateX25519KeypairPure() (priv, pub string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	priv = base64.RawURLEncoding.EncodeToString(key.Bytes())
	pub = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	return priv, pub, nil
}

// generateX25519Keypair tries the configured xray binary first (flexible parse),
// then falls back to pure-Go generation so the panel Generate button always works.
func (h *InboundHandler) generateX25519Keypair() (priv, pub string, err error) {
	bin := ""
	if h != nil && h.S != nil && h.S.Config != nil {
		bin = strings.TrimSpace(h.S.Config.XrayBinaryPath)
	}
	if bin != "" {
		if _, lookErr := exec.LookPath(bin); lookErr == nil {
			out, runErr := exec.Command(bin, "x25519").CombinedOutput()
			if runErr == nil {
				priv, pub = parseX25519Output(string(out))
				if priv != "" && pub != "" {
					return priv, pub, nil
				}
			}
		}
	}
	// Also try PATH "xray" as a last binary attempt
	if path, lookErr := exec.LookPath("xray"); lookErr == nil && path != bin {
		out, runErr := exec.Command(path, "x25519").CombinedOutput()
		if runErr == nil {
			priv, pub = parseX25519Output(string(out))
			if priv != "" && pub != "" {
				return priv, pub, nil
			}
		}
	}
	return generateX25519KeypairPure()
}

// PortAvailability reports whether a port is safe to use for a new inbound.
// We deliberately test both TCP and UDP: an administrator expects "busy" to
// mean another local service already owns that port, regardless of transport.
// The final create path repeats the same check immediately before inserting
// the inbound; the DB unique index is the last line of defense for concurrent
// requests.
// PortAvailability checks whether a requested inbound port is available for binding.
func (h *InboundHandler) PortAvailability(c *gin.Context) {
	requested := 0
	if raw := strings.TrimSpace(c.Query("port")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 || v > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "port must be between 0 and 65535"})
			return
		}
		requested = v
	}
	network := models.Network(strings.ToLower(strings.TrimSpace(c.Query("network"))))
	ipMode := normalizeIPMode(c.Query("ipMode"))
	if requested == 0 {
		strategy := strings.ToLower(strings.TrimSpace(c.Query("strategy")))
		port, err := h.findAvailablePort(strategy, network, ipMode)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "port": port, "available": true, "status": "available"})
		return
	}

	available, reason := h.portAvailableForNetwork(requested, network, 0, ipMode)
	status := "available"
	if !available {
		status = "busy"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "port": requested, "available": available, "status": status, "msg": reason})
}

func (h *InboundHandler) findAvailablePort(strategy string, network models.Network, ipMode ...string) (int, error) {
	if strategy == "recommended" {
		for _, candidate := range []int{443, 8443, 2053, 2087, 2083, 2096, 80, 8080} {
			if ok, _ := h.portAvailableForNetwork(candidate, network, 0, ipMode...); ok {
				return candidate, nil
			}
		}
	}

	// Keep automatically generated ports in the high, non-privileged range.
	// crypto/rand avoids predictable port selection without introducing a
	// global pseudo-random generator.
	const minPort = 20000
	const maxPort = 65000
	span := maxPort - minPort + 1
	for i := 0; i < 100; i++ {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return 0, fmt.Errorf("could not generate a random port: %w", err)
		}
		candidate := minPort + int((uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]))%uint32(span))
		if ok, _ := h.portAvailableForNetwork(candidate, network, 0, ipMode...); ok {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("could not find a free port after 100 attempts")
}

func (h *InboundHandler) portAvailableForNetwork(port int, network models.Network, excludeInboundID int, ipMode ...string) (bool, string) {
	if port < 1 || port > 65535 {
		return false, fmt.Sprintf("port %d is outside the valid range", port)
	}
	if network == "" {
		return h.portAvailable(port, excludeInboundID)
	}
	if port == h.S.Config.XrayAPIPort {
		return false, fmt.Sprintf("port %d is reserved for the local xray API", port)
	}
	if port == h.S.Config.PanelPort {
		return false, fmt.Sprintf("port %d is reserved for the ElectruVPN panel", port)
	}
	var count int64
	q := h.S.DB.Model(&models.Inbound{}).Where("port = ?", port)
	if excludeInboundID > 0 {
		q = q.Where("id <> ?", excludeInboundID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, "could not verify the port in the database"
	}
	if count > 0 {
		return false, fmt.Sprintf("port %d is already assigned to another inbound", port)
	}
	mode := "ipv4"
	if len(ipMode) > 0 && ipMode[0] != "" {
		mode = normalizeIPMode(ipMode[0])
	}
	tcpNeeded := network != models.NetworkUDP && network != models.NetworkQUIC
	udpNeeded := network == models.NetworkUDP || network == models.NetworkQUIC || network == models.NetworkMKCP
	check := func(proto, addr, label string) (bool, string) {
		if proto == "tcp" {
			ln, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
			if err != nil {
				return false, fmt.Sprintf("port %d is busy on %s", port, label)
			}
			_ = ln.Close()
		} else {
			pc, err := net.ListenPacket("udp", net.JoinHostPort(addr, strconv.Itoa(port)))
			if err != nil {
				return false, fmt.Sprintf("port %d is busy on %s", port, label)
			}
			_ = pc.Close()
		}
		return true, ""
	}

	// A Linux IPv6 wildcard listener normally accepts IPv4 too when
	// net.ipv6.bindv6only=0. Checking 0.0.0.0 and :: independently in that
	// case produces a false "busy" result because the first temporary socket
	// intentionally owns the second address family. Mirror the kernel's actual
	// dual-stack mode instead.
	if mode == "dual" {
		v6Only := true
		if raw, err := os.ReadFile("/proc/sys/net/ipv6/bindv6only"); err == nil && strings.TrimSpace(string(raw)) == "0" {
			v6Only = false
		}
		if tcpNeeded {
			if ok, msg := check("tcp", "::", "IPv6/dual TCP"); !ok {
				return false, msg
			}
			if v6Only {
				if ok, msg := check("tcp", "0.0.0.0", "IPv4 TCP"); !ok {
					return false, msg
				}
			}
		}
		if udpNeeded {
			if ok, msg := check("udp", "::", "IPv6/dual UDP"); !ok {
				return false, msg
			}
			if v6Only {
				if ok, msg := check("udp", "0.0.0.0", "IPv4 UDP"); !ok {
					return false, msg
				}
			}
		}
		return true, fmt.Sprintf("port %d is available", port)
	}

	addr := "0.0.0.0"
	label := "IPv4"
	if mode == "ipv6" {
		addr, label = "::", "IPv6"
	}
	if tcpNeeded {
		if ok, msg := check("tcp", addr, label+" TCP"); !ok {
			return false, msg
		}
	}
	if udpNeeded {
		if ok, msg := check("udp", addr, label+" UDP"); !ok {
			return false, msg
		}
	}
	return true, fmt.Sprintf("port %d is available", port)
}

func (h *InboundHandler) portAvailable(port, excludeInboundID int) (bool, string) {
	if port < 1 || port > 65535 {
		return false, fmt.Sprintf("port %d is outside the valid range", port)
	}
	if port == h.S.Config.XrayAPIPort {
		return false, fmt.Sprintf("port %d is reserved for the local xray API", port)
	}
	if port == h.S.Config.PanelPort {
		return false, fmt.Sprintf("port %d is reserved for the ElectruVPN panel", port)
	}
	var count int64
	q := h.S.DB.Model(&models.Inbound{}).Where("port = ?", port)
	if excludeInboundID > 0 {
		q = q.Where("id <> ?", excludeInboundID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, "could not verify the port in the database"
	}
	if count > 0 {
		return false, fmt.Sprintf("port %d is already assigned to another inbound", port)
	}

	if ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port)); err != nil {
		return false, fmt.Sprintf("port %d is busy on TCP", port)
	} else {
		_ = ln.Close()
	}
	if pc, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port)); err != nil {
		return false, fmt.Sprintf("port %d is busy on UDP", port)
	} else {
		_ = pc.Close()
	}
	return true, fmt.Sprintf("port %d is available", port)
}

// List returns every inbound with its clients preloaded, which is all the
// dashboard's inbound table needs to render traffic bars and status dots
// without a second round trip per row.
// Search finds inbounds directly in the database. It intentionally has no
// maximum number of inbounds to search: the page size limits only the number
// of rows returned to the browser. This keeps the feature usable when the
// panel contains hundreds of thousands (or more) of inbounds.
// Search returns inbounds matching the supplied query and filter parameters.
func (h *InboundHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "search query is required"})
		return
	}
	if len([]rune(q)) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "search query is too long"})
		return
	}
	page, pageSize := inboundPageParams(c)
	like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(q)) + "%"
	query := h.S.DB.Model(&models.Inbound{}).Where("LOWER(remark) LIKE ? ESCAPE '\\'", like)
	if protocol := strings.TrimSpace(strings.ToLower(c.Query("protocol"))); protocol != "" && protocol != "all" {
		query = query.Where("LOWER(protocol) = ?", protocol)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "inbound search failed"})
		return
	}
	var inbounds []models.Inbound
	if total > 0 {
		if err := query.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&inbounds).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "inbound search failed"})
			return
		}
	}
	rows := h.decorateInboundRows(c, inbounds)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": rows, "query": q, "page": page, "pageSize": pageSize, "total": total, "hasMore": int64(page*pageSize) < total})
}

func inboundPageParams(c *gin.Context) (int, int) {
	page := 1
	if n, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page", "1"))); err == nil && n > 0 {
		page = n
	}
	pageSize := 50
	if n, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("pageSize", "50"))); err == nil && n >= 10 && n <= 100 {
		pageSize = n
	}
	return page, pageSize
}

func (h *InboundHandler) decorateInboundRows(c *gin.Context, inbounds []models.Inbound) []gin.H {
	if len(inbounds) == 0 {
		return []gin.H{}
	}
	ids := make([]uint, 0, len(inbounds))
	for _, ib := range inbounds {
		ids = append(ids, ib.ID)
	}
	type countRow struct {
		InboundID uint  `gorm:"column:inbound_id"`
		Count     int64 `gorm:"column:count"`
	}
	var counts []countRow
	_ = h.S.DB.Model(&models.Client{}).Select("inbound_id, COUNT(*) AS count").Where("inbound_id IN ?", ids).Group("inbound_id").Scan(&counts).Error
	countMap := make(map[uint]int64, len(counts))
	for _, x := range counts {
		countMap[x.InboundID] = x.Count
	}
	if cl, ok := c.Get("claims"); ok {
		if claims, ok := cl.(*auth.Claims); ok {
			var user models.User
			if h.S.DB.First(&user, claims.UserID).Error == nil {
				role := strings.ToLower(strings.TrimSpace(user.Role))
				// Hide private inbound material from non-staff operators (viewer/sales/reseller).
				if role == "viewer" || role == "sales" || role == "reseller" {
					for i := range inbounds {
						inbounds[i].Settings = ""
						inbounds[i].StreamSettings = ""
					}
				}
			}
		}
	}
	xrayRunning := h.S.Xray.Status().Running
	singboxRunning := h.S.SingBox.Status().Running
	rows := make([]gin.H, 0, len(inbounds))
	for _, ib := range inbounds {
		running, engine := xrayRunning, "xray"
		switch ib.Protocol {
		case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
			engine, running = "sing-box", singboxRunning
		}
		rows = append(rows, gin.H{"id": ib.ID, "remark": ib.Remark, "enable": ib.Enable, "listen": ib.Listen, "ipMode": ib.IPMode, "port": ib.Port, "protocol": ib.Protocol, "network": ib.Network, "security": ib.Security, "settings": ib.Settings, "streamSettings": ib.StreamSettings, "sniffing": ib.Sniffing, "labels": ib.Labels, "tag": ib.Tag, "maintenance": ib.Maintenance, "expiryTime": ib.ExpiryTime, "totalGB": ib.TotalGB, "limitReached": ib.IsLimited(), "up": ib.Up, "down": ib.Down, "createdAt": ib.CreatedAt, "updatedAt": ib.UpdatedAt, "clientCount": countMap[ib.ID], "clients": []models.Client{}, "runtime": gin.H{"running": ib.Enable && running, "engine": engine}})
	}
	return rows
}

// List handles the List API operation.
func (h *InboundHandler) List(c *gin.Context) {
	page, pageSize := inboundPageParams(c)
	var total int64
	if err := h.S.DB.Model(&models.Inbound{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	var inbounds []models.Inbound
	if total > 0 {
		if err := h.S.DB.Order("id asc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&inbounds).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	rows := h.decorateInboundRows(c, inbounds)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": rows, "page": page, "pageSize": pageSize, "total": total, "hasMore": int64(page*pageSize) < total})
}

// maxInboundTotalGB caps the inbound-level volume so TotalGB*1024^3 can never
// overflow int64. 0 always means unlimited.
const maxInboundTotalGB int64 = 10_000_000

// maxInboundDays caps the inbound/client duration (10 years).
const maxInboundDays = 3650

type createInboundRequest struct {
	Remark                     string          `json:"remark" binding:"required"`
	Listen                     string          `json:"listen"`
	IPMode                     string          `json:"ipMode"`
	Port                       int             `json:"port" binding:"required,min=1,max=65535"`
	Protocol                   models.Protocol `json:"protocol" binding:"required"`
	Network                    models.Network  `json:"network" binding:"required"`
	Security                   models.Security `json:"security" binding:"required"`
	Settings                   string          `json:"settings"`
	StreamSettings             string          `json:"streamSettings"`
	Sniffing                   bool            `json:"sniffing"`
	Labels                     string          `json:"labels"`
	SubscriptionEnabled        *bool           `json:"subscriptionEnabled"`
	SubscriptionName           string          `json:"subscriptionName"`
	SubscriptionDefaultAddress string          `json:"subscriptionDefaultAddress"`
	SubscriptionPreset         string          `json:"subscriptionPreset"`
	SubscriptionSmartEnabled   *bool           `json:"subscriptionSmartEnabled"`

	// Optional: create the first batch of clients for this config in the
	// same request, so the person building a config doesn't have to leave
	// the "new inbound" form and go find a separate "add client" screen.
	ClientCount   int    `json:"clientCount"`
	ClientPrefix  string `json:"clientPrefix"`
	ClientTotalGB int64  `json:"clientTotalGB"`
	ClientDays    int    `json:"clientDays"` // per-client duration; 0 = never expires
	ClientLimitIP int    `json:"clientLimitIp"` // per-client concurrent device/IP limit; 0 = unlimited

	// AntiDPIProfile: off | light | balanced | aggressive (protocol-aware).
	AntiDPIProfile string `json:"antiDpiProfile"`

	// Inbound-level limits. They apply to the whole inbound (all of its clients
	// together) and are independent of each client's own volume/time.
	TotalGB int64 `json:"totalGB"` // 0 = unlimited volume
	Days    int   `json:"days"`    // 0 = never expires
}

// Create adds a new inbound and, if requested, its first batch of clients,
// then regenerates and restarts xray exactly once so the port -- with every
// client already on it -- starts listening in a single restart instead of
// one restart for the inbound and another per client.

// quickTransportCreateRequest is the low-friction transport wizard contract.
// It deliberately hides Xray transport details while preserving the same quota,
// expiry, random/free-port and address-family controls for gRPC, WebSocket and
// HTTPUpgrade.
type quickTransportCreateRequest struct {
	Remark      string `json:"remark"`
	ClientName  string `json:"clientName"`
	TotalGB     int64  `json:"totalGB"`
	Days        int    `json:"days"`
	Port        int    `json:"port"`
	IPMode      string `json:"ipMode"`
	AddressType string `json:"addressType"`

	// Optional inbound-level limits (whole inbound, all clients together).
	InboundTotalGB int64 `json:"inboundTotalGB"` // 0 = unlimited
	InboundDays    int   `json:"inboundDays"`    // 0 = never expires
}

// QuickGRPCCreate handles the QuickGRPCCreate API operation.
func (h *InboundHandler) QuickGRPCCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkGRPC)
}

// QuickWebSocketCreate handles the QuickWebSocketCreate API operation.
func (h *InboundHandler) QuickWebSocketCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkWS)
}

// QuickHTTPUpgradeCreate handles the QuickHTTPUpgradeCreate API operation.
func (h *InboundHandler) QuickHTTPUpgradeCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkHTTPUpgrade)
}

// QuickXHTTPCreate handles the QuickXHTTPCreate API operation.
func (h *InboundHandler) QuickXHTTPCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkXHTTP)
}

// QuickVLESSRealityCreate handles the QuickVLESSRealityCreate API operation.
func (h *InboundHandler) QuickVLESSRealityCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkTCP)
}

// quickTransportCertificate returns the first usable managed certificate. It
// prefers a domain matching the configured public domain, then falls back to a
// non-expired active/managed certificate. This lets Quick WS/HTTPUpgrade stay
// one-click while still using real TLS material when strict mode is enabled.
func (h *InboundHandler) quickTransportCertificate() (*models.CertificateRecord, error) {
	var certs []models.CertificateRecord
	if err := h.S.DB.Order("expires_at asc").Find(&certs).Error; err != nil {
		return nil, err
	}
	domain := strings.TrimSpace(h.S.Config.PublicHostDomain)
	now := time.Now()
	usable := func(cert models.CertificateRecord) bool {
		status := strings.ToLower(strings.TrimSpace(cert.Status))
		if status != "" && status != "active" && status != "managed" && status != "valid" {
			return false
		}
		if cert.ExpiresAt != nil && !cert.ExpiresAt.After(now) {
			return false
		}
		if strings.TrimSpace(cert.CertPath) == "" || strings.TrimSpace(cert.KeyPath) == "" {
			return false
		}
		if st, err := os.Stat(cert.CertPath); err != nil || st.Size() == 0 {
			return false
		}
		if st, err := os.Stat(cert.KeyPath); err != nil || st.Size() == 0 {
			return false
		}
		return true
	}
	if domain != "" {
		for i := range certs {
			if strings.EqualFold(strings.TrimSpace(certs[i].Domain), domain) && usable(certs[i]) {
				return &certs[i], nil
			}
		}
	}
	for i := range certs {
		if usable(certs[i]) {
			return &certs[i], nil
		}
	}
	return nil, nil
}

func (h *InboundHandler) quickTransportCreate(c *gin.Context, network models.Network) {
	var req quickTransportCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if req.TotalGB < 0 || req.Days < 0 || req.Days > 3650 || req.Port < 0 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid volume, days or port"})
		return
	}
	if req.InboundTotalGB < 0 || req.InboundTotalGB > maxInboundTotalGB || req.InboundDays < 0 || req.InboundDays > maxInboundDays {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound volume or days"})
		return
	}
	req.Remark, req.ClientName = strings.TrimSpace(req.Remark), strings.TrimSpace(req.ClientName)
	if req.IPMode == "" {
		req.IPMode = "ipv4"
	}
	req.IPMode = normalizeIPMode(req.IPMode)
	if req.AddressType == "" {
		switch req.IPMode {
		case "ipv6":
			req.AddressType = "ipv6"
		case "dual":
			req.AddressType = "all"
		default:
			req.AddressType = "ipv4"
		}
	}
	req.AddressType = normalizeSubscriptionAddress(req.AddressType)
	if err := validateSubscriptionAddressForMode(req.IPMode, req.AddressType); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if network != models.NetworkWS && network != models.NetworkHTTPUpgrade && network != models.NetworkGRPC && network != models.NetworkXHTTP && network != models.NetworkTCP {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "unsupported quick transport"})
		return
	}

	port := req.Port
	if port == 0 {
		var err error
		port, err = h.findAvailablePort("random", network, req.IPMode)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "msg": err.Error()})
			return
		}
	} else if ok, reason := h.portAvailableForNetwork(port, network, 0, req.IPMode); !ok {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": reason, "port": port, "available": false})
		return
	}
	if len([]rune(req.Remark)) > 128 || len([]rune(req.ClientName)) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "remark or client name is too long"})
		return
	}
	if req.ClientName == "" {
		req.ClientName = req.Remark
	}
	var duplicate int64
	if err := h.S.DB.Model(&models.Inbound{}).Where("lower(remark) = ?", strings.ToLower(req.Remark)).Count(&duplicate).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if duplicate > 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "an inbound with this remark already exists"})
		return
	}

	stream := models.StreamSettings{Network: network}
	security := models.SecurityNone
	securityLabel := "none"
	name := "VLESS"
	if network == models.NetworkGRPC {
		name = "gRPC"
	}
	if network == models.NetworkWS {
		name = "WebSocket"
	}
	if network == models.NetworkHTTPUpgrade {
		name = "HTTPUpgrade"
	}
	if network == models.NetworkXHTTP {
		name = "XHTTP"
	}
	if req.Remark == "" {
		req.Remark = fmt.Sprintf("%s-%d", name, port)
	}
	if network == models.NetworkGRPC || network == models.NetworkXHTTP || network == models.NetworkTCP {
		privateKey, publicKey, err := h.generateX25519Keypair()
		if err != nil || privateKey == "" || publicKey == "" {
			msg := "could not generate REALITY keypair"
			if err != nil {
				msg += ": " + err.Error()
			}
			c.JSON(500, gin.H{"success": false, "msg": msg})
			return
		}
		shortIDs := make([]string, 0, 4)
		for i := 0; i < 4; i++ {
			short := make([]byte, 8)
			if _, err := rand.Read(short); err != nil {
				c.JSON(500, gin.H{"success": false, "msg": "could not generate REALITY short id"})
				return
			}
			shortIDs = append(shortIDs, fmt.Sprintf("%x", short))
		}
		// Speed-optimized secure REALITY stack (simple high-throughput presets).
		// Fragment/TCPMasks disabled by default for maximum throughput; enable manually if DPI requires.
		stream.Security = models.SecurityReality
		stream.Reality = &models.RealitySettings{
			Dest:        "www.microsoft.com:443",
			ServerNames: []string{"www.microsoft.com", "microsoft.com", "www.apple.com", "www.cloudflare.com", "dl.google.com"},
			PrivateKey:  privateKey,
			PublicKey:   publicKey,
			ShortIDs:    shortIDs,
			SpiderX:     "/",
			// RequireMlkem left false by default for broader client compatibility (sing-box etc.).
		}
		tcpNoDelay := true
		tcpFastOpen := true
		stream.Sockopt = &models.SockoptSettings{
			Enabled: true, TCPKeepAlive: 30, TCPNoDelay: &tcpNoDelay, TCPFastOpen: &tcpFastOpen,
		}
		// Light mux only (optional concurrency); no padding to reduce overhead for speed.
		stream.Mux = &models.MuxSettings{
			Enabled: true, Concurrency: 4, SessionTTL: 300, XudpConcurrency: 8,
			XudpProxyUDP443: "reject", Padding: false,
		}
		// Advanced sniffing: TLS/HTTP/QUIC + routeOnly (routing without breaking apps)
		stream.Sniffing = &models.SniffingOptions{
			DestOverride: []string{"http", "tls", "quic"},
			RouteOnly:    true,
		}
		// Deceptive fallback: non-VLESS probes forwarded to cover site (looks like normal HTTPS)
		stream.Fallbacks = []models.FallbackEntry{
			{Dest: "www.microsoft.com:443"},
			{Alpn: "h2", Dest: "www.microsoft.com:443"},
		}
		security = models.SecurityReality
		securityLabel = "reality"
		if network == models.NetworkGRPC {
			stream.GRPCService = "grpc-" + uuid.NewString()[:8]
		}
		if network == models.NetworkXHTTP {
			stream.XHTTPPath = "/xhttp-" + uuid.NewString()[:8]
			// stream-one is preferred for direct REALITY (no CDN); auto for CDN/packet-up scenarios.
			stream.XHTTPMode = "stream-one"
		}
	} else {
		cert, err := h.quickTransportCertificate()
		if err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "certificate lookup failed: " + err.Error()})
			return
		}
		if cert != nil {
			security = models.SecurityTLS
			securityLabel = "tls"
			stream.Security = models.SecurityTLS
			stream.TLSSettings = &models.TLSSettings{CertFile: cert.CertPath, KeyFile: cert.KeyPath, SNI: cert.Domain}
		} else if strings.EqualFold(strings.TrimSpace(h.S.Config.SecurityMode), "strict") {
			c.JSON(http.StatusPreconditionFailed, gin.H{"success": false, "msg": "Quick WebSocket/HTTPUpgrade requires a valid managed TLS certificate in strict security mode"})
			return
		}
		if network == models.NetworkWS {
			stream.WSPath = "/ws-" + uuid.NewString()[:8]
		}
		if network == models.NetworkHTTPUpgrade {
			stream.HTTPUpgradePath = "/httpupgrade-" + uuid.NewString()[:8]
		}
		// Speed-oriented socket options for simple WS / HTTPUpgrade
		tcpNoDelay := true
		tcpFastOpen := true
		stream.Sockopt = &models.SockoptSettings{
			Enabled: true, TCPKeepAlive: 30, TCPNoDelay: &tcpNoDelay, TCPFastOpen: &tcpFastOpen,
		}
		stream.Sniffing = &models.SniffingOptions{
			DestOverride: []string{"http", "tls", "quic"},
			RouteOnly:    true,
		}
	}
	stream.Network = network
	streamJSON, _ := json.Marshal(stream)
	listen := normalizeListenForIPMode("", req.IPMode)
	// Tag anti-DPI profile on quick Reality stacks (TCP/gRPC/XHTTP use Reality path above).
	quickProfile := obfuscation.ProfileOff
	if security == models.SecurityReality {
		quickProfile = obfuscation.ProfileAggressive
	}
	ib := models.Inbound{Remark: req.Remark, Enable: true, Listen: listen, IPMode: req.IPMode, SubscriptionEnabled: true, SubscriptionName: req.Remark, SubscriptionDefaultAddress: req.AddressType, SubscriptionPreset: "standard", SubscriptionSmartEnabled: true, Port: port, Protocol: models.ProtocolVLESS, Network: network, Security: security, Settings: `{"clients":[],"decryption":"none"}`, StreamSettings: string(streamJSON), Sniffing: true, Labels: obfuscation.MergeLabel("", quickProfile), Tag: fmt.Sprintf("inbound-%d-%s", port, uuid.NewString()[:8])}
	client := models.Client{InboundID: 0, Email: req.ClientName, UUID: uuid.NewString(), Password: uuid.NewString(), Enable: true, TotalGB: req.TotalGB, SubID: strings.ReplaceAll(uuid.NewString(), "-", ""), SubscriptionEnabled: true, SubscriptionName: req.ClientName, SubscriptionAddressType: req.AddressType, SubscriptionAutoUpdate: true, SubscriptionSmart: true, SubscriptionStatus: "active", SubscriptionTokenVersion: 1, PurchasedAt: time.Now()}
	if network == models.NetworkTCP {
		client.Flow = "xtls-rprx-vision"
	}
	if req.Days > 0 {
		client.ExpiryTime = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).UnixMilli()
	}
	ib.TotalGB = req.InboundTotalGB
	if req.InboundDays > 0 {
		ib.ExpiryTime = time.Now().Add(time.Duration(req.InboundDays) * 24 * time.Hour).UnixMilli()
	}
	if err := h.S.DB.Create(&ib).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "could not create inbound: " + err.Error()})
		return
	}
	client.InboundID = ib.ID
	if err := h.S.DB.Create(&client).Error; err != nil {
		_ = h.S.DB.Delete(&ib).Error
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "could not create client: " + err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Delete(&models.Client{}, client.ID).Error
		_ = h.S.DB.Delete(&models.Inbound{}, ib.ID).Error
		rollbackErr := h.S.RegenerateAndRestartXray()
		msg := "creation was rolled back because runtime apply failed: " + err.Error()
		if rollbackErr != nil {
			msg += "; previous runtime restore also failed: " + rollbackErr.Error()
		}
		c.JSON(500, gin.H{"success": false, "msg": msg})
		return
	}
	auditAction(h.S, c, "inbound.quick_"+string(network)+".create", strconv.Itoa(int(ib.ID)), ib.Remark)
	var links map[string]string
	var linkErr error
	links, linkErr = (&ClientHandler{S: h.S}).buildClientLinks(c, client, ib, req.AddressType)
	if linkErr != nil {
		links = map[string]string{}
	}
	primary := ""
	if req.AddressType == "dual" || req.AddressType == "all" {
		primary = links["ipv4"]
	} else {
		primary = links[req.AddressType]
	}
	result := gin.H{"id": ib.ID, "remark": ib.Remark, "port": ib.Port, "ipMode": ib.IPMode, "protocol": ib.Protocol, "network": ib.Network, "security": ib.Security, "securityLabel": securityLabel}
	if stream.GRPCService != "" {
		result["serviceName"] = stream.GRPCService
	}
	if stream.WSPath != "" {
		result["path"] = stream.WSPath
	}
	if stream.HTTPUpgradePath != "" {
		result["path"] = stream.HTTPUpgradePath
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"inbound": result, "client": client, "link": primary, "links": links}})
}

// Create handles the Create API operation.
func (h *InboundHandler) Create(c *gin.Context) {
	var req createInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := validateInboundRequest(req, h.S.Config.XrayAPIPort, h.S.Config.SecurityMode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if req.Protocol == models.ProtocolShadowsocks {
		req.Settings = xray.EnsureShadowsocksServerKey(req.Settings)
	}
	var duplicate int64
	h.S.DB.Model(&models.Inbound{}).Where("lower(remark) = ?", strings.ToLower(strings.TrimSpace(req.Remark))).Count(&duplicate)
	if duplicate > 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "an inbound with this remark already exists"})
		return
	}
	if ok, reason := h.portAvailableForNetwork(req.Port, req.Network, 0, req.IPMode); !ok {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": reason, "port": req.Port, "available": false, "status": "busy"})
		return
	}
	if req.ClientCount < 0 || req.ClientTotalGB < 0 || req.ClientDays < 0 || req.ClientDays > maxInboundDays {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid client count, quota, or client days (0 = unlimited time)"})
		return
	}
	if req.TotalGB < 0 || req.TotalGB > maxInboundTotalGB || req.Days < 0 || req.Days > maxInboundDays {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound volume or days (0 = unlimited)"})
		return
	}
	if err := validateSubscriptionAddressForMode(req.IPMode, normalizeSubscriptionAddress(req.SubscriptionDefaultAddress)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// REALITY without keys → generate so create does not fail at xray apply
	if strings.EqualFold(string(req.Security), "reality") {
		req.StreamSettings = ensureRealityKeysInStream(req.StreamSettings, h.S)
	}

	listen := normalizeListenForIPMode(req.Listen, req.IPMode)

	ib := models.Inbound{
		Remark:                     strings.TrimSpace(req.Remark),
		Enable:                     true,
		Listen:                     listen,
		IPMode:                     normalizeIPMode(req.IPMode),
		Port:                       req.Port,
		Protocol:                   req.Protocol,
		Network:                    req.Network,
		Security:                   req.Security,
		Settings:                   req.Settings,
		StreamSettings:             req.StreamSettings,
		Sniffing:                   req.Sniffing,
		Labels:                     cleanLabels(req.Labels),
		SubscriptionEnabled:        req.SubscriptionEnabled == nil || *req.SubscriptionEnabled,
		SubscriptionName:           strings.TrimSpace(req.SubscriptionName),
		SubscriptionDefaultAddress: normalizeSubscriptionAddress(req.SubscriptionDefaultAddress),
		SubscriptionPreset:         strings.TrimSpace(req.SubscriptionPreset),
		SubscriptionSmartEnabled:   req.SubscriptionSmartEnabled == nil || *req.SubscriptionSmartEnabled,
		Tag:                        fmt.Sprintf("inbound-%d-%s", req.Port, uuid.NewString()[:8]),
		TotalGB:                    req.TotalGB,
	}
	if req.Days > 0 {
		ib.ExpiryTime = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).UnixMilli()
	}
	// Protocol-aware anti-DPI profile (optional; preserves existing keys/dest).
	if profile := obfuscation.NormalizeProfile(req.AntiDPIProfile); profile != obfuscation.ProfileOff {
		sOut, stOut, labOut, err := obfuscation.ApplyToInboundJSON(ib.Protocol, ib.Settings, ib.StreamSettings, ib.Labels, profile)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "anti-dpi profile: " + err.Error()})
			return
		}
		ib.Settings, ib.StreamSettings, ib.Labels = sOut, stOut, labOut
	}

	if err := h.S.DB.Create(&ib).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "could not create inbound (is the port already in use?): " + err.Error()})
		return
	}

	var clients []models.Client
	if req.ClientCount > 0 {
		if req.ClientCount > 500 {
			req.ClientCount = 500
		}
		prefix := strings.TrimSpace(req.ClientPrefix)
		if prefix == "" {
			prefix = strings.TrimSpace(req.Remark)
		}
		prefix = truncateRunes(prefix, 80) // rune-safe: Persian names must not be cut mid-character
		base := uuid.NewString()[:6]
		creationErr := error(nil)
		for i := 1; i <= req.ClientCount; i++ {
			email := fmt.Sprintf("%s-%s-%02d", prefix, base, i)
			if req.ClientCount == 1 {
				email = prefix // a single client gets a clean, un-numbered label
			}
			client := models.Client{
				InboundID:           ib.ID,
				Email:               email,
				UUID:                uuid.NewString(),
				Password:            newInboundClientPassword(ib),
				Enable:              true,
				TotalGB:             req.ClientTotalGB,
				LimitIP:             req.ClientLimitIP,
				SubID:               strings.ReplaceAll(uuid.NewString(), "-", ""),
				SubscriptionEnabled: true,
				SubscriptionStatus:  "active",
				SubscriptionTokenVersion: 1,
				PurchasedAt:         time.Now(),
			}
			if req.ClientDays > 0 {
				client.ExpiryTime = time.Now().Add(time.Duration(req.ClientDays) * 24 * time.Hour).UnixMilli()
			}
			if ib.Protocol == models.ProtocolWireGuard {
				priv, pub, e := wireguardKeys()
				if e != nil {
					creationErr = fmt.Errorf("WireGuard key generation failed: %w", e)
					break
				}
				client.PrivateKey = priv
				client.PublicKey = pub
				client.Address = allocateWGAddress(h.S.DB, ib.ID)
				if client.Address == "" {
					creationErr = fmt.Errorf("WireGuard address pool exhausted")
					break
				}
			}
			if err := h.S.DB.Create(&client).Error; err != nil {
				creationErr = fmt.Errorf("could not create client %q: %w", client.Email, err)
				break
			}
			clients = append(clients, client)
		}
		if creationErr != nil || len(clients) != req.ClientCount {
			_ = h.S.DB.Where("inbound_id = ?", ib.ID).Delete(&models.Client{}).Error
			_ = h.S.DB.Delete(&models.Inbound{}, ib.ID).Error
			if creationErr == nil {
				creationErr = fmt.Errorf("only %d of %d requested clients were created", len(clients), req.ClientCount)
			}
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": creationErr.Error()})
			return
		}
	}

	if err := h.S.RegenerateAndRestartXray(); err != nil {
		// The database change is already committed. Remove only the rows created
		// by this request and attempt to restore the previous running state.
		_ = h.S.DB.Where("inbound_id = ?", ib.ID).Delete(&models.Client{}).Error
		_ = h.S.DB.Delete(&models.Inbound{}, ib.ID).Error
		rollbackErr := h.S.RegenerateAndRestartXray()
		msg := "xray failed to apply the new inbound; database change was rolled back: " + err.Error()
		if rollbackErr != nil {
			msg += "; previous runtime restore also failed: " + rollbackErr.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
		return
	}

	ib.Clients = clients
	auditAction(h.S, c, "inbound.create", fmt.Sprint(ib.ID), ib.Remark)
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": ib})
}

type updateInboundRequest struct {
	Remark                     *string `json:"remark"`
	Enable                     *bool   `json:"enable"`
	Protocol                   *string `json:"protocol"`
	Network                    *string `json:"network"`
	Security                   *string `json:"security"`
	Settings                   *string `json:"settings"`
	StreamSettings             *string `json:"streamSettings"`
	Sniffing                   *bool   `json:"sniffing"`
	AntiDPIProfile             *string `json:"antiDpiProfile"`
	Labels                     *string `json:"labels"`
	SubscriptionEnabled        *bool   `json:"subscriptionEnabled"`
	SubscriptionName           *string `json:"subscriptionName"`
	SubscriptionDefaultAddress *string `json:"subscriptionDefaultAddress"`
	SubscriptionPreset         *string `json:"subscriptionPreset"`
	SubscriptionSmartEnabled   *bool   `json:"subscriptionSmartEnabled"`
	Maintenance                *bool   `json:"maintenance"`
	Listen                     *string `json:"listen"`
	IPMode                     *string `json:"ipMode"`
	TotalGB                    *int64  `json:"totalGB"`    // inbound volume in GB, 0 = unlimited
	ExpiryTime                 *int64  `json:"expiryTime"` // inbound expiry, unix ms, 0 = never
	Days                       *int    `json:"days"`       // inbound duration counted from now, 0 = never (wins over expiryTime)
}

// Update patches an existing inbound. Protocol/network/security/stream/settings
// can be updated so imported configs (gRPC service name, SS method, TCP, etc.)
// remain fully editable. Port is still fixed to avoid binding conflicts mid-flight.
// Update validates and persists an inbound change, then refreshes the proxy configuration.
func (h *InboundHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	var req updateInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// Fast path: enable/disable only — skip full config validation so legacy
	// inbounds can still be toggled on/off reliably.
	if req.Enable != nil && req.Remark == nil && req.Protocol == nil && req.Network == nil &&
		req.Security == nil && req.Settings == nil && req.StreamSettings == nil && req.Sniffing == nil &&
		req.Labels == nil && req.Listen == nil && req.IPMode == nil && req.SubscriptionEnabled == nil &&
		req.SubscriptionName == nil && req.SubscriptionDefaultAddress == nil && req.SubscriptionPreset == nil &&
		req.SubscriptionSmartEnabled == nil && req.AntiDPIProfile == nil &&
		req.TotalGB == nil && req.ExpiryTime == nil && req.Days == nil {
		enable := *req.Enable
		updates := map[string]any{"enable": enable}
		if req.Maintenance != nil {
			updates["maintenance"] = *req.Maintenance
			if *req.Maintenance {
				updates["enable"] = false
				enable = false
			}
		} else if enable {
			// Turning back on clears maintenance lock if it was on.
			updates["maintenance"] = false
		}
		revisionID, err := h.saveRevisionWithID(ib, "toggle-enable")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not save rollback snapshot: " + err.Error()})
			return
		}
		if err := h.S.DB.Model(&ib).Select("enable", "maintenance").Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
			return
		}
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			_ = restoreRevision(h.S.DB, revisionID)
			_ = h.S.RegenerateAndRestartXray()
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "enable toggle rolled back: " + err.Error()})
			return
		}
		auditAction(h.S, c, "inbound.toggle", strconv.Itoa(id), gin.H{"enable": enable})
		_ = h.S.DB.First(&ib, id)
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"id": ib.ID, "enable": ib.Enable, "maintenance": ib.Maintenance}})
		return
	}
	remark := ib.Remark
	listen := ib.Listen
	ipMode := normalizeIPMode(ib.IPMode)
	if req.Listen != nil {
		listen = strings.TrimSpace(*req.Listen)
	}
	if req.IPMode != nil {
		ipMode = normalizeIPMode(*req.IPMode)
	}
	listen = normalizeListenForIPMode(listen, ipMode)
	if req.Remark != nil {
		remark = strings.TrimSpace(*req.Remark)
	}
	if req.Remark != nil {
		var duplicate int64
		h.S.DB.Model(&models.Inbound{}).Where("id <> ? AND lower(remark) = ?", ib.ID, strings.ToLower(remark)).Count(&duplicate)
		if duplicate > 0 {
			c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "another inbound already uses this remark"})
			return
		}
	}
	settings := ib.Settings
	if req.Settings != nil {
		settings = *req.Settings
	}
	stream := ib.StreamSettings
	if req.StreamSettings != nil {
		stream = *req.StreamSettings
	}
	protocol := ib.Protocol
	if req.Protocol != nil && strings.TrimSpace(*req.Protocol) != "" {
		protocol = models.Protocol(strings.ToLower(strings.TrimSpace(*req.Protocol)))
	}
	if protocol == models.ProtocolShadowsocks {
		if fixed := xray.EnsureShadowsocksServerKey(settings); fixed != settings {
			settings = fixed
			req.Settings = &settings
		}
	}
	network := ib.Network
	if req.Network != nil && strings.TrimSpace(*req.Network) != "" {
		network = models.Network(strings.ToLower(strings.TrimSpace(*req.Network)))
	}
	security := ib.Security
	if req.Security != nil && strings.TrimSpace(*req.Security) != "" {
		security = models.Security(strings.ToLower(strings.TrimSpace(*req.Security)))
	}
	// Keep streamSettings.network/security in sync with inbound columns when provided.
	if req.StreamSettings != nil || req.Network != nil || req.Security != nil {
		var st models.StreamSettings
		if strings.TrimSpace(stream) != "" {
			_ = json.Unmarshal([]byte(stream), &st)
		}
		st.Network = network
		st.Security = security
		if b, err := json.Marshal(st); err == nil {
			stream = string(b)
		}
	}
	// Auto-fill REALITY keypair on update when missing (same as Create).
	if security == models.SecurityReality {
		stream = ensureRealityKeysInStream(stream, h.S)
	}
	sniffing := ib.Sniffing
	if req.Sniffing != nil {
		sniffing = *req.Sniffing
	}
	labels := ib.Labels
	if req.Labels != nil {
		labels = *req.Labels
	}
	probe := createInboundRequest{Remark: remark, Listen: listen, IPMode: ipMode, Port: ib.Port, Protocol: protocol, Network: network, Security: security, Settings: settings, StreamSettings: stream, Sniffing: sniffing, Labels: labels, SubscriptionEnabled: &ib.SubscriptionEnabled, SubscriptionName: ib.SubscriptionName, SubscriptionDefaultAddress: ib.SubscriptionDefaultAddress}
	if err := validateInboundRequest(probe, h.S.Config.XrayAPIPort, h.S.Config.SecurityMode); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := validateSubscriptionAddressForMode(probe.IPMode, normalizeSubscriptionAddress(func() string {
		if req.SubscriptionDefaultAddress != nil {
			return *req.SubscriptionDefaultAddress
		}
		return ib.SubscriptionDefaultAddress
	}())); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	newTotalGB := ib.TotalGB
	newExpiry := ib.ExpiryTime
	if req.TotalGB != nil {
		if *req.TotalGB < 0 || *req.TotalGB > maxInboundTotalGB {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound volume (GB); 0 = unlimited"})
			return
		}
		newTotalGB = *req.TotalGB
	}
	if req.Days != nil {
		if *req.Days < 0 || *req.Days > maxInboundDays {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound days; 0 = never expires"})
			return
		}
		if *req.Days == 0 {
			newExpiry = 0
		} else {
			newExpiry = time.Now().Add(time.Duration(*req.Days) * 24 * time.Hour).UnixMilli()
		}
	} else if req.ExpiryTime != nil {
		if *req.ExpiryTime < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound expiry time"})
			return
		}
		newExpiry = *req.ExpiryTime
	}
	revisionID, err := h.saveRevisionWithID(ib, "update")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not save rollback snapshot: " + err.Error()})
		return
	}

	// Optional re-apply anti-DPI profile on update (does not wipe Reality keys).
	if req.AntiDPIProfile != nil {
		labels := ib.Labels
		if req.Labels != nil {
			labels = *req.Labels
		}
		sOut, stOut, labOut, aerr := obfuscation.ApplyToInboundJSON(protocol, settings, stream, labels, *req.AntiDPIProfile)
		if aerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "anti-dpi profile: " + aerr.Error()})
			return
		}
		settings, stream = sOut, stOut
		// Ensure settings/stream/labels land in updates even if client did not send them.
		req.Settings = &settings
		req.StreamSettings = &stream
		lab := labOut
		req.Labels = &lab
	}

	updates := map[string]any{}
	if req.Remark != nil {
		updates["remark"] = remark
	}
	if req.Enable != nil {
		updates["enable"] = *req.Enable
	}
	if req.Protocol != nil && strings.TrimSpace(*req.Protocol) != "" {
		updates["protocol"] = protocol
	}
	if req.Network != nil && strings.TrimSpace(*req.Network) != "" {
		updates["network"] = network
	}
	if req.Security != nil && strings.TrimSpace(*req.Security) != "" {
		updates["security"] = security
	}
	if req.Settings != nil {
		updates["settings"] = settings
	}
	if req.StreamSettings != nil || req.Network != nil || req.Security != nil {
		updates["stream_settings"] = stream
	}
	if req.Sniffing != nil {
		updates["sniffing"] = *req.Sniffing
	}
	if req.Labels != nil {
		updates["labels"] = cleanLabels(*req.Labels)
	}
	if req.Listen != nil {
		updates["listen"] = listen
	}
	if req.IPMode != nil {
		updates["ip_mode"] = ipMode
	}
	if req.SubscriptionEnabled != nil {
		updates["subscription_enabled"] = *req.SubscriptionEnabled
	}
	if req.SubscriptionName != nil {
		updates["subscription_name"] = strings.TrimSpace(*req.SubscriptionName)
	}
	if req.SubscriptionDefaultAddress != nil {
		updates["subscription_default_address"] = normalizeSubscriptionAddress(*req.SubscriptionDefaultAddress)
	}
	if req.SubscriptionPreset != nil {
		updates["subscription_preset"] = strings.TrimSpace(*req.SubscriptionPreset)
	}
	if req.SubscriptionSmartEnabled != nil {
		updates["subscription_smart_enabled"] = *req.SubscriptionSmartEnabled
	}
	if req.Maintenance != nil {
		updates["maintenance"] = *req.Maintenance
		if *req.Maintenance {
			updates["enable"] = false
		}
	}
	if req.TotalGB != nil {
		updates["total_gb"] = newTotalGB
	}
	if req.Days != nil || req.ExpiryTime != nil {
		updates["expiry_time"] = newExpiry
	}
	if req.TotalGB != nil || req.Days != nil || req.ExpiryTime != nil {
		// Keep the cached limit flag in sync so the next traffic pass does not
		// trigger a second, redundant xray restart.
		preview := ib
		preview.TotalGB, preview.ExpiryTime = newTotalGB, newExpiry
		updates["limit_reached"] = preview.IsLimited()
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": ib})
		return
	}
	if err := h.S.DB.Model(&ib).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		rollbackErr := restoreRevision(h.S.DB, revisionID)
		if rollbackErr == nil {
			rollbackErr = h.S.RegenerateAndRestartXray()
		}
		msg := "inbound update was rolled back because runtime apply failed: " + err.Error()
		if rollbackErr != nil {
			msg += "; previous runtime restore also failed: " + rollbackErr.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": msg})
		return
	}
	auditAction(h.S, c, "inbound.update", strconv.Itoa(id), nil)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func newInboundClientPassword(ib models.Inbound) string {
	if ib.Protocol != models.ProtocolShadowsocks {
		return uuid.NewString()
	}
	var ss models.ShadowsocksSettings
	_ = json.Unmarshal([]byte(ib.Settings), &ss)
	length := 0
	switch ss.Method {
	case "2022-blake3-aes-128-gcm":
		length = 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		length = 32
	}
	if length > 0 {
		b := make([]byte, length)
		if _, err := rand.Read(b); err == nil {
			return base64.StdEncoding.EncodeToString(b)
		}
	}
	return uuid.NewString()
}

func normalizeIPMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ipv6", "dual":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "ipv4"
	}
}

func normalizeListenForIPMode(listen, mode string) string {
	mode = normalizeIPMode(mode)
	listen = strings.TrimSpace(listen)
	switch mode {
	case "ipv6":
		if listen == "" || listen == "0.0.0.0" {
			return "::"
		}
	case "dual":
		// On Linux, an IPv6 wildcard listener can accept IPv4-mapped traffic
		// when net.ipv6.bindv6only=0 (Ubuntu's normal default). We validate both
		// families before apply so a dual-stack listener cannot collide silently.
		if listen == "0.0.0.0" || listen == "" {
			return "::"
		}
	case "ipv4":
		if listen == "" || listen == "::" {
			return "0.0.0.0"
		}
	}
	return listen
}

func normalizeSubscriptionAddress(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "ipv4", "ipv6", "domain", "dual", "all":
		return v
	default:
		return "ipv4"
	}
}

func validateSubscriptionAddressForMode(ipMode, address string) error {
	mode := normalizeIPMode(ipMode)
	address = normalizeSubscriptionAddress(address)
	switch address {
	case "ipv4":
		if mode != "ipv4" && mode != "dual" {
			return fmt.Errorf("IPv4 subscription requires an IPv4-capable inbound")
		}
	case "ipv6":
		if mode != "ipv6" && mode != "dual" {
			return fmt.Errorf("IPv6 subscription requires an IPv6-capable inbound")
		}
	case "domain":
		return nil
	case "dual", "all":
		if mode != "dual" {
			return fmt.Errorf("dual/all subscription requires a dual-stack inbound")
		}
	}
	return nil
}

func validateInboundRequest(req createInboundRequest, apiPort int, securityMode string) error {
	rawMode := strings.ToLower(strings.TrimSpace(req.IPMode))
	if rawMode != "" && rawMode != "ipv4" && rawMode != "ipv6" && rawMode != "dual" {
		return fmt.Errorf("unsupported ipMode %q", req.IPMode)
	}
	if strings.TrimSpace(req.Remark) == "" {
		return fmt.Errorf("remark is required")
	}
	if len([]rune(strings.TrimSpace(req.Remark))) > 128 {
		return fmt.Errorf("remark is too long")
	}
	if len(cleanLabels(req.Labels)) > 512 {
		return fmt.Errorf("labels are too long")
	}
	req.IPMode = normalizeIPMode(req.IPMode)
	if req.Listen != "" {
		ip := strings.TrimSpace(req.Listen)
		if parsed := net.ParseIP(strings.Trim(ip, "[]")); parsed != nil {
			if req.IPMode == "ipv4" && parsed.To4() == nil {
				return fmt.Errorf("IPv4 mode requires an IPv4 listen address")
			}
			if req.IPMode == "ipv6" && parsed.To4() != nil {
				return fmt.Errorf("IPv6 mode requires an IPv6 listen address")
			}
		}
	}
	if req.Port == apiPort {
		return fmt.Errorf("port %d is reserved for the local xray API", apiPort)
	}
	switch req.Protocol {
	case models.ProtocolVLESS, models.ProtocolVMess, models.ProtocolTrojan, models.ProtocolShadowsocks, models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
	default:
		return fmt.Errorf("unsupported protocol %q", req.Protocol)
	}
	switch req.Network {
	case models.NetworkTCP, models.NetworkWS, models.NetworkGRPC, models.NetworkXHTTP, models.NetworkHTTPUpgrade, models.NetworkMKCP, models.NetworkQUIC, models.NetworkUDP:
	default:
		return fmt.Errorf("unsupported network %q", req.Network)
	}
	switch req.Security {
	case models.SecurityNone, models.SecurityTLS, models.SecurityReality:
	default:
		return fmt.Errorf("unsupported security %q", req.Security)
	}
	if strings.EqualFold(strings.TrimSpace(securityMode), "strict") && req.Security == models.SecurityNone && req.Protocol != models.ProtocolWireGuard && req.Protocol != models.ProtocolShadowsocks {
		return fmt.Errorf("strict security mode requires TLS or REALITY for %s", req.Protocol)
	}
	// Reject combinations the engines cannot represent rather than waiting
	// for a runtime restart to discover them.
	switch req.Protocol {
	case models.ProtocolWireGuard:
		if req.Network != models.NetworkUDP {
			return fmt.Errorf("WireGuard requires UDP network")
		}
	case models.ProtocolHysteria2, models.ProtocolTUIC:
		if req.Network != models.NetworkQUIC {
			return fmt.Errorf("%s requires QUIC network", req.Protocol)
		}
	case models.ProtocolNaive:
		// NaiveProxy: TLS HTTP CONNECT — prefer TCP; HTTP/3 (QUIC) optional via settings.http3
		if req.Network != models.NetworkTCP && req.Network != models.NetworkQUIC {
			return fmt.Errorf("naive requires TCP or QUIC network")
		}
	case models.ProtocolAnyTLS, models.ProtocolShadowTLS:
		if req.Network != models.NetworkTCP {
			return fmt.Errorf("%s requires TCP network", req.Protocol)
		}
	}
	if req.Network == models.NetworkWS || req.Network == models.NetworkGRPC || req.Network == models.NetworkXHTTP || req.Network == models.NetworkHTTPUpgrade {
		if req.Protocol == models.ProtocolHysteria2 || req.Protocol == models.ProtocolTUIC || req.Protocol == models.ProtocolWireGuard {
			return fmt.Errorf("%s cannot use %s transport", req.Protocol, req.Network)
		}
	}
	var stream models.StreamSettings
	if strings.TrimSpace(req.StreamSettings) != "" {
		if err := json.Unmarshal([]byte(req.StreamSettings), &stream); err != nil {
			return fmt.Errorf("invalid streamSettings JSON")
		}
	}
	if req.Network == models.NetworkWS && strings.TrimSpace(stream.WSPath) == "" {
		return fmt.Errorf("WebSocket path is required")
	}
	if req.Network == models.NetworkGRPC && strings.TrimSpace(stream.GRPCService) == "" {
		return fmt.Errorf("gRPC service name is required")
	}
	if req.Network == models.NetworkXHTTP {
		if strings.TrimSpace(stream.XHTTPPath) == "" {
			return fmt.Errorf("XHTTP path is required")
		}
		switch stream.XHTTPMode {
		case "", "auto", "stream-one", "stream-up", "packet-up":
		default:
			return fmt.Errorf("unsupported XHTTP mode %q", stream.XHTTPMode)
		}
	}
	if req.Network == models.NetworkHTTPUpgrade && strings.TrimSpace(stream.HTTPUpgradePath) == "" {
		return fmt.Errorf("HTTPUpgrade path is required")
	}
	if req.Security == models.SecurityTLS {
		if stream.TLSSettings == nil ||
			strings.TrimSpace(stream.TLSSettings.CertFile) == "" ||
			strings.TrimSpace(stream.TLSSettings.KeyFile) == "" {
			return fmt.Errorf("TLS requires certificate and key file settings")
		}
	}
	if req.Security == models.SecurityReality {
		if stream.Reality == nil ||
			strings.TrimSpace(stream.Reality.PrivateKey) == "" ||
			strings.TrimSpace(stream.Reality.Dest) == "" ||
			len(stream.Reality.ServerNames) == 0 ||
			strings.TrimSpace(stream.Reality.ServerNames[0]) == "" {
			return fmt.Errorf("REALITY requires destination, SNI and private key")
		}
		if req.Network != models.NetworkTCP && req.Network != models.NetworkXHTTP && req.Network != models.NetworkGRPC {
			return fmt.Errorf("REALITY is supported only with RAW/TCP, XHTTP or gRPC")
		}
	}
	if req.Protocol == models.ProtocolHysteria2 || req.Protocol == models.ProtocolTUIC || req.Protocol == models.ProtocolNaive || req.Protocol == models.ProtocolAnyTLS || req.Protocol == models.ProtocolShadowTLS {
		if req.Security != models.SecurityTLS {
			return fmt.Errorf("%s requires TLS in this panel", req.Protocol)
		}
	}
	if req.Protocol == models.ProtocolHysteria2 {
		if err := validateHysteria2Settings(req.Settings); err != nil {
			return err
		}
	}
	if req.Protocol == models.ProtocolWireGuard && req.Security != models.SecurityNone {
		return fmt.Errorf("WireGuard uses its own encryption and must use security=none")
	}
	if req.Protocol == models.ProtocolWireGuard {
		var wg models.WireGuardSettings
		if strings.TrimSpace(req.Settings) != "" {
			_ = json.Unmarshal([]byte(req.Settings), &wg)
		}
		if strings.TrimSpace(wg.PrivateKey) == "" || strings.TrimSpace(wg.Address) == "" {
			return fmt.Errorf("WireGuard requires server private key and address")
		}
	}
	if req.Protocol == models.ProtocolShadowsocks {
		var ss models.ShadowsocksSettings
		if strings.TrimSpace(req.Settings) != "" {
			if err := json.Unmarshal([]byte(req.Settings), &ss); err != nil {
				return fmt.Errorf("invalid Shadowsocks settings")
			}
		}
		if ss.Method == "" {
			ss.Method = "2022-blake3-aes-128-gcm"
		}
		switch ss.Method {
		case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305",
			"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm",
			"2022-blake3-chacha20-poly1305":
		default:
			return fmt.Errorf("unsupported Shadowsocks method %q", ss.Method)
		}
	}
	return nil
}

// Delete removes an inbound and every client under it.
func (h *InboundHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	revisionID, err := h.saveRevisionWithID(ib, "delete")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not save rollback snapshot: " + err.Error()})
		return
	}
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		clientIDs := tx.Model(&models.Client{}).Select("id").Where("inbound_id = ?", id)
		if err := tx.Where("inbound_id = ? OR client_id IN (?)", id, clientIDs).Delete(&models.ClientConfigProfile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&models.Client{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Inbound{}, id).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		if restoreErr := restoreRevision(h.S.DB, revisionID); restoreErr == nil {
			restoreErr = h.S.RegenerateAndRestartXray()
			if restoreErr != nil {
				err = fmt.Errorf("%v; runtime restore failed: %v", err, restoreErr)
			}
		} else {
			err = fmt.Errorf("%v; database restore failed: %v", err, restoreErr)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "delete was rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.delete", strconv.Itoa(id), ib.Remark)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type inboundSnapshot struct {
	Inbound  models.Inbound               `json:"inbound"`
	Clients  []models.Client              `json:"clients"`
	Profiles []models.ClientConfigProfile `json:"profiles"`
}

func cleanLabels(v string) string {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && len([]rune(p)) <= 48 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

func (h *InboundHandler) saveRevisionWithID(ib models.Inbound, reason string) (uint, error) {
	if len(ib.Clients) == 0 {
		if err := h.S.DB.Preload("Clients").First(&ib, ib.ID).Error; err != nil {
			return 0, err
		}
	}
	var profiles []models.ClientConfigProfile
	if err := h.S.DB.Where("inbound_id = ? OR client_id IN (?)", ib.ID, h.S.DB.Model(&models.Client{}).Select("id").Where("inbound_id = ?", ib.ID)).Find(&profiles).Error; err != nil {
		return 0, err
	}
	b, err := json.Marshal(inboundSnapshot{Inbound: ib, Clients: ib.Clients, Profiles: profiles})
	if err != nil {
		return 0, err
	}
	rev := models.InboundRevision{InboundID: ib.ID, Snapshot: string(b), Reason: reason}
	if err := h.S.DB.Create(&rev).Error; err != nil {
		return 0, err
	}
	// Keep history bounded so a frequently edited inbound cannot grow the
	// SQLite database without limit. The newest 100 revisions are retained.
	var ids []uint
	if err := h.S.DB.Model(&models.InboundRevision{}).Where("inbound_id = ?", ib.ID).Order("id desc").Offset(100).Limit(1000).Pluck("id", &ids).Error; err == nil && len(ids) > 0 {
		_ = h.S.DB.Where("id IN ?", ids).Delete(&models.InboundRevision{}).Error
	}
	return rev.ID, nil
}

func (h *InboundHandler) saveRevision(ib models.Inbound, reason string) error {
	_, err := h.saveRevisionWithID(ib, reason)
	return err
}

func restoreRevision(db *gorm.DB, revID uint) error {
	var rev models.InboundRevision
	if err := db.First(&rev, revID).Error; err != nil {
		return err
	}
	var payload inboundSnapshot
	if err := json.Unmarshal([]byte(rev.Snapshot), &payload); err != nil {
		return err
	}
	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Save(&payload.Inbound).Error; err != nil {
		tx.Rollback()
		return err
	}
	clientIDs := tx.Model(&models.Client{}).Select("id").Where("inbound_id = ?", payload.Inbound.ID)
	if err := tx.Where("inbound_id = ? OR client_id IN (?)", payload.Inbound.ID, clientIDs).Delete(&models.ClientConfigProfile{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Where("inbound_id = ?", payload.Inbound.ID).Delete(&models.Client{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	for _, cl := range payload.Clients {
		if err := tx.Create(&cl).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, pr := range payload.Profiles {
		if err := tx.Create(&pr).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

// ValidateDraft handles the ValidateDraft API operation.
func (h *InboundHandler) ValidateDraft(c *gin.Context) {
	var req createInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := validateInboundRequest(req, h.S.Config.XrayAPIPort, h.S.Config.SecurityMode); err != nil {
		c.JSON(422, gin.H{"success": false, "valid": false, "msg": err.Error()})
		return
	}
	ib := models.Inbound{Remark: strings.TrimSpace(req.Remark), Enable: true, Listen: normalizeListenForIPMode(req.Listen, req.IPMode), IPMode: normalizeIPMode(req.IPMode), Port: req.Port, Protocol: req.Protocol, Network: req.Network, Security: req.Security, Settings: req.Settings, StreamSettings: req.StreamSettings, Sniffing: req.Sniffing, Labels: cleanLabels(req.Labels), Tag: fmt.Sprintf("draft-%d-%s", req.Port, uuid.NewString()[:8])}
	if ib.Listen == "" {
		ib.Listen = "0.0.0.0"
	}
	engine, data, err := h.S.ValidateInboundConfig(ib)
	if err != nil {
		c.JSON(422, gin.H{"success": false, "valid": false, "engine": engine, "config": string(data), "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "valid": true, "engine": engine, "config": string(data)})
}

// RealityKeypair handles the RealityKeypair API operation.
func (h *InboundHandler) RealityKeypair(c *gin.Context) {
	priv, pub, err := h.generateX25519Keypair()
	if err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not generate REALITY keypair: " + err.Error()})
		return
	}
	if priv == "" || pub == "" {
		c.JSON(500, gin.H{"success": false, "msg": "REALITY keypair generation returned empty keys"})
		return
	}
	short := make([]byte, 8)
	if _, err := rand.Read(short); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not generate REALITY short id"})
		return
	}
	// Also return a few extra shortIds for multi-SNI resilience (clients can pick any).
	extra := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			break
		}
		extra = append(extra, fmt.Sprintf("%x", b))
	}
	shortId := fmt.Sprintf("%x", short)
	c.JSON(200, gin.H{
		"success":    true,
		"privateKey": priv,
		"publicKey":  pub,
		"shortId":    shortId,
		"shortIds":   append([]string{shortId}, extra...),
	})
}

// RotateRealityKeys regenerates private/public key and shortIds for an existing
// REALITY inbound, keeps dest/serverNames, restarts xray, and returns the new
// public material so clients can be updated via subscription refresh.
func (h *InboundHandler) RotateRealityKeys(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	if ib.Security != models.SecurityReality {
		c.JSON(400, gin.H{"success": false, "msg": "inbound is not REALITY"})
		return
	}
	priv, pub, err := h.generateX25519Keypair()
	if err != nil || priv == "" || pub == "" {
		msg := "could not generate REALITY keypair"
		if err != nil {
			msg += ": " + err.Error()
		}
		c.JSON(500, gin.H{"success": false, "msg": msg})
		return
	}
	shortIDs := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "could not generate short id"})
			return
		}
		shortIDs = append(shortIDs, fmt.Sprintf("%x", b))
	}
	var stream models.StreamSettings
	_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	if stream.Reality == nil {
		stream.Reality = &models.RealitySettings{}
	}
	stream.Reality.PrivateKey = priv
	stream.Reality.PublicKey = pub
	stream.Reality.ShortIDs = shortIDs
	if len(stream.Reality.ServerNames) == 0 {
		stream.Reality.ServerNames = []string{"www.microsoft.com", "microsoft.com"}
	}
	if stream.Reality.Dest == "" {
		stream.Reality.Dest = "www.microsoft.com:443"
	}
	if stream.Fragment == nil {
		stream.Fragment = &models.FragmentSettings{Enabled: true, Packets: "tlshello", Length: "100-200", Interval: "10-20"}
	}
	raw, _ := json.Marshal(stream)
	ib.StreamSettings = string(raw)
	if err := h.S.DB.Save(&ib).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "keys saved but xray restart failed: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.reality.rotate", strconv.Itoa(int(ib.ID)), ib.Remark)
	c.JSON(200, gin.H{"success": true, "publicKey": pub, "shortIds": shortIDs, "msg": "REALITY keys rotated; clients should refresh subscription"})
}

// ApplyAntiDPI applies a protocol-aware anti-DPI profile to an existing inbound
// without recreate. Preserves Reality private keys and existing dest/SNI when set.
// Body: { "profile": "light"|"balanced"|"aggressive"|"off" }
// Optional query regenerate=0 skips Xray/Sing-box restart (DB only).

// ApplyAntiDPIBulk applies a profile to many inbounds. Body: { "profile": "...", "ids": [1,2,3] }
func (h *InboundHandler) ApplyAntiDPIBulk(c *gin.Context) {
	var body struct {
		Profile string `json:"profile"`
		IDs     []uint `json:"ids"`
	}
	if c.ShouldBindJSON(&body) != nil || len(body.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "profile and non-empty ids required"})
		return
	}
	profile := obfuscation.NormalizeProfile(body.Profile)
	if len(body.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "max 100 inbounds per request"})
		return
	}
	ok, failed := 0, []gin.H{}
	for _, id := range body.IDs {
		var ib models.Inbound
		if h.S.DB.First(&ib, id).Error != nil {
			failed = append(failed, gin.H{"id": id, "msg": "not found"})
			continue
		}
		sOut, stOut, labOut, err := obfuscation.ApplyToInboundJSON(ib.Protocol, ib.Settings, ib.StreamSettings, ib.Labels, profile)
		if err != nil {
			failed = append(failed, gin.H{"id": id, "msg": err.Error()})
			continue
		}
		if profile == obfuscation.ProfileOff {
			labOut = obfuscation.MergeLabel(ib.Labels, obfuscation.ProfileOff)
			sOut, stOut = ib.Settings, ib.StreamSettings
		}
		if err := h.S.DB.Model(&ib).Updates(map[string]any{"settings": sOut, "stream_settings": stOut, "labels": labOut}).Error; err != nil {
			failed = append(failed, gin.H{"id": id, "msg": err.Error()})
			continue
		}
		ok++
	}
	runtimeOK, runtimeMsg := true, ""
	if ok > 0 {
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			runtimeOK, runtimeMsg = false, err.Error()
		}
	}
	auditAction(h.S, c, "inbound.anti_dpi.bulk", "", gin.H{"profile": profile, "ok": ok, "failed": len(failed)})
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"profile": profile, "updated": ok, "failed": failed,
		"runtimeApplied": runtimeOK, "runtimeError": runtimeMsg,
	}})
}

func (h *InboundHandler) ApplyAntiDPI(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var body struct {
		Profile string `json:"profile"`
	}
	_ = c.ShouldBindJSON(&body)
	profile := obfuscation.NormalizeProfile(body.Profile)

	var ib models.Inbound
	if err := h.S.DB.First(&ib, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "inbound not found"})
		return
	}

	sOut, stOut, labOut, aerr := obfuscation.ApplyToInboundJSON(ib.Protocol, ib.Settings, ib.StreamSettings, ib.Labels, profile)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "anti-dpi: " + aerr.Error()})
		return
	}

	// off: only strip label token; leave fragment/mux as operator left them
	if profile == obfuscation.ProfileOff {
		labOut = obfuscation.MergeLabel(ib.Labels, obfuscation.ProfileOff)
		sOut, stOut = ib.Settings, ib.StreamSettings
	}

	if _, err := h.saveRevisionWithID(ib, "anti-dpi:"+profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not save revision: " + err.Error()})
		return
	}

	updates := map[string]any{
		"settings":        sOut,
		"stream_settings": stOut,
		"labels":          labOut,
	}
	if err := h.S.DB.Model(&ib).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": err.Error()})
		return
	}
	_ = h.S.DB.First(&ib, id)

	regen := c.Query("regenerate") != "0"
	runtimeOK, runtimeMsg := true, ""
	if regen {
		// Prefer full regen so both xray and sing-box pick up Hy2 masquerade / stream changes.
		if err := h.S.RegenerateAndRestartXray(); err != nil {
			runtimeOK, runtimeMsg = false, err.Error()
		}
	}

	auditAction(h.S, c, "inbound.anti_dpi", strconv.Itoa(id), gin.H{"profile": profile, "protocol": ib.Protocol})
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{
		"id": ib.ID, "remark": ib.Remark, "protocol": ib.Protocol,
		"profile": profile, "labels": ib.Labels,
		"runtimeApplied": runtimeOK, "runtimeError": runtimeMsg,
	}})
}

// QuickAntiFilterCreate builds a one-click anti-DPI inbound using the current
// best-practice stack: VLESS + TCP + REALITY + Vision + multi shortId +
// multi-SNI + TLS fragment + mux. Optional body mirrors quick transport fields.
func (h *InboundHandler) QuickAntiFilterCreate(c *gin.Context) {
	h.quickTransportCreate(c, models.NetworkTCP)
}

// ResetTraffic resets the accounting window for every client attached to one
// inbound. It reads the current monotonic Xray counters and stores them as
// per-client baselines, so other inbounds/clients remain untouched.
func (h *InboundHandler) ResetTraffic(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	stats, err := xray.PollStats(h.S.Config.XrayBinaryPath, h.S.Config.XrayAPIPort, false)
	if err != nil {
		c.JSON(503, gin.H{"success": false, "msg": "could not read Xray counters: " + err.Error()})
		return
	}
	tx := h.S.DB.Begin()
	if tx.Error != nil {
		c.JSON(500, gin.H{"success": false, "msg": tx.Error.Error()})
		return
	}
	for _, cl := range ib.Clients {
		var up, down int64
		if t := stats[cl.Email]; t != nil {
			up, down = t.Uplink, t.Downlink
		}
		if err := tx.Model(&models.Client{}).Where("id = ?", cl.ID).Updates(map[string]any{"up": 0, "down": 0, "traffic_base_up": up, "traffic_base_down": down}).Error; err != nil {
			tx.Rollback()
			c.JSON(500, gin.H{"success": false, "msg": err.Error()})
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	// After a reset the volume limit is no longer hit; only the expiry date can
	// still keep the inbound limited.
	stillLimited := ib.IsExpired()
	_ = h.S.DB.Model(&models.Inbound{}).Where("id = ?", ib.ID).Updates(map[string]any{"up": 0, "down": 0, "limit_reached": stillLimited}).Error
	for _, cl := range ib.Clients {
		xray.ResetLiveSample(cl.Email)
	}
	if ib.LimitReached && !stillLimited {
		// The inbound was removed from the runtime because of its volume limit;
		// put it back now that the counter is reset.
		_ = h.S.RegenerateAndRestartXray()
	}
	auditAction(h.S, c, "inbound.traffic_reset", strconv.Itoa(id), ib.Remark)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"inboundId": ib.ID, "clients": len(ib.Clients), "resetAt": time.Now()}})
}

type cloneInboundRequest struct {
	IncludeClients bool   `json:"includeClients"`
	Remark         string `json:"remark"`
}

// Clone creates a fresh inbound from an existing validated configuration. Client
// credentials are regenerated instead of copying secrets, and runtime is applied once.
func (h *InboundHandler) Clone(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var src models.Inbound
	if err := h.S.DB.Preload("Clients").First(&src, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	var req cloneInboundRequest
	_ = c.ShouldBindJSON(&req)
	remark := strings.TrimSpace(req.Remark)
	if remark == "" {
		remark = src.Remark + " (clone)"
	}
	var n int64
	h.S.DB.Model(&models.Inbound{}).Where("lower(remark)=?", strings.ToLower(remark)).Count(&n)
	if n > 0 {
		remark += " " + uuid.NewString()[:4]
	}
	port, err := h.findAvailablePort("random", src.Network, src.IPMode)
	if err != nil {
		c.JSON(503, gin.H{"success": false, "msg": err.Error()})
		return
	}
	clone := src
	clone.ID = 0
	clone.Remark = remark
	clone.Port = port
	clone.Tag = fmt.Sprintf("inbound-%d-%s", port, uuid.NewString()[:8])
	clone.Up = 0
	clone.Down = 0
	clone.LimitReached = clone.IsExpired()
	clone.Maintenance = false
	clone.CreatedAt = time.Time{}
	clone.UpdatedAt = time.Time{}
	clone.Clients = nil
	if err := h.S.DB.Create(&clone).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	created := make([]models.Client, 0)
	if req.IncludeClients {
		cloneSuffix := "-clone-" + uuid.NewString()[:5]
		for _, old := range src.Clients {
			cl := models.Client{
				InboundID: clone.ID, Email: old.Email + cloneSuffix, UUID: uuid.NewString(), Password: newInboundClientPassword(clone), Flow: old.Flow,
				Enable: old.Enable, LimitIP: old.LimitIP, TotalGB: old.TotalGB, ExpiryTime: old.ExpiryTime, Group: old.Group,
				AutoExtendGB: old.AutoExtendGB, AutoExtendDays: old.AutoExtendDays, SubID: strings.ReplaceAll(uuid.NewString(), "-", ""),
				SubscriptionEnabled: old.SubscriptionEnabled, SubscriptionName: old.SubscriptionName, SubscriptionGroup: old.SubscriptionGroup,
				SubscriptionRefreshMin: old.SubscriptionRefreshMin, SubscriptionAddressType: old.SubscriptionAddressType,
				SubscriptionAutoUpdate: old.SubscriptionAutoUpdate, SubscriptionSmart: old.SubscriptionSmart, SubscriptionStatus: old.SubscriptionStatus,
				SubscriptionTokenVersion: 1, PurchasedAt: old.PurchasedAt, GracePeriodHours: old.GracePeriodHours,
				MaxActiveSessions: old.MaxActiveSessions, DeviceBindingEnabled: old.DeviceBindingEnabled, NewDeviceApproval: old.NewDeviceApproval,
				LinkLeakThreshold: old.LinkLeakThreshold, PrimaryAddressType: old.PrimaryAddressType, RenewalAuto: old.RenewalAuto,
				RenewalDays: old.RenewalDays,
			}
			if clone.Protocol == models.ProtocolWireGuard {
				if priv, pub, e := wireguardKeys(); e == nil {
					cl.PrivateKey, cl.PublicKey = priv, pub
					cl.Address = allocateWGAddress(h.S.DB, clone.ID)
				}
			}
			if err := h.S.DB.Create(&cl).Error; err != nil {
				_ = h.S.DB.Where("inbound_id=?", clone.ID).Delete(&models.Client{})
				_ = h.S.DB.Delete(&clone)
				c.JSON(500, gin.H{"success": false, "msg": err.Error()})
				return
			}
			created = append(created, cl)
		}
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Where("inbound_id=?", clone.ID).Delete(&models.Client{})
		_ = h.S.DB.Delete(&clone)
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "clone runtime apply failed: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.clone", strconv.Itoa(int(clone.ID)), clone.Remark)
	clone.Clients = created
	c.JSON(200, gin.H{"success": true, "obj": clone})
}

// MassProvision performs a dry-run or creates many inbound copies from an existing
// inbound. It is intentionally database-driven: the source configuration is copied
// once and the runtime is regenerated only once after the batch is committed.
type massProvisionRequest struct {
	SourceInboundID uint   `json:"sourceInboundId" binding:"required"`
	Count           int    `json:"count" binding:"required,min=1,max=10000"`
	NamePrefix      string `json:"namePrefix" binding:"required"`
	StartPort       int    `json:"startPort"`
	IncludeClients  bool   `json:"includeClients"`
	DryRun          bool   `json:"dryRun"`
}

// MassProvision handles the MassProvision API operation.
func (h *InboundHandler) MassProvision(c *gin.Context) {
	var req massProvisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	req.NamePrefix = strings.TrimSpace(req.NamePrefix)
	if n := len([]rune(req.NamePrefix)); n < 1 || n > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "namePrefix must be 1-100 characters"})
		return
	}
	if req.StartPort < 0 || req.StartPort > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "startPort must be 0-65535"})
		return
	}
	var src models.Inbound
	if err := h.S.DB.Preload("Clients").First(&src, req.SourceInboundID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "source inbound not found"})
		return
	}

	// Validate generated names in one database query instead of issuing one
	// COUNT query per requested inbound. This keeps a 10k-item dry run bounded
	// and predictable on SQLite.
	namePrefixLike := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(req.NamePrefix)) + "-%"
	var existingRemarks []string
	if err := h.S.DB.Model(&models.Inbound{}).Where("LOWER(remark) LIKE ? ESCAPE '\\'", namePrefixLike).Pluck("remark", &existingRemarks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not validate inbound names"})
		return
	}
	existingNames := make(map[string]struct{}, len(existingRemarks))
	for _, remark := range existingRemarks {
		existingNames[strings.ToLower(remark)] = struct{}{}
	}

	// Load the currently assigned ports once. The OS-level socket checks below
	// still protect against processes that are not represented in the database.
	var existingPorts []int
	if err := h.S.DB.Model(&models.Inbound{}).Pluck("port", &existingPorts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "could not validate inbound ports"})
		return
	}
	usedPorts := make(map[int]bool, len(existingPorts)+req.Count)
	for _, port := range existingPorts {
		usedPorts[port] = true
	}

	ports := make([]int, 0, req.Count)
	remarks := make([]string, 0, req.Count)
	conflicts := make([]string, 0)
	reserved := map[int]bool{h.S.Config.XrayAPIPort: true, h.S.Config.PanelPort: true}

	checkCandidatePort := func(port int) (bool, string) {
		if port < 1 || port > 65535 {
			return false, fmt.Sprintf("port %d is outside the valid range", port)
		}
		if reserved[port] {
			return false, fmt.Sprintf("port %d is reserved by the panel/runtime", port)
		}
		if usedPorts[port] {
			return false, fmt.Sprintf("port %d is already assigned to another inbound", port)
		}
		if ok, reason := h.portAvailableForNetwork(port, src.Network, 0, src.IPMode); !ok {
			return false, reason
		}
		return true, ""
	}

	for i := 0; i < req.Count; i++ {
		remark := fmt.Sprintf("%s-%d", req.NamePrefix, i+1)
		if _, exists := existingNames[strings.ToLower(remark)]; exists {
			conflicts = append(conflicts, fmt.Sprintf("name already exists: %s", remark))
			continue
		}

		port := 0
		if req.StartPort > 0 {
			port = req.StartPort + i
			if port > 65535 {
				conflicts = append(conflicts, fmt.Sprintf("port out of range for %s", remark))
				continue
			}
			if ok, reason := checkCandidatePort(port); !ok {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", remark, reason))
				continue
			}
		} else {
			var found bool
			for attempts := 0; attempts < 100; attempts++ {
				candidate, err := h.findAvailablePort("random", src.Network, src.IPMode)
				if err != nil {
					break
				}
				if usedPorts[candidate] {
					continue
				}
				port = candidate
				found = true
				break
			}
			if !found {
				conflicts = append(conflicts, fmt.Sprintf("%s: could not find a unique free port", remark))
				continue
			}
		}
		usedPorts[port] = true
		existingNames[strings.ToLower(remark)] = struct{}{}
		ports = append(ports, port)
		remarks = append(remarks, remark)
	}

	if len(conflicts) > 0 {
		c.JSON(http.StatusConflict, gin.H{"success": false, "dryRun": req.DryRun, "requested": req.Count, "ready": len(ports), "conflicts": conflicts, "msg": "dry-run validation found conflicts; no changes were made"})
		return
	}
	if req.DryRun {
		c.JSON(http.StatusOK, gin.H{"success": true, "dryRun": true, "requested": req.Count, "ready": len(ports), "ports": ports, "remarks": remarks, "includeClients": req.IncludeClients})
		return
	}

	createdIDs := make([]uint, 0, req.Count)
	err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		for i, port := range ports {
			clone := src
			clone.ID = 0
			clone.Remark = remarks[i]
			clone.Port = port
			clone.Tag = fmt.Sprintf("inbound-%d-%s", port, uuid.NewString()[:8])
			clone.Up, clone.Down = 0, 0
			clone.LimitReached = clone.IsExpired()
			clone.Maintenance = false
			clone.CreatedAt = time.Time{}
			clone.UpdatedAt = time.Time{}
			clone.Clients = nil
			if err := tx.Create(&clone).Error; err != nil {
				return err
			}
			createdIDs = append(createdIDs, clone.ID)
			if req.IncludeClients {
				for _, old := range src.Clients {
					cl := old
					cl.ID = 0
					cl.InboundID = clone.ID
					cl.Email = old.Email + "-" + uuid.NewString()[:5]
					cl.UUID = uuid.NewString()
					cl.Password = newInboundClientPassword(clone)
					cl.SubID = strings.ReplaceAll(uuid.NewString(), "-", "")
					cl.SubscriptionTokenVersion = 1
					cl.CreatedAt, cl.UpdatedAt = time.Time{}, time.Time{}
					cl.Up, cl.Down = 0, 0
					cl.TrafficBaseUp, cl.TrafficBaseDown = 0, 0
					if clone.Protocol == models.ProtocolWireGuard {
						priv, pub, e := wireguardKeys()
						if e != nil {
							return e
						}
						cl.PrivateKey, cl.PublicKey = priv, pub
						cl.Address = allocateWGAddress(tx, clone.ID)
						if cl.Address == "" {
							return fmt.Errorf("WireGuard address pool exhausted")
						}
					}
					if err := tx.Create(&cl).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "msg": "mass provisioning transaction rolled back: " + err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Where("inbound_id IN ?", createdIDs).Delete(&models.Client{})
		_ = h.S.DB.Where("id IN ?", createdIDs).Delete(&models.Inbound{})
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "msg": "runtime apply failed; batch was rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.mass_provision", fmt.Sprint(req.SourceInboundID), gin.H{"count": req.Count, "namePrefix": req.NamePrefix, "includeClients": req.IncludeClients})
	c.JSON(http.StatusOK, gin.H{"success": true, "created": len(createdIDs), "ids": createdIDs, "ports": ports})
}

// Analytics aggregates the retained hourly client snapshots for an inbound.
func (h *InboundHandler) Analytics(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	days := 7
	if v, e := strconv.Atoi(c.Query("days")); e == nil && v >= 1 && v <= 30 {
		days = v
	}
	var rows []struct {
		Bucket string `gorm:"column:bucket"`
		Up     int64  `gorm:"column:up"`
		Down   int64  `gorm:"column:down"`
	}
	// strftime() exists only in SQLite; PostgreSQL (the production backend)
	// needs to_char(), otherwise this endpoint silently returned no data.
	bucketExpr := "strftime('%Y-%m-%d %H:00', u.created_at)"
	if h.S.DB.Dialector.Name() == "postgres" {
		bucketExpr = "to_char(u.created_at, 'YYYY-MM-DD HH24:00')"
	}
	if err := h.S.DB.Raw(`SELECT `+bucketExpr+` AS bucket, COALESCE(SUM(u.up),0) AS up, COALESCE(SUM(u.down),0) AS down FROM usage_snapshots u JOIN clients c ON c.id=u.client_id WHERE c.inbound_id=? AND u.created_at>=? GROUP BY bucket ORDER BY bucket ASC`, id, time.Now().Add(-time.Duration(days)*24*time.Hour)).Scan(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "analytics query failed"})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"days": days, "points": rows}})
}

// Health returns a compact operational score for an inbound without imposing any
// traffic shaping. The score reflects runtime state, maintenance state and quota health.
func (h *InboundHandler) Health(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	running := h.S.Xray.Status().Running
	if ib.Protocol == models.ProtocolHysteria2 || ib.Protocol == models.ProtocolTUIC || ib.Protocol == models.ProtocolNaive || ib.Protocol == models.ProtocolWireGuard || ib.Protocol == models.ProtocolAnyTLS || ib.Protocol == models.ProtocolShadowTLS {
		running = h.S.SingBox.Status().Running
	}
	cpuPct := 0.0
	if v, e := cpu.Percent(0, false); e == nil && len(v) > 0 {
		cpuPct = v[0]
	}
	memPct := 0.0
	if v, e := mem.VirtualMemory(); e == nil {
		memPct = v.UsedPercent
	}
	score := 100
	reasons := []string{}
	if cpuPct >= 90 {
		score -= 15
		reasons = append(reasons, fmt.Sprintf("CPU %.0f%%", cpuPct))
	} else if cpuPct >= 75 {
		score -= 7
		reasons = append(reasons, fmt.Sprintf("CPU %.0f%%", cpuPct))
	}
	if memPct >= 90 {
		score -= 15
		reasons = append(reasons, fmt.Sprintf("RAM %.0f%%", memPct))
	} else if memPct >= 80 {
		score -= 7
		reasons = append(reasons, fmt.Sprintf("RAM %.0f%%", memPct))
	}
	if !ib.Enable || ib.Maintenance {
		score -= 40
		reasons = append(reasons, "disabled or maintenance")
	}
	if !running {
		score -= 45
		reasons = append(reasons, "engine offline")
	}
	if len(ib.Clients) == 0 {
		score -= 5
		reasons = append(reasons, "no clients")
	}
	if score < 0 {
		score = 0
	}
	status := "Healthy"
	if score < 80 {
		status = "Warning"
	}
	if score < 50 {
		status = "Critical"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"score": score, "status": status, "reasons": reasons, "running": running, "clients": len(ib.Clients), "maintenance": ib.Maintenance, "cpuPercent": cpuPct, "memoryPercent": memPct}})
}

// TestPort handles the TestPort API operation.
func (h *InboundHandler) TestPort(c *gin.Context) { h.PortAvailability(c) }

// Live reports realtime speed, connected devices and per-client activity of one
// inbound. It works for every protocol: Xray protocols use the Xray
// StatsService (+ online IP list), sing-box protocols (Hysteria2, TUIC, Naive,
// AnyTLS, ShadowTLS, WireGuard) use sing-box's loopback Clash API.
func (h *InboundHandler) Live(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	// The live response only needs a small inbound summary and each client's
	// ID/email. Avoid loading every client column on every 5-second UI poll.
	if err := h.S.DB.Select("id", "remark", "protocol", "tag", "port", "up", "down").
		Preload("Clients", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "inbound_id", "email", "limit_ip")
		}).First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	if note := h.S.LiveEngineNote(ib.Protocol); note != "" {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"supported": false, "reason": note}})
		return
	}
	h.S.RefreshLive()
	var up, down int64
	var active int
	subjectsByClient := clientsTrafficSubjects(h.S.DB, ib.Clients)
	users := make([]gin.H, 0, len(ib.Clients))
	allSubjects := make([]string, 0, len(ib.Clients))
	for _, cl := range ib.Clients {
		subjects := subjectsByClient[cl.ID]
		allSubjects = append(allSubjects, subjects...)
		live, ok := xray.LiveSampleForSubjects(subjects...)
		if !ok {
			continue
		}
		up += int64(live.UplinkSpeed)
		down += int64(live.DownlinkSpeed)
		if live.Online {
			active++
		}
		users = append(users, gin.H{"id": cl.ID, "email": cl.Email, "online": live.Online, "uploadSpeed": live.UplinkSpeed, "downloadSpeed": live.DownlinkSpeed, "uplinkBytes": live.UplinkBytes, "downlinkBytes": live.DownlinkBytes, "sampledAt": live.SampledAt, "devices": live.Devices, "connections": live.Connections, "limitIp": cl.LimitIP})
	}
	// Inbound-level numbers: the engine's own inbound counter when it exists
	// (includes traffic of every client), otherwise the sum of the clients.
	uploadSpeed, downloadSpeed := up, down
	if tag, ok := xray.LiveSampleFor(core.InboundLiveSubject(ib)); ok {
		if v := int64(tag.UplinkSpeed); v > uploadSpeed {
			uploadSpeed = v
		}
		if v := int64(tag.DownlinkSpeed); v > downloadSpeed {
			downloadSpeed = v
		}
	}
	// A device (source IP) that uses several accounts is counted once.
	devices := xray.DeviceCount(append([]string{core.InboundLiveSubject(ib)}, allSubjects...)...)
	connections := xray.ConnectionCount(core.InboundLiveSubject(ib))
	if connections == 0 {
		connections = xray.ConnectionCount(allSubjects...)
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"supported": true, "inboundId": ib.ID, "protocol": ib.Protocol, "activeUsers": active, "devices": devices, "connections": connections, "uploadSpeed": uploadSpeed, "downloadSpeed": downloadSpeed, "totalUp": ib.Up, "totalDown": ib.Down, "users": users}})
}

// Status handles the Status API operation.
func (h *InboundHandler) Status(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	running := false
	engine := "xray"
	switch ib.Protocol {
	case models.ProtocolHysteria2, models.ProtocolTUIC, models.ProtocolNaive, models.ProtocolWireGuard, models.ProtocolAnyTLS, models.ProtocolShadowTLS:
		engine = "sing-box"
		running = h.S.SingBox.Status().Running
	default:
		running = h.S.Xray.Status().Running
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"id": ib.ID, "enabled": ib.Enable, "running": ib.Enable && running, "engine": engine, "port": ib.Port, "clients": len(ib.Clients), "up": ib.Up, "down": ib.Down, "labels": ib.Labels, "updatedAt": ib.UpdatedAt}})
}

// Preview handles the Preview API operation.
func (h *InboundHandler) Preview(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	engine, data, err := h.S.ValidateInboundConfig(ib)
	if err != nil {
		c.JSON(422, gin.H{"success": false, "valid": false, "engine": engine, "msg": err.Error(), "config": string(data)})
		return
	}
	c.JSON(200, gin.H{"success": true, "valid": true, "engine": engine, "config": string(data)})
}

// Revisions handles the Revisions API operation.
func (h *InboundHandler) Revisions(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var rows []models.InboundRevision
	if err := h.S.DB.Select("id,inbound_id,reason,created_at").Where("inbound_id = ?", id).Order("id desc").Limit(20).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": rows})
}

// Rollback handles the Rollback API operation.
func (h *InboundHandler) Rollback(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	_ = h.S.DB.Preload("Clients").First(&ib, id)
	var body struct {
		RevisionID uint `json:"revisionId"`
	}
	_ = c.ShouldBindJSON(&body) // optional: pick a specific revision from the history list
	var rev models.InboundRevision
	revQuery := h.S.DB.Where("inbound_id = ?", id)
	if body.RevisionID > 0 {
		revQuery = revQuery.Where("id = ?", body.RevisionID)
	}
	if err := revQuery.Order("id desc").First(&rev).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "no rollback snapshot available"})
		return
	}
	targetRevisionID := rev.ID
	if ib.ID != 0 {
		_ = h.saveRevision(ib, "pre-rollback")
	}
	if err := restoreRevision(h.S.DB, targetRevisionID); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "restore failed: " + err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "snapshot restored but runtime apply failed: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.rollback", strconv.Itoa(id), nil)
	c.JSON(200, gin.H{"success": true})
}

type inboundExport struct {
	Inbound models.Inbound  `json:"inbound"`
	Clients []models.Client `json:"clients"`
}

// Export handles the Export API operation.
func (h *InboundHandler) Export(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound id"})
		return
	}
	var ib models.Inbound
	if err := h.S.DB.Preload("Clients").First(&ib, id).Error; err != nil {
		c.JSON(404, gin.H{"success": false, "msg": "inbound not found"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="electruvpn-inbound-%d.json"`, ib.ID))
	c.JSON(200, inboundExport{Inbound: ib, Clients: ib.Clients})
}

// Import handles the Import API operation.
func (h *InboundHandler) Import(c *gin.Context) {
	var ex inboundExport
	if err := c.ShouldBindJSON(&ex); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid import JSON"})
		return
	}
	ex.Inbound.ID = 0
	// The export embeds the clients twice (inside "inbound" and in "clients").
	// Leaving them on the inbound makes GORM insert them together with the
	// inbound (keeping their old IDs) and the explicit loop below then hits the
	// unique email/subId indexes, so every import with clients failed.
	if len(ex.Clients) == 0 {
		ex.Clients = ex.Inbound.Clients
	}
	ex.Inbound.Clients = nil
	ex.Inbound.Remark = strings.TrimSpace(ex.Inbound.Remark)
	if ex.Inbound.Protocol == models.ProtocolShadowsocks {
		ex.Inbound.Settings = xray.EnsureShadowsocksServerKey(ex.Inbound.Settings)
	}
	ex.Inbound.CreatedAt, ex.Inbound.UpdatedAt = time.Time{}, time.Time{}
	ex.Inbound.Maintenance = false
	if ex.Inbound.TotalGB < 0 || ex.Inbound.TotalGB > maxInboundTotalGB || ex.Inbound.ExpiryTime < 0 {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound volume or expiry in import file"})
		return
	}
	if len(ex.Clients) > 5000 {
		c.JSON(400, gin.H{"success": false, "msg": "import file contains too many clients (max 5000)"})
		return
	}
	ex.Inbound.IPMode = normalizeIPMode(ex.Inbound.IPMode)
	ex.Inbound.Listen = normalizeListenForIPMode(ex.Inbound.Listen, ex.Inbound.IPMode)
	if ex.Inbound.SubscriptionDefaultAddress == "" {
		ex.Inbound.SubscriptionDefaultAddress = "ipv4"
	}
	if err := validateInboundRequest(createInboundRequest{
		Remark: ex.Inbound.Remark, Listen: ex.Inbound.Listen, IPMode: ex.Inbound.IPMode, Port: ex.Inbound.Port,
		Protocol: ex.Inbound.Protocol, Network: ex.Inbound.Network, Security: ex.Inbound.Security,
		Settings: ex.Inbound.Settings, StreamSettings: ex.Inbound.StreamSettings, Sniffing: ex.Inbound.Sniffing, Labels: ex.Inbound.Labels,
	}, h.S.Config.XrayAPIPort, h.S.Config.SecurityMode); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid inbound in import file: " + err.Error()})
		return
	}
	// A remark must stay unique (Create/Update enforce it); suffix instead of failing.
	baseRemark := ex.Inbound.Remark
	for n := 2; n < 1000; n++ {
		var dup int64
		h.S.DB.Model(&models.Inbound{}).Where("lower(remark) = ?", strings.ToLower(ex.Inbound.Remark)).Count(&dup)
		if dup == 0 {
			break
		}
		ex.Inbound.Remark = truncateRunes(baseRemark, 120) + fmt.Sprintf(" (%d)", n)
	}
	ex.Inbound.LimitReached = ex.Inbound.IsLimited()
	ex.Inbound.Tag = fmt.Sprintf("inbound-%d-%s", ex.Inbound.Port, uuid.NewString()[:8])
	ex.Inbound.Labels = cleanLabels(ex.Inbound.Labels)
	if ok, reason := h.portAvailableForNetwork(ex.Inbound.Port, ex.Inbound.Network, 0, ex.Inbound.IPMode); !ok {
		c.JSON(409, gin.H{"success": false, "msg": reason})
		return
	}
	if _, _, err := h.S.ValidateInboundConfig(ex.Inbound); err != nil {
		c.JSON(422, gin.H{"success": false, "msg": "config validation failed: " + err.Error()})
		return
	}
	tx := h.S.DB.Begin()
	if tx.Error != nil {
		c.JSON(500, gin.H{"success": false, "msg": tx.Error.Error()})
		return
	}
	if err := tx.Create(&ex.Inbound).Error; err != nil {
		tx.Rollback()
		c.JSON(409, gin.H{"success": false, "msg": err.Error()})
		return
	}
	for i := range ex.Clients {
		cl := &ex.Clients[i]
		cl.ID = 0
		cl.InboundID = ex.Inbound.ID
		cl.CreatedAt, cl.UpdatedAt = time.Time{}, time.Time{}
		// New subscription token: re-using the exported subId would make two
		// clients share one subscription URL (and violates the unique index).
		cl.SubID = strings.ReplaceAll(uuid.NewString(), "-", "")
		cl.SubscriptionTokenVersion = 1
		cl.TrafficBaseUp, cl.TrafficBaseDown = 0, 0
		email := strings.TrimSpace(cl.Email)
		if email == "" {
			email = fmt.Sprintf("import-%d-%d", ex.Inbound.Port, i+1)
		}
		base := truncateRunes(email, 110)
		cl.Email = truncateRunes(email, 128)
		for n := 2; ; n++ {
			var count int64
			if e := tx.Model(&models.Client{}).Where("email = ?", cl.Email).Count(&count).Error; e != nil {
				tx.Rollback()
				c.JSON(500, gin.H{"success": false, "msg": e.Error()})
				return
			}
			if count == 0 {
				break
			}
			cl.Email = base + fmt.Sprintf("-%d", n)
		}
		if err := tx.Create(cl).Error; err != nil {
			tx.Rollback()
			c.JSON(409, gin.H{"success": false, "msg": "could not import client " + cl.Email + ": " + err.Error()})
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		_ = h.S.DB.Where("inbound_id = ?", ex.Inbound.ID).Delete(&models.Client{})
		_ = h.S.DB.Delete(&models.Inbound{}, ex.Inbound.ID)
		_ = h.S.RegenerateAndRestartXray()
		c.JSON(500, gin.H{"success": false, "msg": "import rolled back: " + err.Error()})
		return
	}
	auditAction(h.S, c, "inbound.import", strconv.FormatUint(uint64(ex.Inbound.ID), 10), ex.Inbound.Remark)
	ex.Inbound.Clients = ex.Clients
	c.JSON(200, gin.H{"success": true, "obj": ex.Inbound})
}

// truncateRunes shortens s to at most max characters without splitting a
// multi-byte (e.g. Persian) character, which byte slicing would corrupt.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

type bulkInboundRequest struct {
	IDs    []uint `json:"ids" binding:"required,min=1"`
	Action string `json:"action" binding:"required"`
}

// BulkAction handles the BulkAction API operation.
func (h *InboundHandler) BulkAction(c *gin.Context) {
	var req bulkInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": err.Error()})
		return
	}
	allowed := map[string]bool{"enable": true, "disable": true, "delete": true}
	if !allowed[req.Action] {
		c.JSON(400, gin.H{"success": false, "msg": "unsupported bulk action"})
		return
	}
	var rows []models.Inbound
	if err := h.S.DB.Where("id IN ?", req.IDs).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if len(rows) == 0 {
		c.JSON(404, gin.H{"success": false, "msg": "none of the selected inbounds exist"})
		return
	}
	revisionIDs := make(map[uint]uint, len(rows))
	for _, ib := range rows {
		revID, err := h.saveRevisionWithID(ib, "bulk-"+req.Action)
		if err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "could not save rollback snapshot: " + err.Error()})
			return
		}
		revisionIDs[ib.ID] = revID
	}
	if err := h.S.DB.Transaction(func(tx *gorm.DB) error {
		for _, ib := range rows {
			if req.Action == "delete" {
				clientIDs := tx.Model(&models.Client{}).Select("id").Where("inbound_id = ?", ib.ID)
				if err := tx.Where("inbound_id = ? OR client_id IN (?)", ib.ID, clientIDs).Delete(&models.ClientConfigProfile{}).Error; err != nil {
					return err
				}
				if err := tx.Where("inbound_id = ?", ib.ID).Delete(&models.Client{}).Error; err != nil {
					return err
				}
				if err := tx.Delete(&models.Inbound{}, ib.ID).Error; err != nil {
					return err
				}
			} else {
				upd := map[string]any{"enable": req.Action == "enable"}
				if req.Action == "enable" {
					upd["maintenance"] = false // otherwise the inbound is "enabled" yet still locked in maintenance
				}
				if err := tx.Model(&models.Inbound{}).Where("id = ?", ib.ID).Updates(upd).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": err.Error()})
		return
	}
	if err := h.S.RegenerateAndRestartXray(); err != nil {
		var restoreErr error
		for _, ib := range rows {
			if revID := revisionIDs[ib.ID]; revID != 0 {
				if e := restoreRevision(h.S.DB, revID); e != nil {
					if restoreErr == nil {
						restoreErr = e
					}
				}
			}
		}
		if restoreErr == nil {
			restoreErr = h.S.RegenerateAndRestartXray()
		}
		msg := "bulk inbound change was rolled back because runtime apply failed: " + err.Error()
		if restoreErr != nil {
			msg += "; restore failed: " + restoreErr.Error()
		}
		c.JSON(500, gin.H{"success": false, "msg": msg})
		return
	}
	c.JSON(200, gin.H{"success": true, "changed": len(rows)})
}

// ensureRealityKeysInStream fills privateKey/publicKey/shortIds when security=reality and keys are empty.
func ensureRealityKeysInStream(streamJSON string, s *core.Server) string {
	streamJSON = strings.TrimSpace(streamJSON)
	if streamJSON == "" {
		streamJSON = "{}"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(streamJSON), &m); err != nil {
		return streamJSON
	}
	rs, _ := m["realitySettings"].(map[string]any)
	if rs == nil {
		rs = map[string]any{}
	}
	priv, _ := rs["privateKey"].(string)
	pub, _ := rs["publicKey"].(string)
	if strings.TrimSpace(priv) != "" && strings.TrimSpace(pub) != "" {
		return streamJSON
	}
	// Generate via xray (flexible parse) or pure-Go fallback so create never fails on key format.
	privOut, pubOut := "", ""
	bin := ""
	if s != nil && s.Config != nil {
		bin = strings.TrimSpace(s.Config.XrayBinaryPath)
	}
	tryBins := []string{}
	if bin != "" {
		tryBins = append(tryBins, bin)
	}
	if pathBin, err := exec.LookPath("xray"); err == nil {
		tryBins = append(tryBins, pathBin)
	}
	for _, b := range tryBins {
		out, err := exec.Command(b, "x25519").CombinedOutput()
		if err != nil {
			continue
		}
		privOut, pubOut = parseX25519Output(string(out))
		if privOut != "" && pubOut != "" {
			break
		}
	}
	if privOut == "" || pubOut == "" {
		if p, u, err := generateX25519KeypairPure(); err == nil {
			privOut, pubOut = p, u
		}
	}
	if privOut != "" {
		rs["privateKey"] = privOut
	}
	if pubOut != "" {
		rs["publicKey"] = pubOut
	}
	if _, ok := rs["shortIds"]; !ok {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		rs["shortIds"] = []string{fmt.Sprintf("%x", b)}
	}
	if _, ok := rs["serverNames"]; !ok {
		rs["serverNames"] = []string{"www.microsoft.com"}
	}
	if _, ok := rs["dest"]; !ok {
		rs["dest"] = "www.microsoft.com:443"
	}
	m["realitySettings"] = rs
	m["security"] = "reality"
	b, err := json.Marshal(m)
	if err != nil {
		return streamJSON
	}
	return string(b)
}

var hysteriaHopRE = regexp.MustCompile(`^\d{1,5}(-\d{1,5})?(,\d{1,5}(-\d{1,5})?)*$`)
var hysteriaPinRE = regexp.MustCompile(`^[0-9a-fA-F]{2}(:?[0-9a-fA-F]{2}){31}$`)

// validateHysteria2Settings rejects option combinations that make sing-box
// refuse the whole config (an unknown obfs type, a masquerade without its
// required field...) before they are saved, instead of failing at apply time.
func validateHysteria2Settings(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var st models.Hysteria2Settings
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return fmt.Errorf("invalid Hysteria2 settings JSON")
	}
	if st.UpMbps < 0 || st.DownMbps < 0 || st.UpMbps > 100000 || st.DownMbps > 100000 {
		return fmt.Errorf("Hysteria2 up/down Mbps must be between 0 and 100000")
	}
	if ot := strings.ToLower(strings.TrimSpace(st.ObfsType)); ot != "" {
		if ot != "salamander" {
			return fmt.Errorf("Hysteria2 obfs type must be empty or \"salamander\"")
		}
		if len(st.ObfsPassword) < 4 {
			return fmt.Errorf("Hysteria2 obfs password is required (min 4 characters)")
		}
	}
	switch strings.ToLower(strings.TrimSpace(st.MasqueradeType)) {
	case "":
	case "proxy":
		u, err := url.Parse(strings.TrimSpace(st.MasqueradeURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("Hysteria2 masquerade proxy needs a valid http(s) URL")
		}
	case "file":
		if strings.TrimSpace(st.MasqueradeDir) == "" {
			return fmt.Errorf("Hysteria2 masquerade file needs a directory")
		}
	case "string":
		if st.MasqueradeStatus != 0 && (st.MasqueradeStatus < 100 || st.MasqueradeStatus > 599) {
			return fmt.Errorf("Hysteria2 masquerade status must be 100-599")
		}
	default:
		return fmt.Errorf("Hysteria2 masquerade type must be proxy, file or string")
	}
	if hop := strings.TrimSpace(st.PortHopping); hop != "" {
		if !hysteriaHopRE.MatchString(hop) {
			return fmt.Errorf("Hysteria2 port hopping must look like 20000-30000")
		}
	}
	if pin := strings.TrimSpace(st.PinSHA256); pin != "" && !hysteriaPinRE.MatchString(pin) {
		return fmt.Errorf("Hysteria2 pinSHA256 must be a 32-byte SHA-256 hex fingerprint")
	}
	return nil
}
