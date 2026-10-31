package automation

import (
	"testing"
	"time"
)

func TestNextSchedule(t *testing.T) {
	n := time.Unix(100, 0)
	got := nextSchedule("@every 5m", n)
	if got == nil || !got.Equal(n.Add(5*time.Minute)) {
		t.Fatalf("unexpected next: %v", got)
	}
	if nextSchedule("manual", n) != nil {
		t.Fatal("manual schedule should not auto-run")
	}
}
func TestEvaluateRuleEmptyCondition(t *testing.T) {
	ok, msg := evaluateRule(nil, "always", "", time.Now())
	if !ok || msg != "" {
		t.Fatalf("got %v %q", ok, msg)
	}
}
