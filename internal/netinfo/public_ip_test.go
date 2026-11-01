package netinfo

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsIP(t *testing.T) {
	if !IsIP("203.0.113.10") || !IsIP("2001:db8::1") {
		t.Fatal("expected IP literals")
	}
	if IsIP("vpn.example.com") || IsIP("") {
		t.Fatal("domain must not count as IP")
	}
}

func TestIsUsablePublicIP(t *testing.T) {
	for _, raw := range []string{"10.0.0.2", "192.168.1.5", "172.16.0.3", "100.64.0.1", "127.0.0.1", "169.254.1.1"} {
		if isUsablePublicIP(net.ParseIP(raw)) {
			t.Errorf("%s must not be accepted as public", raw)
		}
	}
	if !isUsablePublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("expected public IPv4 to be accepted")
	}
}

func TestDetectExternalIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("8.8.4.4\n"))
	}))
	defer server.Close()
	if got := detectExternalIP(server.URL); got != "8.8.4.4" {
		t.Fatalf("detectExternalIP() = %q, want 8.8.4.4", got)
	}
}

func TestDetectExternalIPRejectsPrivateAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("192.168.1.10\n"))
	}))
	defer server.Close()
	if got := detectExternalIP(server.URL); got != "" {
		t.Fatalf("detectExternalIP() = %q, want empty for private IP", got)
	}
}
