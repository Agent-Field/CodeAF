package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE ESCAPE HATCH'S OWN TABLE, pinned to the numbers it exists to restore.
// These are exec's exit codes as they were before the one ladder, and they are
// reachable only under AFORGE_EXIT_CODES=legacy.
func TestExecLegacyExitCodeIsTheOldTable(t *testing.T) {
	tests := []struct {
		name string
		stop exec.StopReason
		text string
		want int
	}{
		{name: "done", stop: exec.StopDone, text: "answer", want: 0},
		{name: "budget", stop: exec.StopBudget, text: "partial", want: 2},
		{name: "turn cap", stop: exec.StopTurnCap, text: "partial", want: 3},
		{name: "deadline", stop: exec.StopDeadline, text: "partial", want: 4},
		{name: "error", stop: exec.StopError, text: "partial", want: 5},
		{name: "done empty", stop: exec.StopDone, text: "", want: 6},
		{name: "done whitespace", stop: exec.StopDone, text: " \n\t", want: 6},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := execLegacyExitCode(test.stop, test.text); got != test.want {
				t.Fatalf("execLegacyExitCode(%q, %q) = %d, want %d", test.stop, test.text, got, test.want)
			}
		})
	}
}

func TestExecDeadline(t *testing.T) {
	if got := execDeadline(150_000, 0); got != 15*time.Minute {
		t.Fatalf("default deadline = %s, want 15m", got)
	}
	if got := execDeadline(2_000_000, 0); got != 40*time.Minute {
		t.Fatalf("scaled deadline = %s, want 40m", got)
	}
	if got := execDeadline(2_000_000, 75); got != 75*time.Second {
		t.Fatalf("explicit deadline = %s, want 75s", got)
	}
}

