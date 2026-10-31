package singbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Live monitoring for sing-box protocols (Hysteria2, TUIC, Naive, AnyTLS,
// ShadowTLS, WireGuard).
//
// sing-box has no StatsService in the standard release, but its Clash API is
// part of the official builds and lists every open connection with its
// inbound, source IP and upload/download byte counters. The panel enables that
// API on 127.0.0.1 only, polls /connections, and turns the per-connection
// counters into cumulative per-inbound and per-user counters so the same
// speed/online pipeline used for Xray can be reused.
//
// sing-box does not report the authenticated user in /connections. The user is
// recovered from its own log ("[user] inbound connection from IP:port") and
// matched to a connection by (inbound tag, source IP).

var liveAPI = struct {
	sync.RWMutex
	port    int
	secret  string
	enabled bool
}{}

// ConfigureLiveAPI sets the loopback port and shared secret used for the Clash
// API. A port <= 0 disables it. The secret is derived from a server secret so
// that it stays identical across panel restarts (sing-box may keep running).
func ConfigureLiveAPI(port int, seed string) {
	liveAPI.Lock()
	defer liveAPI.Unlock()
	liveAPI.port = port
	liveAPI.enabled = port > 0
	sum := sha256.Sum256([]byte("electruvpn-singbox-live|" + seed))
	liveAPI.secret = hex.EncodeToString(sum[:16])
}

// LiveAPIEnabled reports whether the Clash API block is added to configs.
func LiveAPIEnabled() bool {
	liveAPI.RLock()
	defer liveAPI.RUnlock()
	return liveAPI.enabled
}

// SetLiveAPIEnabled switches the Clash API block on or off without touching the
// configured port/secret.
func SetLiveAPIEnabled(on bool) {
	liveAPI.Lock()
	defer liveAPI.Unlock()
	liveAPI.enabled = on && liveAPI.port > 0
}

// DisableLiveAPI turns the Clash API off (used when the installed sing-box
// build rejects it so the rest of the configuration keeps working).
func DisableLiveAPI() {
	liveAPI.Lock()
	defer liveAPI.Unlock()
	liveAPI.enabled = false
}

func liveAPIInfo() (port int, secret string, ok bool) {
	liveAPI.RLock()
	defer liveAPI.RUnlock()
	return liveAPI.port, liveAPI.secret, liveAPI.enabled
}

// experimentalBlock returns the "experimental" section of the sing-box config.
func experimentalBlock() map[string]any {
	port, secret, ok := liveAPIInfo()
	if !ok {
		return nil
	}
	return map[string]any{"clash_api": map[string]any{
		"external_controller": fmt.Sprintf("127.0.0.1:%d", port),
		"secret":              secret,
	}}
}

// ---------------------------------------------------------------------------
// log -> user mapping

type srcKey struct{ tag, ip string }

type userSeen struct {
	user string
	at   time.Time
}

const userMapTTL = 6 * time.Hour

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Example: "INFO [123 0ms] inbound/hysteria2[hy2-in]: [alice] inbound connection from 1.2.3.4:5555"
var connLogRe = regexp.MustCompile(`inbound/[A-Za-z0-9_-]+\[([^\]]+)\]: (?:\[([^\]]+)\] )?inbound (?:packet )?connection from (\S+)`)

var liveState = struct {
	sync.Mutex
	users    map[srcKey]userSeen
	prev     map[string]connPrev
	started  bool
	counters map[string]*cumulative
}{
	users:    make(map[srcKey]userSeen),
	prev:     make(map[string]connPrev),
	counters: make(map[string]*cumulative),
}

type connPrev struct{ up, down int64 }

type cumulative struct{ up, down int64 }

// ObserveLogLine learns which user owns which (inbound, source IP). Called for
// every line the sing-box process prints.
func ObserveLogLine(line string, now time.Time) {
	if !strings.Contains(line, "inbound connection from") && !strings.Contains(line, "inbound packet connection from") {
		return
	}
	line = ansiRe.ReplaceAllString(line, "")
	m := connLogRe.FindStringSubmatch(line)
	if m == nil || strings.TrimSpace(m[2]) == "" {
		return
	}
	host := hostOnly(m[3])
	if host == "" {
		return
	}
	liveState.Lock()
	defer liveState.Unlock()
	liveState.users[srcKey{tag: m[1], ip: host}] = userSeen{user: strings.TrimSpace(m[2]), at: now}
	if len(liveState.users) > 20000 {
		for k, v := range liveState.users {
			if now.Sub(v.at) > userMapTTL {
				delete(liveState.users, k)
			}
		}
	}
}

func hostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(addr, "[]")
}

