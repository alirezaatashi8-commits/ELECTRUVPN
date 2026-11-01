package singbox

import "testing"

func TestGenerateWithDNSUsesEncryptedResolvers(t *testing.T) {
	cfg, err := GenerateWithDNS(nil, []string{"https://1.1.1.1/dns-query"})
	if err != nil {
		t.Fatal(err)
	}
	dns, ok := cfg.DNS["servers"].([]map[string]any)
	if !ok || len(dns) < 2 {
		t.Fatalf("unexpected dns servers: %#v", cfg.DNS["servers"])
	}
	hasHTTPS := false
	for _, s := range dns {
		if s["type"] == "https" || s["type"] == "tls" {
			hasHTTPS = true
			break
		}
	}
	if !hasHTTPS {
		t.Fatalf("expected DoH/DoT servers: %#v", dns)
	}
	if cfg.DNS["final"] != "secure-dns-1" {
		t.Fatal("secure dns final missing")
	}
}
