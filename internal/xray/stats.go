package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Traffic is a single up/down counter pair, keyed by whatever xray tag
// produced it (an inbound tag, or "user>>>email>>>traffic>>>uplink" style
// per-client keys).
type Traffic struct {
	Uplink   int64
	Downlink int64
}

var statNameRe = regexp.MustCompile(`^(inbound|user)>>>(.+?)>>>traffic>>>(uplink|downlink)$`)

// statsPollCache coalesces simultaneous live/API requests. The panel can
// request the same Xray counters from the inbound live view, client live view,
// and background accounting at nearly the same time. A very short cache keeps
// those reads from spawning redundant xray CLI processes while preserving the
// existing UI polling cadence. Reset reads always bypass and invalidate it.
var statsPollMu sync.Mutex
var statsPollCachedAt time.Time
var statsPollCacheKey string
var statsPollCache map[string]*Traffic

const (
	statsPollCacheTTL   = 1500 * time.Millisecond
	statsCommandTimeout = 4 * time.Second
)

// PollStats shells out to `xray api statsquery`, which talks to the
// StatsService gRPC endpoint xray exposes on apiPort (see config.go's
// dedicated "api" inbound). This avoids vendoring xray-core's protobuf
// stubs just to read two counters per client.
//
// Returns a map of subject ("inbound tag" or "client email") to Traffic.
// reset controls whether xray zeroes the counters after reading them
// (matching the panel's own "reset" traffic buttons).
func PollStats(binaryPath string, apiPort int, reset bool) (map[string]*Traffic, error) {
	key := fmt.Sprintf("%s:%d", binaryPath, apiPort)
	statsPollMu.Lock()
	defer statsPollMu.Unlock()

	if !reset && statsPollCache != nil && statsPollCacheKey == key && time.Since(statsPollCachedAt) < statsPollCacheTTL {
		return cloneTrafficMap(statsPollCache), nil
	}

	args := []string{"api", "statsquery", "-server", fmt.Sprintf("127.0.0.1:%d", apiPort)}
	if reset {
		args = append(args, "-reset")
	}

	ctx, cancel := context.WithTimeout(context.Background(), statsCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if reset {
			statsPollCache = nil
			statsPollCachedAt = time.Time{}
		}
		return nil, fmt.Errorf("xray api statsquery failed: %w", err)
	}
	stats, err := parseStatsOutput(out)
	if err != nil {
		if reset {
			statsPollCache = nil
			statsPollCachedAt = time.Time{}
		}
		return nil, err
	}
	if reset {
		// Reset changes Xray's counters; never let a previous snapshot survive.
		statsPollCache = nil
		statsPollCachedAt = time.Time{}
		statsPollCacheKey = ""
		return stats, nil
	}
	statsPollCacheKey = key
	statsPollCachedAt = time.Now()
	statsPollCache = cloneTrafficMap(stats)
	// The parsed map is owned by this call; the cache has its own deep copy.
	// Returning it directly avoids a second allocation/copy of every counter.
	return stats, nil
}

func cloneTrafficMap(in map[string]*Traffic) map[string]*Traffic {
	out := make(map[string]*Traffic, len(in))
	for key, value := range in {
		if value == nil {
			continue
		}
		copyValue := *value
		out[key] = &copyValue
	}
	return out
}

// parseStatsOutput accepts both string and numeric JSON values. Xray builds
// have emitted both forms for StatsService counters; decoding directly into a
// string silently made PollStats fail on numeric values, which in turn left
// live counters and quota accounting stale.
func parseStatsOutput(data []byte) (map[string]*Traffic, error) {
	var parsed struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("unexpected statsquery output: %w", err)
	}

	results := make(map[string]*Traffic)
	for _, stat := range parsed.Stat {
		m := statNameRe.FindStringSubmatch(strings.TrimSpace(stat.Name))
		if m == nil {
			continue
		}
		raw := strings.TrimSpace(string(stat.Value))
		if raw == "" || raw == "null" {
			continue
		}
		var valueText string
		if len(raw) > 0 && raw[0] == '"' {
			if err := json.Unmarshal(stat.Value, &valueText); err != nil {
				continue
			}
		} else {
			valueText = raw
		}
		value, err := strconv.ParseInt(strings.TrimSpace(valueText), 10, 64)
		if err != nil || value < 0 {
			continue
		}

		subject, direction := m[2], m[3]
		if results[subject] == nil {
			results[subject] = &Traffic{}
		}
		if direction == "uplink" {
			results[subject].Uplink = value
		} else {
			results[subject].Downlink = value
		}
	}
	return results, nil
}

// SumTraffic aggregates counters for one logical client across its primary
// email and any profile aliases. Xray reports per-user counters by email;
// profile aliases therefore need to be folded back into the owning Client so
// quota, expiry and dashboard accounting remain consistent.
func SumTraffic(stats map[string]*Traffic, subjects ...string) Traffic {
	seen := make(map[string]struct{}, len(subjects))
	var out Traffic
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		if t := stats[subject]; t != nil {
			out.Uplink += t.Uplink
			out.Downlink += t.Downlink
		}
	}
	return out
}
