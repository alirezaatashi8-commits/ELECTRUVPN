package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds every runtime setting the panel needs. Everything can be
// overridden with an environment variable so the same binary works in
// Docker, systemd, or a plain `go run`.
type Config struct {
	// PanelPort is the port the ElectruVPN web panel itself listens on.
	PanelPort int
	// PanelBindAddr controls the listen interface. Keep 0.0.0.0 for direct public use;
	// set 127.0.0.1 when placing the panel behind a reverse proxy.
	PanelBindAddr string
	// PanelVPNAccess keeps the panel reachable through the same VPN/proxy path.
	PanelVPNAccess bool
	// PanelBasePath lets the whole UI live under a random path (e.g.
	// /8f3ac9d2/) instead of "/", which is a cheap but effective way to
	// hide the login page from mass scanners.
	PanelBasePath string
	// DBDSN is the preferred PostgreSQL connection string (ELECTRU_DB_DSN).
	// When set, the panel uses PostgreSQL. Example:
	// host=127.0.0.1 user=electruvpn password=secret dbname=electruvpn port=5432 sslmode=disable TimeZone=UTC
	DBDSN string
	// DBPath is the legacy SQLite file path (used only when DBDSN is empty).
	DBPath string
	// JWTSecret signs session tokens. Always override this in production.
	JWTSecret string
	// ConfigEncryptionKey is a 32-byte key (hex or any string) used to encrypt
	// sensitive config fields at rest and to sign subscription payloads.
	// Generate: openssl rand -hex 32
	ConfigEncryptionKey string
	// XrayBinaryPath points at the xray-core executable installed on the host.
	XrayBinaryPath string
	// XrayConfigPath is where the generated xray config.json is written.
	XrayConfigPath string
	// XrayAPIPort is the internal gRPC port xray exposes for stats/queries.
	XrayAPIPort int
	// PublicHost is the default address embedded into generated client share links.
	// V4/V6 can be set independently for dual-stack deployments.
	PublicHost           string
	PublicHostV4         string
	PreviousPublicHostV4 string
	PublicHostV6         string
	PublicHostDomain     string
	SingBoxBinaryPath    string
	SingBoxConfigPath    string
	// SingBoxAPIPort is the loopback-only Clash API port sing-box exposes for
	// live per-protocol stats (0 disables it).
	SingBoxAPIPort    int
	PanelTLSEnabled   bool
	PanelTLSCertPath  string
	PanelTLSKeyPath   string
	PanelTLSHost      string
	EnvFilePath       string
	BackupIntervalMin int
	BackupRetention   int
	BackupRemoteDir   string
	// SecurityMode enables strict privacy defaults for generated client links and runtime DNS.
	SecurityMode string
	// SubscriptionBaseURL is the public URL used by clients to fetch subscriptions.
	// It should point to a privacy-preserving public endpoint, not the admin panel origin.
	SubscriptionBaseURL       string
	SubscriptionBaseURLV4     string
	SubscriptionBaseURLV6     string
	SubscriptionBaseURLDomain string
	// DNSResolvers are server-side encrypted DNS endpoints used by Xray/sing-box.
	DNSResolvers []string
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Load reads configuration from the environment, falling back to sensible
// defaults for a fresh single-server install.
func Load() *Config {
	basePath := getEnv("ELECTRU_BASE_PATH", "/")
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	if basePath == "" {
		basePath = "/"
	}
	dns := strings.FieldsFunc(getEnv("ELECTRU_DNS_RESOLVERS", "https+local://1.1.1.1/dns-query,https+local://8.8.8.8/dns-query"), func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	if len(dns) == 0 {
		dns = []string{"https+local://1.1.1.1/dns-query"}
	}
	return &Config{
		PanelPort:                 getEnvInt("ELECTRU_PANEL_PORT", 2087),
		PanelBindAddr:             getEnv("ELECTRU_PANEL_BIND_ADDR", "0.0.0.0"),
		PanelVPNAccess:            getEnv("ELECTRU_PANEL_VPN_ACCESS", "1") == "1",
		PanelBasePath:             basePath,
		DBDSN:                     getEnv("ELECTRU_DB_DSN", ""),
		DBPath:                    getEnv("ELECTRU_DB_PATH", "/etc/electruvpn/electruvpn.db"),
		JWTSecret:                 getEnv("ELECTRU_JWT_SECRET", "change-me-in-production-please"),
		ConfigEncryptionKey:       getEnv("ELECTRU_CONFIG_ENCRYPTION_KEY", ""),
		XrayBinaryPath:            getEnv("ELECTRU_XRAY_BIN", "/usr/local/bin/xray"),
		XrayConfigPath:            getEnv("ELECTRU_XRAY_CONFIG", "/etc/electruvpn/xray-config.json"),
		XrayAPIPort:               getEnvInt("ELECTRU_XRAY_API_PORT", 10085),
		PublicHost:                getEnv("ELECTRU_PUBLIC_HOST", ""),
		PublicHostV4:              getEnv("ELECTRU_PUBLIC_HOST_V4", ""),
		PreviousPublicHostV4:      getEnv("ELECTRU_PREVIOUS_PUBLIC_HOST_V4", ""),
		PublicHostV6:              getEnv("ELECTRU_PUBLIC_HOST_V6", ""),
		PublicHostDomain:          getEnv("ELECTRU_PUBLIC_HOST_DOMAIN", ""),
		SingBoxBinaryPath:         getEnv("ELECTRU_SINGBOX_BIN", "/usr/local/bin/sing-box"),
		SingBoxConfigPath:         getEnv("ELECTRU_SINGBOX_CONFIG", "/etc/electruvpn/sing-box.json"),
		SingBoxAPIPort:            getEnvInt("ELECTRU_SINGBOX_API_PORT", 10086),
		PanelTLSEnabled:           getEnv("ELECTRU_PANEL_TLS_ENABLED", "0") == "1",
		PanelTLSCertPath:          getEnv("ELECTRU_PANEL_TLS_CERT", ""),
		PanelTLSKeyPath:           getEnv("ELECTRU_PANEL_TLS_KEY", ""),
		PanelTLSHost:              getEnv("ELECTRU_PANEL_TLS_HOST", ""),
		EnvFilePath:               getEnv("ELECTRU_ENV_FILE", "/etc/electruvpn/electruvpn.env"),
		BackupIntervalMin:         getEnvInt("ELECTRU_BACKUP_INTERVAL_MIN", 1440),
		BackupRetention:           getEnvInt("ELECTRU_BACKUP_RETENTION", 14),
		BackupRemoteDir:           getEnv("ELECTRU_BACKUP_REMOTE_DIR", ""),
		SecurityMode:              getEnv("ELECTRU_SECURITY_MODE", "strict"),
		SubscriptionBaseURL:       strings.TrimRight(getEnv("ELECTRU_SUBSCRIPTION_BASE_URL", ""), "/"),
		SubscriptionBaseURLV4:     strings.TrimRight(getEnv("ELECTRU_SUBSCRIPTION_BASE_URL_V4", ""), "/"),
		SubscriptionBaseURLV6:     strings.TrimRight(getEnv("ELECTRU_SUBSCRIPTION_BASE_URL_V6", ""), "/"),
		SubscriptionBaseURLDomain: strings.TrimRight(getEnv("ELECTRU_SUBSCRIPTION_BASE_URL_DOMAIN", ""), "/"),
		DNSResolvers:              dns,
	}
}

// DataDir returns a filesystem directory suitable for backups and disk stats.
// When using PostgreSQL (DBDSN set), it prefers the directory of XrayConfigPath
// or falls back to /etc/electruvpn. For SQLite it uses the directory of DBPath.
func (c *Config) DataDir() string {
	if strings.TrimSpace(c.DBDSN) != "" {
		if c.XrayConfigPath != "" {
			if d := filepath.Dir(c.XrayConfigPath); d != "" && d != "." {
				return d
			}
		}
		return "/etc/electruvpn"
	}
	if c.DBPath != "" {
		if d := filepath.Dir(c.DBPath); d != "" && d != "." {
			return d
		}
	}
	return "/etc/electruvpn"
}

// UsingPostgres reports whether the panel is configured for PostgreSQL.
func (c *Config) UsingPostgres() bool {
	return strings.TrimSpace(c.DBDSN) != ""
}
