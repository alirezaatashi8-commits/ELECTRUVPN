package xray

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/electruvpn/electruvpn/internal/models"
)

func TestNormalizePublicHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"example.com", "example.com"}, {"[2001:db8::1]", "2001:db8::1"}, {"2001:db8::1", "2001:db8::1"}} {
		got, err := normalizePublicHost(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("%q => %q, %v", tc.in, got, err)
		}
	}
	for _, in := range []string{"", "https://example.com", "example.com/path", "bad host"} {
		if _, err := normalizePublicHost(in); err == nil {
			t.Fatalf("expected invalid host %q", in)
		}
	}
}

func TestBuildShareLinkIPv4AndIPv6Hosts(t *testing.T) {
	ib := models.Inbound{Port: 443, Protocol: models.ProtocolVLESS, Network: models.NetworkTCP, Security: models.SecurityTLS, StreamSettings: `{"tlsSettings":{"sni":"example.com"}}`}
	c := models.Client{UUID: "550e8400-e29b-41d4-a716-446655440000", Email: "ipv6-test", Enable: true}
	v4, err := BuildShareLink(ib, c, "203.0.113.10")
	if err != nil {
		t.Fatalf("ipv4 link: %v", err)
	}
	if !strings.Contains(v4, "@203.0.113.10:443") {
		t.Fatalf("unexpected ipv4 host: %s", v4)
	}
	v6, err := BuildShareLink(ib, c, "2001:db8::10")
	if err != nil {
		t.Fatalf("ipv6 link: %v", err)
	}
	if !strings.Contains(v6, "@[2001:db8::10]:443") {
		t.Fatalf("unexpected ipv6 host: %s", v6)
	}
}

func TestBuildSubscriptionBodyDualStack(t *testing.T) {
	v4 := "vless://550e8400-e29b-41d4-a716-446655440000@203.0.113.10:443?type=tcp&security=tls#v4"
	v6 := "vless://550e8400-e29b-41d4-a716-446655440000@[2001:db8::10]:443?type=tcp&security=tls#v6"
	body := BuildSubscriptionBody([]string{v4, v6})
	if body == "" {
		t.Fatal("dual-stack subscription body is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	text := string(decoded)
	if !strings.Contains(text, v4) || !strings.Contains(text, v6) {
		t.Fatalf("dual-stack subscription missing family links: %s", text)
	}
}

func TestBuildShareLinkDomainAndIPv6Shadowsocks(t *testing.T) {
	vless := models.Inbound{Port: 443, Protocol: models.ProtocolVLESS, Network: models.NetworkTCP, Security: models.SecurityTLS}
	c := models.Client{UUID: "550e8400-e29b-41d4-a716-446655440000", Email: "domain-test"}
	domain, err := BuildShareLink(vless, c, "vpn.example.com")
	if err != nil || !strings.Contains(domain, "@vpn.example.com:443") {
		t.Fatalf("unexpected domain link: %s (%v)", domain, err)
	}
	ss := models.Inbound{Port: 8388, Protocol: models.ProtocolShadowsocks, Network: models.NetworkTCP, Security: models.SecurityNone, Settings: `{"method":"aes-256-gcm"}`}
	ssLink, err := BuildShareLink(ss, models.Client{Password: "secret", Email: "ss-v6"}, "2001:db8::10")
	if err != nil {
		t.Fatalf("shadowsocks v6 link: %v", err)
	}
	if !strings.Contains(ssLink, "@[2001:db8::10]:8388") {
		t.Fatalf("unexpected shadowsocks IPv6 link: %s", ssLink)
	}
}
