package session

import (
	"os"
	"testing"
)

func TestRunAdmissionUsesRealHostMemoryAndSharedLanes(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("host has no proc memory reading")
	}
	lanes := NewTaskLanes()
	gate := NewRunAdmission(0, 1<<40, lanes)
	if gate == nil || gate.MayStart() {
		t.Fatal("real host reading admitted a worker under an impossible memory floor")
	}
	if lanes.running() != 0 {
		t.Fatalf("held admission took %d lanes", lanes.running())
	}
	gate.Started()
	if lanes.running() != 1 {
		t.Fatalf("started worker took %d lanes", lanes.running())
	}
	gate.Returned()
	if lanes.running() != 0 {
		t.Fatalf("returned worker kept %d lanes", lanes.running())
	}
	if off := NewRunAdmission(0, 0, lanes); off != nil {
		t.Fatal("zeroed limits built a gate")
	}
}
