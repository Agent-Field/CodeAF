package session

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

// requireHostMemoryReading skips a test that is about the REAL reading. It is
// /proc/meminfo (hostMemoryMB), and a host without it — macOS — leaves the
// governor unknown, and an unknown governor admits everything
// (admissionGovernor.admits: silence is not pressure). A test of held-run
// behaviour that merely needs a hold states its machine with littleMemoryHost
// instead and runs everywhere; only a test whose claim is about this host's
// own numbers belongs behind this skip.
func requireHostMemoryReading(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("host has no proc memory reading")
	}
}

func TestRunAdmissionUsesRealHostMemoryAndSharedLanes(t *testing.T) {
	requireHostMemoryReading(t)
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

// A door with no rail says which ceiling held its work, so the gate names the
// row whose ceiling the last refusal met — and names nothing before a refusal.
func TestRunAdmissionNamesTheCeilingThatHeldIt(t *testing.T) {
	reading := machineReading{loadPerCore: 3, availableMB: 4096, totalMB: 8192, cores: 4}
	gate := &runAdmission{governor: &admissionGovernor{
		maxLoad: 2, minFreeMB: 1024,
		read: func() (machineReading, bool) { return reading, true },
	}, lanes: NewTaskLanes()}
	if gate.HeldBy() != "" {
		t.Fatalf("held by %q before any refusal", gate.HeldBy())
	}
	if gate.MayStart() || gate.HeldBy() != config.KeyTaskMaxLoad {
		t.Fatalf("a load over its ceiling: held by %q, want %s", gate.HeldBy(), config.KeyTaskMaxLoad)
	}
	reading = machineReading{loadPerCore: 0.5, availableMB: 512, totalMB: 8192, cores: 4}
	gate.governor.at = gate.governor.at.Add(-2 * taskPressureTTL)
	if gate.MayStart() || gate.HeldBy() != config.KeyTaskMinFreeMB {
		t.Fatalf("memory under its floor: held by %q, want %s", gate.HeldBy(), config.KeyTaskMinFreeMB)
	}
}
