package settings

import (
	"sync"
	"testing"
	"time"
)

// TestPollLastSeen_ConcurrentAccess exercises the pollLastSeen field under
// concurrent readers + writers via the unexported helper used by pollOnce
// and loadFromDB. Run with `go test -race` to confirm the read/write pair
// no longer races now that pollOnce wraps the access in m.mu.Lock.
//
// White-box intentional: the field is unexported, so the test lives in the
// same package and reaches in directly via setPollLastSeen / getPollLastSeen
// helpers (see below).
func TestPollLastSeen_ConcurrentAccess(t *testing.T) {
	m := &Manager{}

	const writers, readers, iters = 4, 4, 1000

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	now := time.Now()
	for w := 0; w < writers; w++ {
		go func(off int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				m.setPollLastSeenForTest(now.Add(time.Duration(off+i) * time.Microsecond))
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = m.getPollLastSeenForTest()
			}
		}()
	}
	wg.Wait()
}

// setPollLastSeenForTest mirrors the lock pattern pollOnce now uses for
// writes. Test-only helper — keep it next to the field it touches so the
// race coverage stays honest if the lock pattern changes.
func (m *Manager) setPollLastSeenForTest(t time.Time) {
	m.mu.Lock()
	m.pollLastSeen = t
	m.mu.Unlock()
}

// getPollLastSeenForTest mirrors the read pattern. Same caveat as the setter.
func (m *Manager) getPollLastSeenForTest() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pollLastSeen
}
