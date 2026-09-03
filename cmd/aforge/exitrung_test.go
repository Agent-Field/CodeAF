package main

import (
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// ── EXIT 1 MEANS NOTHING RAN, AND IT MAY NOT MEAN ANYTHING ELSE ─────────────
//
// `aforge exec` mapped `exec.StopError` onto `stopError`, and `stopError` is
// the rung whose published meaning is "it could not be run at all — no key, bad
// arguments, the store would not open". But `exec.StopError` IS AN OUTCOME: the
// executor writes it from inside the turn loop when a model call fails and
// cannot be absorbed (internal/exec/linear.go). So a run that reached turn
// nine, spent real money and was holding half an answer left with the number
// that says it never began — and a script branching on the ladder either
// retried it as a startup failure or threw the evidence away.

// A run that got as far as turn nine and then broke.
func failedAtTurnNine() *exec.Outcome {
	return &exec.Outcome{
		Stop:      exec.StopError,
		Text:      "half an answer, which is the half worth reading",
		Turns:     9,
		Artifacts: []string{"notes.md"},
		Elapsed:   4 * time.Minute,
		Usage:     exec.Usage{Calls: 9, PromptTokens: 88_000, CompletionTokens: 5_100, Cost: 0.42},
	}
}

func TestARunThatSpentMoneyAndFailedDoesNotTellAScriptItNeverRan(t *testing.T) {
	outcome := failedAtTurnNine()
	said := errors.New("node task-1: upstream provider returned 500 after 3 attempts")

	got := execExit(outcome, said)
	if got != exitIncomplete {
		t.Fatalf("a run that reached turn %d, spent $%.2f and produced %q left with exit %d — %q\n"+
			"  it ran, so the ladder's rung for it is exit %d — %q\n"+
			"  exit %d is reserved for a refusal before any work starts",
			outcome.Turns, outcome.Usage.Cost, outcome.Text,
			int(got), exitMeaning(got),
			int(exitIncomplete), exitMeaning(exitIncomplete), int(exitCannotRun))
	}

	// AND THE ENVELOPE SAYS THE SAME THING IN WORDS. `stop` is what a script is
	// meant to branch on, so a right number under the wrong word is half a fix.
	envelope := buildExecEnvelope(outcome, said, "a/model")
	if envelope.Stop != stopIncomplete {
		t.Fatalf("the same run published stop %q, and the ladder puts it on exit %d, whose reason is %q",
			envelope.Stop, int(exitIncomplete), stopIncomplete)
	}
	if envelope.OK {
		t.Fatalf("a run that failed at turn %d published ok:true", outcome.Turns)
	}
	// The evidence it paid for is still on the object.
	if envelope.Answer != outcome.Text {
		t.Fatalf("the partial answer was thrown away\n  answer: %q\n  want:   %q", envelope.Answer, outcome.Text)
	}
	if envelope.SpendUSD != outcome.Usage.Cost || envelope.Steps != outcome.Turns {
		t.Fatalf("the bill for the run went missing: spend_usd %v steps %d, want %v and %d",
			envelope.SpendUSD, envelope.Steps, outcome.Usage.Cost, outcome.Turns)
	}
	// And the provider's own sentence is not lost — it moves to `incomplete`,
	// which is where every other run that started and did not finish says why.
	said_, carried := envelopeFields(t, envelope)[envelopeIncomplete].(string)
	if !carried || said_ != execFailureWords(said) {
		t.Fatalf("the failure sentence is nowhere a script can read it: %q under %q, want %q",
			said_, envelopeIncomplete, execFailureWords(said))
	}
}

// AND NOTHING THE EXECUTOR CAN HAND BACK LANDS ON EXIT 1, whatever it is
// called. The rule is about the OUTCOME EXISTING and not about which of the
// eleven stop reasons it carries, so the test walks all eleven: a new reason
// added to internal/exec that quietly landed on "it never started" would
// otherwise be found by a person reading a retry log, months later.
func TestOnlyARunWithNoOutcomeAtAllCouldNotBeRunAtAll(t *testing.T) {
	for _, reason := range []exec.StopReason{
		exec.StopDone, exec.StopTurnCap, exec.StopBudget, exec.StopDeadline, exec.StopError,
		exec.StopPromote, exec.StopPaused, exec.StopCancelled, exec.StopEmpty,
		exec.StopOverrun, exec.StopSplit,
	} {
		outcome := &exec.Outcome{Stop: reason, Text: "something got done", Turns: 3}
		for _, said := range []error{nil, errors.New("node task-1: the connection went away")} {
			if stop := execStop(outcome, said); stop == stopError {
				t.Fatalf("an outcome that stopped %q says %q, which is the ladder's word for a run that never started",
					reason, stopError)
			}
			if code := execExit(outcome, said); code == exitCannotRun {
				t.Fatalf("an outcome that stopped %q leaves with exit %d — %q\n"+
					"  an outcome exists only after the run started, so it may not leave on that rung",
					reason, int(exitCannotRun), exitMeaning(exitCannotRun))
			}
		}
	}

	// AND NO OUTCOME IS THE ONE THING THAT DOES. This is `aforge exec` refusing
	// before it opens a connection: nothing was attempted and nothing was spent.
	refused := errors.New("node task-1: OPENROUTER_API_KEY (or OPENAI_API_KEY) is required")
	if stop := execStop(nil, refused); stop != stopError {
		t.Fatalf("a run with no outcome at all says %q, want %q", stop, stopError)
	}
	if code := execExit(nil, refused); code != exitCannotRun {
		t.Fatalf("a run with no outcome at all leaves with exit %d — %q, want exit %d — %q",
			int(code), exitMeaning(code), int(exitCannotRun), exitMeaning(exitCannotRun))
	}
}

// THE ESCAPE HATCH DID NOT MOVE WITH THE RUNG. AFORGE_EXIT_CODES=legacy exists
// so a harness pinned to `aforge exec`'s old 2/3/4/5/6 keeps working for one
// release, and a change to which rung a run lands on is exactly the change that
// would break it without anybody noticing — the hatch is only ever exercised by
// somebody else's script.
func TestTheLegacyExitCodesAreUntouchedByTheRungThatMoved(t *testing.T) {
	t.Setenv("AFORGE_EXIT_CODES", "legacy")
	for _, row := range []struct {
		what    string
		outcome *exec.Outcome
		runErr  error
		want    exitStatus
	}{
		{"the provider failed mid-run", failedAtTurnNine(),
			errors.New("node task-1: upstream returned 500"), 5},
		{"the provider failed and nothing was handed back with it",
			&exec.Outcome{Stop: exec.StopError, Turns: 9}, nil, 5},
		{"the model answered", &exec.Outcome{Stop: exec.StopDone, Text: "here"}, nil, 0},
		{"it finished with nothing to show", &exec.Outcome{Stop: exec.StopDone, Text: "  "}, nil, 6},
		{"the token budget ran out", &exec.Outcome{Stop: exec.StopBudget, Text: "half"}, nil, 2},
		{"the turn cap ran out", &exec.Outcome{Stop: exec.StopTurnCap, Text: "half"}, nil, 3},
		{"the wall arrived", &exec.Outcome{Stop: exec.StopDeadline, Text: "half"}, nil, 4},
		{"it could not be run at all", nil, errors.New("node task-1: no key"), 5},
	} {
		if got := execExit(row.outcome, row.runErr); got != row.want {
			t.Fatalf("with AFORGE_EXIT_CODES=legacy, %s leaves with %d, and exec's old table says %d\n"+
				"  the hatch's whole job is to be the OLD numbers; the new rung may not reach it",
				row.what, int(got), int(row.want))
		}
	}

	// And with the hatch unset the same run is on the new ladder, so the test
	// above is testing the hatch and not the absence of one.
	t.Setenv("AFORGE_EXIT_CODES", "")
	if got := execExit(failedAtTurnNine(), errors.New("node task-1: upstream returned 500")); got != exitIncomplete {
		t.Fatalf("without the hatch the mid-run failure leaves with %d, want %d", int(got), int(exitIncomplete))
	}
}
