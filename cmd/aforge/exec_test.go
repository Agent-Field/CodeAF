package main

import (
	"encoding/json"
	"errors"
	"flag"
	"go/parser"
	"go/printer"
	"go/token"
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
	if got := execDeadline(2_000_000, 75*time.Second); got != 75*time.Second {
		t.Fatalf("explicit deadline = %s, want 75s", got)
	}
	// A WALL UNDER A SECOND IS THE WALL, not a rounding down into the table's
	// fifteen minutes. It was the latter while this door took an integer of
	// seconds and the call site divided the duration down to reach it.
	if got := execDeadline(2_000_000, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("sub-second deadline = %s, want 500ms", got)
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

// execFlagsForTest builds the three walls exactly as runExec does — the printed
// spellings AND the hidden old ones — so the tests below exercise the real flag
// package rather than a stand-in for it. The whole contract turns on flag.Visit
// reporting what was typed, and on the aliases resolving to the printed name
// before it is read (rename.go).
func execFlagsForTest(t *testing.T, args ...string) (*flag.FlagSet, *int, *int, *wallFlag) {
	t.Helper()
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	maxTurns := flags.Int("max-turns", 200, "")
	renamedFlag(flags, "turns", "max-turns")
	maxTokens := flags.Int("token-budget", 150000, "")
	renamedFlag(flags, "budget", "token-budget")
	wall := &wallFlag{}
	flags.Var(wall, "timeout", "")
	if err := flags.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return flags, maxTurns, maxTokens, wall
}

func fakeEnv(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

// With nothing in the environment the defaults have to survive untouched.
func TestApplyExecEnvLeavesDefaultsAlone(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(nil), turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 150000 || wall.wall != 0 {
		t.Fatalf("max-turns/token-budget/timeout = %d/%d/%s, want 200/150000/0s", *turns, *budget, wall.wall)
	}
}

// The point of the fallback: a harness sets the walls once for a campaign.
func TestApplyExecEnvFillsWallsNobodyPassed(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t)
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  " 20000 ",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns != 3 || *budget != 20000 || wall.wall != 150*time.Second {
		t.Fatalf("max-turns/token-budget/timeout = %d/%d/%s, want 3/20000/2m30s", *turns, *budget, wall.wall)
	}
	// And the wall the environment named is the wall the run actually gets.
	if got := execDeadline(*budget, wall.wall); got != 150*time.Second {
		t.Fatalf("deadline = %s, want 150s", got)
	}
	// THE VARIABLE READS A DURATION TOO, because the flag it stands in for does.
	// AFORGE_EXEC_TIMEOUT=2m was a refusal on a machine where --timeout 2m works.
	flags, turns, budget, wall = execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TIMEOUT": "2m"}), turns, budget, wall); err != nil {
		t.Fatalf("AFORGE_EXEC_TIMEOUT=2m was refused: %v", err)
	}
	if wall.wall != 2*time.Minute {
		t.Fatalf("AFORGE_EXEC_TIMEOUT=2m gave %s, want 2m0s", wall.wall)
	}
}

// A typed flag is a decision and outranks the environment — including when the
// value typed is the same as the default, which is the case flag.Visit exists
// to distinguish and the one an implementation reading only the value gets
// wrong.
func TestApplyExecEnvNeverOverrulesATypedFlag(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t,
		"-max-turns", "200", "-token-budget", "9000", "-timeout", "42")
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  "20000",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 9000 || wall.wall != 42*time.Second {
		t.Fatalf("max-turns/token-budget/timeout = %d/%d/%s, want 200/9000/42s", *turns, *budget, wall.wall)
	}
	// AND THE OLD SPELLING IS THE SAME DECISION. A person who typed --budget
	// named the wall as surely as one who typed --token-budget, and an
	// environment variable that overruled the first and not the second would be
	// the silent overrule this whole fallback is guarded against.
	flags, turns, budget, wall = execFlagsForTest(t, "-budget", "9000")
	if err := applyExecEnv(flags, env, turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *budget != 9000 {
		t.Fatalf("--budget 9000 became %d — the old spelling did not count as typed", *budget)
	}
}

// One flag typed, the others left to the environment: the fallback is per-wall,
// not all-or-nothing.
func TestApplyExecEnvIsPerWall(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t, "-token-budget", "9000")
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "3",
		"AFORGE_EXEC_BUDGET":  "20000",
		"AFORGE_EXEC_TIMEOUT": "150",
	})
	if err := applyExecEnv(flags, env, turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns != 3 || *budget != 9000 || wall.wall != 150*time.Second {
		t.Fatalf("max-turns/token-budget/timeout = %d/%d/%s, want 3/9000/2m30s", *turns, *budget, wall.wall)
	}
}

