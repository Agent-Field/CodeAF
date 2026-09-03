package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// ONE EXIT LADDER AND ONE RESULT ENVELOPE, for every headless verb this binary
// has: `aforge do`, `aforge exec`, and `aforge run`.
//
// THIS FILE IS THE WHOLE OF BOTH CONTRACTS. It exists because there used to be
// three exit tables, each written where its own command was, and two of them
// said the opposite thing with the same number: `do` exit 1 meant "nothing
// usable came back", `aforge run` exit 1 meant "it could not be run at
// all", and `exec` returned 2, 3, 4, 5 and 6 and never returned 1. A script
// that branched across any two of them branched WRONG on at least one, and
// there was no way to read the code and find out which — the tables were three
// separate pieces of prose that had never been put side by side.
//
// The `--json` shapes disagreed the same way. `do` called the answer
// `deliverable` and `exec` called it `text`; `do` reported `seconds` and `exec`
// reported `elapsed_ms`; `do` had `settled` and `exec` had `stop`. A tool that
// parsed one could not parse the other, and nothing anywhere said so.
//
// So: one table below, one builder below, and NO COMMAND WRITES AN EXIT NUMBER
// OR AN ENVELOPE FIELD OF ITS OWN. A verb decides what happened — which is its
// own business and nobody else's — and says it as a [stopReason]. What that
// costs the process, and what a caller reads on stdout, are decided here.

// exitStatus ends the process with a particular code and nothing more said. The
// command has already written its result to the right stream; an "error:" line
// after an honest partial answer would only be noise.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// THE LADDER. Five rungs, and every headless verb leaves on one of them.
//
// The numbers are ordered by how much the caller has to do about it: 0 needs
// nothing, 1 is a machine that was never able to start, and 2, 3 and 4 are
// three different reasons a run that DID start did not land — none of which is
// the other, and all three of which used to share a number somewhere.
const (
	// exitDone: it is done, and what is on stdout is the answer.
	exitDone exitStatus = 0
	// exitCannotRun: it could not be run at all — no key, bad arguments, a store
	// that would not open, a name that is not a program. Nothing was attempted,
	// so nothing was spent and there is nothing on stdout to read.
	//
	// THAT SENTENCE IS A PROMISE AND NOT A DESCRIPTION. A run that started and
	// then failed leaves on exitIncomplete however early it broke, because it
	// may have spent money and what it did manage is worth reading; only a
	// refusal BEFORE any work starts belongs here. `aforge exec` published a
	// mid-run provider failure on this rung for a while, and a script reading
	// the ladder retried a run that had already cost real money as though it
	// had never begun (execStop).
	exitCannotRun exitStatus = 1
	// exitIncomplete: it ran and it did not finish. Part of the work does not
	// stand — a step failed, a delivery did not land whole, or the run produced
	// nothing at all. Whatever it DID manage is on stdout and is worth reading.
	exitIncomplete exitStatus = 2
	// exitLimit: a limit you set stopped it — the wall, the token budget, the
	// turn cap, or the price you asked to be consulted about. The work was going
	// when it was cut off; raising the limit and running it again is the remedy.
	exitLimit exitStatus = 3
	// exitUnanswered: it needs an answer from you and nobody was there. A
	// headless run has no keyboard, so a question ends it. The question is on
	// stderr verbatim and in `blocked_on`; say the answer in the ask itself and
	// run it again.
	exitUnanswered exitStatus = 4
)

// stopReason is the ONE vocabulary the `stop` field speaks, across all three
// verbs. It is what a script should have been reading all along: the exit code
// says how much is wrong, `stop` says what.
//
// The five words `aforge exec` already published — done, budget, turn-cap,
// deadline, error — are kept spelled exactly as they were, because harnesses in
// the wild read them. The three that are new name states exec never had.
type stopReason string

const (
	stopDone       stopReason = "done"       // the work is finished and stdout is the answer
	stopError      stopReason = "error"      // it could not be run at all
	stopIncomplete stopReason = "incomplete" // it ran and part of it does not stand
	stopBudget     stopReason = "budget"     // the token budget ran out
	stopTurnCap    stopReason = "turn-cap"   // the turn cap ran out
	stopDeadline   stopReason = "deadline"   // the wall arrived
	stopPrice      stopReason = "price"      // the price crossed what you asked to approve
	stopQuestion   stopReason = "question"   // it asked something and nobody was there
)

