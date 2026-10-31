package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/electruvpn/electruvpn/internal/core"
	"github.com/gin-gonic/gin"
)

type NetworkHandler struct{ S *core.Server }

// NewNetworkHandler creates or exposes the NewNetworkHandler handler.
func NewNetworkHandler(s *core.Server) *NetworkHandler { return &NetworkHandler{S: s} }

func cmdOut(name string, args ...string) string {
	b, e := exec.Command(name, args...).CombinedOutput()
	if e != nil {
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(string(b))
}

// Status handles the Status API operation.
func (h *NetworkHandler) Status(c *gin.Context) {
	av := cmdOut("sysctl", "-n", "net.ipv4.tcp_available_congestion_control")
	cc := cmdOut("sysctl", "-n", "net.ipv4.tcp_congestion_control")
	q := cmdOut("sysctl", "-n", "net.core.default_qdisc")
	kv := cmdOut("uname", "-r")
	modules := cmdOut("bash", "-lc", "lsmod 2>/dev/null | awk '$1==\"tcp_bbr\"{print $1}'")
	// Linux registers BBRv3 under the same congestion-control name, "bbr", as
	// BBRv1. Do not infer v3 from tcp_available_congestion_control. Only mark it
	// confirmed when an administrator/package has installed our explicit marker.
	_, markerErr := os.Stat("/etc/electruvpn/bbrv3-kernel-installed")
	bbrv3Confirmed := markerErr == nil
	bbrv3Status := "not_verified"
	if bbrv3Confirmed {
		bbrv3Status = "confirmed_by_marker"
	}
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"kernel": kv, "arch": runtime.GOARCH, "available": strings.Fields(av), "current": cc, "qdisc": q, "bbrModule": modules == "tcp_bbr", "bbrv3Available": bbrv3Confirmed, "bbrv3Status": bbrv3Status, "bbrv3Note": "BBRv3 uses the kernel algorithm name 'bbr'; sysctl cannot distinguish it from BBRv1. BBRv3 is only marked confirmed when the kernel/package provider creates /etc/electruvpn/bbrv3-kernel-installed.", "optimized": cc == "bbr" && q == "fq"}})
}

// Test handles the Test API operation.
func (h *NetworkHandler) Test(c *gin.Context) {
	before := h.snapshot()
	c.JSON(200, gin.H{"success": true, "obj": before})
}
func (h *NetworkHandler) snapshot() map[string]string {
	return map[string]string{"congestion_control": cmdOut("sysctl", "-n", "net.ipv4.tcp_congestion_control"), "default_qdisc": cmdOut("sysctl", "-n", "net.core.default_qdisc"), "tcp_mtu_probing": cmdOut("sysctl", "-n", "net.ipv4.tcp_mtu_probing")}
}

// Apply handles the Apply API operation.
func (h *NetworkHandler) Apply(c *gin.Context) {
	if os.Geteuid() != 0 {
		c.JSON(403, gin.H{"success": false, "msg": "root privileges required on the node"})
		return
	}
	snap := h.snapshot()
	b, _ := json.Marshal(snap)
	_ = os.WriteFile("/var/lib/electruvpn-bbr-backup.json", b, 0600)
	// Conservative tuning: only congestion control, fq pacing, and MTU probing.
	cmds := [][]string{{"modprobe", "tcp_bbr"}, {"sysctl", "-w", "net.core.default_qdisc=fq"}, {"sysctl", "-w", "net.ipv4.tcp_congestion_control=bbr"}, {"sysctl", "-w", "net.ipv4.tcp_mtu_probing=1"}}
	var out []string
	for _, x := range cmds {
		z, e := exec.Command(x[0], x[1:]...).CombinedOutput()
		out = append(out, strings.TrimSpace(string(z)))
		if e != nil {
			c.JSON(500, gin.H{"success": false, "msg": fmt.Sprintf("%s: %v", x[0], e), "output": out})
			return
		}
	}
	// Persist only if sysctl.d exists; this is idempotent and easy to roll back.
	_ = os.WriteFile("/etc/sysctl.d/99-electruvpn-bbr.conf", []byte("# Managed by ElectruVPN Network Optimizer\nnet.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\nnet.ipv4.tcp_mtu_probing=1\n"), 0644)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"before": snap, "after": h.snapshot(), "appliedAt": time.Now().UTC(), "output": out}})
}

// Rollback handles the Rollback API operation.
func (h *NetworkHandler) Rollback(c *gin.Context) {
	if os.Geteuid() != 0 {
		c.JSON(403, gin.H{"success": false, "msg": "root privileges required on the node"})
		return
	}
	b, e := os.ReadFile("/var/lib/electruvpn-bbr-backup.json")
	if e != nil {
		c.JSON(404, gin.H{"success": false, "msg": "no BBR backup found"})
		return
	}
	var s map[string]string
	if json.Unmarshal(b, &s) != nil {
		c.JSON(500, gin.H{"success": false, "msg": "invalid backup"})
		return
	}
	keys := map[string]string{
		"congestion_control": "net.ipv4.tcp_congestion_control",
		"default_qdisc":      "net.core.default_qdisc",
		"tcp_mtu_probing":    "net.ipv4.tcp_mtu_probing",
	}
	for k, v := range s {
		sysctlKey, ok := keys[k]
		if !ok || v == "" {
			continue
		}
		if out, err := exec.Command("sysctl", "-w", sysctlKey+"="+v).CombinedOutput(); err != nil {
			c.JSON(500, gin.H{"success": false, "msg": "rollback failed for " + sysctlKey, "output": strings.TrimSpace(string(out))})
			return
		}
	}
	_ = os.Remove("/etc/sysctl.d/99-electruvpn-bbr.conf")
	_ = exec.Command("sysctl", "--system").Run()
	c.JSON(200, gin.H{"success": true, "obj": gin.H{"restored": s, "after": h.snapshot()}})
}
