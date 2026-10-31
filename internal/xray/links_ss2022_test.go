package xray

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/electruvpn/electruvpn/internal/models"
)

func TestShareLinkFixesAndSS2022(t *testing.T) {
	cl := models.Client{Email: "کاربر", UUID: "11111111-1111-1111-1111-111111111111", Password: "pw"}
	// vless gets encryption=none; mkcp -> kcp
	ib := models.Inbound{Remark: "r", Port: 443, Protocol: models.ProtocolVLESS, Network: models.NetworkMKCP, Security: models.SecurityNone}
	l, err := BuildShareLink(ib, cl, "example.com")
	if err != nil || !strings.Contains(l, "encryption=none") || !strings.Contains(l, "type=kcp") {
		t.Fatal(l, err)
	}
	// vmess grpc has path + tls fields
	ib = models.Inbound{Remark: "r", Port: 443, Protocol: models.ProtocolVMess, Network: models.NetworkGRPC, Security: models.SecurityTLS,
		StreamSettings: `{"grpcServiceName":"svc","tlsSettings":{"sni":"a.example.com"}}`}
	l, err = BuildShareLink(ib, cl, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(l, "vmess://"))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["path"] != "svc" || m["tls"] != "tls" || m["sni"] != "a.example.com" {
		t.Fatal(string(raw))
	}
	// SS-2022 key generation and link
	s := EnsureShadowsocksServerKey(`{"method":"2022-blake3-aes-128-gcm"}`)
	var ss models.ShadowsocksSettings
	_ = json.Unmarshal([]byte(s), &ss)
	if !validShadowsocksKey(ss.Password, 16) {
		t.Fatal(s)
	}
	if EnsureShadowsocksServerKey(s) != s {
		t.Fatal("key must be stable once valid")
	}
	if EnsureShadowsocksServerKey(`{"method":"aes-256-gcm"}`) != `{"method":"aes-256-gcm"}` {
		t.Fatal("legacy changed")
	}
	ib = models.Inbound{Remark: "r", Port: 8388, Protocol: models.ProtocolShadowsocks, Settings: s}
	l, err = BuildShareLink(ib, cl, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	u := strings.TrimPrefix(strings.SplitN(l, "@", 2)[0], "ss://")
	dec, _ := base64.RawURLEncoding.DecodeString(u)
	if !strings.HasPrefix(string(dec), "2022-blake3-aes-128-gcm:"+ss.Password+":pw") {
		t.Fatal(string(dec))
	}
}
