// Package honeypot implements an advanced active probe defense (2026):
// invalid inbound traffic (via Xray VLESS/Trojan fallbacks) is diverted to a
// local multi-port tarpit that wastes scanner time/CPU, serves realistic fake
// admin login pages, auto-bans repeat offenders via Fail2ban/ufw, and reports
// to Telegram admin + optional global blacklist.
package honeypot

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"gorm.io/gorm"
)

// Default is set by main when the panel starts.
var Default *Server

// Server is a bounded aggressive multi-port tarpit for DPI scanners.
type Server struct {
	DB     *gorm.DB
	Listen string // primary host:port, default 127.0.0.1:19601

	mu       sync.Mutex
	ln       net.Listener
	extraLns []net.Listener
	active   int32
	perIP    map[string]int32
	total    atomic.Int64
	bytesOut atomic.Int64
	banned   atomic.Int64

	alertMu   sync.Mutex
	lastAlert map[string]time.Time
}

type runtimeCfg struct {
	Enabled       bool
	MaxConn       int
	PerIP         int
	MinHold       time.Duration
	MaxHold       time.Duration
	Aggression    int
	AutoInject    bool
	AutoBan       bool
	BanThreshold  int
	BanWindow     time.Duration
	BanDurationH  int
	TelegramAlert bool
	FakeLogin     bool
	ExtraPorts    []string
	GlobalListURL string
}

func (s *Server) loadCfg() runtimeCfg {
	c := runtimeCfg{
		Enabled: true, MaxConn: 256, PerIP: 6,
		MinHold: 45 * time.Second, MaxHold: 180 * time.Second,
		Aggression: 8, AutoInject: true, AutoBan: true,
		BanThreshold: 3, BanWindow: 30 * time.Minute, BanDurationH: 24,
		TelegramAlert: true, FakeLogin: true,
	}
	if s.DB == nil {
		return c
	}
	get := func(k, def string) string {
		var row models.PanelSetting
		if s.DB.Where("key = ?", k).First(&row).Error == nil && row.Value != "" {
			return row.Value
		}
		return def
	}
	c.Enabled = get(models.HoneypotEnabledKey, "1") != "0"
	if v := get(models.HoneypotMaxConnKey, "256"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			c.MaxConn = n
		}
	}
	if v := get(models.HoneypotPerIPKey, "6"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			c.PerIP = n
		}
	}
	minSec, maxSec := 45, 180
	if v := get(models.HoneypotMinHoldKey, "45"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 5 && n <= 600 {
			minSec = n
		}
	}
	if v := get(models.HoneypotMaxHoldKey, "180"); v != "" {
		if n, _ := strconv.Atoi(v); n >= minSec && n <= 900 {
			maxSec = n
		}
	}
	c.MinHold = time.Duration(minSec) * time.Second
	c.MaxHold = time.Duration(maxSec) * time.Second
	if v := get(models.HoneypotAggressionKey, "8"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 1 && n <= 10 {
			c.Aggression = n
		}
	}
	c.AutoInject = get(models.HoneypotAutoInjectKey, "1") != "0"
	c.AutoBan = get(models.HoneypotAutoBanKey, "1") != "0"
	if v := get(models.HoneypotBanThresholdKey, "3"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 1 && n <= 50 {
			c.BanThreshold = n
		}
	}
	if v := get(models.HoneypotBanWindowKey, "30"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 5 && n <= 1440 {
			c.BanWindow = time.Duration(n) * time.Minute
		}
	}
	if v := get(models.HoneypotBanDurationKey, "24"); v != "" {
		if n, _ := strconv.Atoi(v); n >= 0 && n <= 8760 {
			c.BanDurationH = n
		}
	}
	c.TelegramAlert = get(models.HoneypotTelegramAlertKey, "1") != "0"
	c.FakeLogin = get(models.HoneypotFakeLoginKey, "1") != "0"
	if ports := get(models.HoneypotExtraPortsKey, ""); ports != "" {
		for _, p := range strings.Split(ports, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				c.ExtraPorts = append(c.ExtraPorts, p)
			}
		}
	}
	c.GlobalListURL = strings.TrimSpace(get(models.HoneypotGlobalListKey, ""))
	return c
}

