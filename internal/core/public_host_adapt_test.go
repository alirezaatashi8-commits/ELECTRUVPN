package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/electruvpn/electruvpn/internal/config"
)

func TestSetPublicIPv4ManualOverride(t *testing.T) {
	oldAuto, hadAuto := os.LookupEnv("ELECTRU_AUTO_PUBLIC_IP")
	t.Cleanup(func() {
		if hadAuto {
			_ = os.Setenv("ELECTRU_AUTO_PUBLIC_IP", oldAuto)
		} else {
			_ = os.Unsetenv("ELECTRU_AUTO_PUBLIC_IP")
		}
	})
	envPath := filepath.Join(t.TempDir(), "electruvpn.env")
	if err := os.WriteFile(envPath, []byte("ELECTRU_AUTO_PUBLIC_IP=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{Config: &config.Config{
		EnvFilePath:           envPath,
		PublicHost:            "198.51.100.10",
		PublicHostV4:          "198.51.100.10",
		SubscriptionBaseURL:   "https://vpn.example.com/panel",
		SubscriptionBaseURLV4: "https://198.51.100.10:443/panel",
	}}
	if err := s.SetPublicIPv4("203.0.113.44"); err != nil {
		t.Fatalf("SetPublicIPv4: %v", err)
	}
	if s.Config.PublicHostV4 != "203.0.113.44" || s.Config.PublicHost != "203.0.113.44" {
		t.Fatalf("manual IPv4 not applied: host=%q v4=%q", s.Config.PublicHost, s.Config.PublicHostV4)
	}
	if s.Config.PreviousPublicHostV4 != "198.51.100.10" {
		t.Fatalf("previous IPv4 not preserved: %q", s.Config.PreviousPublicHostV4)
	}
	if s.Config.SubscriptionBaseURLV4 != "https://203.0.113.44:443/panel" {
		t.Fatalf("literal IPv4 subscription base not updated: %q", s.Config.SubscriptionBaseURLV4)
	}
	if s.Config.SubscriptionBaseURL != "https://vpn.example.com/panel" {
		t.Fatalf("domain subscription base should remain unchanged: %q", s.Config.SubscriptionBaseURL)
	}
	if os.Getenv("ELECTRU_AUTO_PUBLIC_IP") != "0" {
		t.Fatal("manual mode should disable automatic IP adaptation")
	}
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ELECTRU_PUBLIC_HOST_V4=203.0.113.44", "ELECTRU_PREVIOUS_PUBLIC_HOST_V4=198.51.100.10", "ELECTRU_AUTO_PUBLIC_IP=0", "ELECTRU_SUBSCRIPTION_BASE_URL_V4=https://203.0.113.44:443/panel"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("env file missing %q: %s", want, b)
		}
	}
}

func TestSetPublicIPv4RejectsIPv6AndInvalid(t *testing.T) {
	for _, raw := range []string{"2001:db8::1", "not-an-ip", ""} {
		s := &Server{Config: &config.Config{EnvFilePath: filepath.Join(t.TempDir(), "env")}}
		if err := s.SetPublicIPv4(raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}