// exitRung is one row of the table. The meaning is a full sentence because it
// is the sentence: `internal/manual/chat/exit-codes-and-json.md` prints these
// words and so does the before/after in docs/design/polish/envelope-and-exits.md.
type exitRung struct {
	Code exitStatus
	// Meaning is what the number means, in the words a person reads.
	Meaning string
	// Short is the same thing in a clause, for the one line `--help` prints
	// beside each headless verb. It is here rather than typed out in main.go
	// three times, because it WAS typed out three times and the three said
	// different things.
	Short string
	// Stops are the reasons that produce this rung, and they are the only
	// conditions that do. A [stopReason] that appears in no row lands on
	// exitIncomplete, which is the honest answer for "it ran, and this build
	// does not have a better word for how it ended".
	Stops []stopReason
}

// exitLadder is THE TABLE. Every rung, its number, and the conditions that
// produce it, in one place so that a change to any of the three verbs that
// disagrees with it fails by name in TestTheExitLadderIsOneTable.
var exitLadder = []exitRung{
	{
		Code:    exitDone,
		Short:   "done",
		Meaning: "it is done, and what is on stdout is the answer",
		Stops:   []stopReason{stopDone},
	},
	{
		Code:    exitCannotRun,
		Short:   "could not be run at all",
		Meaning: "it could not be run at all — no key, bad arguments, the store would not open",
		Stops:   []stopReason{stopError},
	},
	{
		Code:    exitIncomplete,
		Short:   "ran and did not finish",
		Meaning: "it ran and did not finish: part of the work does not stand",
		Stops:   []stopReason{stopIncomplete},
	},
	{
		Code:    exitLimit,
		Short:   "a limit you set stopped it",
		Meaning: "a limit you set stopped it — the wall, the token budget, the turn cap, the price",
		Stops:   []stopReason{stopBudget, stopTurnCap, stopDeadline, stopPrice},
	},
	{
		Code:    exitUnanswered,
		Short:   "needed an answer and nobody was there",
		Meaning: "it needs an answer from you and nobody was there",
		Stops:   []stopReason{stopQuestion},
	},
}

// exitLadderLine is the ladder in one line, for the help table. It is BUILT
// FROM THE TABLE rather than typed out beside each verb, because it used to be
// typed out beside each verb and the three copies disagreed — which is the
// defect this whole file exists to close.
var exitLadderLine = func() string {
	var parts []string
	for _, rung := range exitLadder {
		parts = append(parts, fmt.Sprintf("%d %s", int(rung.Code), rung.Short))
	}
	return "exit " + strings.Join(parts, " · ")
}()

// exitFor is the only reader of the table, and therefore the only place in this
// binary where a stop reason becomes an exit code.
//
// An unknown reason is exitIncomplete rather than exitCannotRun or a panic: a
// run whose ending this build has no word for still RAN, and telling a script
// "it could not be started" about a run that spent money would be worse than
// telling it "something did not land".
func exitFor(stop stopReason) exitStatus {
	for _, rung := range exitLadder {
		for _, named := range rung.Stops {
			if named == stop {
				return rung.Code
			}
		}
	}
	return exitIncomplete
}

// exitMeaning is the sentence beside a number, for anything that prints the
// table. An unknown code renders as nothing, which is the emptiness law.
func exitMeaning(code exitStatus) string {
	for _, rung := range exitLadder {
		if rung.Code == code {
			return rung.Meaning
		}
	}
	return ""
}

// legacyExitCodes is THE ESCAPE HATCH, and it is one line and one release.
//
// `aforge exec`'s old rungs — 2 budget, 3 turn cap, 4 deadline, 5 error, 6
// finished with nothing to show — are read by harnesses that were written
// against them, and this change moves every one of those numbers. Setting
// AFORGE_EXIT_CODES=legacy puts exec's old table back and CHANGES NOTHING ELSE:
// not `do`, not `run`, not one field of the envelope, not one word on stderr.
//
// IT IS NOT A GENERAL COMPATIBILITY MODE AND MUST NOT BECOME ONE. If a second
// thing is ever tempted to read this variable, that is the signal to give that
// thing its own switch and its own removal date, not to widen this one.
func legacyExitCodes() bool {
	return strings.TrimSpace(os.Getenv("AFORGE_EXIT_CODES")) == "legacy"
}

// legacyExitCodesHelp is the one line `--help` carries about the hatch. It is
// spelled once so the manual page and the flag table cannot disagree.
const legacyExitCodesHelp = `"legacy" restores ` + "`aforge exec`" + `'s old 2/3/4/5/6 exit codes for one
                       release, and changes nothing else`

// jsonFlagHelp is the ONE sentence `--json` is described with, on `do`, `exec`
// and `run` alike. It is spelled once for the same reason modelFlagHelp is: the
// three doors return the same object, and three help strings describing it
// would be three chances for one of them to describe it wrongly.
const jsonFlagHelp = "print one machine-readable object instead of the answer: ok says whether the work stands, " +
	"stop says why it ended, answer carries what was produced, files what it wrote, and error the sentence " +
	"when it could not be run at all"