func (s *Server) Addr() string {
	if s.Listen != "" {
		return s.Listen
	}
	if s.DB != nil {
		var row models.PanelSetting
		if s.DB.Where("key = ?", models.HoneypotListenKey).First(&row).Error == nil && strings.TrimSpace(row.Value) != "" {
			return strings.TrimSpace(row.Value)
		}
	}
	return "127.0.0.1:19601"
}

func (s *Server) LocalDest() string { return s.Addr() }

func (s *Server) Start(ctx context.Context) {
	if s.perIP == nil {
		s.perIP = make(map[string]int32)
	}
	if s.lastAlert == nil {
		s.lastAlert = make(map[string]time.Time)
	}
	cfg := s.loadCfg()
	if !cfg.Enabled {
		slog.Info("honeypot disabled")
		<-ctx.Done()
		return
	}
	addr := s.Addr()
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		slog.Error("honeypot listen failed", "addr", addr, "err", err)
		<-ctx.Done()
		return
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	slog.Info("honeypot active probe defense listening",
		"addr", addr, "aggression", cfg.Aggression, "maxConn", cfg.MaxConn,
		"autoBan", cfg.AutoBan, "fakeLogin", cfg.FakeLogin)

	host := "127.0.0.1"
	if h, _, e := net.SplitHostPort(addr); e == nil && h != "" {
		host = h
	}
	for _, port := range cfg.ExtraPorts {
		extraAddr := net.JoinHostPort(host, port)
		eln, e := lc.Listen(ctx, "tcp", extraAddr)
		if e != nil {
			slog.Warn("honeypot extra port listen failed", "addr", extraAddr, "err", e)
			continue
		}
		s.mu.Lock()
		s.extraLns = append(s.extraLns, eln)
		s.mu.Unlock()
		slog.Info("honeypot fake service port open", "addr", extraAddr)
		go s.acceptLoop(ctx, eln)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		s.mu.Lock()
		for _, el := range s.extraLns {
			_ = el.Close()
		}
		s.mu.Unlock()
	}()

	s.acceptLoop(ctx, ln)
}

func (s *Server) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		cfg := s.loadCfg()
		if !cfg.Enabled {
			_ = conn.Close()
			continue
		}
		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		if s.isBanned(ip) {
			_ = conn.Close()
			continue
		}
		if !s.tryAcquire(ip, cfg) {
			_ = conn.Close()
			continue
		}
		s.total.Add(1)
		go s.handle(conn, ip, cfg)
	}
}

func (s *Server) isBanned(ip string) bool {
	if s.DB == nil || ip == "" {
		return false
	}
	var ban models.HoneypotBan
	if s.DB.Where("ip = ?", ip).First(&ban).Error != nil {
		return false
	}
	if ban.ExpiresAt != nil && time.Now().After(*ban.ExpiresAt) {
		_ = s.DB.Delete(&ban).Error
		return false
	}
	return true
}

func (s *Server) tryAcquire(ip string, cfg runtimeCfg) bool {
	if atomic.LoadInt32(&s.active) >= int32(cfg.MaxConn) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perIP[ip] >= int32(cfg.PerIP) {
		return false
	}
	s.perIP[ip]++
	atomic.AddInt32(&s.active, 1)
	return true
}

func (s *Server) release(ip string) {
	atomic.AddInt32(&s.active, -1)
	s.mu.Lock()
	if s.perIP[ip] > 0 {
		s.perIP[ip]--
		if s.perIP[ip] == 0 {
			delete(s.perIP, ip)
		}
	}
	s.mu.Unlock()
}

