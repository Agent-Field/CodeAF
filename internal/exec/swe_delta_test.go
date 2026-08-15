package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The two halves of audit-notes §14.4.1, from the executor's side: what a run
// that hit the wall clock with finished work is allowed to report, and what the
// engine's baseline delta is allowed to carry out of the stream.

func TestSWEDeliversFinishedWorkAtTheBell(t *testing.T) {
	// cli#2217: the patch was written, committed, verified against the
	// repository's own suite and passed the audit — and the leaf reported
	// "the time limit was reached before anything finished".
	probe := newSWEProbe(t, "bell")
	probe.worker.deadline = 2 * time.Second
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a delivered run returned an error: %v", err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %q, want %q — finished, verified work was discarded at the bell",
			outcome.Stop, StopDone)
	}
	if outcome.Verdict != provider.VerdictVerifiedSuccess {
		t.Fatalf("verdict = %q, want %q", outcome.Verdict, provider.VerdictVerifiedSuccess)
	}
	if outcome.Exhausted != "" {
		t.Fatalf("exhausted = %q — the work was not still running when the clock stopped it",
			outcome.Exhausted)
	}
	if !strings.Contains(outcome.Text, "wall clock") ||
		!strings.Contains(outcome.Text, "delivered as it stands") {
		t.Fatalf("the text does not say what happened: %q", outcome.Text)
	}
}

// The other three arms: every one of them is a deadline that must still fail,
// because something a finished run is judged on was not true.
func TestSWEBellDeliveryRefusesAnythingUnfinished(t *testing.T) {
	for _, testCase := range []struct {
		scenario string
		why      string
	}{
		{"bell-unaudited", "the audit had not passed"},
		{"bell-untouched", "the workspace was never changed"},
		{"hang", "neither gate ever spoke"},
	} {
		probe := newSWEProbe(t, testCase.scenario)
		probe.worker.deadline = 2 * time.Second
		outcome, err := probe.worker.Run(context.Background(), probe.task())
		if err != nil {
			t.Fatalf("%s returned an error: %v", testCase.scenario, err)
		}
		if outcome.Stop != StopDeadline {
			t.Fatalf("%s: stop = %q, want %q — %s",
				testCase.scenario, outcome.Stop, StopDeadline, testCase.why)
		}
		if outcome.Verdict == provider.VerdictVerifiedSuccess {
			t.Fatalf("%s: an unfinished run claimed verified success", testCase.scenario)
		}
	}
}

func TestSWECarriesTheBaselineDeltaOutOfTheStream(t *testing.T) {
	// cobra#2257: `make all` exited 2 on a test that was already failing. The
	// engine knows; the delivery gate cannot, unless this carries it.
	probe := newSWEProbe(t, "baseline")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Baseline) != 1 {
		t.Fatalf("baseline = %#v, want the one pre-existing failure, said once", outcome.Baseline)
	}
	if !strings.Contains(outcome.Baseline[0], "TestFailGenFishCompletionFile") ||
		!strings.Contains(outcome.Baseline[0], "ALREADY failing") {
		t.Fatalf("baseline note = %q", outcome.Baseline[0])
	}
}

func TestSWELeavesTheBaselineEmptyWhenNothingWasAlreadyRed(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Baseline) != 0 {
		t.Fatalf("baseline = %#v, want none — a green repository claims nothing", outcome.Baseline)
	}
}
