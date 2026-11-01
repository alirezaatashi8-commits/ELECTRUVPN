package xray

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manager owns the running xray-core subprocess. ElectruVPN deliberately
// runs xray as a plain child process rather than importing xray-core as a
// library: it keeps this codebase small and means upgrading xray-core is
// just "replace the binary and restart", independent of the panel's own
// release cycle.
type Manager struct {
	mu         sync.Mutex
	binaryPath string
	configPath string
	cmd        *exec.Cmd
	waitCh     chan struct{}
	startedAt  time.Time
	logLines   []string
	maxLog     int
	lastError  string
}

func NewManager(binaryPath, configPath string) *Manager {
	return &Manager{binaryPath: binaryPath, configPath: configPath, maxLog: 500}
}

// WriteConfig serializes cfg to the configured path.
func (m *Manager) WriteConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(m.configPath, data, 0o640)
}

// Start launches xray-core if it isn't already running.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.cmd != nil && m.cmd.Process != nil {
		pid := m.cmd.Process.Pid
		m.mu.Unlock()
		return fmt.Errorf("xray is already running (pid %d)", pid)
	}

	cmd := exec.Command(m.binaryPath, "run", "-config", m.configPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to start xray-core at %s: %w", m.binaryPath, err)
	}

	m.cmd = cmd
	m.waitCh = make(chan struct{})
	m.startedAt = time.Now()
	m.logLines = nil
	m.lastError = ""
	waitCh := m.waitCh
	m.mu.Unlock()

	go m.pipeLogs(stdout)
	go m.waitProcess(cmd, waitCh)

	// Detect immediate runtime failures (port collision, bad file permissions,
	// missing certificate, etc.) instead of reporting a false successful apply.
	select {
	case <-waitCh:
		m.mu.Lock()
		errMsg := m.lastError
		m.mu.Unlock()
		if errMsg == "" {
			errMsg = "process exited during startup"
		}
		return fmt.Errorf("xray-core exited during startup: %s", errMsg)
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

func (m *Manager) waitProcess(cmd *exec.Cmd, waitCh chan struct{}) {
	err := cmd.Wait()
	m.mu.Lock()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		m.lastError = err.Error()
	}
	if m.cmd == cmd {
		m.cmd = nil
	}
	if m.waitCh == waitCh {
		close(waitCh)
		m.waitCh = nil
	}
	m.mu.Unlock()
}

func (m *Manager) pipeLogs(stdout interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		m.mu.Lock()
		m.logLines = append(m.logLines, line)
		if len(m.logLines) > m.maxLog {
			m.logLines = m.logLines[len(m.logLines)-m.maxLog:]
		}
		m.mu.Unlock()
	}
}

// Stop terminates the running xray-core process, if any.
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cmd == nil || m.cmd.Process == nil {
		m.mu.Unlock()
		return fmt.Errorf("xray is not running")
	}
	cmd := m.cmd
	waitCh := m.waitCh
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()

	// Wait until the old process has actually exited before a restart. This
	// prevents a fast restart from racing the old process while it still owns
	// listening sockets.
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-waitCh:
			case <-time.After(2 * time.Second):
				return fmt.Errorf("timed out waiting for xray to stop")
			}
		}
	}
	return nil
}

// Restart stops (if running) then starts xray-core again, e.g. after the
// config was regenerated because an inbound or client changed.
func (m *Manager) Restart() error {
	if err := m.Stop(); err != nil && !strings.Contains(err.Error(), "not running") {
		return err
	}
	return m.Start()
}

// Status reports whether xray is running and, if so, since when.
type Status struct {
	Running   bool      `json:"running"`
	Pid       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd == nil || m.cmd.Process == nil {
		return Status{Running: false}
	}
	return Status{Running: true, Pid: m.cmd.Process.Pid, StartedAt: m.startedAt}
}

// RecentLogs returns the last N captured log lines (most recent last).
func (m *Manager) RecentLogs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.logLines))
	copy(out, m.logLines)
	return out
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".electruvpn-xray-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