func (s *Server) handle(conn net.Conn, ip string, cfg runtimeCfg) {
	defer s.release(ip)
	defer conn.Close()

	start := time.Now()
	hold := cfg.MinHold
	if cfg.MaxHold > cfg.MinHold {
		var b [2]byte
		_, _ = rand.Read(b[:])
		span := cfg.MaxHold - cfg.MinHold
		hold = cfg.MinHold + time.Duration((int64(b[0])<<8+int64(b[1]))%int64(span))
	}
	var recentCount int64
	if s.DB != nil {
		since := time.Now().Add(-cfg.BanWindow)
		_ = s.DB.Model(&models.HoneypotProbe{}).Where("remote_ip = ? AND created_at >= ?", ip, since).Count(&recentCount).Error
		if recentCount >= 5 {
			hold = hold + hold/2
		}
		if recentCount >= 20 {
			hold = cfg.MaxHold * 2
			if hold > 15*time.Minute {
				hold = 15 * time.Minute
			}
		}
	}

	_ = conn.SetDeadline(time.Now().Add(hold + 5*time.Second))

	var readN, writeN int64
	mode := "tarpit"
	ua := ""

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	if n > 0 {
		readN += int64(n)
		head := buf[:n]
		if looksHTTP(head) {
			ua = extractUA(head)
			if cfg.FakeLogin && (looksLoginPath(head) || cfg.Aggression >= 6) {
				mode = "fake_login"
				w := s.writeFakeLogin(conn, cfg, hold, head)
				writeN += w
			} else {
				mode = "http_fake"
				w := s.writeSlowHTTP(conn, cfg, hold)
				writeN += w
			}
		} else if looksTLS(head) {
			mode = "tls_junk"
			w := s.writeSlowTLSJunk(conn, cfg, hold, head)
			writeN += w
		} else if looksSSH(head) {
			mode = "ssh_tarpit"
			w := s.writeSSHTarpit(conn, cfg, hold)
			writeN += w
		} else {
			w := s.writeSlowJunk(conn, cfg, hold)
			writeN += w
		}
	} else {
		w := s.writeSlowJunk(conn, cfg, hold)
		writeN += w
	}

	_ = conn.SetReadDeadline(time.Now().Add(hold))
	end := time.Now().Add(hold)
	tiny := make([]byte, 1)
	for time.Now().Before(end) {
		_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		rn, err := conn.Read(tiny)
		if rn > 0 {
			readN += int64(rn)
		}
		if err != nil && err != io.EOF {
			time.Sleep(time.Duration(50+cfg.Aggression*20) * time.Millisecond)
		}
		if time.Now().After(end) {
			break
		}
		if cfg.Aggression >= 5 {
			chunk := make([]byte, 16+cfg.Aggression*8)
			_, _ = rand.Read(chunk)
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			wn, _ := conn.Write(chunk)
			writeN += int64(wn)
			s.bytesOut.Add(int64(wn))
		}
		time.Sleep(time.Duration(30+cfg.Aggression*15) * time.Millisecond)
	}

	dur := time.Since(start).Milliseconds()
	banned := false
	if cfg.AutoBan {
		banned = s.maybeBan(ip, recentCount+1, cfg)
	}
	s.record(ip, remotePort(conn), readN, writeN, dur, mode, ua, banned)

	if cfg.TelegramAlert {
		s.maybeNotify(ip, mode, recentCount+1, banned)
	}
}

func (s *Server) maybeBan(ip string, probeCount int64, cfg runtimeCfg) bool {
	if s.DB == nil || ip == "" || probeCount < int64(cfg.BanThreshold) {
		return false
	}
	if s.isBanned(ip) {
		return true
	}
	var exp *time.Time
	if cfg.BanDurationH > 0 {
		t := time.Now().Add(time.Duration(cfg.BanDurationH) * time.Hour)
		exp = &t
	}
	ban := models.HoneypotBan{
		IP: ip, Reason: fmt.Sprintf("honeypot probes=%d window=%s", probeCount, cfg.BanWindow),
		ProbeCount: int(probeCount), Source: "honeypot", ExpiresAt: exp, CreatedAt: time.Now(),
	}
	if err := s.DB.Where("ip = ?", ip).Assign(ban).FirstOrCreate(&ban).Error; err != nil {
		slog.Warn("honeypot ban record failed", "ip", ip, "err", err)
	}
	s.banned.Add(1)
	go s.applySystemBan(ip, cfg.BanDurationH)
	if cfg.GlobalListURL != "" {
		go s.postGlobalList(cfg.GlobalListURL, ip, ban.Reason)
	}
	slog.Info("honeypot auto-ban", "ip", ip, "probes", probeCount)
	return true
}