func TestBuildExecEnvelopeJSONShape(t *testing.T) {
	outcome := &exec.Outcome{
		Text: "answer",
		Usage: exec.Usage{
			Calls:            2,
			PromptTokens:     30,
			CompletionTokens: 12,
			CachedTokens:     4,
			Cost:             0.125,
		},
		Stop:    exec.StopDone,
		Turns:   7,
		Elapsed: 1234 * time.Millisecond,
	}

	encoded, err := json.Marshal(buildExecEnvelope(outcome, nil, "openai/gpt-5"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	// The contract, as envelope.go states it.
	for name, want := range map[string]any{
		"ok": true, "stop": "done", "answer": "answer", "error": "",
		"spend_usd": 0.125, "seconds": 1.234, "model": "openai/gpt-5", "steps": float64(7),
	} {
		if got := fields[name]; got != want {
			t.Fatalf("%s = %v, want %v\n%s", name, got, want, encoded)
		}
	}
	// And the old spellings, still readable for one release.
	for name, want := range map[string]any{
		"text": "answer", "turns": float64(7), "elapsed_ms": float64(1234),
	} {
		if got := fields[name]; got != want {
			t.Fatalf("the old field %s = %v, want %v\n%s", name, got, want, encoded)
		}
	}
	if _, ok := fields["usage"]; !ok {
		t.Fatalf("the old usage object went away in one release:\n%s", encoded)
	}
}

func TestBuildExecEnvelopeToleratesNilOutcome(t *testing.T) {
	encoded, err := json.Marshal(buildExecEnvelope(nil, nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["stop"] != string(stopError) || fields["ok"] != false {
		t.Fatalf("a nil outcome is not reported as unrunnable:\n%s", encoded)
	}
	if code := exitFor(buildExecEnvelope(nil, nil, "").Stop); code != exitCannotRun {
		t.Fatalf("exit code for a nil outcome = %d, want 1", code)
	}
}

func TestExecTask(t *testing.T) {
	task := execTask("  First line  \nSecond line", "be exact", "/tmp/workspace")

	if task.NodeID != 1 {
		t.Fatalf("NodeID = %d, want 1", task.NodeID)
	}
	if task.Title != "First line" {
		t.Fatalf("Title = %q, want first trimmed line", task.Title)
	}
	if task.Brief != "  First line  \nSecond line\n\nWorkspace root (your working directory): /tmp/workspace" {
		t.Fatalf("Brief = %q", task.Brief)
	}
	if !strings.Contains(task.Brief, "/tmp/workspace") {
		t.Fatal("Brief does not contain the workspace root")
	}
	if task.Contract != "be exact" {
		t.Fatalf("Contract = %q, want system value", task.Contract)
	}
	if task.OutputHint != "" {
		t.Fatalf("OutputHint = %q, want empty", task.OutputHint)
	}
	if task.Goal != "" {
		t.Fatalf("Goal = %q, want empty", task.Goal)
	}
	if len(task.Inputs) != 0 {
		t.Fatalf("Inputs has %d entries, want none", len(task.Inputs))
	}
}

// execFlagsForTest builds the three walls exactly as runExec does, so the tests
// below exercise the real flag package rather than a stand-in for it — the
// whole contract turns on flag.Visit reporting what was typed.
func execFlagsForTest(t *testing.T, args ...string) (*flag.FlagSet, *int, *int, *int) {
	t.Helper()
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	maxTurns := flags.Int("turns", 200, "")
	maxTokens := flags.Int("budget", 150000, "")
	timeout := flags.Int("timeout", 0, "")
	if err := flags.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return flags, maxTurns, maxTokens, timeout
}

func fakeEnv(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

// With nothing in the environment the defaults have to survive untouched.
func TestApplyExecEnvLeavesDefaultsAlone(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(nil), turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 150000 || *timeout != 0 {
		t.Fatalf("turns/budget/timeout = %d/%d/%d, want 200/150000/0", *turns, *budget, *timeout)
	}
}

// The point of the fallback: a harness sets the walls once for a campaign.
func TestApplyExecEnvFillsWallsNobodyPassed(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t)
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  " 20000 ",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns != 3 || *budget != 20000 || *timeout != 150 {
		t.Fatalf("turns/budget/timeout = %d/%d/%d, want 3/20000/150", *turns, *budget, *timeout)
	}
	// And the wall the environment named is the wall the run actually gets.
	if got := execDeadline(*budget, *timeout); got != 150*time.Second {
		t.Fatalf("deadline = %s, want 150s", got)
	}
}

// A typed flag is a decision and outranks the environment — including when the
// value typed is the same as the default, which is the case flag.Visit exists
// to distinguish and the one an implementation reading only the value gets
// wrong.
func TestApplyExecEnvNeverOverrulesATypedFlag(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t,
		"-turns", "200", "-budget", "9000", "-timeout", "42")
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  "20000",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 9000 || *timeout != 42 {
		t.Fatalf("turns/budget/timeout = %d/%d/%d, want 200/9000/42", *turns, *budget, *timeout)
	}
}

// One flag typed, the others left to the environment: the fallback is per-wall,
// not all-or-nothing.
func TestApplyExecEnvIsPerWall(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t, "-budget", "9000")
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  "20000",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns != 3 || *budget != 9000 || *timeout != 150 {
		t.Fatalf("turns/budget/timeout = %d/%d/%d, want 3/9000/150", *turns, *budget, *timeout)
	}
}

// A variable that is set but empty is not a value; it must not become one.
func TestApplyExecEnvIgnoresEmptyVariables(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t)
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "",
		"AFORGE_EXEC_BUDGET":  "   ",
		"AFORGE_EXEC_TIMEOUT": "",
	})
	if err := applyExecEnv(flags, env, turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 150000 || *timeout != 0 {
		t.Fatalf("turns/budget/timeout = %d/%d/%d, want the defaults", *turns, *budget, *timeout)
	}
}

// A typo in a wall must stop the run, not be silently dropped. A campaign that
// thinks it capped every call at 150 seconds because of an unnoticed typo
// measures the wrong thing all night.
func TestApplyExecEnvRefusesNonNumericValues(t *testing.T) {
	for _, variable := range []string{"AFORGE_EXEC_TURNS", "AFORGE_EXEC_BUDGET", "AFORGE_EXEC_TIMEOUT"} {
		flags, turns, budget, timeout := execFlagsForTest(t)
		err := applyExecEnv(flags, fakeEnv(map[string]string{variable: "2m"}), turns, budget, timeout)
		if err == nil {
			t.Fatalf("%s=2m was accepted", variable)
		}
		if !strings.Contains(err.Error(), variable) {
			t.Fatalf("%s error = %q, want it to name the variable", variable, err)
		}
	}
}

