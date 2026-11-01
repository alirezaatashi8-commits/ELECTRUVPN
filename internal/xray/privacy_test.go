package xray

import (
	"testing"

	"github.com/electruvpn/electruvpn/internal/models"
)

func TestGenerateWithDNSPrivacyDefaults(t *testing.T) {
	cfg, err := GenerateWithDNS([]models.Inbound{}, 10085, []string{"https+local://1.1.1.1/dns-query"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DNS.Servers) < 2 {
		t.Fatalf("expected multi-query smart DNS, got: %#v", cfg.DNS.Servers)
	}
	if cfg.Routing.DomainStrategy != "IPIfNonMatch" {
		t.Fatalf("unexpected strategy: %s", cfg.Routing.DomainStrategy)
	}
	if len(cfg.Routing.Rules) < 2 {
		t.Fatal("routing rules missing")
	}
	// Rule 0 must route the loopback stats/handler API; the private-destination
	// block rule follows it.
	api := cfg.Routing.Rules[0]
	if api.OutboundTag != "api" || len(api.InboundTag) != 1 || api.InboundTag[0] != "api" {
		t.Fatalf("api routing rule must be first, got %#v", api)
	}
	if cfg.Routing.Rules[1].OutboundTag != "block" {
		t.Fatal("private destination block rule missing")
	}
}