func (s *Server) applySystemBan(ip string, hours int) {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return
	}
	layers := make([]string, 0, 4)
	// Layer 1: fail2ban honeypot jail (preferred), then auth jail, then sshd
	if path, err := exec.LookPath("fail2ban-client"); err == nil {
		ok := false
		for _, jail := range []string{"electruvpn-honeypot", "electruvpn-auth", "sshd"} {
			if out, e := exec.Command(path, "set", jail, "banip", ip).CombinedOutput(); e == nil {
				layers = append(layers, "fail2ban:"+jail)
				ok = true
				break
			} else {
				_ = out
			}
		}
		if !ok {
			layers = append(layers, "fail2ban:failed")
		}
	}
	// Layer 2: UFW deny (always try; does not replace fail2ban)
	if path, err := exec.LookPath("ufw"); err == nil {
		if err := exec.Command(path, "deny", "from", ip, "comment", "electruvpn-honeypot").Run(); err == nil {
			layers = append(layers, "ufw")
		} else if err := exec.Command(path, "insert", "1", "deny", "from", ip).Run(); err == nil {
			layers = append(layers, "ufw-insert")
		}
	}
	// Layer 3: iptables DROP as last resort when UFW unavailable
	if path, err := exec.LookPath("iptables"); err == nil {
		// Avoid duplicate rules
		check := exec.Command(path, "-C", "INPUT", "-s", ip, "-j", "DROP")
		if check.Run() != nil {
			if err := exec.Command(path, "-I", "INPUT", "-s", ip, "-j", "DROP").Run(); err == nil {
				layers = append(layers, "iptables")
				if hours > 0 {
					if at, e := exec.LookPath("at"); e == nil {
						script := fmt.Sprintf("%s -D INPUT -s %s -j DROP", path, ip)
						cmd := exec.Command(at, "now", "+", strconv.Itoa(hours), "hours")
						cmd.Stdin = strings.NewReader(script)
						_ = cmd.Run()
					}
				}
			}
		} else {
			layers = append(layers, "iptables-exists")
		}
	}
	slog.Info("honeypot multi-layer ban applied", "ip", ip, "layers", strings.Join(layers, ","), "hours", hours)
}

// applySystemUnban reverses multi-layer bans best-effort.
func (s *Server) applySystemUnban(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	if path, err := exec.LookPath("fail2ban-client"); err == nil {
		for _, jail := range []string{"electruvpn-honeypot", "electruvpn-auth", "sshd"} {
			_ = exec.Command(path, "set", jail, "unbanip", ip).Run()
		}
	}
	if path, err := exec.LookPath("ufw"); err == nil {
		_ = exec.Command(path, "delete", "deny", "from", ip).Run()
		_ = exec.Command(path, "status", "numbered").Run() // no-op ensure path works
	}
	if path, err := exec.LookPath("iptables"); err == nil {
		for i := 0; i < 5; i++ {
			if exec.Command(path, "-D", "INPUT", "-s", ip, "-j", "DROP").Run() != nil {
				break
			}
		}
	}
	slog.Info("honeypot multi-layer unban attempted", "ip", ip)
}

// DailyReport builds a 24h summary and optionally sends it to Telegram.

// BanIP is the public multi-layer ban entry used by the admin API.
func (s *Server) BanIP(ip string, hours int) {
	s.applySystemBan(ip, hours)
	s.banned.Add(1)
}

// UnbanIP is the public multi-layer unban entry used by the admin API.
func (s *Server) UnbanIP(ip string) {
	s.applySystemUnban(ip)
}

func (s *Server) DailyReport(sendTelegram bool) map[string]any {
	out := map[string]any{
		"probes24h": int64(0), "uniqueIPs24h": int64(0), "bans24h": int64(0),
		"activeBans": int64(0), "topIPs": []map[string]any{}, "generatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if s == nil || s.DB == nil {
		return out
	}
	since := time.Now().Add(-24 * time.Hour)
	var probes, uniq, bans24, active int64
	s.DB.Model(&models.HoneypotProbe{}).Where("created_at >= ?", since).Count(&probes)
	s.DB.Model(&models.HoneypotProbe{}).Where("created_at >= ?", since).Distinct("remote_ip").Count(&uniq)
	s.DB.Model(&models.HoneypotBan{}).Where("created_at >= ?", since).Count(&bans24)
	s.DB.Model(&models.HoneypotBan{}).Where("expires_at IS NULL OR expires_at > ?", time.Now()).Count(&active)
	out["probes24h"] = probes
	out["uniqueIPs24h"] = uniq
	out["bans24h"] = bans24
	out["activeBans"] = active
	type row struct {
		RemoteIP string
		Cnt      int64
	}
	var top []row
	s.DB.Model(&models.HoneypotProbe{}).Select("remote_ip, count(*) as cnt").
		Where("created_at >= ?", since).Group("remote_ip").Order("cnt desc").Limit(10).Scan(&top)
	topOut := make([]map[string]any, 0, len(top))
	for _, t := range top {
		topOut = append(topOut, map[string]any{"ip": t.RemoteIP, "probes": t.Cnt})
	}
	out["topIPs"] = topOut
	live := s.Stats()
	out["live"] = live
	if sendTelegram {
		var b strings.Builder
		b.WriteString("🛡 <b>Honeypot Daily Report</b>\n")
		b.WriteString(fmt.Sprintf("Probes 24h: <b>%d</b>\n", probes))
		b.WriteString(fmt.Sprintf("Unique IPs 24h: <b>%d</b>\n", uniq))
		b.WriteString(fmt.Sprintf("New bans 24h: <b>%d</b>\n", bans24))
		b.WriteString(fmt.Sprintf("Active bans: <b>%d</b>\n", active))
		if len(top) > 0 {
			b.WriteString("Top scanners:\n")
			for i, t := range top {
				if i >= 5 {
					break
				}
				b.WriteString(fmt.Sprintf("• <code>%s</code> (%d)\n", t.RemoteIP, t.Cnt))
			}
		}
		_ = notify.Telegram(b.String())
	}
	return out
}

