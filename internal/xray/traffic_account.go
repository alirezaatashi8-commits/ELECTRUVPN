package xray

// EffectiveTrafficInput is the panel-side state used to derive billed usage
// from cumulative xray counters (which reset when the process restarts).
type EffectiveTrafficInput struct {
	Up, Down                       int64
	TrafficBaseUp, TrafficBaseDown int64
	AggregateUp, AggregateDown     int64
	// SubjectPresent is true when at least one of this client's traffic
	// subjects appears in the current stats payload (not a partial/empty poll).
	SubjectPresent bool
	// StatsEmpty means the whole stats map had zero entries — incomplete poll.
	StatsEmpty bool
	// XrayRestarted is true when the process start time/pid changed since last sync.
	XrayRestarted bool
}

// EffectiveTrafficResult is the updated panel counters after one sync step.
type EffectiveTrafficResult struct {
	Up, Down                       int64
	TrafficBaseUp, TrafficBaseDown int64
	Skip                           bool // do not write to DB
}

// ComputeEffectiveTraffic folds xray aggregates into panel Up/Down using TrafficBase.
// Rules:
//  1. Empty stats or missing client subjects → skip (avoid false "restart").
//  2. Explicit process restart → base = -previous effective so usage is preserved.
//  3. Otherwise aggregate decrease vs lastAbs is treated as restart only when
//     subjects are present (not on partial polls).
func ComputeEffectiveTraffic(in EffectiveTrafficInput) EffectiveTrafficResult {
	if in.StatsEmpty || !in.SubjectPresent {
		return EffectiveTrafficResult{Skip: true}
	}

	baseUp, baseDown := in.TrafficBaseUp, in.TrafficBaseDown
	lastUp := in.Up + in.TrafficBaseUp
	lastDown := in.Down + in.TrafficBaseDown

	restart := in.XrayRestarted
	if !restart {
		// Only trust counter regression when we actually saw this client's stats.
		if in.AggregateUp < lastUp || in.AggregateDown < lastDown {
			restart = true
		}
	}
	if restart {
		baseUp = -in.Up
		baseDown = -in.Down
	}

	effUp := in.AggregateUp - baseUp
	effDown := in.AggregateDown - baseDown
	if effUp < 0 {
		effUp = 0
	}
	if effDown < 0 {
		effDown = 0
	}
	return EffectiveTrafficResult{
		Up: effUp, Down: effDown,
		TrafficBaseUp: baseUp, TrafficBaseDown: baseDown,
	}
}

// SubjectPresentInStats reports whether any subject key exists in the stats map.
func SubjectPresentInStats(stats map[string]*Traffic, subjects ...string) bool {
	for _, s := range subjects {
		if s == "" {
			continue
		}
		if stats[s] != nil {
			return true
		}
	}
	return false
}