// A variable that is set but empty is not a value; it must not become one.
func TestApplyExecEnvIgnoresEmptyVariables(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t)
	env := fakeEnv(map[string]string{
		"AFORGE_EXEC_TURNS":   "",
		"AFORGE_EXEC_BUDGET":  "   ",
		"AFORGE_EXEC_TIMEOUT": "",
	})
	if err := applyExecEnv(flags, env, turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns != 200 || *budget != 150000 || wall.wall != 0 {
		t.Fatalf("max-turns/token-budget/timeout = %d/%d/%s, want the defaults", *turns, *budget, wall.wall)
	}
}

// A typo in a wall must stop the run, not be silently dropped. A campaign that
// thinks it capped every call at 150 seconds because of an unnoticed typo
// measures the wrong thing all night.
func TestApplyExecEnvRefusesNonNumericValues(t *testing.T) {
	// AFORGE_EXEC_TIMEOUT is not on this list any more and `2m` is not the typo
	// to probe it with: the wall reads durations now, on the flag and in the
	// environment alike, so `2m` is a value there and `later` is the typo.
	for _, variable := range []string{"AFORGE_EXEC_TURNS", "AFORGE_EXEC_BUDGET"} {
		flags, turns, budget, timeout := execFlagsForTest(t)
		err := applyExecEnv(flags, fakeEnv(map[string]string{variable: "2m"}), turns, budget, timeout)
		if err == nil {
			t.Fatalf("%s=2m was accepted", variable)
		}
		if !strings.Contains(err.Error(), variable) {
			t.Fatalf("%s error = %q, want it to name the variable", variable, err)
		}
	}
	flags, turns, budget, wall := execFlagsForTest(t)
	err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TIMEOUT": "later"}), turns, budget, wall)
	if err == nil || !strings.Contains(err.Error(), "AFORGE_EXEC_TIMEOUT") {
		t.Fatalf("AFORGE_EXEC_TIMEOUT=later answered %v, want a refusal naming the variable", err)
	}
}

// Out-of-range values from the environment land in exactly the same guard the
// flags have always had, so there is one rule about what a wall may be.
func TestApplyExecEnvValuesStillMeetTheFlagGuards(t *testing.T) {
	flags, turns, budget, wall := execFlagsForTest(t)
	if err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TURNS": "0"}), turns, budget, wall); err != nil {
		t.Fatal(err)
	}
	if *turns > 0 {
		t.Fatalf("max-turns = %d, want the environment's 0 to reach the guard", *turns)
	}
	// THE WALL'S GUARD MOVED INTO THE WALL. A negative timeout used to be
	// carried past this function to a check further in; the duration flag
	// refuses it here, naming the variable that held it, which is one refusal
	// instead of two readings of one rule (wall.go).
	flags, turns, budget, wall = execFlagsForTest(t)
	err := applyExecEnv(flags, fakeEnv(map[string]string{"AFORGE_EXEC_TIMEOUT": "-1"}), turns, budget, wall)
	if err == nil || !strings.Contains(err.Error(), "AFORGE_EXEC_TIMEOUT") {
		t.Fatalf("AFORGE_EXEC_TIMEOUT=-1 answered %v, want a refusal naming the variable", err)
	}
}

