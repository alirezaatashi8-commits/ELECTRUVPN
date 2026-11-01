package auth

import (
	"testing"
	"time"
)

func TestRequestLimiterBounded(t *testing.T) {
	r := NewRequestLimiter(1, time.Minute)
	for i := 0; i < 12000; i++ {
		r.Allow(string(rune(i%256)) + "-" + time.Now().String())
	}
	if len(r.buckets) > 10000 {
		t.Fatalf("buckets grew to %d", len(r.buckets))
	}
}
func TestLoginGuardBounded(t *testing.T) {
	g := NewLoginGuard(5, time.Minute)
	for i := 0; i < 12000; i++ {
		g.RecordFailure(string(rune(i%256)) + "-" + time.Now().String())
	}
	if len(g.attempts) > 10000 || len(g.lastSeen) > 10000 {
		t.Fatalf("guard maps grew: %d/%d", len(g.attempts), len(g.lastSeen))
	}
}
