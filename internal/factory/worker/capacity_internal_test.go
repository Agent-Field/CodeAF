package worker

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// TestCapacityLimitsAndRetirement pins the shared bound and the retire rule:
// a slot above the limit leaves, one below stays, and a raise that lands
// first keeps the slot.
func TestCapacityLimitsAndRetirement(t *testing.T) {
	if clampSlots(0) != api.DefaultRunnerSlots || clampSlots(api.MaxRunnerSlots+50) != api.MaxRunnerSlots || clampSlots(3) != 3 {
		t.Fatal("clampSlots does not use the shared bounds")
	}
	c := &capacity{limit: 3, running: map[int]bool{0: true, 1: true, 2: true}, wake: make(chan struct{}, 1)}
	c.set(1)
	select {
	case <-c.wake:
	default:
		t.Fatal("a change did not wake the supervisor")
	}
	c.set(1)
	select {
	case <-c.wake:
		t.Fatal("an unchanged limit woke the supervisor")
	default:
	}
	if c.retire(0) || !c.retire(2) || !c.retire(1) || len(c.running) != 1 {
		t.Fatalf("retire: %+v", c.running)
	}
	c.set(api.MaxRunnerSlots * 4)
	if c.limit != api.MaxRunnerSlots {
		t.Fatalf("limit = %d, want the ceiling", c.limit)
	}
	c.set(-5)
	if c.limit != 1 {
		t.Fatalf("limit = %d, want at least one slot", c.limit)
	}
}
