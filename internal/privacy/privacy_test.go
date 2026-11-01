package privacy

import "testing"

func TestIdentifierHashIsStableAndNonRaw(t *testing.T) {
	a := IdentifierHash("203.0.113.9", "secret")
	b := IdentifierHash("203.0.113.9", "secret")
	c := IdentifierHash("203.0.113.10", "secret")
	if a == "" || a != b || a == c {
		t.Fatal("unexpected keyed hash behavior")
	}
	if a == "203.0.113.9" {
		t.Fatal("raw IP leaked into hash field")
	}
	if got := NormalizeIP("203.0.113.9:443"); got != "203.0.113.9" {
		t.Fatalf("normalize IP: %q", got)
	}
}