func (s *Server) postGlobalList(urlStr, ip, reason string) {
	body := fmt.Sprintf(`{"ip":%q,"reason":%q,"source":"electruvpn-honeypot","ts":%d}`, ip, reason, time.Now().Unix())
	req, err := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ElectruVPN-Honeypot/1.0")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func (s *Server) maybeNotify(ip, mode string, count int64, banned bool) {
	s.alertMu.Lock()
	last, ok := s.lastAlert[ip]
	if ok && time.Since(last) < 10*time.Minute {
		s.alertMu.Unlock()
		return
	}
	s.lastAlert[ip] = time.Now()
	if len(s.lastAlert) > 500 {
		for k, t := range s.lastAlert {
			if time.Since(t) > time.Hour {
				delete(s.lastAlert, k)
			}
		}
	}
	s.alertMu.Unlock()

	msg := fmt.Sprintf(
		"🛡 <b>Honeypot Alert</b>\nIP: <code>%s</code>\nMode: %s\nProbes (window): %d\nBanned: %v\nTime: %s",
		ip, mode, count, banned, time.Now().Format(time.RFC3339),
	)
	_ = notify.Telegram(msg)
}

func remotePort(c net.Conn) int {
	_, p, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

func (s *Server) record(ip string, port int, r, w, dur int64, mode, ua string, banned bool) {
	if s.DB == nil {
		return
	}
	if len(ua) > 120 {
		ua = ua[:120]
	}
	_ = s.DB.Create(&models.HoneypotProbe{
		RemoteIP: ip, RemotePort: port, BytesRead: r, BytesWritten: w,
		DurationMs: dur, Mode: mode, UserAgentHint: ua, Banned: banned, CreatedAt: time.Now(),
	}).Error
	var cnt int64
	s.DB.Model(&models.HoneypotProbe{}).Count(&cnt)
	if cnt > 5000 {
		var oldest []models.HoneypotProbe
		s.DB.Order("id asc").Limit(int(cnt - 5000)).Find(&oldest)
		ids := make([]uint, 0, len(oldest))
		for _, r := range oldest {
			ids = append(ids, r.ID)
		}
		if len(ids) > 0 {
			s.DB.Where("id IN ?", ids).Delete(&models.HoneypotProbe{})
		}
	}
}

func looksHTTP(b []byte) bool {
	s := string(b)
	return strings.HasPrefix(s, "GET ") || strings.HasPrefix(s, "POST ") ||
		strings.HasPrefix(s, "HEAD ") || strings.HasPrefix(s, "OPTIONS ") ||
		strings.HasPrefix(s, "PUT ") || strings.HasPrefix(s, "CONNECT ")
}

func looksLoginPath(b []byte) bool {
	low := strings.ToLower(string(b))
	return strings.Contains(low, "/login") || strings.Contains(low, "/admin") ||
		strings.Contains(low, "/panel") || strings.Contains(low, "/dashboard") ||
		strings.Contains(low, "/wp-admin") || strings.Contains(low, "/manager")
}

func looksTLS(b []byte) bool {
	return len(b) >= 3 && b[0] == 0x16 && b[1] == 0x03
}

func looksSSH(b []byte) bool {
	return len(b) >= 4 && strings.HasPrefix(string(b), "SSH-")
}

func extractUA(b []byte) string {
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ln)), "user-agent:") {
			return strings.TrimSpace(ln[strings.Index(ln, ":")+1:])
		}
	}
	return ""
}

