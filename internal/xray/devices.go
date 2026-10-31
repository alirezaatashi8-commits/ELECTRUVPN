package xray

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Live device tracking.
//
// Xray's StatsService can list the source IPs that recently opened a
// connection for a user (policy statsUserOnline). Those IPs are kept here in
// memory only (never written to disk or the database) and exposed as a count
// of distinct IPs ("devices"). sing-box feeds the same tracker from its
// connection list, see internal/core/live.go.
const (
	// liveDeviceWindow is how long an IP keeps counting as a connected device
	// after it was last reported. Xray itself forgets an IP ~20s after the last
	// *new* connection, so the window is a little longer to bridge that gap.
	liveDeviceWindow = 45 * time.Second
	// liveConnTTL is how long a connection count stays valid.
	liveConnTTL = 15 * time.Second

	onlinePollMinGap   = 4 * time.Second
	onlinePollMaxUsers = 40
	onlinePollWorkers  = 6
	onlineCmdTimeout   = 2500 * time.Millisecond
)

type connCount struct {
	n  int
	at time.Time
}

var deviceTracker = struct {
	sync.RWMutex
	ips   map[string]map[string]time.Time // subject -> ip -> last seen
	conns map[string]connCount            // subject -> open connections
}{ips: make(map[string]map[string]time.Time), conns: make(map[string]connCount)}

// RecordDeviceIPs marks the given source IPs as seen "now" for a subject.
func RecordDeviceIPs(subject string, ips []string, now time.Time) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	deviceTracker.Lock()
	defer deviceTracker.Unlock()
	m := deviceTracker.ips[subject]
	if m == nil {
		m = make(map[string]time.Time, len(ips))
		deviceTracker.ips[subject] = m
	}
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip != "" {
			m[ip] = now
		}
	}
	for ip, at := range m {
		if now.Sub(at) > 5*liveDeviceWindow {
			delete(m, ip)
		}
	}
	if len(m) == 0 {
		delete(deviceTracker.ips, subject)
	}
}

// RecordConnections stores the number of currently open connections.
func RecordConnections(subject string, n int, now time.Time) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	deviceTracker.Lock()
	defer deviceTracker.Unlock()
	if n <= 0 {
		delete(deviceTracker.conns, subject)
		return
	}
	deviceTracker.conns[subject] = connCount{n: n, at: now}
}

// deviceStats returns the number of distinct live IPs and open connections
// across several subjects (a client and its profile aliases).
func deviceStats(subjects []string, now time.Time) (devices, conns int) {
	deviceTracker.RLock()
	defer deviceTracker.RUnlock()
	var seen map[string]struct{}
	for _, subject := range subjects {
		if cc, ok := deviceTracker.conns[subject]; ok && now.Sub(cc.at) <= liveConnTTL {
			conns += cc.n
		}
		for ip, at := range deviceTracker.ips[subject] {
			if now.Sub(at) > liveDeviceWindow {
				continue
			}
			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}
			seen[ip] = struct{}{}
		}
	}
	return len(seen), conns
}

// DeviceCount returns the number of distinct live device IPs across subjects.
// Used for inbound-level totals where one IP may appear under several users.
func DeviceCount(subjects ...string) int {
	d, _ := deviceStats(subjects, time.Now())
	return d
}

// ConnectionCount returns the summed open connection count across subjects.
func ConnectionCount(subjects ...string) int {
	_, c := deviceStats(subjects, time.Now())
	return c
}

// ---------------------------------------------------------------------------
// Xray online-IP polling

var onlineNameRe = regexp.MustCompile(`user>>>(.+?)>>>online`)

var onlinePoll = struct {
	sync.Mutex
	at         time.Time
	allMode    int       // 0 unknown, 1 works, -1 unsupported
	allRetryAt time.Time // when to retry an unsupported statsgetallonlineusers
	ipMode     int       // 0 unknown, 1 "-email" flag, 2 positional name, -1 unsupported
	ipRetryAt  time.Time
}{}

