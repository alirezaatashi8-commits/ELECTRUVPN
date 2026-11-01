package xray

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// ShadowsocksKeyLength returns the raw key length for Shadowsocks-2022 methods
// and 0 for legacy AEAD methods (which use free-form passwords).
func ShadowsocksKeyLength(method string) int {
	switch strings.TrimSpace(method) {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	}
	return 0
}

func validShadowsocksKey(key string, n int) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	return err == nil && len(raw) == n
}

// EnsureShadowsocksServerKey makes sure a Shadowsocks-2022 inbound has the
// server (identity) key Xray requires for multi-user mode. Without it Xray
// refuses to start ("invalid PSK length"), so every create/apply of a
// Shadowsocks-2022 inbound failed and was rolled back. Legacy methods and
// settings that already hold a valid key are returned unchanged.
func EnsureShadowsocksServerKey(settings string) string {
	m := map[string]any{}
	if strings.TrimSpace(settings) != "" {
		if err := json.Unmarshal([]byte(settings), &m); err != nil {
			return settings
		}
	}
	method, _ := m["method"].(string)
	if strings.TrimSpace(method) == "" {
		method = "2022-blake3-aes-128-gcm"
	}
	n := ShadowsocksKeyLength(method)
	if n == 0 {
		return settings
	}
	if pw, _ := m["password"].(string); validShadowsocksKey(pw, n) {
		return settings
	}
	key := make([]byte, n)
	if _, err := rand.Read(key); err != nil {
		return settings
	}
	m["method"] = method
	m["password"] = base64.StdEncoding.EncodeToString(key)
	out, err := json.Marshal(m)
	if err != nil {
		return settings
	}
	return string(out)
}
