package exec

// The second reading, held to ink's own shape.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// inkSuite stages a project whose suite behaves the way ink's does under this
// program's ceiling: it STREAMS its checks in TAP and then keeps going, so the
// reading is killed with a roster already in hand.
//
// The hang is a sleep rather than a real suite because what is under test is
// what this program does with a cut reading, not what a runner does.
func inkSuite(t *testing.T, streamed int, hang time.Duration) (*Workspace, verify.Strategy) {
	t.Helper()
	root := t.TempDir()
	script := "#!/bin/sh\n"
	for index := 1; index <= streamed; index++ {
		script += "echo 'ok " + strconv.Itoa(index) + " - grid > box layout " + strconv.Itoa(index) + "'\n"
	}
	script += "sleep " + strconv.Itoa(int(hang.Seconds())) + "\n"
	suite := filepath.Join(root, "suite.sh")
	if err := os.WriteFile(suite, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, verify.Strategy{
		Command: "./suite.sh", Read: verify.FormatPlain, Runner: "ava",
		Declared: "npm test", Source: "package.json#scripts.test", Scope: verify.ScopeWhole,
	}
}

// A CUT ROSTER IS STILL A ROSTER, ON BOTH SIDES OF THE PHOTOGRAPH.
//
// ink's after-reading has come back `read: false, named: 0` in two whole sweeps —
// every leaf of s10 and s11 — while the baseline of the same run, on the same
// `npx ava --tap`, named 44 checks and 156 in one case. Nothing was wrong with
// the reader, the scope or the room: the runner streamed its checks, the ceiling
// fired, and THIS SIDE THREW AWAY WHAT IT HAD ALREADY READ. The before half has
// kept a cut roster since ink s7; this half never did.
func TestACutSecondReadingKeepsWhatItNamed(t *testing.T) {
	workspace, strategy := inkSuite(t, 6, 30*time.Second)
	reading := verify.Reading{
		Taken: true, Budget: 900 * time.Millisecond,
		Before: verify.Result{
			Strategy: strategy,
			Reported: []string{"grid > box layout 1", "grid > box layout 2"},
		},
	}
	outcome := &Outcome{}
	PhotographAfter(context.Background(), workspace, nil, time.Hour,
		Task{Goal: t.Name()}, reading, true, false, outcome)

	after := outcome.Verification
	if !after.AfterTaken {
		t.Fatalf("a runner that named six checks before its ceiling was read as unread: %q",
			after.Unread)
	}
	if len(after.After.Reported) == 0 {
		t.Fatal("the roster the runner streamed was thrown away")
	}
	if !after.After.TimedOut {
		t.Error("the ceiling fired and the result does not say so")
	}
	// PARTIAL, so the subtraction is refused: the checks it never reached are
	// missing for a reason that has nothing to do with this change.
	if !after.Partial {
		t.Error("a cut second reading was offered for subtraction")
	}
	if len(outcome.Regressed) != 0 {
		t.Errorf("a partial roster was subtracted anyway: %v", outcome.Regressed)
	}
}

// AND A COMMAND THAT RAN IS NOT A TREE NOBODY COULD READ. Those are two facts and
// they used to share one sentence, which is the sentence a gate is handed when it
// has to say why nothing was measured.
func TestACommandThatRanAndNamedNothingSaysSo(t *testing.T) {
	workspace, strategy := inkSuite(t, 0, 30*time.Second)
	reading := verify.Reading{
		Taken: true, Budget: 900 * time.Millisecond,
		Before: verify.Result{Strategy: strategy, Reported: []string{"grid > box layout 1"}},
	}
	outcome := &Outcome{}
	PhotographAfter(context.Background(), workspace, nil, time.Hour,
		Task{Goal: t.Name()}, reading, true, false, outcome)

	why := outcome.Verification.Unread
	if outcome.Verification.AfterTaken {
		t.Fatal("a runner that named nothing at all was read as a reading")
	}
	if !strings.Contains(why, "ran on the finished tree") {
		t.Errorf("the record does not say the command ran: %q", why)
	}
	if strings.Contains(why, "was not read") {
		t.Errorf("a command that ran is still spelled as a tree nobody could read: %q", why)
	}
}

// AND A DERIVED RUNG THAT WILL NOT RUN FALLS BACK TO THE ONE THE BASELINE PROVED.
//
// The after reading is allowed to differ from the before one — widened by the
// run's own checks, or aimed at the diff where the whole suite did not fit — and
// both of those choose a command the baseline never watched work. A selection
// this program built is the one thing here a retake can fix.
func TestASecondReadingRetakesOnTheBaselinesOwnRung(t *testing.T) {
	workspace, baseline := inkSuite(t, 4, 0)
	reading := verify.Reading{
		Taken: true, Budget: 2 * time.Second,
		Before: verify.Result{Strategy: baseline, Reported: []string{"grid > box layout 1"}},
	}
	// A rung this program derived and that names nothing — a runner handed a
	// path it cannot run on its own.
	derived := baseline
	derived.Base = "./suite.sh"
	derived.Selected = []string{"test/nothing-here.js"}
	derived.Command = "./suite.sh --files test/nothing-here.js >/dev/null"
	derived.Scope = "touched packages (1 file)"

	after, ok := readFinishedTree(context.Background(), workspace.Root(), derived, reading, nil)
	if !ok {
		t.Fatal("neither the derived rung nor the baseline's could be started")
	}
	if len(after.Reported) == 0 {
		t.Fatal("a derived rung that named nothing was reported without retaking on the " +
			"one command this job has watched work")
	}
	if after.Strategy.Command != baseline.Command {
		t.Errorf("the retake did not use the baseline's own rung: %q", after.Strategy.Command)
	}
	// And a rung that IS the baseline's is not run twice to learn the same thing.
	same, _ := readFinishedTree(context.Background(), workspace.Root(), baseline, reading, nil)
	if len(same.Reported) == 0 {
		t.Error("the baseline's own rung came back empty")
	}
}