// ---------------------------------------------------------------------------
// The envelope.

// resultEnvelope is THE MACHINE CONTRACT for `--json` and `-o` on `do`, `exec`
// and `run`. One object, on stdout, always parseable, printed even when the run
// failed.
//
// The guarantee, which is also written in the manual: WITHIN A RELEASE A FIELD
// IS NEVER REMOVED AND NEVER CHANGES MEANING. New fields may appear. `error`
// non-empty means the run did not produce an answer; `stop` always names why it
// ended; `ok` is true on exactly the runs that leave with exit 0.
//
// Nothing outside [buildResultEnvelope] may construct one, which is what keeps
// stdout, `-o` and the sentence on stderr from disagreeing — they are three
// renderings of this one object and never three readings of the same facts.
type resultEnvelope struct {
	// OK is the verdict: the work stands. It is true on exactly the runs that
	// leave with exit 0 and false on every other, so a script may branch on
	// either and get the same answer.
	//
	// IT IS NOT `do`'s OLD `settled` FIELD UNDER A NEW NAME, whatever the
	// rename in COMMANDS.md says. `settled` means "nothing this process is
	// waiting for can still move", which is true of a run that asked a question
	// and did nothing — settled: true under exit 1. A caller that read the new
	// name with the old meaning would record every refusal as a success. So
	// `settled` keeps its own meaning, in its own field, unchanged.
	OK bool `json:"ok"`
	// Stop names why it ended, in the one vocabulary above.
	Stop stopReason `json:"stop"`
	// Answer is what was produced, in prose: `do`'s deliverable, `exec`'s text,
	// `run`'s typed output. Empty on a run that produced nothing.
	Answer string `json:"answer"`
	// Files are the paths the run wrote, as the run's own registry recorded
	// them. Never null: a run that wrote nothing carries an empty list, because
	// a caller ranging over null is a caller crashing on a successful run.
	Files []string `json:"files"`
	// Error is why the run COULD NOT BE RUN AT ALL, in the same words a person
	// would have read on stderr, with no wrapped Go chain (plainwords.go).
	// Empty on every run that started, however it ended: a limit that cut a run
	// short and a provider that gave up at turn nine both say why under
	// `incomplete`, beside whatever answer the run had managed.
	Error string `json:"error"`
	// SpendUSD is what this run cost, whole, in dollars.
	SpendUSD float64 `json:"spend_usd"`
	// Tokens is what it cost in tokens, prompt and completion.
	Tokens envelopeTokens `json:"tokens"`
	// Seconds is how long it took, wall clock.
	Seconds float64 `json:"seconds"`
	// Model is the model the work ran on, as the seat ladder resolved it.
	Model string `json:"model"`
	// Steps is how many pieces of work ran: `do`'s nodes, `exec`'s turns. A
	// saved program DOES NOT MEASURE IT, and the key is still there with
	// nothing behind it — a machine contract keeps its keys even where the
	// screen would print nothing, because a caller that reaches for a key which
	// vanished is a caller crashing. `0` here is an absent measurement and not
	// a count of zero, which is why nothing may report it as one.
	Steps int `json:"steps"`

	// extra is what one verb carries beyond the contract, and it is two things:
	// the OLD field names, kept readable for one release so that a tool written
	// against `do --json` or `exec --json` keeps working, and the facts only one
	// verb has — `do`'s two seats and the rung that chose each, `run`'s typed
	// output. It is unexported and therefore invisible to the encoder;
	// [resultEnvelope.MarshalJSON] merges it in, and THE CONTRACT ALWAYS WINS a
	// collision, so nothing here can quietly overwrite a field above.
	extra map[string]any
}

