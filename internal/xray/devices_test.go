package xray

import (
	"sort"
	"testing"
	"time"
)

func resetDevices() {
	deviceTracker.Lock()
	deviceTracker.ips = make(map[string]map[string]time.Time)
	deviceTracker.conns = make(map[string]connCount)
	deviceTracker.Unlock()
}

func TestParseOnlineIPsHandlesNestedAndEmpty(t *testing.T) {
	got := parseOnlineIPs([]byte(`{"name":"user>>>a@x>>>online","ips":{"1.2.3.4":1730000000,"5.6.7.8":1730000001}}`))
	sort.Strings(got)
	if len(got) != 2 || got[0] != "1.2.3.4" || got[1] != "5.6.7.8" {
		t.Fatalf("unexpected ips: %#v", got)
	}
	nested := parseOnlineIPs([]byte(`{"stat":{"Ips":{"9.9.9.9":1}}}`))
	if len(nested) != 1 || nested[0] != "9.9.9.9" {
		t.Fatalf("nested ips not found: %#v", nested)
	}
	if got := parseOnlineIPs([]byte(`{}`)); len(got) != 0 {
		t.Fatalf("expected none, got %#v", got)
	}
	if got := parseOnlineIPs([]byte(`not json`)); got != nil {
		t.Fatalf("expected nil for bad json, got %#v", got)
	}
}

func TestDevicesCountDistinctIPsAndMakeClientOnline(t *testing.T) {
	ResetAllLiveSamples()
	resetDevices()
	now := time.Now()
	RecordDeviceIPs("a@x", []string{"1.1.1.1", "2.2.2.2"}, now)
	RecordDeviceIPs("a@x#profile-1", []string{"2.2.2.2", "3.3.3.3"}, now)
	s, ok := LiveSampleForSubjects("a@x", "a@x#profile-1")
	if !ok || s.Devices != 3 {
		t.Fatalf("want 3 distinct devices, got ok=%v %+v", ok, s)
	}
	if !s.Online {
		t.Fatal("a connected device must make the client online even without a counter delta")
	}
	if n := DeviceCount("a@x", "a@x#profile-1"); n != 3 {
		t.Fatalf("DeviceCount = %d", n)
	}
}

func TestDevicesExpireAfterWindow(t *testing.T) {
	resetDevices()
	now := time.Now()
	RecordDeviceIPs("b@x", []string{"1.1.1.1"}, now.Add(-liveDeviceWindow-time.Second))
	RecordConnections("b@x", 2, now.Add(-liveConnTTL-time.Second))
	d, c := deviceStats([]string{"b@x"}, now)
	if d != 0 || c != 0 {
		t.Fatalf("stale data must not count: devices=%d conns=%d", d, c)
	}
	RecordConnections("b@x", 2, now)
	if _, c := deviceStats([]string{"b@x"}, now); c != 2 {
		t.Fatalf("fresh connections = %d", c)
	}
	RecordConnections("b@x", 0, now)
	if _, c := deviceStats([]string{"b@x"}, now); c != 0 {
		t.Fatalf("zero connections must clear, got %d", c)
	}
}