// Out-of-range values from the environment land in exactly the same guard the
// flags have always had, so there is one rule about what a wall may be.
func TestApplyExecEnvValuesStillMeetTheFlagGuards(t *testing.T) {
	flags, turns, budget, timeout := execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TURNS": "0"}), turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *turns > 0 {
		t.Fatalf("turns = %d, want the environment's 0 to reach the guard", *turns)
	}
	flags, turns, budget, timeout = execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TIMEOUT": "-1"}), turns, budget, timeout); err != nil {
		t.Fatal(err)
	}
	if *timeout != -1 {
		t.Fatalf("timeout = %d, want the environment's -1 to reach the guard", *timeout)
	}
}

// The help text is where a harness author finds out the variables exist.
func TestUsageMentionsExecEnvironmentFallbacks(t *testing.T) {
	for _, variable := range []string{"AFORGE_EXEC_TIMEOUT", "AFORGE_EXEC_BUDGET", "AFORGE_EXEC_TURNS"} {
		if !strings.Contains(usageText, variable) {
			t.Fatalf("usageText does not mention %s", variable)
		}
	}
}

// HEADLESS.md is a contract other people program against, so the parts of it a
// caller cannot discover any other way — the environment fallbacks and the
// exit-code table — have to actually be in it. A capability the product has and
// does not document is one a harness author meets by accident.
func TestHeadlessDocumentsExec(t *testing.T) {
	raw, err := os.ReadFile("../../docs/HEADLESS.md")
	if err != nil {
		t.Fatal(err)
	}
	document := string(raw)
	for _, want := range []string{
		"aforge exec",
		"AFORGE_EXEC_TURNS",
		"AFORGE_EXEC_BUDGET",
		"AFORGE_EXEC_TIMEOUT",
		"elapsed_ms",
		"turn-cap",
	} {
		if !strings.Contains(document, want) {
			t.Fatalf("docs/HEADLESS.md never mentions %q", want)
		}
	}
}

// A FAILURE ENVELOPE THAT CANNOT SAY WHY IS NOT A CONTRACT.
//
// `exec --json` used to write `{"text":"","stop":"error",…}` and put the only
// copy of the sentence on stderr, so a caller reading stdout could learn THAT
// the run failed and never WHY. `do --json` shipped the same field for the same
// reason; exec never got it.
func TestExecJSONSaysWhyTheRunFailed(t *testing.T) {
	runErr := errors.New("node " + execNodeKey + ": API error (400): nosuch/model-xyz is not a valid model ID")
	envelope := buildExecEnvelope(&exec.Outcome{Stop: exec.StopError}, runErr, "")
	if envelope.Error == "" {
		t.Fatal("the failure envelope carries stop=error and no reason at all, so a script can never learn why")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"error":`) {
		t.Fatalf("--json carries no error field:\n%s", encoded)
	}
	if !strings.Contains(envelope.Error, "nosuch/model-xyz is not a valid model ID") {
		t.Fatalf("the reason names something other than the cause: %q", envelope.Error)
	}
	for _, machinery := range []string{"API error", "node " + execNodeKey} {
		if strings.Contains(envelope.Error, machinery) {
			t.Fatalf("the machine contract leaks the internal verb %q: %q", machinery, envelope.Error)
		}
	}
	if !strings.Contains(envelope.Error, "aforge models") {
		t.Fatalf("the reason never says what to do about it: %q", envelope.Error)
	}
	// A run that worked says nothing. The KEY IS STILL THERE and empty, which is
	// the envelope's guarantee: a field is never absent, so a caller reading
	// `.error` on an older or newer run is never handed null.
	clean := buildExecEnvelope(&exec.Outcome{Stop: exec.StopDone, Text: "ok"}, nil, "")
	if clean.Error != "" {
		t.Fatalf("a run that worked reported an error: %q", clean.Error)
	}
	encoded, err = json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"error":""`) {
		t.Fatalf("a clean run's envelope dropped the error key instead of leaving it empty:\n%s", encoded)
	}
}
