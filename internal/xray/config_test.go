package xray

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/electruvpn/electruvpn/internal/models"
)

func TestGenerateSkipsDisabledAndExpiredClients(t *testing.T) {
	active := models.Client{Email: "active", UUID: "11111111-1111-4111-8111-111111111111", Enable: true}
	disabled := models.Client{Email: "disabled", UUID: "11111111-1111-4111-8111-111111111111", Enable: false}
	ib := models.Inbound{ID: 1, Tag: "test", Listen: "0.0.0.0", Port: 12345, Enable: true, Protocol: models.ProtocolVLESS, Network: models.NetworkTCP, Security: models.SecurityNone, Clients: []models.Client{active, disabled}}
	cfg, err := Generate([]models.Inbound{ib}, 10085)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "active") || strings.Contains(s, "disabled") {
		t.Fatalf("unexpected generated clients: %s", s)
	}
}

func TestGenerateRejectsSingBoxProtocol(t *testing.T) {
	ib := models.Inbound{Enable: true, Protocol: models.ProtocolHysteria2}
	if _, err := Generate([]models.Inbound{ib}, 10085); err == nil {
		t.Fatal("expected sing-box protocol to be rejected by xray generator")
	}
}

func TestGenerateSkipsLimitedInbounds(t *testing.T) {
	cl := models.Client{Email: "alice", UUID: "11111111-1111-4111-8111-111111111111", Enable: true}
	base := models.Inbound{ID: 1, Tag: "limited-tag", Listen: "0.0.0.0", Port: 12345, Enable: true, Protocol: models.ProtocolVLESS, Network: models.NetworkTCP, Security: models.SecurityNone, Clients: []models.Client{cl}}

	over := base
	over.TotalGB = 1
	over.Up = 2 * 1024 * 1024 * 1024
	expired := base
	expired.Tag = "expired-tag"
	expired.Port = 12346
	expired.ExpiryTime = time.Now().Add(-time.Hour).UnixMilli()
	healthy := base
	healthy.Tag = "healthy-tag"
	healthy.Port = 12347
	healthy.TotalGB = 10

	cfg, err := Generate([]models.Inbound{over, expired, healthy}, 10085)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "limited-tag") || strings.Contains(s, "expired-tag") {
		t.Fatalf("limited inbound must not be served: %s", s)
	}
	if !strings.Contains(s, "healthy-tag") {
		t.Fatalf("healthy inbound must be served: %s", s)
	}
}
