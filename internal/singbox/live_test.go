package singbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestObserveLogLineMapsUserToSource(t *testing.T) {
	ResetLive()
	now := time.Now()
	ObserveLogLine("\x1b[32mINFO\x1b[0m [123 0ms] inbound/hysteria2[hy2-in]: [alice@x] inbound connection from 1.2.3.4:5555", now)
	ObserveLogLine("INFO [1 0ms] inbound/anytls[any-in]: [bob@x] inbound connection from [2001:db8::1]:4000", now)
	ObserveLogLine("INFO [1 0ms] inbound/wireguard[wg]: inbound connection from 9.9.9.9:1", now) // no user
	liveState.Lock()
	defer liveState.Unlock()
	if u := liveState.users[srcKey{"hy2-in", "1.2.3.4"}]; u.user != "alice@x" {
		t.Fatalf("hysteria2 mapping missing: %+v", liveState.users)
	}
	if u := liveState.users[srcKey{"any-in", "2001:db8::1"}]; u.user != "bob@x" {
		t.Fatalf("ipv6 mapping missing: %+v", liveState.users)
	}
	if len(liveState.users) != 2 {
		t.Fatalf("lines without a user must be ignored: %+v", liveState.users)
	}
}

func TestPollLiveCountsDeltasUsersAndDevices(t *testing.T) {
	ResetLive()
	liveState.Lock()
	liveState.counters = make(map[string]*cumulative)
	liveState.users = make(map[srcKey]userSeen)
	liveState.Unlock()

	var conns []map[string]any
	conn := func(id, tag, ip string, up, down int64) map[string]any {
		return map[string]any{"id": id, "upload": up, "download": down,
			"metadata": map[string]any{"type": "hysteria2/" + tag, "sourceIP": ip}}
	}
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": conns})
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	ConfigureLiveAPI(port, "test-seed")

	now := time.Now()
	ObserveLogLine("INFO inbound/hysteria2[hy2]: [alice@x] inbound connection from 1.1.1.1:1000", now)

	conns = []map[string]any{conn("c1", "hy2", "1.1.1.1", 100, 200)}
	snap, err := PollLive(now)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth == "" {
		t.Fatal("clash api secret must be sent")
	}
	if s := snap[InboundKey("hy2")]; s.Up != 0 || s.Down != 0 || s.Conns != 1 {
		t.Fatalf("first poll is a baseline: %+v", s)
	}

	// c1 moved 1000/3000 more, c2 is brand new (500/700) from a second device.
	conns = []map[string]any{conn("c1", "hy2", "1.1.1.1", 1100, 3200), conn("c2", "hy2", "2.2.2.2", 500, 700)}
	snap, err = PollLive(now.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	in := snap[InboundKey("hy2")]
	if in.Up != 1500 || in.Down != 3700 || in.Conns != 2 || len(in.IPs) != 2 {
		t.Fatalf("inbound totals wrong: %+v", in)
	}
	al := snap["alice@x"]
	if al.Up != 1000 || al.Down != 3000 || al.Conns != 1 || len(al.IPs) != 1 {
		t.Fatalf("alice must only get her own connection: %+v", al)
	}
	if _, ok := snap[""]; ok {
		t.Fatal("empty user key leaked")
	}

	// A closed connection must not produce negative deltas.
	conns = []map[string]any{conn("c2", "hy2", "2.2.2.2", 600, 700)}
	snap, _ = PollLive(now.Add(4 * time.Second))
	if in := snap[InboundKey("hy2")]; in.Up != 1600 || in.Down != 3700 || in.Conns != 1 {
		t.Fatalf("after close: %+v", in)
	}
}

func TestExperimentalBlockOnlyWhenEnabled(t *testing.T) {
	ConfigureLiveAPI(0, "x")
	if experimentalBlock() != nil {
		t.Fatal("port 0 must disable the block")
	}
	ConfigureLiveAPI(10086, "x")
	if experimentalBlock() == nil {
		t.Fatal("expected clash_api block")
	}
	SetLiveAPIEnabled(false)
	if LiveAPIEnabled() || experimentalBlock() != nil {
		t.Fatal("SetLiveAPIEnabled(false) must remove the block")
	}
}
