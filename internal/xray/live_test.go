package xray

import (
	"testing"
	"time"
)

func TestUpdateLiveSamplesPrunesStaleEntries(t *testing.T) {
	ResetAllLiveSamples()
	t0 := time.Unix(1000, 0)
	UpdateLiveSamples(map[string]*Traffic{"old": {Uplink: 10, Downlink: 20}}, t0)
	UpdateLiveSamples(map[string]*Traffic{"new": {Uplink: 1, Downlink: 2}}, t0.Add(6*time.Minute))
	if _, ok := LiveSampleFor("old"); ok {
		t.Fatal("expected stale live entry to be pruned")
	}
	if _, ok := LiveSampleFor("new"); !ok {
		t.Fatal("expected fresh live entry to remain")
	}
}

func TestLiveOnlineSurvivesImmediateResample(t *testing.T) {
	ResetAllLiveSamples()
	t0 := time.Now()
	UpdateLiveSamples(map[string]*Traffic{"u": {Uplink: 0, Downlink: 0}}, t0.Add(-10*time.Second))
	UpdateLiveSamples(map[string]*Traffic{"u": {Uplink: 100, Downlink: 1000}}, t0.Add(-5*time.Second))
	// A second handler reads the same cached counters half a second later.
	UpdateLiveSamples(map[string]*Traffic{"u": {Uplink: 100, Downlink: 1000}}, t0.Add(-4500*time.Millisecond))
	s, ok := LiveSampleFor("u")
	if !ok {
		t.Fatal("expected live sample")
	}
	if !s.Online {
		t.Fatal("expected subject to stay online after an immediate re-sample")
	}
	if s.DownlinkSpeed != 200 {
		t.Fatalf("expected 200 B/s, got %v", s.DownlinkSpeed)
	}
}

func TestLiveSampleForSubjectsMergesAliases(t *testing.T) {
	ResetAllLiveSamples()
	t0 := time.Now()
	first := map[string]*Traffic{"a@x": {Uplink: 0, Downlink: 0}, "a@x#profile-1": {Uplink: 0, Downlink: 0}}
	second := map[string]*Traffic{"a@x": {Uplink: 0, Downlink: 0}, "a@x#profile-1": {Uplink: 50, Downlink: 500}}
	UpdateLiveSamples(first, t0.Add(-10*time.Second))
	UpdateLiveSamples(second, t0.Add(-5*time.Second))
	s, ok := LiveSampleForSubjects("a@x", "a@x#profile-1", "a@x")
	if !ok || !s.Online {
		t.Fatal("expected merged sample to be online through the alias subject")
	}
	if s.DownlinkSpeed != 100 {
		t.Fatalf("expected 100 B/s, got %v", s.DownlinkSpeed)
	}
}