// PollOnlineIPs asks Xray which source IPs are online for the given users and
// records them in the device tracker. candidates are users believed to be
// active (recent traffic); users reported by statsgetallonlineusers are added.
// It is rate limited and silently does nothing when the installed Xray build
// has no online-IP API, so a failure never breaks live traffic stats.
func PollOnlineIPs(binaryPath string, apiPort int, candidates []string, now time.Time) {
	onlinePoll.Lock()
	if now.Sub(onlinePoll.at) < onlinePollMinGap {
		onlinePoll.Unlock()
		return
	}
	onlinePoll.at = now
	allMode := onlinePoll.allMode
	if allMode < 0 && now.After(onlinePoll.allRetryAt) {
		allMode = 0
	}
	ipMode := onlinePoll.ipMode
	if ipMode < 0 && now.After(onlinePoll.ipRetryAt) {
		ipMode = 0
	}
	onlinePoll.Unlock()
	if ipMode < 0 {
		return
	}

	server := "127.0.0.1:" + strconv.Itoa(apiPort)
	users := make([]string, 0, len(candidates)+8)
	seen := make(map[string]struct{}, len(candidates)+8)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		users = append(users, u)
	}
	for _, c := range candidates {
		add(c)
	}
	if allMode >= 0 {
		out, err := runXrayAPI(binaryPath, "statsgetallonlineusers", "-s", server)
		onlinePoll.Lock()
		if err != nil {
			onlinePoll.allMode = -1
			onlinePoll.allRetryAt = now.Add(10 * time.Minute)
		} else {
			onlinePoll.allMode = 1
			for _, m := range onlineNameRe.FindAllStringSubmatch(string(out), -1) {
				add(m[1])
			}
		}
		onlinePoll.Unlock()
	}
	if len(users) > onlinePollMaxUsers {
		users = users[:onlinePollMaxUsers]
	}
	if len(users) == 0 {
		return
	}

	type result struct {
		user string
		ips  []string
		ok   bool
		mode int
	}
	jobs := make(chan string)
	results := make(chan result, len(users))
	var wg sync.WaitGroup
	workers := onlinePollWorkers
	if len(users) < workers {
		workers = len(users)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range jobs {
				ips, mode, ok := queryOnlineIPs(binaryPath, server, u, ipMode)
				results <- result{user: u, ips: ips, ok: ok, mode: mode}
			}
		}()
	}
	for _, u := range users {
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	close(results)

	anyOK := false
	workedMode := 0
	for r := range results {
		if r.ok {
			anyOK = true
			workedMode = r.mode
			RecordDeviceIPs(r.user, r.ips, now)
		}
	}
	onlinePoll.Lock()
	if anyOK {
		onlinePoll.ipMode = workedMode
	} else if ipMode == 0 && len(users) >= 3 {
		// Three or more users and not a single successful answer: the command is
		// most likely missing in this Xray build. Back off instead of spawning
		// processes every few seconds.
		onlinePoll.ipMode = -1
		onlinePoll.ipRetryAt = now.Add(10 * time.Minute)
	}
	onlinePoll.Unlock()
}

// queryOnlineIPs runs statsonlineiplist for one user. Different Xray releases
// take the user either as -email or as the full stat name, so both forms are
// tried once and the one that works is remembered.
func queryOnlineIPs(binaryPath, server, user string, mode int) (ips []string, usedMode int, ok bool) {
	try := func(m int) ([]string, bool) {
		var out []byte
		var err error
		if m == 1 {
			out, err = runXrayAPI(binaryPath, "statsonlineiplist", "-s", server, "-email", user)
		} else {
			out, err = runXrayAPI(binaryPath, "statsonlineiplist", "-s", server, "user>>>"+user+">>>online")
		}
		if err != nil {
			return nil, false
		}
		return parseOnlineIPs(out), true
	}
	if mode == 1 || mode == 2 {
		got, good := try(mode)
		return got, mode, good
	}
	if got, good := try(1); good {
		return got, 1, true
	}
	got, good := try(2)
	return got, 2, good
}

func runXrayAPI(binaryPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), onlineCmdTimeout)
	defer cancel()
	full := append([]string{"api"}, args...)
	return exec.CommandContext(ctx, binaryPath, full...).Output()
}

// parseOnlineIPs extracts the IP keys of the "ips" object from the CLI JSON.
// The object may be nested depending on the Xray version, so it is searched
// recursively; a missing object means no device is online.
func parseOnlineIPs(data []byte) []string {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if strings.EqualFold(k, "ips") {
					if m, ok := val.(map[string]any); ok {
						for ip := range m {
							out = append(out, ip)
						}
						continue
					}
				}
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(root)
	return out
}
