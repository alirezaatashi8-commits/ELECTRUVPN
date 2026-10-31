package models

import "math"

const bytesPerGB int64 = 1024 * 1024 * 1024

// quotaReached compares usage against a byte or GiB limit without allowing
// corrupt counters or unusually large configured limits to overflow int64.
// A non-positive limit means unlimited.
func quotaReached(up, down, totalGB, limitBytes int64) bool {
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	used := int64(math.MaxInt64)
	if up <= math.MaxInt64-down {
		used = up + down
	}
	if limitBytes > 0 {
		return used >= limitBytes
	}
	if totalGB <= 0 || totalGB > math.MaxInt64/bytesPerGB {
		return false
	}
	return used >= totalGB*bytesPerGB
}
