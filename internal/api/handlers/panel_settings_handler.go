package handlers

import (
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// PanelSettings are deliberately limited to settings that require a process
// restart. Account password changes remain in ChangePassword so a panel
// reconfiguration can never accidentally mix credential and listener changes.
type panelSettingsRequest struct {
	Port             int    `json:"port"`
	TLSEnabled       bool   `json:"tlsEnabled"`
	TLSHost          string `json:"tlsHost"`
	CertificatePEM   string `json:"certificatePEM"`
	PrivateKeyPEM    string `json:"privateKeyPEM"`
	ClearCertificate bool   `json:"clearCertificate"`
}

// PanelSettings handles the PanelSettings API operation.
func (h *AuthHandler) PanelSettings(c *gin.Context) {
	certConfigured := strings.TrimSpace(h.S.Config.PanelTLSCertPath) != "" && fileNonEmpty(h.S.Config.PanelTLSCertPath)
	keyConfigured := strings.TrimSpace(h.S.Config.PanelTLSKeyPath) != "" && fileNonEmpty(h.S.Config.PanelTLSKeyPath)
	c.JSON(200, gin.H{"success": true, "obj": gin.H{
		"port":                  h.S.Config.PanelPort,
		"bindAddr":              h.S.Config.PanelBindAddr,
		"tlsEnabled":            h.S.Config.PanelTLSEnabled,
		"tlsHost":               h.S.Config.PanelTLSHost,
		"certificateConfigured": certConfigured,
		"privateKeyConfigured":  keyConfigured,
		"envFile":               h.S.Config.EnvFilePath,
	}})
}

func fileNonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// ApplyPanelSettings handles the ApplyPanelSettings API operation.
func (h *AuthHandler) ApplyPanelSettings(c *gin.Context) {
	var req panelSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "msg": "invalid settings payload"})
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		c.JSON(400, gin.H{"success": false, "msg": "panel port must be between 1 and 65535"})
		return
	}
	if req.Port != h.S.Config.PanelPort {
		if !h.portLooksFree(req.Port) {
			c.JSON(409, gin.H{"success": false, "msg": fmt.Sprintf("panel port %d is already in use", req.Port)})
			return
		}
	}

	certPath := h.S.Config.PanelTLSCertPath
	keyPath := h.S.Config.PanelTLSKeyPath
	if certPath == "" {
		certPath = filepath.Join(filepath.Dir(h.S.Config.EnvFilePath), "panel-tls.crt")
	}
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(h.S.Config.EnvFilePath), "panel-tls.key")
	}

	backupDir := filepath.Join(filepath.Dir(h.S.Config.EnvFilePath), "panel-settings-backups", time.Now().UTC().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not create settings backup"})
		return
	}
	if err := copyIfExists(h.S.Config.EnvFilePath, filepath.Join(backupDir, "electruvpn.env")); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not back up panel environment"})
		return
	}
	if err := copyIfExists(certPath, filepath.Join(backupDir, "panel-tls.crt")); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not back up certificate"})
		return
	}
	if err := copyIfExists(keyPath, filepath.Join(backupDir, "panel-tls.key")); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "could not back up private key"})
		return
	}

	if req.TLSEnabled {
		certPEM := strings.TrimSpace(req.CertificatePEM)
		keyPEM := strings.TrimSpace(req.PrivateKeyPEM)
		if certPEM == "" {
			certPEM = readFileString(certPath)
		}
		if keyPEM == "" {
			keyPEM = readFileString(keyPath)
		}
		if err := validateTLSMaterial(certPEM, keyPEM); err != nil {
			_ = os.RemoveAll(backupDir)
			c.JSON(400, gin.H{"success": false, "msg": err.Error()})
			return
		}
		if err := atomicWrite(certPath, normalizePEM(certPEM), 0644); err != nil {
			restoreBackupFile(filepath.Join(backupDir, "panel-tls.crt"), certPath)
			restoreBackupFile(filepath.Join(backupDir, "panel-tls.key"), keyPath)
			_ = os.RemoveAll(backupDir)
			c.JSON(500, gin.H{"success": false, "msg": "could not store certificate"})
			return
		}
		if err := atomicWrite(keyPath, normalizePEM(keyPEM), 0600); err != nil {
			restoreBackupFile(filepath.Join(backupDir, "panel-tls.crt"), certPath)
			restoreBackupFile(filepath.Join(backupDir, "panel-tls.key"), keyPath)
			_ = os.RemoveAll(backupDir)
			c.JSON(500, gin.H{"success": false, "msg": "could not store private key"})
			return
		}
	} else if req.ClearCertificate {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}

	if err := writeEnvValues(h.S.Config.EnvFilePath, map[string]string{
		"ELECTRU_PANEL_PORT":        strconv.Itoa(req.Port),
		"ELECTRU_PANEL_TLS_ENABLED": boolEnv(req.TLSEnabled),
		"ELECTRU_PANEL_TLS_CERT":    certPath,
		"ELECTRU_PANEL_TLS_KEY":     keyPath,
		"ELECTRU_PANEL_TLS_HOST":    strings.TrimSpace(req.TLSHost),
	}); err != nil {
		restoreBackupFile(filepath.Join(backupDir, "panel-tls.crt"), certPath)
		restoreBackupFile(filepath.Join(backupDir, "panel-tls.key"), keyPath)
		restoreBackupFile(filepath.Join(backupDir, "electruvpn.env"), h.S.Config.EnvFilePath)
		_ = os.RemoveAll(backupDir)
		c.JSON(500, gin.H{"success": false, "msg": "could not update panel environment"})
		return
	}

	_ = os.Chmod(h.S.Config.EnvFilePath, 0600)
	auditAction(h.S, c, "panel.settings.apply", "panel", fmt.Sprintf("port=%d tls=%t", req.Port, req.TLSEnabled))

	// The old process must survive long enough to send this response. The
	// helper is a separate process and can therefore restart systemd and
	// automatically restore the backed-up configuration if the new listener
	// does not come up.
	helper := filepath.Join(filepath.Dir(h.S.Config.EnvFilePath), "panel-settings-apply.sh")
	if _, err := os.Stat(helper); err != nil {
		helper = filepath.Join("/opt/electruvpn", "panel-settings-apply.sh")
	}
	if _, err := os.Stat(helper); err != nil {
		helper = filepath.Join("/opt/electruvpn", "scripts", "panel-settings-apply.sh")
	}
	cmd := exec.Command("nohup", helper, h.S.Config.EnvFilePath, backupDir, strconv.Itoa(req.Port))
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		c.JSON(500, gin.H{"success": false, "msg": "settings saved but restart helper could not start; restart ElectruVPN manually"})
		return
	}
	_ = cmd.Process.Release()

	c.JSON(200, gin.H{"success": true, "restartRequired": true, "msg": "settings saved; panel is restarting"})
}