// The help text is where a harness author finds out the variables exist.
func TestUsageMentionsExecEnvironmentFallbacks(t *testing.T) {
	// The environment table moved out of `--help` and into `aforge help env`
	// when `--help` was 127 lines and more than half of them were this table.
	// So the variables are looked for where they now are, and `--help` is held
	// to naming the door that carries them.
	for _, variable := range []string{"AFORGE_EXEC_TIMEOUT", "AFORGE_EXEC_BUDGET", "AFORGE_EXEC_TURNS"} {
		if !strings.Contains(environmentText, variable) {
			t.Fatalf("`aforge help env` does not mention %s", variable)
		}
	}
	if !strings.Contains(usageText, "aforge help env") {
		t.Fatal("`aforge --help` never says where the environment table went")
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
//
// THE SENTENCE IS ON THE OBJECT; WHICH FIELD CARRIES IT FOLLOWS THE RUNG. A run
// that never started says it under `error`; a run that started and broke says it
// under `incomplete`, because `error` means "it could not be run at all" and
// that run was run. Both rows are here so that neither door can lose it.
func TestExecJSONSaysWhyTheRunFailed(t *testing.T) {
	runErr := errors.New("node " + execNodeKey + ": API error (400): nosuch/model-xyz is not a valid model ID")
	for _, door := range []struct {
		what string
		// outcome is nil for the run that never started.
		outcome *exec.Outcome
		// field is the envelope key a script reads the sentence from.
		field string
	}{
		{"a run that could not be started", nil, "error"},
		{"a run that broke after it started", &exec.Outcome{Stop: exec.StopError, Turns: 3}, envelopeIncomplete},
	} {
		envelope := buildExecEnvelope(door.outcome, runErr, "")
		fields := envelopeFields(t, envelope)
		said, _ := fields[door.field].(string)
		if said == "" {
			t.Fatalf("%s carries stop=%q and no reason at all under %q, so a script can never learn why:\n  %v",
				door.what, envelope.Stop, door.field, fields)
		}
		if !strings.Contains(said, "nosuch/model-xyz is not a valid model ID") {
			t.Fatalf("%s names something other than the cause: %q", door.what, said)
		}
		for _, machinery := range []string{"API error", "node " + execNodeKey} {
			if strings.Contains(said, machinery) {
				t.Fatalf("%s leaks the internal verb %q into the machine contract: %q", door.what, machinery, said)
			}
		}
		if !strings.Contains(said, "aforge models") {
			t.Fatalf("%s never says what to do about it: %q", door.what, said)
		}
	}

	// AND THE `error` KEY IS EMPTY, NOT MISSING, ON THE RUN THAT STARTED.
	ran := buildExecEnvelope(&exec.Outcome{Stop: exec.StopError, Turns: 3}, runErr, "")
	encoded, err := json.Marshal(ran)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"error":""`) {
		t.Fatalf("a run that started and broke filled `error`, which means it could not be run at all:\n%s", encoded)
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

// A WALL UNDER A SECOND IS STILL A WALL. `--timeout` is a duration on every
// door that has one, and two of them used to fold it down to a whole number of
// seconds and multiply it back up. `--timeout 500ms` truncated to zero, and a
// zero wall is no wall at all — so the run was handed the FULL DEFAULT, the
// opposite of what was typed, and `1500ms` quietly became one second.
//
// THE SOURCE IS READ WITH ITS SPACES TAKEN OUT, because the first draft of this
// test matched `"/ time.Second"` and passed against `wall.wall/time.Second` —
// the defect written without a space. A guard that the defect can walk past is
// not a guard, so the comparison is made on a form the author's formatting
// cannot vary.
func TestAWallUnderASecondIsNotRoundedAwayOnAnyDoor(t *testing.T) {
	for _, source := range []string{"exec.go", "wake.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		// THE COMMENTS ARE NOT THE CODE. The second draft of this test read the
		// file as text and failed on a comment in exec.go that QUOTES the old
		// shape in order to explain why it is gone — so the tree is parsed and
		// only what runs is judged.
		var printed strings.Builder
		if err := printer.Fprint(&printed, token.NewFileSet(), parsed); err != nil {
			t.Fatal(err)
		}
		tight := strings.Join(strings.Fields(printed.String()), "")
		for _, shape := range []string{"wall.wall/time.Second", "time.Duration(*maxSeconds)", "time.Duration(int("} {
			if strings.Contains(tight, shape) {
				t.Errorf("%s folds a wall through whole seconds (%s); a duration flag is a duration all the way down",
					source, shape)
			}
		}
	}
}
