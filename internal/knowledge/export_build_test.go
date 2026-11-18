package knowledge

import (
	"testing"
	"time"
)

// SetBuildTimingForTest shortens the build loop's waits and puts its idle
// window on a clock the test holds (build_idle.go), restoring all of them when
// the test ends.
func SetBuildTimingForTest(t testing.TB, every, quietMax, idle time.Duration, now func() time.Time) {
	t.Helper()
	oldEvery, oldMax, oldIdle, oldNow, oldBatch := buildEvery, buildQuietMax, buildIdle, buildNow, buildBatchMax
	buildEvery, buildQuietMax, buildIdle, buildNow = every, quietMax, idle, now
	t.Cleanup(func() {
		buildEvery, buildQuietMax, buildIdle, buildNow, buildBatchMax = oldEvery, oldMax, oldIdle, oldNow, oldBatch
	})
}

// SetBuildBatchForTest sets how long a quiet chat waits for the busy ones
// (build_idle.go); SetBuildTimingForTest restores it.
func SetBuildBatchForTest(max time.Duration) {
	buildBatchMax = max
}
