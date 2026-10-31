package singbox

import (
	"github.com/electruvpn/electruvpn/internal/models"
	"testing"
)

func TestGenerateHysteria2(t *testing.T) {
	ib := models.Inbound{Enable: true, Tag: "hy2", Listen: "0.0.0.0", Port: 443, Protocol: models.ProtocolHysteria2, Network: models.NetworkQUIC, Security: models.SecurityTLS, Clients: []models.Client{{Email: "c1", Password: "secret", Enable: true}}}
	cfg, err := Generate([]models.Inbound{ib})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 {
		t.Fatalf("expected one inbound, got %d", len(cfg.Inbounds))
	}
	if cfg.Inbounds[0]["type"] != "hysteria2" {
		t.Fatalf("unexpected type: %#v", cfg.Inbounds[0]["type"])
	}
}