func (s *Server) writeFakeLogin(conn net.Conn, cfg runtimeCfg, hold time.Duration, req []byte) int64 {
	html := `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Admin Login — Panel</title>
<style>
body{font-family:system-ui,-apple-system,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.card{background:#1e293b;border-radius:12px;padding:2rem;width:100%;max-width:380px;box-shadow:0 25px 50px -12px rgba(0,0,0,.5)}
h1{font-size:1.25rem;margin:0 0 1.5rem;text-align:center}
label{display:block;font-size:.875rem;margin-bottom:.25rem;color:#94a3b8}
input{width:100%;padding:.6rem .75rem;border:1px solid #334155;border-radius:8px;background:#0f172a;color:#f1f5f9;margin-bottom:1rem;box-sizing:border-box}
button{width:100%;padding:.7rem;border:none;border-radius:8px;background:#3b82f6;color:#fff;font-weight:600;cursor:pointer}
button:hover{background:#2563eb}
.err{color:#f87171;font-size:.8rem;text-align:center;margin-top:.75rem;display:none}
</style></head><body>
<div class="card"><h1>🔐 Panel Administration</h1>
<form method="POST" action="/login" id="f">
<label>Username</label><input name="username" autocomplete="username" required>
<label>Password</label><input name="password" type="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
<p class="err" id="e">Invalid credentials. Please try again.</p>
</form></div>
<script>
document.getElementById('f').onsubmit=function(e){e.preventDefault();document.getElementById('e').style.display='block';setTimeout(function(){location.reload()},1800);};
</script></body></html>`

	headers := "HTTP/1.1 200 OK\r\n" +
		"Server: nginx/1.24.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Length: " + strconv.Itoa(len(html)) + "\r\n" +
		"Connection: keep-alive\r\n" +
		"X-Frame-Options: DENY\r\n\r\n"
	var total int64
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, _ := io.WriteString(conn, headers+html)
	total += int64(n)
	s.bytesOut.Add(int64(n))

	if strings.HasPrefix(string(req), "POST") {
		_ = conn.SetReadDeadline(time.Now().Add(hold))
		body := make([]byte, 4096)
		_, _ = conn.Read(body)
	}

	end := time.Now().Add(hold)
	for time.Now().Before(end) {
		time.Sleep(time.Duration(200+cfg.Aggression*30) * time.Millisecond)
		chunk := make([]byte, 8)
		_, _ = rand.Read(chunk)
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		wn, err := conn.Write(chunk)
		total += int64(wn)
		if err != nil {
			break
		}
	}
	return total
}

func (s *Server) writeSSHTarpit(conn net.Conn, cfg runtimeCfg, hold time.Duration) int64 {
	banner := "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13\r\n"
	var total int64
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	n, _ := io.WriteString(conn, banner)
	total += int64(n)
	end := time.Now().Add(hold)
	for time.Now().Before(end) {
		time.Sleep(time.Duration(300+cfg.Aggression*80) * time.Millisecond)
		blob := make([]byte, 64+cfg.Aggression*16)
		_, _ = rand.Read(blob)
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		wn, err := conn.Write(blob)
		total += int64(wn)
		s.bytesOut.Add(int64(wn))
		if err != nil {
			break
		}
	}
	return total
}

func (s *Server) writeSlowHTTP(conn net.Conn, cfg runtimeCfg, hold time.Duration) int64 {
	headers := "HTTP/1.1 200 OK\r\n" +
		"Server: nginx/1.24.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Connection: keep-alive\r\n\r\n"
	var total int64
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, _ := io.WriteString(conn, headers)
	total += int64(n)
	end := time.Now().Add(hold)
	chunkSize := 64 + cfg.Aggression*32
	for time.Now().Before(end) {
		payload := make([]byte, chunkSize)
		_, _ = rand.Read(payload)
		for i := range payload {
			payload[i] = byte(0x20 + int(payload[i])%95)
		}
		block := fmt.Sprintf("%x\r\n%s\r\n", len(payload), payload)
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		wn, err := io.WriteString(conn, block)
		total += int64(wn)
		s.bytesOut.Add(int64(wn))
		if err != nil {
			break
		}
		time.Sleep(time.Duration(80+cfg.Aggression*40) * time.Millisecond)
	}
	return total
}

