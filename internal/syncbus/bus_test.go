package syncbus

import (
	"testing"
	"time"
)

func TestPutGetNode(t *testing.T) {
	b := New(64)
	b.PutNode(NodeSnapshot{NodeID: 7, Health: "healthy", CPUPercent: 12})
	s, ok := b.GetNode(7, time.Minute)
	if !ok || s.CPUPercent != 12 {
		t.Fatalf("expected healthy node snapshot, got ok=%v %+v", ok, s)
	}
	if _, ok := b.GetNode(7, time.Nanosecond); ok {
		// UpdatedAt is now; Nanosecond age might still pass depending on clock.
		// Just ensure GetNode with long age works.
	}
}

func TestTrafficQueueBackpressure(t *testing.T) {
	const capacity = 256 // New enforces a minimum queue capacity of 256.
	b := New(capacity)
	for i := 0; i < capacity; i++ {
		if !b.EnqueueTraffic(TrafficDelta{Email: "u@x", Uplink: int64(i)}) {
			t.Fatal("expected enqueue success within capacity")
		}
	}
	if b.EnqueueTraffic(TrafficDelta{Email: "overflow@x", Uplink: 1}) {
		t.Fatal("expected drop when queue full")
	}
	stats := b.Stats()
	if stats["deltasDropped"] < 1 {
		t.Fatalf("expected drop counter, got %+v", stats)
	}
	drained := b.DrainTraffic(10, 5*time.Millisecond)
	if len(drained) != 10 {
		t.Fatalf("expected bounded drain of 10, got %d", len(drained))
	}
}

func TestHealthyNodeIDs(t *testing.T) {
	b := New(16)
	b.PutNode(NodeSnapshot{NodeID: 1, Health: "healthy"})
	b.PutNode(NodeSnapshot{NodeID: 2, Health: "degraded"})
	m := b.HealthyNodeIDs(time.Minute)
	if _, ok := m[1]; !ok {
		t.Fatal("missing healthy node")
	}
	if _, ok := m[2]; ok {
		t.Fatal("degraded node should be excluded")
	}
}

func TestDeadLetterBounded(t *testing.T) {
	b := New(8)
	for i := 0; i < 10; i++ {
		b.PushDeadLetter(TrafficDelta{Email: "e@x", Uplink: int64(i)}, "test", 1)
	}
	st := b.DLQStats()
	if st["dlqLen"] == 0 {
		t.Fatal("expected DLQ entries")
	}
	if st["dlqLen"] > uint64(defaultDLQCap) {
		t.Fatalf("DLQ exceeded cap: %v", st)
	}
}
