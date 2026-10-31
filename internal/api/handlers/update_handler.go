package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/electruvpn/electruvpn/internal/version"
	"github.com/gin-gonic/gin"
)

const (
	githubRawVersionURL = "https://raw.githubusercontent.com/alirezaatashi8-commits/ELECTRUVPN/main/RELEASE_VERSION.txt"
	githubZipURL        = "https://github.com/alirezaatashi8-commits/ELECTRUVPN/archive/refs/heads/main.zip"
	updateStatusFile    = "/var/lib/electruvpn/panel-update-status"
)

type UpdateHandler struct{ S *core.Server }

func NewUpdateHandler(s *core.Server) *UpdateHandler { return &UpdateHandler{S: s} }

var updateMu sync.Mutex

func localPanelVersion() string {
	// Prefer installed release file, then CWD, then build constant.
	for _, p := range []string{
		"/opt/electruvpn/RELEASE_VERSION.txt",
		"RELEASE_VERSION.txt",
	} {
		if b, err := os.ReadFile(p); err == nil {
			v := strings.TrimSpace(string(b))
			if v != "" {
				return v
			}
		}
	}
	return version.Display
}

func fetchGitHubVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubRawVersionURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ELECTRUVPN-Panel-UpdateCheck/1.0")
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(body))
	if v == "" {
		return "", fmt.Errorf("empty version on github")
	}
	return v, nil
}

// CheckUpdate compares local RELEASE_VERSION.txt with GitHub main.
func (h *UpdateHandler) CheckUpdate(c *gin.Context) {
	local := localPanelVersion()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	remote, err := fetchGitHubVersion(ctx)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "msg": "اتصال به گیت‌هاب ناموفق: " + err.Error(), "obj": gin.H{
			"localVersion": local, "remoteVersion": "", "updateAvailable": false,
		}})
		return
	}
	avail := normalizeVer(local) != normalizeVer(remote)
	msg := "نسخه شما به‌روز است."
	if avail {
		msg = "نسخه جدید در گیت‌هاب موجود است."
	}
	c.JSON(200, gin.H{"success": true, "msg": msg, "obj": gin.H{
		"localVersion":    local,
		"remoteVersion":   remote,
		"updateAvailable": avail,
		"zipURL":          githubZipURL,
	}})
}

func normalizeVer(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

// ApplyUpdate starts electruvpn update from GitHub in the background.
// The HTTP response returns immediately; the service will restart during update.
func (h *UpdateHandler) ApplyUpdate(c *gin.Context) {
	if !updateMu.TryLock() {
		c.JSON(409, gin.H{"success": false, "msg": "یک بروزرسانی هم‌اکنون در حال اجراست"})
		return
	}
	// keep lock until process starts; child continues independently

	local := localPanelVersion()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	remote, err := fetchGitHubVersion(ctx)
	cancel()
	if err != nil {
		updateMu.Unlock()
		c.JSON(400, gin.H{"success": false, "msg": "بررسی گیت‌هاب ناموفق: " + err.Error()})
		return
	}
	if normalizeVer(local) == normalizeVer(remote) {
		updateMu.Unlock()
		c.JSON(200, gin.H{"success": true, "msg": "نسخه شما به‌روز است — نیازی به نصب نیست.", "obj": gin.H{
			"localVersion": local, "remoteVersion": remote, "updateAvailable": false,
		}})
		return
	}

	bin, err := exec.LookPath("electruvpn")
	if err != nil {
		bin = "/usr/local/bin/electruvpn"
		if _, err2 := os.Stat(bin); err2 != nil {
			updateMu.Unlock()
			c.JSON(500, gin.H{"success": false, "msg": "دستور electruvpn پیدا نشد — پنل را با install.sh نصب کنید"})
			return
		}
	}

	_ = os.MkdirAll(filepath.Dir(updateStatusFile), 0o755)
	_ = os.WriteFile(updateStatusFile, []byte(fmt.Sprintf("started\t%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), local, remote)), 0o644)

	cmd := exec.Command(bin, "update", githubZipURL)
	cmd.Env = os.Environ()
	logFile, _ := os.OpenFile("/var/log/electruvpn-panel-update.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	// Detach so panel restart does not kill the updater.
	if err := cmd.Start(); err != nil {
		updateMu.Unlock()
		if logFile != nil {
			_ = logFile.Close()
		}
		c.JSON(500, gin.H{"success": false, "msg": "شروع بروزرسانی ناموفق: " + err.Error()})
		return
	}
	go func() {
		defer updateMu.Unlock()
		if logFile != nil {
			defer logFile.Close()
		}
		err := cmd.Wait()
		status := "ok"
		if err != nil {
			status = "fail:" + err.Error()
		}
		_ = os.WriteFile(updateStatusFile, []byte(fmt.Sprintf("%s\t%s\t%s\t%s\n", status, time.Now().UTC().Format(time.RFC3339), local, remote)), 0o644)
	}()

	auditAction(h.S, c, "panel.update.apply", "system", map[string]any{"from": local, "to": remote})
	c.JSON(200, gin.H{"success": true, "msg": "بروزرسانی از گیت‌هاب شروع شد. پنل ممکن است چند دقیقه قطع باشد؛ بعد صفحه را رفرش کنید.", "obj": gin.H{
		"localVersion":    local,
		"remoteVersion":   remote,
		"updateAvailable": true,
		"started":         true,
	}})
}

// UpdateStatus returns last background update marker (if any).
func (h *UpdateHandler) UpdateStatus(c *gin.Context) {
	b, err := os.ReadFile(updateStatusFile)
	if err != nil {
		c.JSON(200, gin.H{"success": true, "obj": gin.H{"status": "none"}})
		return
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"status": strings.TrimSpace(string(b)), "localVersion": localPanelVersion()}})
}