func (s *Server) writeSlowTLSJunk(conn net.Conn, cfg runtimeCfg, hold time.Duration, clientHello []byte) int64 {
	_ = clientHello
	var total int64
	end := time.Now().Add(hold)
	for time.Now().Before(end) {
		bodyLen := 32 + cfg.Aggression*24
		body := make([]byte, bodyLen)
		_, _ = rand.Read(body)
		rec := make([]byte, 5+bodyLen)
		rec[0] = 0x16
		rec[1] = 0x03
		rec[2] = 0x03
		rec[3] = byte(bodyLen >> 8)
		rec[4] = byte(bodyLen)
		copy(rec[5:], body)
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		wn, err := conn.Write(rec)
		total += int64(wn)
		s.bytesOut.Add(int64(wn))
		if err != nil {
			break
		}
		time.Sleep(time.Duration(60+cfg.Aggression*50) * time.Millisecond)
	}
	return total
}

func (s *Server) writeSlowJunk(conn net.Conn, cfg runtimeCfg, hold time.Duration) int64 {
	var total int64
	end := time.Now().Add(hold)
	for time.Now().Before(end) {
		n := 48 + cfg.Aggression*16
		buf := make([]byte, n)
		_, _ = rand.Read(buf)
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		wn, err := conn.Write(buf)
		total += int64(wn)
		s.bytesOut.Add(int64(wn))
		if err != nil {
			break
		}
		time.Sleep(time.Duration(50+cfg.Aggression*30) * time.Millisecond)
	}
	return total
}

func (s *Server) Stats() map[string]any {
	s.mu.Lock()
	ips := len(s.perIP)
	extras := len(s.extraLns)
	s.mu.Unlock()
	return map[string]any{
		"active": atomic.LoadInt32(&s.active), "uniqueIPs": ips,
		"totalProbes": s.total.Load(), "bytesWritten": s.bytesOut.Load(),
		"banned": s.banned.Load(), "listen": s.Addr(), "extraPorts": extras,
	}
}

func EnsureSettings(db *gorm.DB) {
	if db == nil {
		return
	}
	defaults := map[string]string{
		models.HoneypotEnabledKey: "1", models.HoneypotListenKey: "127.0.0.1:19601",
		models.HoneypotExtraPortsKey: "", models.HoneypotMaxConnKey: "256",
		models.HoneypotPerIPKey: "6", models.HoneypotMinHoldKey: "45",
		models.HoneypotMaxHoldKey: "180", models.HoneypotAggressionKey: "8",
		models.HoneypotAutoInjectKey: "1", models.HoneypotAutoBanKey: "1",
		models.HoneypotBanThresholdKey: "3", models.HoneypotBanWindowKey: "30",
		models.HoneypotBanDurationKey: "24", models.HoneypotTelegramAlertKey: "1",
		models.HoneypotFakeLoginKey: "1", models.HoneypotGlobalListKey: "",
	}
	for k, v := range defaults {
		var n int64
		db.Model(&models.PanelSetting{}).Where("key = ?", k).Count(&n)
		if n == 0 {
			_ = db.Create(&models.PanelSetting{Key: k, Value: v}).Error
		}
	}
}

func InjectFallbacks(db *gorm.DB, existing []map[string]any) []map[string]any {
	if db == nil {
		return existing
	}
	var en models.PanelSetting
	if db.Where("key = ?", models.HoneypotEnabledKey).First(&en).Error == nil && en.Value == "0" {
		return existing
	}
	var inj models.PanelSetting
	if db.Where("key = ?", models.HoneypotAutoInjectKey).First(&inj).Error == nil && inj.Value == "0" {
		return existing
	}
	dest := "127.0.0.1:19601"
	if db.Where("key = ?", models.HoneypotListenKey).First(&en).Error == nil && strings.TrimSpace(en.Value) != "" {
		dest = strings.TrimSpace(en.Value)
	}
	for _, f := range existing {
		if d, ok := f["dest"].(string); ok && d == dest {
			return existing
		}
	}
	out := make([]map[string]any, 0, len(existing)+1)
	out = append(out, map[string]any{"dest": dest})
	out = append(out, existing...)
	return out
}

var _ = tls.VersionTLS12
