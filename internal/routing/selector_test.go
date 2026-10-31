package routing

import "testing"

func TestSortCandidates(t *testing.T) {
	list := []Candidate{
		{Name: "b", Score: 10, Priority: 2},
		{Name: "a", Score: 50, Priority: 1},
		{Name: "c", Score: 50, Priority: 0},
	}
	sortCandidates(list, "health")
	if list[0].Name != "c" {
		t.Fatalf("expected c first (same score, lower priority), got %s", list[0].Name)
	}
}

func TestHostForCandidate(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.2.3.4:443", "1.2.3.4"},
		{"node.example.com", "node.example.com"},
		{"https://vpn.example.com/path", "vpn.example.com"},
		{"[2001:db8::1]:8443", "[2001:db8::1]"},
	}
	for _, tc := range cases {
		got := HostForCandidate(Candidate{Address: tc.in})
		if got != tc.want {
			t.Fatalf("%q => %q, want %q", tc.in, got, tc.want)
		}
	}
}
