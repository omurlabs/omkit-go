// quota_time.go — quota_time module.
//
// exports: none
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

package quota

import "time"

// nowUTC is the package clock. Tests in the same package may reassign it directly.
// No public setter — avoids race conditions under go test -race.
var nowUTC = func() time.Time { return time.Now().UTC() }

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
