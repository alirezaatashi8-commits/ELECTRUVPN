package xray

import "testing"

func TestSumTrafficIncludesProfileAliases(t *testing.T) {
	stats := map[string]*Traffic{
		"alice":            {Uplink: 100, Downlink: 200},
		"alice#profile-42": {Uplink: 30, Downlink: 40},
	}
	got := SumTraffic(stats, "alice", "alice#profile-42", "alice#profile-42")
	if got.Uplink != 130 || got.Downlink != 240 {
		t.Fatalf("unexpected aggregate: %+v", got)
	}
}

func TestParseStatsOutputAcceptsStringAndNumericCounters(t *testing.T) {
	input := []byte(`{"stat":[
		{"name":"user>>>alice>>>traffic>>>uplink","value":"123"},
		{"name":"user>>>alice>>>traffic>>>downlink","value":456},
		{"name":"user>>>bob>>>traffic>>>uplink","value":789},
		{"name":"inbound>>>main>>>traffic>>>downlink","value":"1000"}
	]}`)
	got, err := parseStatsOutput(input)
	if err != nil {
		t.Fatalf("parseStatsOutput returned error: %v", err)
	}
	if got["alice"] == nil || got["alice"].Uplink != 123 || got["alice"].Downlink != 456 {
		t.Fatalf("alice counters not parsed: %#v", got["alice"])
	}
	if got["bob"] == nil || got["bob"].Uplink != 789 {
		t.Fatalf("bob counters not parsed: %#v", got["bob"])
	}
	if got["main"] == nil || got["main"].Downlink != 1000 {
		t.Fatalf("inbound counters not parsed: %#v", got["main"])
	}
}

func TestParseStatsOutputSkipsInvalidCounterValues(t *testing.T) {
	input := []byte(`{"stat":[
		{"name":"user>>>alice>>>traffic>>>uplink","value":"not-a-number"},
		{"name":"user>>>alice>>>traffic>>>downlink","value":-2},
		{"name":"user>>>alice>>>traffic>>>uplink","value":null}
	]}`)
	got, err := parseStatsOutput(input)
	if err != nil {
		t.Fatalf("parseStatsOutput returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected invalid counters to be skipped, got %#v", got)
	}
}
