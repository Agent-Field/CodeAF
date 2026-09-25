package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestDoRunHonoursProfileMachineFloor(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("host has no proc memory reading")
	}
	beltRunEnv(t)
	profile := config.ProfileDir()
	settings := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	memory, ok := settings.Row(config.KeyTaskMinFreeMB)
	if !ok {
		t.Fatal("task memory setting is absent")
	}
	if err := memory.Apply("1099511627776"); err != nil {
		t.Fatal(err)
	}
	load, ok := settings.Row(config.KeyTaskMaxLoad)
	if !ok {
		t.Fatal("task load setting is absent")
	}
	if err := load.Apply("0"); err != nil {
		t.Fatal(err)
	}
	workspace := beltRepoWorkspace(t)
	seat := &beltSeat{}
	var stderr strings.Builder
	outcome, err := runErrand(doRequest{
		task: "do the work", workspace: workspace, timeout: 120 * time.Millisecond,
		stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat },
	}, config.ResolveSeats(profile, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Nodes != 0 || seat.seen != 0 {
		t.Fatalf("held do launched %d workers and made %d model calls", outcome.Nodes, seat.seen)
	}
	if err := memory.Apply("0"); err != nil {
		t.Fatal(err)
	}
	seat = finishingSeat(0)
	stderr.Reset()
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	outcome, err = runErrand(doRequest{
		task: "do the work", workspace: workspace, timeout: 30 * time.Second,
		stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat },
	}, config.ResolveSeats(profile, "", ""))
	if err != nil || outcome.Nodes == 0 {
		t.Fatalf("zeroed machine gate did not start: nodes=%d err=%v", outcome.Nodes, err)
	}
}

// THE CONTRACT, from the owner's ruling on #1410 (2026-09-24): when `codeaf do`
// is held by the machine, the person sees why on stderr, once, in the rail's
// own words and naming the limit that held it; and a run whose --timeout
// arrives before anything started says, in its --json stop, that the machine
// held it. Before this, a held run waited in silence and ended `deadline` with
// nothing to say why nothing had happened.
func TestDoHeldByTheMachineSaysWhyOnStderrAndInItsStop(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("host has no proc memory reading")
	}
	beltRunEnv(t)
	profile := config.ProfileDir()
	settings := config.NewSettings(config.SettingsOptions{ProfileDir: profile})
	memory, ok := settings.Row(config.KeyTaskMinFreeMB)
	if !ok {
		t.Fatal("task memory setting is absent")
	}
	if err := memory.Apply("1099511627776"); err != nil {
		t.Fatal(err)
	}
	seat := &beltSeat{}
	var stdout, stderr strings.Builder
	request := doRequest{
		task: "do the work", workspace: beltRepoWorkspace(t), timeout: 300 * time.Millisecond,
		asJSON: true, stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}
	outcome, err := runErrand(request, config.ResolveSeats(profile, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Nodes != 0 || seat.seen != 0 {
		t.Fatalf("held do launched %d workers and made %d model calls", outcome.Nodes, seat.seen)
	}
	said := stderr.String()
	if got := strings.Count(said, "waiting · machine busy"); got != 1 {
		t.Fatalf("a held run says it is waiting on the machine exactly once on stderr; said it %d times:\n%s", got, said)
	}
	if !strings.Contains(said, config.KeyTaskMinFreeMB) {
		t.Fatalf("the held line does not name the limit that held it (%s):\n%s", config.KeyTaskMinFreeMB, said)
	}

	status := reportErrand(request, outcome)
	var code exitStatus
	if !errors.As(status, &code) || code != 124 {
		t.Fatalf("a run the wall ended leaves with 124; got %v", status)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &envelope); err != nil {
		t.Fatalf("--json printed no object: %v\n%s", err, stdout.String())
	}
	if envelope["stop"] != string(stopDeadline) {
		t.Fatalf("stop = %v, want %s", envelope["stop"], stopDeadline)
	}
	blocked, _ := envelope["blocked_on"].(string)
	if !strings.Contains(blocked, "machine busy") || !strings.Contains(blocked, config.KeyTaskMinFreeMB) {
		t.Fatalf("a timeout with nothing started does not say the machine held it: blocked_on = %q", blocked)
	}
	if strings.Contains(stderr.String(), "it stopped to ask:") {
		t.Fatalf("a machine hold is not a question, but stderr says it stopped to ask:\n%s", stderr.String())
	}
}

// scriptedMachine is a machine gate that refuses its first `refuse` asks on
// the load ceiling and admits every ask after them.
type scriptedMachine struct {
	refuse, asked, started, returned int
}

func (m *scriptedMachine) MayStart() bool { m.asked++; return m.asked > m.refuse }
func (m *scriptedMachine) Started()       { m.started++ }
func (m *scriptedMachine) Returned()      { m.returned++ }
func (m *scriptedMachine) HeldBy() string { return config.KeyTaskMaxLoad }

// THE SAME CONTRACT OVER A MACHINE THAT CLEARS: a gate re-asked every pass
// while the load stays high says it once, not once a pass, and the first worker
// that starts after it says the machine has room again, once. The engine's
// own verbs still reach the real gate underneath.
func TestDoSaysAMachineHoldOnceAndItsEndOnce(t *testing.T) {
	machine := &scriptedMachine{refuse: 5}
	var stderr strings.Builder
	hold := watchMachineHold(machine, &stderr, 1.5, 0)
	gate := hold.runGate()
	for i := 0; i < 5; i++ {
		if gate.MayStart() {
			t.Fatalf("ask %d was admitted while the machine refuses", i+1)
		}
	}
	if !gate.MayStart() {
		t.Fatal("the machine cleared and the start was still refused")
	}
	gate.Started()
	gate.Started()
	gate.Returned()
	want := "waiting · machine busy · load per core at or above task.max_load 1.5\n" +
		"starting · the machine has room again\n"
	if stderr.String() != want {
		t.Fatalf("stderr =\n%s\nwant\n%s", stderr.String(), want)
	}
	if machine.started != 2 || machine.returned != 1 {
		t.Fatalf("the real gate saw %d starts and %d returns, want 2 and 1", machine.started, machine.returned)
	}
	if watchMachineHold(nil, &stderr, 1.5, 0).runGate() != nil {
		t.Fatal("no gate at all must reach the engine as nil, not as a wrapper around nothing")
	}
}
