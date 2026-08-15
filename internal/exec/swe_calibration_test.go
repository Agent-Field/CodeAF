package exec

import (
	"context"
	"strings"
	"testing"
)

// calibrationSaying reports whether any of a run's self-observations contains a
// phrase. The notes are prose by design, so the assertions are about what they
// say rather than about their exact wording.
func calibrationSaying(notes []string, want string) bool {
	for _, note := range notes {
		if strings.Contains(note, want) {
			return true
		}
	}
	return false
}

// The bottom of the envelope. The engine cut the goal to a single leaf on an
// xs band and finished for a cent, and both facts have to survive the run — the
// first is the engine's own judgement that the job was small, the second is
// arithmetic against the ceiling this worker was given.
func TestSWENotesAJobThatSatUnderItsEnvelope(t *testing.T) {
	probe := newSWEProbe(t, "trivial")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if !calibrationSaying(outcome.Calibration, "root-cut: xs") {
		t.Fatalf("the engine's own sizing never reached the record: %#v", outcome.Calibration)
	}
	if !calibrationSaying(outcome.Calibration, "a lighter worker may have sufficed") {
		t.Fatalf("nothing said the job may have been too small: %#v", outcome.Calibration)
	}
	if !calibrationSaying(outcome.Calibration, "far inside this worker's envelope") {
		t.Fatalf("a one-cent run against a ten-dollar ceiling went unremarked: %#v", outcome.Calibration)
	}
}

// The top of it, twice over: the audit used every cycle it had. The cheap-run
// note must stay silent here, or the same record would say the work was both
// under the floor and at the ceiling.
func TestSWENotesAJobThatSatAtTheTopOfItsEnvelope(t *testing.T) {
	probe := newSWEProbe(t, "strained")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if !calibrationSaying(outcome.Calibration, "every one of its 5 cycles") {
		t.Fatalf("an audit at its ceiling said nothing: %#v", outcome.Calibration)
	}
	if calibrationSaying(outcome.Calibration, "a lighter worker may have sufficed") {
		t.Fatalf("a run at full stretch was recorded as trivial: %#v", outcome.Calibration)
	}
}

// A leaf that spent the whole ceiling and was still working is the clearest
// evidence there is that the ruler let too much into one node.
func TestSWENotesABudgetExhaustedRunAtItsCeiling(t *testing.T) {
	probe := newSWEProbe(t, "budget")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("budget exhaustion is not a failure: %v", err)
	}
	if !calibrationSaying(outcome.Calibration, "top of this worker's envelope") {
		t.Fatalf("an exhausted ceiling said nothing: %#v", outcome.Calibration)
	}
}

// An ordinary run in the middle of the range says nothing at all. A note on
// every leaf is a note on none of them: the recalibration call reads these as
// evidence that something is misplaced, and a worker that always says it fits
// is a worker whose ruler can never move.
func TestSWEIsSilentAboutAnOrdinaryRun(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	// A ceiling this run's $0.4212 sits comfortably inside a fifth of.
	probe.worker.WithMaxCost(1.0)
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if len(outcome.Calibration) != 0 {
		t.Fatalf("an ordinary run editorialised about itself: %#v", outcome.Calibration)
	}
}

// The generalist writes none of these, ever. It is the baseline every other
// worker is measured against, and a baseline with an opinion about its own fit
// would be calibrating against itself.
func TestTheGeneralistLeavesCalibrationEmpty(t *testing.T) {
	outcome := &Outcome{}
	if len(outcome.Calibration) != 0 {
		t.Fatal("a fresh outcome arrived with self-observations in it")
	}
	outcome.Calibrate("   ")
	if len(outcome.Calibration) != 0 {
		t.Fatalf("blank notes were kept: %#v", outcome.Calibration)
	}
	outcome.Calibrate("  it fit  ")
	if len(outcome.Calibration) != 1 || outcome.Calibration[0] != "it fit" {
		t.Fatalf("the note was not trimmed and kept: %#v", outcome.Calibration)
	}
}
