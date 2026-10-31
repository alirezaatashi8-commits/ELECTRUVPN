// ElectruVPN Node Agent: a small, allowlisted agent for node health and jobs.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	netstat "github.com/shirou/gopsutil/v4/net"
)

type job struct {
	ID         uint   `json:"id"`
	Kind       string `json:"kind"`
	Payload    string `json:"payload"`
	LeaseToken string `json:"leaseToken"`
}

func post(client *http.Client, url, token string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, out.String())
	}
	return out.Bytes(), nil
}
func get(client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", res.StatusCode)
	}
	return b.Bytes(), nil
}
func main() {
	server := flag.String("server", os.Getenv("ELECTRUVPN_AGENT_SERVER"), "Panel agent endpoint, e.g. https://panel.example.com/api/agent")
	token := flag.String("token", os.Getenv("ELECTRUVPN_AGENT_TOKEN"), "agent token")
	interval := flag.Duration("interval", 10*time.Second, "heartbeat interval")
	flag.Parse()
	if *server == "" || *token == "" {
		fmt.Println("usage: electruvpn-agent -server URL -token TOKEN")
		os.Exit(2)
	}
	base := strings.TrimRight(*server, "/")
	// Shared transport reuses TCP/TLS connections across heartbeats under load.
	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        8,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}
	agentID := loadAgentID()
	xrayBin := envOr("ELECTRU_XRAY_BIN", "/usr/local/bin/xray")
	xrayAPI := envOr("ELECTRU_XRAY_API_PORT", "10085")
	backoff := 2 * time.Second
	for {
		if err := heartbeat(client, base, *token, agentID); err != nil {
			// Network / filter outage: exponential backoff, hard-capped at 60s.
			// No extra goroutines — single loop prevents leak under long outages.
			time.Sleep(backoff)
			if backoff < 60*time.Second {
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			}
			continue
		}
		backoff = 2 * time.Second
		// Traffic report is best-effort; failures must not delay poll/jobs.
		_ = reportTraffic(client, base, *token, xrayBin, xrayAPI)
		_ = poll(client, base, *token)
		time.Sleep(*interval)
	}
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// reportTraffic optionally shells out to local xray statsquery and posts a
// compact batch to the Master. Failures are silent so a missing local xray
// does not break the control loop on pure edge nodes.
func reportTraffic(client *http.Client, base, token, xrayBin, apiPort string) error {
	if !commandExists(xrayBin) && !commandExists("xray") {
		return nil
	}
	bin := xrayBin
	if !commandExists(bin) {
		bin = "xray"
	}
	cmd := exec.Command(bin, "api", "statsquery", "-s", "127.0.0.1:"+apiPort)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var parsed struct {
		Stat []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"stat"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		return nil
	}
	// Aggregate user>>>email>>>traffic>>>uplink|downlink into samples map.
	type pair struct{ up, down int64 }
	acc := map[string]*pair{}
	for _, s := range parsed.Stat {
		// user>>>email>>>traffic>>>uplink
		parts := strings.Split(s.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		email := parts[1]
		p := acc[email]
		if p == nil {
			p = &pair{}
			acc[email] = p
		}
		var v int64
		fmt.Sscan(s.Value, &v)
		if parts[3] == "uplink" {
			p.up = v
		} else if parts[3] == "downlink" {
			p.down = v
		}
	}
	if len(acc) == 0 {
		return nil
	}
	samples := make(map[string][]int64, len(acc))
	for email, p := range acc {
		samples[email] = []int64{p.up, p.down}
	}
	// Retry with short backoff — never block the agent loop more than ~ a few seconds.
	var last error
	backoff := 200 * time.Millisecond
	for attempt := 1; attempt <= 3; attempt++ {
		var response []byte
		response, last = post(client, base+"/traffic", token, map[string]any{"samples": samples})
		if last == nil {
			last = validateTrafficResponse(response)
		}
		if last == nil {
			return nil
		}
		if attempt < 3 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return last
}

// validateTrafficResponse treats HTTP 200 as insufficient when the Master
// reports dropped/rejected samples. Re-sending absolute counters is safe for
// the current max-counter coalescing path, while a nonzero rejected count is
// surfaced as an error so it cannot be mistaken for a complete report.
func validateTrafficResponse(body []byte) error {
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			Dropped  int `json:"dropped"`
			Rejected int `json:"rejected"`
		} `json:"obj"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("invalid traffic response: %w", err)
	}
	if !response.Success {
		if response.Msg == "" {
			response.Msg = "master rejected traffic report"
		}
		return fmt.Errorf("traffic report failed: %s", response.Msg)
	}
	if response.Obj.Rejected > 0 {
		return fmt.Errorf("master rejected %d traffic sample(s)", response.Obj.Rejected)
	}
	if response.Obj.Dropped > 0 {
		return fmt.Errorf("master dropped %d traffic sample(s)", response.Obj.Dropped)
	}
	return nil
}

func loadAgentID() string {
	if v := strings.TrimSpace(os.Getenv("ELECTRUVPN_AGENT_ID")); v != "" {
		return v
	}
	const path = "/etc/electruvpn/agent-id"
	if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	}
	id := fmt.Sprintf("%x", b)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err == nil {
		_ = os.WriteFile(path, []byte(id+"\n"), 0600)
	}
	return id
}

func measureMasterRTT(base string) float64 {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return 0
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 3*time.Second)
	if err != nil {
		return 0
	}
	_ = conn.Close()
	return float64(time.Since(start).Microseconds()) / 1000
}

func commandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }

func heartbeat(client *http.Client, base, token, agentID string) error {
	cp := 0.0
	if v, err := cpu.Percent(0, false); err == nil && len(v) > 0 {
		cp = v[0]
	}
	var memoryBytes, memoryTotalBytes, diskBytes, diskTotalBytes uint64
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		memoryBytes, memoryTotalBytes = vm.Used, vm.Total
	}
	if du, err := disk.Usage("/"); err == nil && du != nil {
		diskBytes, diskTotalBytes = du.Used, du.Total
	}
	io, _ := netstat.IOCounters(false)
	var rx, tx uint64
	if len(io) > 0 {
		rx, tx = io[0].BytesRecv, io[0].BytesSent
	}
	osName := runtime.GOOS
	if info, err := host.Info(); err == nil && info != nil && strings.TrimSpace(info.OS) != "" {
		osName = info.OS
	}
	hostname, _ := os.Hostname()
	rttMs := measureMasterRTT(base)
	_, err := post(client, base+"/heartbeat", token, map[string]any{"version": "1.4.0", "os": osName, "arch": runtime.GOARCH, "hostname": hostname, "agentId": agentID, "cpuPercent": cp, "memoryBytes": memoryBytes, "memoryTotalBytes": memoryTotalBytes, "diskBytes": diskBytes, "diskTotalBytes": diskTotalBytes, "rxBytes": rx, "txBytes": tx, "rttMs": rttMs, "capabilities": map[string]any{"diagnostics": true, "xray": commandExists("xray"), "sing-box": commandExists("sing-box"), "bbr": commandExists("sysctl"), "network": commandExists("ip"), "configRollback": true, "trafficReport": true}})
	return err
}
func poll(client *http.Client, base, token string) error {
	b, err := get(client, base+"/poll", token)
	if err != nil {
		return err
	}
	var env struct {
		Obj []job `json:"obj"`
	}
	if json.Unmarshal(b, &env) != nil {
		return nil
	}
	for _, j := range env.Obj {
		if len(j.Payload) > 5*1024*1024 {
			_, _ = post(client, fmt.Sprintf("%s/job/%d/result", base, j.ID), token, map[string]any{"leaseToken": j.LeaseToken, "status": "failed", "progress": 100, "result": "", "error": "job payload exceeds agent limit"})
			continue
		}
		status, result, e := execute(j)
		payload := map[string]any{"leaseToken": j.LeaseToken, "status": status, "progress": 100, "result": result, "error": ""}
		if e != nil {
			payload["status"] = "failed"
			payload["error"] = e.Error()
		}
		_, _ = post(client, fmt.Sprintf("%s/job/%d/result", base, j.ID), token, payload)
	}
	return nil
}
func pruneConfigHistory(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	for i := 0; i < len(entries)-keep; i++ {
		_ = os.Remove(filepath.Join(dir, entries[i].Name()))
	}
}

func latestConfigSnapshot(historyDir string) ([]byte, bool) {
	entries, err := os.ReadDir(historyDir)
	if err != nil || len(entries) == 0 {
		return nil, false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsDir() || !strings.HasSuffix(entries[i].Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(historyDir, entries[i].Name()))
		if err == nil && len(b) > 0 {
			return b, true
		}
	}
	return nil, false
}

func restoreLatestConfig(historyDir, configPath string) bool {
	b, ok := latestConfigSnapshot(historyDir)
	if !ok {
		return false
	}
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		return false
	}
	return true
}

func execute(j job) (string, string, error) {
	var args []string
	switch strings.ToLower(j.Kind) {
	case "xray-restart":
		args = []string{"restart", "xray"}
	case "singbox-restart":
		args = []string{"restart", "sing-box"}
	case "xray-status":
		args = []string{"is-active", "xray"}
	case "singbox-status":
		args = []string{"is-active", "sing-box"}
	case "bbr-apply":
		if out, err := exec.Command("modprobe", "tcp_bbr").CombinedOutput(); err != nil {
			return "failed", string(out), err
		}
		cmds := [][]string{{"sysctl", "-w", "net.core.default_qdisc=fq"}, {"sysctl", "-w", "net.ipv4.tcp_congestion_control=bbr"}, {"sysctl", "-w", "net.ipv4.tcp_mtu_probing=1"}}
		var all strings.Builder
		for _, a := range cmds {
			out, err := exec.Command(a[0], a[1:]...).CombinedOutput()
			all.Write(out)
			if err != nil {
				return "failed", all.String(), err
			}
		}
		return "done", strings.TrimSpace(all.String()), nil
	case "bbr-status":
		out, err := exec.Command("bash", "-lc", "printf 'kernel=%s\\ncc=%s\\nqdisc=%s\\navailable=%s\\n' \"$(uname -r)\" \"$(sysctl -n net.ipv4.tcp_congestion_control)\" \"$(sysctl -n net.core.default_qdisc)\" \"$(sysctl -n net.ipv4.tcp_available_congestion_control)\"").CombinedOutput()
		if err != nil {
			return "failed", string(out), err
		}
		return "done", strings.TrimSpace(string(out)), nil
	case "agent-diagnostics":
		checks := []map[string]any{}
		check := func(name string, args ...string) {
			if len(args) == 0 {
				return
			}
			out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
			checks = append(checks, map[string]any{"name": name, "ok": err == nil, "output": strings.TrimSpace(string(out))})
		}
		check("xray", "systemctl", "is-active", "xray")
		check("sing-box", "systemctl", "is-active", "sing-box")
		check("disk", "df", "-h", "/")
		check("memory", "free", "-h")
		check("network", "ip", "route")
		check("bbr", "sysctl", "-n", "net.ipv4.tcp_congestion_control")
		b, _ := json.Marshal(map[string]any{"agentVersion": "1.3.0", "go": runtime.Version(), "capabilities": []string{"diagnostics", "xray", "sing-box", "bbr", "network", "config-rollback"}, "checks": checks})
		return "done", string(b), nil
	case "network-status":
		out, err := exec.Command("bash", "-lc", "printf 'kernel=%s\ncc=%s\nqdisc=%s\navailable=%s\n' \"$(uname -r)\" \"$(sysctl -n net.ipv4.tcp_congestion_control)\" \"$(sysctl -n net.core.default_qdisc)\" \"$(sysctl -n net.ipv4.tcp_available_congestion_control)\"").CombinedOutput()
		if err != nil {
			return "failed", string(out), err
		}
		return "done", strings.TrimSpace(string(out)), nil
	case "config-validate":
		var p struct{ Config, Engine string }
		if json.Unmarshal([]byte(j.Payload), &p) != nil || strings.TrimSpace(p.Config) == "" {
			return "failed", "config payload required", fmt.Errorf("invalid config payload")
		}
		configPath := strings.TrimSpace(p.Config)
		cleanConfig := filepath.Clean(configPath)
		if cleanConfig != "/etc/electruvpn/xray-config.json" && cleanConfig != "/etc/electruvpn/sing-box.json" {
			return "failed", "config path is not an ElectruVPN-managed config", fmt.Errorf("config path not allowlisted")
		}
		if p.Engine == "singbox" {
			out, err := exec.Command("/usr/local/bin/sing-box", "check", "-c", cleanConfig).CombinedOutput()
			if err != nil {
				return "failed", string(out), err
			}
			return "done", strings.TrimSpace(string(out)), nil
		}
		out, err := exec.Command("/usr/local/bin/xray", "run", "-test", "-config", cleanConfig).CombinedOutput()
		if err != nil {
			return "failed", string(out), err
		}
		return "done", strings.TrimSpace(string(out)), nil
	case "xray-config-apply", "singbox-config-apply":
		var p struct{ Engine, Content, SHA256 string }
		if json.Unmarshal([]byte(j.Payload), &p) != nil || strings.TrimSpace(p.Content) == "" {
			return "failed", "invalid config payload", fmt.Errorf("invalid config payload")
		}
		engine := strings.ToLower(strings.TrimSpace(p.Engine))
		var configPath, binary, service string
		switch engine {
		case "xray":
			configPath, binary, service = "/etc/electruvpn/xray-config.json", "/usr/local/bin/xray", "xray"
		case "singbox":
			configPath, binary, service = "/etc/electruvpn/sing-box.json", "/usr/local/bin/sing-box", "sing-box"
		default:
			return "failed", "unsupported engine", fmt.Errorf("unsupported engine")
		}
		sum := sha256.Sum256([]byte(p.Content))
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, strings.TrimSpace(p.SHA256)) {
			return "failed", "checksum mismatch", fmt.Errorf("checksum mismatch")
		}
		tmpDir, err := os.MkdirTemp("/tmp", "electruvpn-agent-config-")
		if err != nil {
			return "failed", "temp dir failed", err
		}
		defer os.RemoveAll(tmpDir)
		tmp := filepath.Join(tmpDir, filepath.Base(configPath))
		if err := os.WriteFile(tmp, []byte(p.Content), 0600); err != nil {
			return "failed", "write temp config failed", err
		}
		var check *exec.Cmd
		if engine == "singbox" {
			check = exec.Command(binary, "check", "-c", tmp)
		} else {
			check = exec.Command(binary, "run", "-test", "-config", tmp)
		}
		if out, err := check.CombinedOutput(); err != nil {
			return "failed", string(out), err
		}
		historyDir := filepath.Join("/var/lib/electruvpn-agent", "config-history", engine)
		if err := os.MkdirAll(historyDir, 0700); err != nil {
			return "failed", "could not create config history directory", err
		}
		if current, err := os.ReadFile(configPath); err == nil && len(current) > 0 {
			history := filepath.Join(historyDir, fmt.Sprintf("%d.json", time.Now().UTC().UnixNano()))
			if err := os.WriteFile(history, current, 0600); err != nil {
				return "failed", "could not save rollback snapshot", err
			}
			pruneConfigHistory(historyDir, 10)
		}
		if err := os.Rename(tmp, configPath); err != nil {
			if b, readErr := os.ReadFile(tmp); readErr == nil {
				err = os.WriteFile(configPath, b, 0600)
			}
			if err != nil {
				return "failed", "install config failed", err
			}
		}
		out, err := exec.Command("systemctl", "restart", service).CombinedOutput()
		if err != nil {
			if restored := restoreLatestConfig(historyDir, configPath); restored {
				_ = exec.Command("systemctl", "restart", service).Run()
			}
			return "failed", string(out), err
		}
		if out, err := exec.Command("systemctl", "is-active", "--quiet", service).CombinedOutput(); err != nil {
			if restored := restoreLatestConfig(historyDir, configPath); restored {
				_ = exec.Command("systemctl", "restart", service).Run()
			}
			return "failed", string(out), err
		}
		return "done", "configuration validated, applied and service is healthy", nil
	case "xray-config-rollback", "singbox-config-rollback":
		engine := strings.TrimSuffix(strings.TrimSuffix(j.Kind, "-rollback"), "-config")
		if engine != "xray" && engine != "singbox" {
			return "failed", "unsupported rollback engine", fmt.Errorf("unsupported rollback engine")
		}
		var configPath, binary, service string
		switch engine {
		case "xray":
			configPath, binary, service = "/etc/electruvpn/xray-config.json", "/usr/local/bin/xray", "xray"
		case "singbox":
			configPath, binary, service = "/etc/electruvpn/sing-box.json", "/usr/local/bin/sing-box", "sing-box"
		}
		historyDir := filepath.Join("/var/lib/electruvpn-agent", "config-history", engine)
		b, ok := latestConfigSnapshot(historyDir)
		if !ok {
			return "failed", "no rollback snapshot available", fmt.Errorf("rollback artifact unavailable")
		}
		tmpDir, err := os.MkdirTemp("/tmp", "electruvpn-agent-rollback-")
		if err != nil {
			return "failed", "temp dir failed", err
		}
		defer os.RemoveAll(tmpDir)
		tmp := filepath.Join(tmpDir, filepath.Base(configPath))
		if err := os.WriteFile(tmp, b, 0600); err != nil {
			return "failed", "write rollback temp failed", err
		}
		var check *exec.Cmd
		if engine == "singbox" {
			check = exec.Command(binary, "check", "-c", tmp)
		} else {
			check = exec.Command(binary, "run", "-test", "-config", tmp)
		}
		if out, err := check.CombinedOutput(); err != nil {
			return "failed", string(out), err
		}
		current, _ := os.ReadFile(configPath)
		if err := os.WriteFile(configPath, b, 0600); err != nil {
			return "failed", "install rollback config failed", err
		}
		out, err := exec.Command("systemctl", "restart", service).CombinedOutput()
		if err != nil {
			if len(current) > 0 {
				_ = os.WriteFile(configPath, current, 0600)
				_ = exec.Command("systemctl", "restart", service).Run()
			}
			return "failed", string(out), err
		}
		if out, err := exec.Command("systemctl", "is-active", "--quiet", service).CombinedOutput(); err != nil {
			if len(current) > 0 {
				_ = os.WriteFile(configPath, current, 0600)
				_ = exec.Command("systemctl", "restart", service).Run()
			}
			return "failed", string(out), err
		}
		return "done", "latest versioned configuration restored and service is healthy", nil
	default:
		return "failed", "", fmt.Errorf("job kind not allowlisted")
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return "done", strings.TrimSpace(string(out)), err
}