// ---------------------------------------------------------------------------
// /connections polling

// Subject is the live state of one inbound or one user.
type Subject struct {
	Up, Down int64 // cumulative bytes since the panel started observing
	Conns    int   // currently open connections
	IPs      []string
}

// InboundKey is the key used in the PollLive result for an inbound tag.
func InboundKey(tag string) string { return "inbound:" + tag }

type clashConn struct {
	ID       string `json:"id"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Metadata struct {
		Type     string `json:"type"`
		SourceIP string `json:"sourceIP"`
		Inbound  string `json:"inbound"`
		User     string `json:"user"`
	} `json:"metadata"`
}

var liveClient = &http.Client{Timeout: 2500 * time.Millisecond}

// PollLive reads the connection list once and returns the cumulative counters
// and current connection/device information for every inbound ("inbound:<tag>")
// and every user that could be identified.
func PollLive(now time.Time) (map[string]Subject, error) {
	port, secret, ok := liveAPIInfo()
	if !ok {
		return nil, fmt.Errorf("sing-box live API disabled")
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/connections", port), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := liveClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sing-box clash api status %d", resp.StatusCode)
	}
	var payload struct {
		Connections []clashConn `json:"connections"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	liveState.Lock()
	defer liveState.Unlock()

	cur := make(map[string]connPrev, len(payload.Connections))
	conns := make(map[string]int)
	ips := make(map[string]map[string]struct{})
	touch := func(key, ip string) {
		conns[key]++
		if ip != "" {
			if ips[key] == nil {
				ips[key] = make(map[string]struct{}, 2)
			}
			ips[key][ip] = struct{}{}
		}
	}
	add := func(key string, du, dd int64) {
		c := liveState.counters[key]
		if c == nil {
			c = &cumulative{}
			liveState.counters[key] = c
		}
		c.up += du
		c.down += dd
	}
	for _, c := range payload.Connections {
		tag := inboundTag(c)
		if tag == "" {
			continue
		}
		prev, seen := liveState.prev[c.ID]
		var du, dd int64
		switch {
		case seen:
			du, dd = c.Upload-prev.up, c.Download-prev.down
		case liveState.started:
			// Connection opened since the previous poll: everything it moved
			// so far happened inside this interval.
			du, dd = c.Upload, c.Download
		}
		if du < 0 {
			du = 0
		}
		if dd < 0 {
			dd = 0
		}
		cur[c.ID] = connPrev{up: c.Upload, down: c.Download}

		ip := strings.Trim(c.Metadata.SourceIP, "[]")
		user := strings.TrimSpace(c.Metadata.User)
		if user == "" {
			if u, ok := liveState.users[srcKey{tag: tag, ip: ip}]; ok {
				user = u.user
			}
		}
		ikey := InboundKey(tag)
		touch(ikey, ip)
		add(ikey, du, dd)
		if user != "" {
			touch(user, ip)
			add(user, du, dd)
		}
	}
	liveState.prev = cur
	liveState.started = true

	out := make(map[string]Subject, len(liveState.counters))
	for key, c := range liveState.counters {
		s := Subject{Up: c.up, Down: c.down, Conns: conns[key]}
		for ip := range ips[key] {
			s.IPs = append(s.IPs, ip)
		}
		out[key] = s
	}
	return out, nil
}

// inboundTag extracts the inbound tag of a connection. Newer sing-box versions
// put it in metadata.inbound; older ones only in metadata.type as
// "<type>/<tag>".
func inboundTag(c clashConn) string {
	if t := strings.TrimSpace(c.Metadata.Inbound); t != "" {
		return t
	}
	if i := strings.Index(c.Metadata.Type, "/"); i >= 0 {
		return strings.TrimSpace(c.Metadata.Type[i+1:])
	}
	return ""
}

// ResetLive forgets all live state (sing-box restarted, counters are new).
func ResetLive() {
	liveState.Lock()
	defer liveState.Unlock()
	liveState.prev = make(map[string]connPrev)
	liveState.started = false
}
