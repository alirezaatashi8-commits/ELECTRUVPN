package singbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Manager struct {
	mu                     sync.Mutex
	binaryPath, configPath string
	cmd                    *exec.Cmd
	waitCh                 chan struct{}
	startedAt              time.Time
	logs                   []string
}

func NewManager(b, c string) *Manager { return &Manager{binaryPath: b, configPath: c} }
func (m *Manager) Installed() bool    { _, e := os.Stat(m.binaryPath); return e == nil }
func (m *Manager) WriteConfig(cfg any) error {
	b, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return e
	}
	return writeAtomic(m.configPath, b, 0640)
}
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.cmd != nil && m.cmd.Process != nil {
		pid := m.cmd.Process.Pid
		m.mu.Unlock()
		return fmt.Errorf("sing-box is already running (pid %d)", pid)
	}
	cmd := exec.Command(m.binaryPath, "run", "-c", m.configPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to start sing-box at %s: %w", m.binaryPath, err)
	}
	m.cmd = cmd
	m.waitCh = make(chan struct{})
	m.startedAt = time.Now()
	m.logs = nil
	waitCh := m.waitCh
	m.mu.Unlock()

	go m.pipe(stdout)
	go m.waitProcess(cmd, waitCh)
	select {
	case <-waitCh:
		return fmt.Errorf("sing-box exited during startup")
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

func (m *Manager) waitProcess(cmd *exec.Cmd, waitCh chan struct{}) {
	_ = cmd.Wait()
	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
	}
	if m.waitCh == waitCh {
		close(waitCh)
		m.waitCh = nil
	}
	m.mu.Unlock()
}

func (m *Manager) pipe(r interface{ Read([]byte) (int, error) }) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		ObserveLogLine(sc.Text(), time.Now())
		m.mu.Lock()
		m.logs = append(m.logs, sc.Text())
		if len(m.logs) > 500 {
			m.logs = m.logs[len(m.logs)-500:]
		}
		m.mu.Unlock()
	}
}
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cmd == nil || m.cmd.Process == nil {
		m.mu.Unlock()
		return fmt.Errorf("sing-box is not running")
	}
	cmd := m.cmd
	ch := m.waitCh
	e := cmd.Process.Signal(syscall.SIGTERM)
	m.mu.Unlock()
	if e != nil {
		return e
	}
	if ch != nil {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-ch:
			case <-time.After(2 * time.Second):
				return fmt.Errorf("timed out waiting for sing-box")
			}
		}
	}
	return nil
}
func (m *Manager) Restart() error {
	if e := m.Stop(); e != nil && !strings.Contains(e.Error(), "not running") {
		return e
	}
	return m.Start()
}

type Status struct {
	Running   bool      `json:"running"`
	Pid       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Installed bool      `json:"installed"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Installed: m.Installed()}
	if m.cmd != nil && m.cmd.Process != nil {
		st.Running = true
		st.Pid = m.cmd.Process.Pid
		st.StartedAt = m.startedAt
	}
	return st
}
func (m *Manager) RecentLogs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.logs...)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".electruvpn-singbox-*.tmp")
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
