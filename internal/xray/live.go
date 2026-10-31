package xray

import (
	"sync"
	"time"
)

type LiveSample struct {
	UplinkBytes   int64     `json:"uplinkBytes"`
	DownlinkBytes int64     `json:"downlinkBytes"`
	UplinkSpeed   float64   `json:"uplinkSpeed"`
	DownlinkSpeed float64   `json:"downlinkSpeed"`
	Online        bool      `json:"online"`
	SampledAt     time.Time `json:"sampledAt"`
	// Devices is the number of distinct source IPs seen for this subject
	// within liveDeviceWindow (one phone/PC usually = one IP). Connections is the
	// number of open connections when the engine can report it (sing-box).
	Devices     int `json:"devices"`
	Connections int `json:"connections"`
}

type liveEntry struct {
	traffic  *Traffic
	at       time.Time
	sample   LiveSample
	activeAt time.Time
}

const (
	liveEntryTTL     = 5 * time.Minute
	liveOnlineGrace  = 15 * time.Second
	liveMinSampleGap = time.Second
)

var liveTracker = struct {
	sync.RWMutex
	entries map[string]liveEntry
}{entries: make(map[string]liveEntry)}

// UpdateLiveSamples records a fresh statsquery result and calculates byte/sec
// from the previous sample. "Online" intentionally means recent traffic;
// Xray's StatsService exposes counters, not an authoritative active-session
// list, so an idle TCP connection cannot safely be called online.
//
// Several handlers (dashboard, online users, client live view) feed this
// tracker. Two samples taken less than liveMinSampleGap apart usually carry the
// same cached counters, so they are ignored instead of being read as "no
// traffic". A subject stays online for liveOnlineGrace after its counters last
// moved, which keeps the online state from flickering between polls.
func UpdateLiveSamples(stats map[string]*Traffic, now time.Time) {
	liveTracker.Lock()
	defer liveTracker.Unlock()
	// StatsService may stop returning a subject after an inbound/client is
	// removed. Prune old entries so the process cannot retain an unbounded map.
	for subject, entry := range liveTracker.entries {
		if now.Sub(entry.at) > liveEntryTTL {
			delete(liveTracker.entries, subject)
		}
	}
	for subject, t := range stats {
		if t == nil {
			continue
		}
		prev, ok := liveTracker.entries[subject]
		sample := LiveSample{UplinkBytes: t.Uplink, DownlinkBytes: t.Downlink, SampledAt: now}
		var activeAt time.Time
		if ok {
			activeAt = prev.activeAt
		}
		if ok && prev.traffic != nil && now.After(prev.at) {
			elapsed := now.Sub(prev.at)
			if elapsed < liveMinSampleGap {
				// Same counter window sampled again; keep the previous rates.
				continue
			}
			seconds := elapsed.Seconds()
			up := float64(t.Uplink - prev.traffic.Uplink)
			down := float64(t.Downlink - prev.traffic.Downlink)
			if up >= 0 {
				sample.UplinkSpeed = up / seconds
			}
			if down >= 0 {
				sample.DownlinkSpeed = down / seconds
			}
			if up > 0 || down > 0 {
				activeAt = now
			}
		}
		sample.Online = !activeAt.IsZero() && now.Sub(activeAt) <= liveOnlineGrace
		liveTracker.entries[subject] = liveEntry{traffic: &Traffic{Uplink: t.Uplink, Downlink: t.Downlink}, at: now, sample: sample, activeAt: activeAt}
	}
}

func LiveSampleFor(subject string) (LiveSample, bool) {
	liveTracker.RLock()
	defer liveTracker.RUnlock()
	e, ok := liveTracker.entries[subject]
	if !ok {
		return LiveSample{}, false
	}
	s := e.sample
	s.Online = !e.activeAt.IsZero() && time.Since(e.activeAt) <= liveOnlineGrace
	if time.Since(e.at) > 30*time.Second {
		s.Online = false
		s.UplinkSpeed = 0
		s.DownlinkSpeed = 0
	}
	return s, true
}

// LiveSampleForSubjects merges the live samples of one logical client: its
// primary email plus any profile aliases ("email#profile-N"). Speeds and byte
// counters are summed and the client is online if any subject is online or has
// a device connected right now.
func LiveSampleForSubjects(subjects ...string) (LiveSample, bool) {
	var out LiveSample
	found := false
	seen := make(map[string]struct{}, len(subjects))
	uniq := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		if _, dup := seen[subject]; dup {
			continue
		}
		seen[subject] = struct{}{}
		uniq = append(uniq, subject)
		sample, ok := LiveSampleFor(subject)
		if !ok {
			continue
		}
		found = true
		out.UplinkBytes += sample.UplinkBytes
		out.DownlinkBytes += sample.DownlinkBytes
		out.UplinkSpeed += sample.UplinkSpeed
		out.DownlinkSpeed += sample.DownlinkSpeed
		out.Online = out.Online || sample.Online
		if sample.SampledAt.After(out.SampledAt) {
			out.SampledAt = sample.SampledAt
		}
	}
	out.Devices, out.Connections = deviceStats(uniq, time.Now())
	if out.Devices > 0 || out.Connections > 0 {
		out.Online = true
		found = true
	}
	return out, found
}

// HasLiveSamples reports whether the tracker has been fed at least once. A cold
// tracker cannot know who is online because speeds need two samples.
func HasLiveSamples() bool {
	liveTracker.RLock()
	defer liveTracker.RUnlock()
	return len(liveTracker.entries) > 0
}

// ResetLiveSample drops the previous sample for one subject so a scoped
// traffic reset cannot produce a false negative/zero speed calculation.
func ResetLiveSample(subject string) {
	liveTracker.Lock()
	defer liveTracker.Unlock()
	delete(liveTracker.entries, subject)
}

// ResetAllLiveSamples drops all previous samples after a global counter reset.
func ResetAllLiveSamples() {
	liveTracker.Lock()
	defer liveTracker.Unlock()
	liveTracker.entries = make(map[string]liveEntry)
}
