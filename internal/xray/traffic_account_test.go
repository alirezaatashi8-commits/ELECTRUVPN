package xray

import "testing"

func TestComputeEffectiveTraffic_SkipEmptyOrMissing(t *testing.T) {
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 100, AggregateUp: 0, StatsEmpty: true, SubjectPresent: false,
	})
	if !r.Skip {
		t.Fatal("expected skip on empty stats")
	}
	r = ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 100, AggregateUp: 0, StatsEmpty: false, SubjectPresent: false,
	})
	if !r.Skip {
		t.Fatal("expected skip when subject missing")
	}
}

func TestComputeEffectiveTraffic_BaseZeroRestart(t *testing.T) {
	// User with base=0, up=500; after restart agg=0 — must preserve 500 via negative base.
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 500, Down: 10, TrafficBaseUp: 0, TrafficBaseDown: 0,
		AggregateUp: 0, AggregateDown: 0,
		SubjectPresent: true, StatsEmpty: false,
	})
	if r.Skip {
		t.Fatal("should not skip")
	}
	if r.Up != 500 || r.Down != 10 {
		t.Fatalf("usage lost: up=%d down=%d", r.Up, r.Down)
	}
	if r.TrafficBaseUp != -500 || r.TrafficBaseDown != -10 {
		t.Fatalf("bad base: %d %d", r.TrafficBaseUp, r.TrafficBaseDown)
	}
}

func TestComputeEffectiveTraffic_PositiveBaseRestart(t *testing.T) {
	// effective 50 with base 1000 (abs was 1050); restart agg=0
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 50, Down: 20, TrafficBaseUp: 1000, TrafficBaseDown: 200,
		AggregateUp: 0, AggregateDown: 0,
		SubjectPresent: true,
	})
	if r.Up != 50 || r.Down != 20 {
		t.Fatalf("got up=%d down=%d", r.Up, r.Down)
	}
}

func TestComputeEffectiveTraffic_SecondRestart(t *testing.T) {
	// After first restart: up=50, base=-50; grow to up=80 with agg=30
	// Second restart agg=0 again
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 80, Down: 5, TrafficBaseUp: -50, TrafficBaseDown: -5,
		AggregateUp: 0, AggregateDown: 0,
		SubjectPresent: true,
	})
	if r.Up != 80 {
		t.Fatalf("second restart lost usage: %d", r.Up)
	}
}

func TestComputeEffectiveTraffic_ExplicitRestartFlag(t *testing.T) {
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 10, TrafficBaseUp: 0, AggregateUp: 100, // agg grew but process restarted
		SubjectPresent: true, XrayRestarted: true,
	})
	// With explicit restart, base=-10, effective = 100 - (-10) = 110
	if r.Up != 110 {
		t.Fatalf("want 110 got %d", r.Up)
	}
}

func TestComputeEffectiveTraffic_NormalGrowth(t *testing.T) {
	r := ComputeEffectiveTraffic(EffectiveTrafficInput{
		Up: 10, TrafficBaseUp: 100, AggregateUp: 150,
		SubjectPresent: true,
	})
	if r.Up != 50 { // 150-100
		t.Fatalf("got %d", r.Up)
	}
	if r.TrafficBaseUp != 100 {
		t.Fatalf("base should stay 100, got %d", r.TrafficBaseUp)
	}
}