func boolEnv(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func readFileString(path string) string { b, _ := os.ReadFile(path); return string(b) }
func normalizePEM(s string) string      { return strings.TrimSpace(s) + "\n" }

func validateTLSMaterial(certPEM, keyPEM string) error {
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("certificate must be a PEM CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("certificate is invalid: %v", err)
	}
	if time.Now().After(cert.NotAfter) {
		return fmt.Errorf("certificate is expired")
	}
	if time.Now().Before(cert.NotBefore.Add(-5 * time.Minute)) {
		return fmt.Errorf("certificate is not valid yet")
	}
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return fmt.Errorf("private key must be PEM encoded")
	}
	var pub any
	if k, e := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); e == nil {
		pub = k
	}
	if pub == nil {
		if k, e := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); e == nil {
			pub = k
		}
	}
	if pub == nil {
		if k, e := x509.ParseECPrivateKey(keyBlock.Bytes); e == nil {
			pub = k
		}
	}
	signer, ok := pub.(interface{ Public() any })
	if !ok {
		return fmt.Errorf("unsupported private key format")
	}
	certPub, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("could not read certificate public key")
	}
	keyPub, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return fmt.Errorf("could not read private key public component")
	}
	if subtle.ConstantTimeCompare(certPub, keyPub) != 1 {
		return fmt.Errorf("certificate and private key do not match")
	}
	return nil
}

func atomicWrite(path, data string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".electru-panel-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func restoreBackupFile(src, dst string) {
	b, err := os.ReadFile(src)
	if err != nil {
		_ = os.Remove(dst)
		return
	}
	_ = atomicWrite(dst, string(b), 0600)
}

func copyIfExists(src, dst string) error {
	b, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return atomicWrite(dst, string(b), 0600)
}

func writeEnvValues(path string, values map[string]string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b = []byte{}
	} else if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for key, value := range values {
		prefix := key + "="
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, prefix) {
				lines[i] = prefix + envQuote(value)
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, prefix+envQuote(value))
		}
	}
	return atomicWrite(path, strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n", 0600)
}

func envQuote(v string) string {
	if v == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}

func (h *AuthHandler) portLooksFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort(h.S.Config.PanelBindAddr, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
