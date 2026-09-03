package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE LADDER, WRITTEN OUT ONCE MORE IN THE PLACE THAT CHECKS IT.
//
// This is the test that makes the ladder real. It names every rung, its number,
// the reason that produces it, and — for each of the three headless verbs — the
// ending in that verb's own vocabulary that lands on it. A change to `do`,
// `exec` or `run` that disagrees with the table fails here, by the name of the
// row it broke, rather than in a harness three weeks later.
//
// It is deliberately a SECOND SPELLING of exitLadder rather than a loop over
// it. A test that read the table and asserted the table would pass whatever the
// table said, which is not a test of anything.
func TestTheExitLadderIsOneTable(t *testing.T) {
	for _, rung := range []struct {
		code exitStatus
		// meaning is the sentence the manual and --help carry beside the number.
		meaning string
		// stop is the reason that produces this rung.
		stop stopReason
		// condition is what actually happened, in the words the change entry
		// and the before/after table use.
		condition string
	}{
		{code: 0, stop: stopDone, condition: "it is done and stdout is the answer",
			meaning: "it is done, and what is on stdout is the answer"},
		{code: 1, stop: stopError, condition: "it could not be run at all: no key, bad arguments, the store would not open",
			meaning: "it could not be run at all — no key, bad arguments, the store would not open"},
		{code: 2, stop: stopIncomplete, condition: "it ran and part of the work does not stand",
			meaning: "it ran and did not finish: part of the work does not stand"},
		{code: 3, stop: stopBudget, condition: "the token budget ran out",
			meaning: "a limit you set stopped it — the wall, the token budget, the turn cap, the price"},
		{code: 3, stop: stopTurnCap, condition: "the turn cap ran out",
			meaning: "a limit you set stopped it — the wall, the token budget, the turn cap, the price"},
		{code: 3, stop: stopDeadline, condition: "the wall arrived",
			meaning: "a limit you set stopped it — the wall, the token budget, the turn cap, the price"},
		{code: 3, stop: stopPrice, condition: "the price crossed the threshold you asked to be consulted about",
			meaning: "a limit you set stopped it — the wall, the token budget, the turn cap, the price"},
		{code: 4, stop: stopQuestion, condition: "it needed an answer and nobody was there",
			meaning: "it needs an answer from you and nobody was there"},
	} {
		t.Run(string(rung.stop)+"/"+rung.condition, func(t *testing.T) {
			if got := exitFor(rung.stop); got != rung.code {
				t.Fatalf("%q leaves with %d, and the table says %d", rung.stop, got, rung.code)
			}
			if got := exitMeaning(rung.code); got != rung.meaning {
				t.Fatalf("exit %d means %q, and the table says %q", rung.code, got, rung.meaning)
			}
		})
	}

	// Every reason this binary can name is on the ladder. A new stopReason
	// added without a rung would otherwise fall through to exitIncomplete and
	// nobody would find out.
	for _, stop := range []stopReason{
		stopDone, stopError, stopIncomplete, stopBudget, stopTurnCap, stopDeadline, stopPrice, stopQuestion,
	} {
		named := false
		for _, rung := range exitLadder {
			for _, listed := range rung.Stops {
				if listed == stop {
					named = true
				}
			}
		}
		if !named {
			t.Fatalf("%q is a reason this binary can report and no rung claims it", stop)
		}
	}

	// AND THE THREE VERBS AGREE WITH IT, each in its own vocabulary. These are
	// the rows that fail when somebody changes what a verb decides.
	t.Run("do", func(t *testing.T) {
		for _, row := range []struct {
			what string
			stop stopReason
			want exitStatus
		}{
			{what: "the deliverable stands", stop: stopDone, want: exitDone},
			{what: "the store would not open", stop: stopError, want: exitCannotRun},
			{what: "a part of the job failed", stop: stopIncomplete, want: exitIncomplete},
			{what: "the plan crossed the consent threshold", stop: stopPrice, want: exitLimit},
			{what: "the wall arrived", stop: stopDeadline, want: exitLimit},
			{what: "it stopped to ask", stop: stopQuestion, want: exitUnanswered},
		} {
			outcome := headlessOutcome{stop: row.stop}
			if got := outcome.status(); got != row.want {
				t.Fatalf("do: %s leaves with %d, want %d", row.what, got, row.want)
			}
		}
	})

	t.Run("exec", func(t *testing.T) {
		for _, row := range []struct {
			what    string
			outcome exec.Outcome
			runErr  error
			stop    stopReason
			want    exitStatus
		}{
			{what: "the model answered", outcome: exec.Outcome{Stop: exec.StopDone, Text: "here"},
				stop: stopDone, want: exitDone},
			// THE PROVIDER FAILING IS A RUN THAT RAN. It is `incomplete` and
			// not `error`, because an outcome exists at all only after the
			// executor started (execStop); exit 1 is `aforge exec` refusing
			// before it opens a connection, and never a run that spent money.
			{what: "the provider failed mid-run", outcome: exec.Outcome{Stop: exec.StopError, Turns: 9},
				runErr: errors.New("upstream returned 500"), stop: stopIncomplete, want: exitIncomplete},
			{what: "it finished with nothing to show", outcome: exec.Outcome{Stop: exec.StopDone, Text: "  "},
				stop: stopIncomplete, want: exitIncomplete},
			{what: "the token budget ran out", outcome: exec.Outcome{Stop: exec.StopBudget, Text: "half"},
				stop: stopBudget, want: exitLimit},
			{what: "the turn cap ran out", outcome: exec.Outcome{Stop: exec.StopTurnCap, Text: "half"},
				stop: stopTurnCap, want: exitLimit},
			{what: "the wall arrived", outcome: exec.Outcome{Stop: exec.StopDeadline, Text: "half"},
				stop: stopDeadline, want: exitLimit},
			{what: "it was cancelled between turns", outcome: exec.Outcome{Stop: exec.StopCancelled},
				stop: "cancelled", want: exitIncomplete},
		} {
			outcome := row.outcome
			if got := execStop(&outcome, row.runErr); got != row.stop {
				t.Fatalf("exec: %s stopped %q, want %q", row.what, got, row.stop)
			}
			if got := execExit(&outcome, row.runErr); got != row.want {
				t.Fatalf("exec: %s leaves with %d, want %d", row.what, got, row.want)
			}
		}
	})

	t.Run("run", func(t *testing.T) {
		run := subharnessRun{journal: &runJournal{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		finished := exec.RunResult{Output: json.RawMessage(`{"fixed":true}`), Report: "the test passes"}
		if err := reportSubharnessRun(run, finished); err != nil {
			t.Fatalf("run: a program that finished left with %v, want 0", err)
		}
		var status exitStatus
		unfinished := exec.RunResult{Incomplete: "it ran out of turns"}
		err := reportSubharnessRun(run, unfinished)
		if !errors.As(err, &status) || status != exitIncomplete {
			t.Fatalf("run: a program that did not finish left with %v, want 2", err)
		}
		failed := subharnessRun{journal: &runJournal{}, asJSON: true,
			stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		err = failed.sayFailedEnvelope(errors.New("there is no subharness called \"nosuch\""))
		if !errors.As(err, &status) || status != exitCannotRun {
			t.Fatalf("run: a program that could not be run left with %v, want 1", err)
		}
	})
}

// ONE ENVELOPE, AND THE SAME KEYS OUT OF ALL THREE DOORS.
//
// The defect this pins: `do` called the answer `deliverable` and `exec` called
// it `text`; `do` reported `seconds` and `exec` reported `elapsed_ms`. A tool
// that parsed one could not parse the other. A failure and a success on each of
// the three verbs now come out of the one builder, so the keys cannot diverge
// again without this failing.
func TestTheThreeVerbsReturnOneEnvelope(t *testing.T) {
	runEnvelope := func(t *testing.T, run subharnessRun, do func(subharnessRun) error) map[string]any {
		t.Helper()
		stdout := &bytes.Buffer{}
		run.stdout, run.stderr, run.asJSON, run.journal = stdout, &bytes.Buffer{}, true, &runJournal{}
		_ = do(run)
		var fields map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
			t.Fatalf("run --json is not one object: %v\n%s", err, stdout.String())
		}
		return fields
	}
	decode := func(t *testing.T, envelope resultEnvelope) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}

	shapes := map[string]map[string]any{
		"do/success": decode(t, errandEnvelope(headlessOutcome{
			stop: stopDone, Deliverable: "the note is written", Artifacts: []string{"NOTE.md"},
			Settled: true, Nodes: 3, Seconds: 4.5, Spend: 0.02, Model: "openai/gpt-5",
		})),
		"do/failure": decode(t, errandEnvelope(headlessOutcome{
			stop: stopError, Artifacts: []string{}, Error: "the store directory could not be made",
		})),
		"exec/success": decode(t, buildExecEnvelope(
			&exec.Outcome{Stop: exec.StopDone, Text: "the answer", Turns: 2, Elapsed: time.Second}, nil, "openai/gpt-5", "")),
		"exec/failure": decode(t, buildExecEnvelope(
			&exec.Outcome{Stop: exec.StopError}, errors.New("no key"), "openai/gpt-5", "")),
		"run/success": runEnvelope(t, subharnessRun{model: "openai/gpt-5"}, func(run subharnessRun) error {
			return reportSubharnessRun(run, exec.RunResult{
				Output: json.RawMessage(`{"fixed":true}`), Report: "the test passes"})
		}),
		"run/failure": runEnvelope(t, subharnessRun{model: "openai/gpt-5"}, func(run subharnessRun) error {
			return run.sayFailedEnvelope(errors.New("there is no subharness called \"nosuch\""))
		}),
	}

	for name, fields := range shapes {
		for _, key := range envelopeContract {
			if _, present := fields[key]; !present {
				missing := make([]string, 0, len(fields))
				for got := range fields {
					missing = append(missing, got)
				}
				sort.Strings(missing)
				t.Fatalf("%s does not carry %q — it carries %s", name, key, strings.Join(missing, ", "))
			}
		}
		// `files` is never null: a caller ranging over it must not crash on a
		// run that wrote nothing.
		if fields["files"] == nil {
			t.Fatalf("%s carries a null files list", name)
		}
		// `ok` and `stop` agree with each other through the one ladder.
		ok, isBool := fields["ok"].(bool)
		if !isBool {
			t.Fatalf("%s carries a non-boolean ok: %v", name, fields["ok"])
		}
		stop, isString := fields["stop"].(string)
		if !isString || stop == "" {
			t.Fatalf("%s carries no stop reason: %v", name, fields["stop"])
		}
		if want := exitFor(stopReason(stop)) == exitDone; ok != want {
			t.Fatalf("%s says ok=%v with stop=%q, which the ladder disagrees with", name, ok, stop)
		}
		if strings.HasSuffix(name, "/failure") && ok {
			t.Fatalf("%s reports a failure as ok", name)
		}
	}

	// AND THE OLD SPELLINGS ARE STILL THERE, for one release, on the two verbs
	// that published them.
	for _, old := range []string{"deliverable", "artifacts", "spend", "nodes", "settled", "subharness"} {
		if _, present := shapes["do/success"][old]; !present {
			t.Fatalf("do --json dropped its old field %q inside one release", old)
		}
	}
	for _, old := range []string{"text", "artifacts", "turns", "elapsed_ms", "usage"} {
		if _, present := shapes["exec/success"][old]; !present {
			t.Fatalf("exec --json dropped its old field %q inside one release", old)
		}
	}
	// The new spelling is authoritative: where a name is in both, the contract's
	// value is what a caller reads.
	if shapes["do/success"]["answer"] != shapes["do/success"]["deliverable"] {
		t.Fatal("do's answer and its old deliverable say different things")
	}
	if shapes["exec/success"]["answer"] != shapes["exec/success"]["text"] {
		t.Fatal("exec's answer and its old text say different things")
	}

	// `ok` IS NOT `settled` UNDER A NEW NAME. A run that asked a question is
	// over — settled — and did nothing; ok must be false on it or every refusal
	// reads as a success.
	blocked := decode(t, errandEnvelope(headlessOutcome{
		stop: stopQuestion, Settled: true, BlockedOn: "which ledger did you mean?",
	}))
	if blocked["ok"] != false {
		t.Fatalf("a settled run that answered nothing reported ok=true: %v", blocked)
	}
	if blocked["settled"] != true {
		t.Fatalf("settled changed meaning: %v", blocked)
	}
}

// THE ESCAPE HATCH, and nothing but it.
//
// AFORGE_EXIT_CODES=legacy puts `aforge exec`'s old 2/3/4/5/6 back for one
// release. This asserts it restores exactly those numbers, and that it changes
// nothing else — not `do`, not `run`, not one field of the envelope.
func TestLegacyExitCodesRestoresExecsOldRungsAndNothingElse(t *testing.T) {
	rows := []struct {
		name    string
		outcome exec.Outcome
		runErr  error
		legacy  exitStatus
		ladder  exitStatus
	}{
		{name: "done", outcome: exec.Outcome{Stop: exec.StopDone, Text: "answer"}, legacy: 0, ladder: exitDone},
		{name: "budget", outcome: exec.Outcome{Stop: exec.StopBudget, Text: "half"}, legacy: 2, ladder: exitLimit},
		{name: "turn cap", outcome: exec.Outcome{Stop: exec.StopTurnCap, Text: "half"}, legacy: 3, ladder: exitLimit},
		{name: "deadline", outcome: exec.Outcome{Stop: exec.StopDeadline, Text: "half"}, legacy: 4, ladder: exitLimit},
		// AN OUTCOME EXISTS ONLY AFTER THE RUN STARTED, so on the ladder these
		// two are `ran and did not finish` — the rung the old 5 always meant,
		// under its new number. The legacy column is unmoved, which is the
		// whole point of the hatch.
		{name: "error", outcome: exec.Outcome{Stop: exec.StopError}, legacy: 5, ladder: exitIncomplete},
		{name: "done with nothing to show", outcome: exec.Outcome{Stop: exec.StopDone}, legacy: 6, ladder: exitIncomplete},
		{name: "an error came back", outcome: exec.Outcome{Stop: exec.StopError},
			runErr: errors.New("no key"), legacy: 5, ladder: exitIncomplete},
	}

	for _, row := range rows {
		outcome := row.outcome
		if got := execExit(&outcome, row.runErr); got != row.ladder {
			t.Fatalf("%s leaves with %d off the ladder, want %d", row.name, got, row.ladder)
		}
	}

	t.Setenv("AFORGE_EXIT_CODES", "legacy")
	for _, row := range rows {
		outcome := row.outcome
		if got := execExit(&outcome, row.runErr); got != row.legacy {
			t.Fatalf("under legacy, %s leaves with %d, want the old %d", row.name, got, row.legacy)
		}
	}

	// AND NOTHING ELSE MOVES. `do` and `run` keep the one ladder, and the
	// envelope is untouched: the hatch is about exec's exit numbers and about
	// nothing else at all.
	if got := (headlessOutcome{stop: stopQuestion}).status(); got != exitUnanswered {
		t.Fatalf("the legacy switch reached `do`: a question left with %d, want 4", got)
	}
	if got := (headlessOutcome{stop: stopIncomplete}).status(); got != exitIncomplete {
		t.Fatalf("the legacy switch reached `do`: an incomplete run left with %d, want 2", got)
	}
	run := subharnessRun{journal: &runJournal{}, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	var status exitStatus
	err := reportSubharnessRun(run, exec.RunResult{Incomplete: "it ran out of turns"})
	if !errors.As(err, &status) || status != exitIncomplete {
		t.Fatalf("the legacy switch reached `run`: %v, want exit 2", err)
	}
	envelope := buildExecEnvelope(&exec.Outcome{Stop: exec.StopDone, Text: "answer", Turns: 1}, nil, "openai/gpt-5", "")
	if !envelope.OK || envelope.Stop != stopDone || envelope.Answer != "answer" {
		t.Fatalf("the legacy switch changed the envelope: %+v", envelope)
	}

	t.Setenv("AFORGE_EXIT_CODES", "")
	if got := execExit(&exec.Outcome{Stop: exec.StopBudget, Text: "half"}, nil); got != exitLimit {
		t.Fatalf("the hatch did not close: budget left with %d, want 3", got)
	}
}

// TestTheEnvelopeNamesItsRunAndCountsItsCallsAndRounds is the half of row 5
// that a script reads.
//
// `do --json` carried spend, steps, seconds, settled and the models, and no
// call count and no round count — so a developer who wanted either went to
// `~/.aforge/logs/calls.jsonl` and counted rows by hand. The two figures are on
// the object now, and beside them the run id that makes that file joinable to
// this one.
//
// THE THREE KEYS ARE ALWAYS THERE. A verb that measures none of them publishes
// them at their zero, exactly as `steps` has always done for a saved program: a
// caller reaching for a key that vanished is a caller crashing, and that is the
// one thing this contract may not do.
func TestTheEnvelopeNamesItsRunAndCountsItsCallsAndRounds(t *testing.T) {
	for name, result := range map[string]runResult{
		"a run that measured nothing": {},
		"a run that measured all three": {
			Run: "0123456789abcdef", Calls: 422, Rounds: 8,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fields := envelopeFields(t, buildResultEnvelope(result))
			for _, key := range []string{"run", "calls", "rounds"} {
				if _, present := fields[key]; !present {
					t.Errorf("the envelope has no %q key at all — a caller that reaches for one "+
						"which vanished is a caller crashing:\n%v", key, fields)
				}
			}
		})
	}

	// And the values are the ones the run was given, not a shape with the right
	// keys and nothing behind them.
	fields := envelopeFields(t, buildResultEnvelope(runResult{
		Run: "0123456789abcdef", Calls: 422, Rounds: 8,
	}))
	if fields["run"] != "0123456789abcdef" {
		t.Errorf("run = %v, and the id names the folder the debug record went to and every row "+
			"this run wrote into the call log", fields["run"])
	}
	if fields["calls"] != float64(422) {
		t.Errorf("calls = %v, want the 422 the run made", fields["calls"])
	}
	if fields["rounds"] != float64(8) {
		t.Errorf("rounds = %v, want the 8 rounds of work it bought", fields["rounds"])
	}
}
