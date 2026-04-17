package quota

import "time"

// nowUTC is a replaceable clock used by secondsUntilNextMonth.
// Tests inject a fake via SetNowUTC; production code always uses time.Now().UTC().
var nowUTC = func() time.Time { return time.Now().UTC() }

// SetNowUTC replaces the clock for tests. Pass nil to restore the default.
func SetNowUTC(fn func() time.Time) {
	if fn == nil {
		nowUTC = func() time.Time { return time.Now().UTC() }
	} else {
		nowUTC = fn
	}
}

func firstOfNextMonth() time.Time {
	n := nowUTC()
	return time.Date(n.Year(), n.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func capAt32Days(s int) int {
	const max = 32 * 24 * 3600
	if s < 0 {
		return 60
	}
	if s > max {
		return max
	}
	return s
}