// envelopeTokens is the token half of the bill. Two numbers, because those are
// the two a caller comparing runs actually divides by.
type envelopeTokens struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// MarshalJSON writes the contract, then the old spellings underneath it.
func (e resultEnvelope) MarshalJSON() ([]byte, error) {
	// A type with no methods, so this does not call itself.
	type contract resultEnvelope
	encoded, err := json.Marshal(contract(e))
	if err != nil {
		return nil, err
	}
	if len(e.extra) == 0 {
		return encoded, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	for name, value := range e.extra {
		if _, taken := fields[name]; taken {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[name] = raw
	}
	return json.Marshal(fields)
}

// runResult is what a verb hands the builder: the facts of one run, in nobody's
// vocabulary but this file's. Each of the three verbs fills it from its own
// result type, and that mapping is the only place a verb's private shape and
// the public contract meet.
type runResult struct {
	Stop      stopReason
	Answer    string
	Files     []string
	Error     string
	SpendUSD  float64
	TokensIn  int
	TokensOut int
	Seconds   float64
	Model     string
	Steps     int
	// Extra is this verb's own fields: its old spellings, and whatever it knows
	// that the contract has no room for. Nil for a verb with neither.
	Extra map[string]any
}

// buildResultEnvelope is THE ONE PLACE the machine contract is built, so the
// object written to `--json`, the object written to `-o` and what the exit code
// says cannot drift apart.
func buildResultEnvelope(result runResult) resultEnvelope {
	stop := result.Stop
	if stop == "" {
		// A verb that named no reason: an error says the run never produced an
		// answer, and anything else is a finished run. Guessed here rather than
		// at four call sites, and there is exactly one right guess.
		stop = stopDone
		if strings.TrimSpace(result.Error) != "" {
			stop = stopError
		}
	}
	files := result.Files
	if files == nil {
		files = []string{}
	}
	return resultEnvelope{
		OK:       exitFor(stop) == exitDone,
		Stop:     stop,
		Answer:   result.Answer,
		Files:    files,
		Error:    result.Error,
		SpendUSD: result.SpendUSD,
		Tokens:   envelopeTokens{In: result.TokensIn, Out: result.TokensOut},
		Seconds:  result.Seconds,
		Model:    result.Model,
		Steps:    result.Steps,
		extra:    result.Extra,
	}
}

// envelopeIncomplete is the ONE name for "the reason it did not finish", and it
// is spelled here because two verbs publish it: `aforge run` when a saved
// program stopped part of the way through, and `aforge exec` when a limit cut a
// run that had already produced text.
//
// IT IS NOT `error`, AND THAT IS THE WHOLE POINT OF IT. `error` means the run
// never produced an answer at all; a run stopped by its own budget with partial
// text produced one. The limit's diagnostic is worth keeping machine-readable,
// so it gets a field with a documented meaning of its own rather than squatting
// in one whose meaning it contradicts.
const envelopeIncomplete = "incomplete"

// envelopeContract names every field of the contract above, in the order the
// struct declares them. It is here so a test can assert that all three verbs
// return the same keys without restating the list, and so that adding a field
// to the struct without adding it here fails rather than passing silently.
var envelopeContract = []string{
	"ok", "stop", "answer", "files", "error",
	"spend_usd", "tokens", "seconds", "model", "steps",
}

// ---------------------------------------------------------------------------
// The old spellings, one small function per verb, kept beside the contract they
// are deprecated against so that removing them in a release's time is one edit.

// legacyErrandFields are `aforge do --json`'s field names as they were before
// the envelope. Every one of them is going away after one release; the new
// spelling for each is named in docs/design/polish/envelope-and-exits.md.
//
// `settled`, `spend_work`, `spend_overhead`, `blocked_on`, `learned`,
// `plan_model`, the two `*_source` fields and `subharness` are NOT duplicates of
// anything in the contract — they are facts only `do` has — and they stay for
// that reason rather than for compatibility.
func legacyErrandFields(outcome headlessOutcome) map[string]any {
	fields := map[string]any{
		"deliverable":    outcome.Deliverable,
		"artifacts":      outcome.Artifacts,
		"spend":          outcome.Spend,
		"spend_work":     outcome.SpendWork,
		"spend_overhead": outcome.SpendOverhead,
		"nodes":          outcome.Nodes,
		"settled":        outcome.Settled,
		// The two seats and the rung that chose each, which is the only way a
		// campaign can read back what actually ran (#166).
		"plan_model":        outcome.PlanModel,
		"model_source":      outcome.ModelSource,
		"plan_model_source": outcome.PlanModelSource,
		"subharness":        outcome.Subharness,
	}
	if fields["artifacts"] == nil {
		fields["artifacts"] = []string{}
	}
	// These two were omitempty and stay omitempty: a caller that tested for the
	// key's presence must keep getting the same answer.
	if strings.TrimSpace(outcome.BlockedOn) != "" {
		fields["blocked_on"] = outcome.BlockedOn
	}
	if len(outcome.Learned) > 0 {
		fields["learned"] = outcome.Learned
	}
	return fields
}

// legacyExecFields are `aforge exec --json`'s field names as they were before
// the envelope: `text` is now `answer`, `artifacts` is `files`, `turns` is
// `steps`, `elapsed_ms` is `seconds`, and `usage` is `tokens` plus `spend_usd`.
// All five go away after one release.
func legacyExecFields(outcome *exec.Outcome) map[string]any {
	if outcome == nil {
		return nil
	}
	artifacts := make([]string, len(outcome.Artifacts))
	copy(artifacts, outcome.Artifacts)
	return map[string]any{
		"text":       outcome.Text,
		"artifacts":  artifacts,
		"turns":      outcome.Turns,
		"elapsed_ms": outcome.Elapsed.Milliseconds(),
		"usage":      outcome.Usage,
	}
}
