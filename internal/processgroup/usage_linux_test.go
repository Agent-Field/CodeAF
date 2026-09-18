//go:build linux

package processgroup

import (
	"os"
	"testing"
)

// TestUsageReadsALiveProcessGroup is the reader's own contract: a group whose
// leader is still this process is read, and the reading carries a live process
// and a cumulative CPU figure. NOTHING IS SPAWNED AND NOTHING IS SIGNALLED —
// the group is the test process's own and the reading is /proc.
func TestUsageReadsALiveProcessGroup(t *testing.T) {
	group := CaptureGroup(os.Getpid())
	usage, ok := group.Usage()
	if !ok {
		t.Fatal("a live process group could not be read")
	}
	if usage.Processes < 1 {
		t.Fatalf("a live group reported %d processes, want at least itself", usage.Processes)
	}
	if usage.CPUSeconds < 0 {
		t.Fatalf("a group reported %v CPU seconds", usage.CPUSeconds)
	}
}

// TestUsageRefusesAGroupItCannotAttribute is the safe half: a group whose pid
// is not this process's own — identity cannot be checked — says it cannot say,
// so the bound never cuts somebody else's tree.
func TestUsageRefusesAGroupItCannotAttribute(t *testing.T) {
	if usage, ok := (Group{}).Usage(); ok {
		t.Fatalf("a group with no leader was read anyway: %+v", usage)
	}
}
