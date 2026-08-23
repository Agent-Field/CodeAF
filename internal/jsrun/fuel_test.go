package jsrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// OUT OF ANY BUDGET IS INCOMPLETE WITH A REASON, never a hang and never a panic.
// The three tests below are the three ceilings; the fourth is the one that
// proves a budget cannot be caught.

func TestRunningOutOfStepsStopsTheRunIncomplete(t *testing.T) {
	const program = `
function run(input) {
  log("one");
  log("two");
  log("three");
  return { summary: "it got to the end" };
}
`
	bundle := Bundle{Manifest: toyManifest(), Program: program, Fuel: Fuel{Steps: 2}}
	result, journal := runToy(t, bundle, &scriptedEnv{}, `{}`)

	if result.Finished() {
		t.Fatalf("a run out of steps should not finish: %+v", result)
	}
	if !strings.Contains(result.Incomplete, "2 steps") {
		t.Fatalf("the reason should say what ran out, got %q", result.Incomplete)
	}
	if result.FellBack != "" {
		t.Fatalf("running out is not a fallback: %q", result.FellBack)
	}
	if len(journal.entries) != 2 {
		t.Fatalf("only the calls that happened are journaled, got %d", len(journal.entries))
	}
}

func TestRunningOutOfTokensStopsTheRunIncomplete(t *testing.T) {
	const program = `
function run(input) {
  ai("summarise", input);
  ai("summarise", input);
  ai("summarise", input);
  return { summary: "it got to the end" };
}
`
	env := &scriptedEnv{answers: map[string]exec.Answer{
		"summarise": {Text: "said", Spend: exec.Spend{Calls: 1, Input: 400, Output: 100}},
	}}
	bundle := Bundle{
		Manifest: toyManifest(),
		Program:  program,
		Prompts:  map[string]string{"summarise": "Summarise."},
		Fuel:     Fuel{Tokens: 900},
	}
	result, journal := runToy(t, bundle, env, `{}`)

	if result.Finished() {
		t.Fatalf("a run out of tokens should not finish: %+v", result)
	}
	if !strings.Contains(result.Incomplete, "900 tokens") {
		t.Fatalf("the reason should say what ran out, got %q", result.Incomplete)
	}
	// Two calls at 500 each crosses 900; the third never happened.
	if len(journal.entries) != 2 {
		t.Fatalf("the run made %d calls, wanted 2", len(journal.entries))
	}
	if result.Spend.Input+result.Spend.Output != 1000 {
		t.Fatalf("the ledger says %+v", result.Spend)
	}
}

// THE DEADLINE IS THE ONLY CEILING THAT CAN STOP A LOOP THAT CALLS NOTHING, and
// it is why it exists separately: this program makes no host call and spends no
// token, so nothing but the watchdog is ever going to see it again.
func TestALoopThatCallsNothingIsStoppedByTheDeadline(t *testing.T) {
	const program = `function run(input) { while (true) {} }`
	runner, err := New(Bundle{Manifest: toyManifest(), Program: program, Fuel: Fuel{Wall: 50 * time.Millisecond}})
	if err != nil {
		t.Fatalf("this bundle would not compile: %v", err)
	}

	done := make(chan exec.RunResult, 1)
	go func() {
		result, _ := runner.Run(context.Background(), json.RawMessage(`{}`), &scriptedEnv{})
		done <- result
	}()
	select {
	case result := <-done:
		if result.Finished() {
			t.Fatalf("a loop that never ends should not finish: %+v", result)
		}
		if !strings.Contains(result.Incomplete, "ran out of time") {
			t.Fatalf("the reason should say what ran out, got %q", result.Incomplete)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run hung — the deadline did not stop it")
	}
}

// A BUDGET IS NOT CATCHABLE. A program that could `try { … } catch` its way past
// its own ceiling would be a program with no ceiling.
func TestAProgramCannotCatchItsOwnBudget(t *testing.T) {
	const program = `
function run(input) {
  try {
    log("one");
    log("two");
    log("three");
  } catch (e) {
    return { summary: "caught it" };
  }
  return { summary: "it got to the end" };
}
`
	bundle := Bundle{Manifest: toyManifest(), Program: program, Fuel: Fuel{Steps: 1}}
	result, _ := runToy(t, bundle, &scriptedEnv{}, `{}`)
	if len(result.Output) != 0 {
		t.Fatalf("the program caught its own budget: %s", result.Output)
	}
	if !strings.Contains(result.Incomplete, "ran out of room") {
		t.Fatalf("the reason said %q", result.Incomplete)
	}
}

// A cancelled run is INCOMPLETE, not broken and not a fallback: somebody pressed
// the ✕, the run happened, and it did not finish. Reporting it as an error would
// send the caller off to do the work the long way after a person asked for it to
// stop.
func TestACancelledRunIsIncompleteRatherThanAnError(t *testing.T) {
	const program = `function run(input) { while (true) {} }`
	runner, err := New(Bundle{Manifest: toyManifest(), Program: program})
	if err != nil {
		t.Fatalf("this bundle would not compile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	result, runErr := runner.Run(ctx, json.RawMessage(`{}`), &scriptedEnv{})
	if runErr != nil {
		t.Fatalf("a cancelled run is not an error: %v", runErr)
	}
	if result.FellBack != "" {
		t.Fatalf("a cancelled run is not a fallback: %q", result.FellBack)
	}
	if !strings.Contains(result.Incomplete, "stopped before it finished") {
		t.Fatalf("the reason said %q", result.Incomplete)
	}
}

// With no fuel stated, the deadline is the manifest's OWN cost shape — the one
// spelling of a budget the whole wave uses, and the reason there is no second
// deadline constant in this package.
func TestTheDeadlineComesFromTheManifestsCostShape(t *testing.T) {
	manifest := toyManifest()
	manifest.DeadlineFloor = 90 * time.Second
	if got := (Fuel{}).wall(manifest.SubharnessInfo); got != 90*time.Second {
		t.Fatalf("the deadline is %s, wanted the manifest's 90s", got)
	}
	// An explicit wall wins, because a caller with a grant of its own knows
	// something the manifest's prior does not.
	if got := (Fuel{Wall: time.Second}).wall(manifest.SubharnessInfo); got != time.Second {
		t.Fatalf("the caller's own deadline was ignored, got %s", got)
	}
	if got := (Fuel{}).steps(); got != DefaultSteps {
		t.Fatalf("the default step budget is %d", got)
	}
}
