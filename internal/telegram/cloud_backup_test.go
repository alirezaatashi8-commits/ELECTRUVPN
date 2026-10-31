package telegram

import (
	"strings"
	"testing"
)

func TestGenerateBackupCodeFormat(t *testing.T) {
	code, err := generateBackupCode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "EV-") {
		t.Fatalf("unexpected code %s", code)
	}
	h1 := hashBackupCode(code)
	h2 := hashBackupCode(strings.ToLower(code))
	if h1 != h2 {
		t.Fatal("hash should be case-insensitive")
	}
}

func TestNormalizeBackupCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"EV-AB12-CD34EF", "EV-AB12-CD34EF"},
		{"ev-ab12-cd34ef", "EV-AB12-CD34EF"},
		{"EV AB12 CD34EF", "EV-AB12-CD34EF"},
		{"evab12cd34ef", "EV-AB12-CD34EF"},
		{"  EV-AB12-CD34EF  ", "EV-AB12-CD34EF"},
	}
	for _, c := range cases {
		got := normalizeBackupCode(c.in)
		if got != c.want {
			t.Fatalf("normalize(%q)=%q want %q", c.in, got, c.want)
		}
	}
	// generated codes round-trip
	code, err := generateBackupCode()
	if err != nil {
		t.Fatal(err)
	}
	if normalizeBackupCode(strings.ReplaceAll(code, "-", "")) != code {
		t.Fatalf("strip-hyphen normalize failed for %s", code)
	}
}
