package xray

import "testing"

func TestLiveApplyEnabled(t *testing.T) {
	cases := map[string]bool{
		"true":  true,
		"1":     true,
		"yes":   true,
		"on":    true,
		"TRUE":  true,
		"false": false,
		"":      false,
		"0":     false,
		"no":    false,
	}
	for in, want := range cases {
		got := LiveApplyEnabled(func(string) string { return in })
		if got != want {
			t.Fatalf("LiveApplyEnabled(%q)=%v want %v", in, got, want)
		}
	}
}
