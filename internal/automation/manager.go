package automation

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
	"github.com/electruvpn/electruvpn/internal/notify"
	"gorm.io/gorm"
)

var certMu sync.Mutex

func publicIP(host string) bool {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return false
		}
		if v4 := ip.To4(); v4 != nil && v4[0] == 169 && v4[1] == 254 {
			return false
		}
	}
	return true
}

func CheckDomain(domain string) (string, int64, string, *time.Time) {
	domain = strings.TrimSpace(strings.TrimSuffix(domain, "."))
	if domain == "" || strings.Contains(domain, "/") || !publicIP(domain) {
		return "unhealthy", 0, "domain does not resolve to public addresses", nil
	}
	u := &url.URL{Scheme: "https", Host: domain}
	start := time.Now()
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("redirects disabled") }}
	req, err := http.NewRequest(http.MethodHead, u.String(), nil)
	if err != nil {
		return "unhealthy", 0, err.Error(), nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return "unhealthy", time.Since(start).Milliseconds(), err.Error(), nil
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return "degraded", time.Since(start).Milliseconds(), fmt.Sprintf("HTTP %d", resp.StatusCode), nil
	}
	// HTTP/TLS validation is performed by net/http; the second request captures the peer certificate expiry.
	tr := &http.Transport{TLSClientConfig: nil}
	hc := &http.Client{Timeout: 8 * time.Second, Transport: tr, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("redirects disabled") }}
	r2, err := hc.Get(u.String())
	if err != nil {
		return "unhealthy", time.Since(start).Milliseconds(), err.Error(), nil
	}
	r2.Body.Close()
	var expires *time.Time
	if r2.TLS != nil && len(r2.TLS.PeerCertificates) > 0 {
		t := r2.TLS.PeerCertificates[0].NotAfter
		expires = &t
	}
	return "healthy", time.Since(start).Milliseconds(), "", expires
}

func SyncCertificateRevision(db *gorm.DB, cert *models.CertificateRecord) error {
	if cert == nil || cert.CertPath == "" {
		return nil
	}
	certMu.Lock()
	defer certMu.Unlock()
	b, err := os.ReadFile(cert.CertPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return fmt.Errorf("certificate PEM not found")
	}
	x, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(x.Raw)
	fp := hex.EncodeToString(sum[:])
	var count int64
	if err := db.Model(&models.CertificateRevision{}).Where("certificate_id = ?", cert.ID).Count(&count).Error; err != nil {
		return err
	}
	var latest models.CertificateRevision
	if err := db.Where("certificate_id = ?", cert.ID).Order("version desc").First(&latest).Error; err == nil && latest.Fingerprint == fp {
		return nil
	}
	rev := models.CertificateRevision{CertificateID: cert.ID, Version: int(count) + 1, Issuer: x.Issuer.CommonName, Fingerprint: fp, ExpiresAt: &x.NotAfter, CertPath: cert.CertPath, KeyPath: cert.KeyPath, Status: "active"}
	if latest.ID != 0 {
		latest.Status = "superseded"
		if err := db.Save(&latest).Error; err != nil {
			return err
		}
	}
	cert.Issuer, cert.ExpiresAt, cert.Status = x.Issuer.CommonName, &x.NotAfter, "active"
	if err := db.Save(cert).Error; err != nil {
		return err
	}
	return db.Create(&rev).Error
}

func RenewCertificates(db *gorm.DB) {
	var certs []models.CertificateRecord
	if db.Where("auto_renew = ?", true).Find(&certs).Error != nil {
		return
	}
	due := false
	now := time.Now()
	for _, c := range certs {
		if c.ExpiresAt == nil || c.ExpiresAt.Before(now.Add(30*24*time.Hour)) {
			due = true
			break
		}
	}
	if !due {
		return
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		return
	}
	for i := range certs {
		if !strings.HasPrefix(certs[i].CertPath, "/etc/letsencrypt/") {
			continue
		}
		certName := strings.TrimPrefix(certs[i].Domain, "*.")
		certMu.Lock()
		cmd := exec.Command("certbot", "renew", "--cert-name", certName, "--quiet")
		out, err := cmd.CombinedOutput()
		certMu.Unlock()
		if err != nil {
			_ = notify.Telegram(fmt.Sprintf("ElectruVPN SSL renewal failed for %s: %s", certs[i].Domain, strings.TrimSpace(string(out))))
			continue
		}
		if err := SyncCertificateRevision(db, &certs[i]); err != nil {
			_ = notify.Telegram(fmt.Sprintf("ElectruVPN certificate sync failed for %s: %v", certs[i].Domain, err))
		}
	}
}

func CheckDomains(db *gorm.DB) {
	var domains []models.DomainRecord
	if db.Where("enabled = ?", true).Order("priority asc, id asc").Find(&domains).Error != nil {
		return
	}
	for i := range domains {
		status, latency, errMsg, expires := CheckDomain(domains[i].Domain)
		now := time.Now()
		d := &domains[i]
		if status == "healthy" {
			d.HealthFailures = 0
		} else {
			d.HealthFailures++
		}
		d.HealthStatus, d.LastLatencyMs, d.LastError, d.LastHealthAt = status, latency, errMsg, &now
		if expires != nil && d.CertificateID != 0 {
			var cert models.CertificateRecord
			if db.First(&cert, d.CertificateID).Error == nil {
				cert.ExpiresAt = expires
				cert.Status = "active"
				_ = db.Save(&cert).Error
			}
		}
		_ = db.Save(d).Error
	}
	var active models.DomainRecord
	if db.Where("enabled = ? AND primary = ?", true, true).First(&active).Error == nil && active.HealthFailures >= 3 {
		var next models.DomainRecord
		if db.Where("enabled = ? AND id <> ? AND health_status = ?", true, active.ID, "healthy").Order("priority asc, id asc").First(&next).Error == nil {
			now := time.Now()
			_ = db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&models.DomainRecord{}).Where("id = ?", active.ID).Update("primary", false).Error; err != nil {
					return err
				}
				if err := tx.Model(&models.DomainRecord{}).Where("id = ?", next.ID).Updates(map[string]any{"primary": true, "last_failover_at": now}).Error; err != nil {
					return err
				}
				return nil
			})
			_ = notify.Telegram(fmt.Sprintf("ElectruVPN domain failover: %s -> %s", active.Domain, next.Domain))
		}
	}
}
