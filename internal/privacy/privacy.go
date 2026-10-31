package privacy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
)

func IdentifierHash(value, secret string) string {
	value = strings.TrimSpace(value)
	if value == "" || secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func NormalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return ""
}
