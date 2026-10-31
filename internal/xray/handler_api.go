package xray

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// LiveUser describes a single VLESS/VMess/Trojan/SS user for live HandlerService ops.
// Only email + uuid/password are required for most protocols; flow is optional (Vision).
type LiveUser struct {
	Email    string
	UUID     string
	Password string
	Flow     string
	Level    int
}

// LiveAddUser tries to add a user to a running inbound via `xray api adu` without
// restarting the process. Returns nil on success. Callers must still persist
// the full config on disk so the next cold start matches live state.
func LiveAddUser(binaryPath string, apiPort int, inboundTag string, user LiveUser) error {
	if strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(user.Email) == "" {
		return fmt.Errorf("inbound tag and email are required")
	}
	server := fmt.Sprintf("127.0.0.1:%d", apiPort)

	// Build a minimal user JSON accepted by xray api adu (-inboundTag + stdin/arg).
	// Prefer the structured -email / -id style when available; fall back to raw JSON.
	args := []string{
		"api", "adu",
		"-s", server,
		"-inboundTag", inboundTag,
	}
	payload := map[string]any{
		"email": user.Email,
		"level": user.Level,
	}
	if user.UUID != "" {
		payload["id"] = user.UUID
	}
	if user.Password != "" {
		payload["password"] = user.Password
	}
	if user.Flow != "" {
		payload["flow"] = user.Flow
	}
	body, _ := json.Marshal(payload)

	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = strings.NewReader(string(body) + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Some Xray builds expect -email/-uuid flags instead of JSON stdin.
		args2 := []string{"api", "adu", "-s", server, "-inboundTag", inboundTag, "-email", user.Email}
		if user.UUID != "" {
			args2 = append(args2, "-id", user.UUID)
		}
		if user.Password != "" {
			args2 = append(args2, "-password", user.Password)
		}
		cmd2 := exec.Command(binaryPath, args2...)
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("xray api adu failed: %v (%s); fallback: %v (%s)", err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)))
		}
		return nil
	}
	_ = out
	return nil
}

// LiveRemoveUser removes a user from a running inbound via `xray api rmu`.
func LiveRemoveUser(binaryPath string, apiPort int, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	server := fmt.Sprintf("127.0.0.1:%d", apiPort)
	cmd := exec.Command(binaryPath, "api", "rmu", "-s", server, "-email", email)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray api rmu failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// LiveApplyEnabled reports whether the panel setting for live user apply is on.
// Setting key: settings.flag_liveUserApply = "true"
func LiveApplyEnabled(getSetting func(key string) string) bool {
	v := strings.ToLower(strings.TrimSpace(getSetting("settings.flag_liveUserApply")))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

// PingAPI checks that the local Xray API port answers a statsquery quickly.
func PingAPI(binaryPath string, apiPort int) error {
	cmd := exec.Command(binaryPath, "api", "statsquery", "-s", fmt.Sprintf("127.0.0.1:%d", apiPort))
	done := make(chan error, 1)
	go func() {
		_, err := cmd.CombinedOutput()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("xray api timeout")
	}
}
