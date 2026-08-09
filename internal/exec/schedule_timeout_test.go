package exec

import (
	"testing"
	"time"
)

// The watchdog wears the worker's size. A flat fifteen-minute NodeTimeout
// abandoned a coding pipeline at seventeen minutes with a verification pass in
// hand; the fix is that a flight's timeout is shaped to its worker's budget
// floor, while the generalist's stays exactly as configured.
func TestWatchdogWearsTheWorkersSize(t *testing.T) {
	defer ForgetSubharnesses()
	RegisterSubharness(SubharnessInfo{
		Name:          "hourling",
		Purpose:       "a worker whose floor is an hour",
		DeadlineFloor: time.Hour,
	})

	scheduler := &Scheduler{NodeTimeout: 17 * time.Minute}

	if got := scheduler.timeoutFor(Task{}); got != 17*time.Minute {
		t.Fatalf("generalist watchdog moved: %s", got)
	}
	if got := scheduler.timeoutFor(Task{Subharness: "hourling"}); got != time.Hour+2*time.Minute {
		t.Fatalf("specialist watchdog not shaped to its floor: %s", got)
	}
	if got := scheduler.timeoutFor(Task{Subharness: "nobody-registered"}); got != 17*time.Minute {
		t.Fatalf("unknown worker should keep the configured watchdog: %s", got)
	}

	// A specialist whose floor sits under the configured watchdog keeps the
	// configured one — shaping only ever loosens, never tightens.
	RegisterSubharness(SubharnessInfo{Name: "quickling", Purpose: "small", DeadlineFloor: time.Minute})
	if got := scheduler.timeoutFor(Task{Subharness: "quickling"}); got != 17*time.Minute {
		t.Fatalf("shaping tightened the watchdog: %s", got)
	}

	// Zero stays disabled for everyone.
	off := &Scheduler{}
	if got := off.timeoutFor(Task{Subharness: "hourling"}); got != 0 {
		t.Fatalf("a disabled watchdog grew a timeout: %s", got)
	}
}
