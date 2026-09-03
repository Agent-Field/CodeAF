package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// ── `error` MEANS ONE THING, AND A LIMIT IS NOT IT ──────────────────────────
//
// envelope.go states the contract in the field's own doc comment: `error` is
// "why the run did not produce an answer", and is "empty on every run that
// produced one". `--json` is a contract a script is written against, so that
// sentence is the law and the code moves to match it.
//
// `aforge exec` did not. It copied EVERY executor error into `error`, while
// [execStop] deliberately keeps `budget`, `turn-cap` and `deadline` when the
// executor hands back an outcome and an error together — so a run cut off by
// its own token budget, holding half an answer, published the answer AND an
// error at once. A script following the written contract either threw the
// partial answer away or reported a startup failure that never happened.

// envelopeFields is the object a caller actually parses: the envelope through
// its own marshaller, so the extra fields a verb carries are read the same way
// a script reads them.
func envelopeFields(t *testing.T, envelope resultEnvelope) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal the envelope: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("the envelope did not parse (%v):\n%s", err, encoded)
	}
	return fields
}

// A LIMIT THAT CUT A RUN SHORT PUBLISHES THE ANSWER IT GOT, AND NO `error`.
//
// The three limits are one row each, because they are three different stops
// that [execStop] preserves for the same reason and any of them could be the one
// somebody re-adds the old line for.
func TestALimitedExecRunPublishesItsAnswerAndNoError(t *testing.T) {
	for _, cut := range []struct {
		name string
		stop exec.StopReason
		want stopReason
		// said is what the executor handed back beside the outcome, wrapped in
		// the node key the way internal/exec wraps everything.
		said string
	}{
		{"the token budget", exec.StopBudget, stopBudget, "node task-1: token budget exhausted after 12 turns"},
		{"the turn cap", exec.StopTurnCap, stopTurnCap, "node task-1: turn cap reached"},
		{"the wall", exec.StopDeadline, stopDeadline, "node task-1: deadline exceeded"},
	} {
		t.Run(cut.name, func(t *testing.T) {
			outcome := &exec.Outcome{
				Stop:    cut.stop,
				Text:    "half of an answer, which is the half worth reading",
				Turns:   12,
				Elapsed: 90 * time.Second,
			}
			envelope := buildExecEnvelope(outcome, errors.New(cut.said), "a/model")
			fields := envelopeFields(t, envelope)

			if envelope.Stop != cut.want {
				t.Fatalf("%s ended with stop %q, want %q", cut.name, envelope.Stop, cut.want)
			}
			if envelope.Answer != outcome.Text {
				t.Fatalf("%s threw the partial answer away\n  answer: %q\n  want:   %q",
					cut.name, envelope.Answer, outcome.Text)
			}
			if envelope.Error != "" {
				t.Fatalf("%s published an answer AND an error, which the contract forbids\n"+
					"  answer: %q\n  error:  %q\n"+
					"  `error` is why the run did not produce an answer, and this run produced one",
					cut.name, envelope.Answer, envelope.Error)
			}
			if got, _ := fields["error"].(string); got != "" {
				t.Fatalf("%s put %q in the `error` field a script reads", cut.name, got)
			}
			// AND THE DIAGNOSTIC IS NOT LOST. It moves to `incomplete`, which is
			// already the name `aforge run` publishes "the reason it did not
			// finish" under, in the words a person would have read on stderr and
			// with no wrapped Go chain in front of them.
			said, ok := fields[envelopeIncomplete].(string)
			if !ok {
				t.Fatalf("%s says nothing about why it stopped: the envelope has no %q field\n  %v",
					cut.name, envelopeIncomplete, fields)
			}
			if said != execFailureWords(errors.New(cut.said)) {
				t.Fatalf("%s reported %q under %q\n  want: %q",
					cut.name, said, envelopeIncomplete, execFailureWords(errors.New(cut.said)))
			}
			if bytes.Contains([]byte(said), []byte("node task-1:")) {
				t.Fatalf("%s carried the node key into what a person reads: %q", cut.name, said)
			}
		})
	}
}

// AND A RUN THAT COULD NOT BE RUN AT ALL STILL FILLS `error` AND NOTHING ELSE.
// The fix must not have moved the failure out of the one field a script has
// always read it from.
//
// THE ROW IS A NIL OUTCOME, which is what "it never started" is. It used to be
// `exec.Outcome{Stop: exec.StopError}`, and that outcome is a run that STARTED
// and broke — the executor writes it from inside the turn loop — so the test
// asserting the old premise was the defect written down as a passing check.
func TestARunThatCouldNotStartStillFillsTheErrorField(t *testing.T) {
	envelope := buildExecEnvelope(
		nil,
		errors.New("node task-1: OPENROUTER_API_KEY (or OPENAI_API_KEY) is required"),
		"a/model")
	fields := envelopeFields(t, envelope)

	if envelope.Stop != stopError {
		t.Fatalf("a run that could not start ended with stop %q, want %q", envelope.Stop, stopError)
	}
	if envelope.Error == "" {
		t.Fatal("a run that could not start published no `error` at all; a script has nothing to read")
	}
	if _, said := fields[envelopeIncomplete]; said {
		t.Fatalf("a run that never started reported %q as well as `error`: %v",
			envelopeIncomplete, fields[envelopeIncomplete])
	}
}

// `error` IS FILLED ON THE ERROR STOP AND ON NO OTHER, ON EVERY DOOR THAT
// BUILDS THIS ENVELOPE.
//
// It is one test across the three verbs because the contract is one object's
// and not one command's, and because `exec` is not the only door that could
// grow the shape again: `do` and `run` map their own private results onto the
// same builder, and each of them is one line away from copying a failure in
// beside an answer.
func TestOnlyTheStopThatMeansItNeverRanFillsTheErrorField(t *testing.T) {
	for _, door := range []struct {
		verb  string
		built resultEnvelope
	}{
		{
			verb: "aforge exec, cut off by its token budget",
			built: buildExecEnvelope(
				&exec.Outcome{Stop: exec.StopBudget, Text: "half an answer"},
				errors.New("node task-1: token budget exhausted"), "a/model"),
		},
		{
			verb: "aforge do, cut off by the wall",
			built: errandEnvelope(headlessOutcome{
				Deliverable: "half an answer",
				Artifacts:   []string{},
				stop:        stopDeadline,
			}),
		},
		{
			verb:  "aforge run, a saved program that did not finish",
			built: savedProgramEnvelope(t, "half an answer", "the run stopped part of the way through"),
		},
	} {
		t.Run(door.verb, func(t *testing.T) {
			if door.built.Stop == stopError {
				t.Fatalf("%s: this row was meant to be a run that STARTED, and it says stop %q",
					door.verb, door.built.Stop)
			}
			if door.built.Answer == "" {
				t.Fatalf("%s: this row was meant to carry a partial answer and carries none", door.verb)
			}
			if door.built.Error != "" {
				t.Fatalf("%s published `error: %q` on a run whose stop is %q and whose answer is %q\n"+
					"  `error` is why the run did not produce an answer; only stop %q may fill it",
					door.verb, door.built.Error, door.built.Stop, door.built.Answer, stopError)
			}
		})
	}
}

// savedProgramEnvelope is `aforge run`'s own mapping, driven through the door
// that writes it. The run keeps no journal and names no model, which the type
// documents as a real case rather than an error, so nothing here opens a store
// or reaches for a provider.
func savedProgramEnvelope(t *testing.T, report, incomplete string) resultEnvelope {
	t.Helper()
	var printed bytes.Buffer
	run := subharnessRun{stdout: &printed, stderr: &printed, asJSON: true}
	_ = run.sayEnvelope(stopIncomplete, exec.RunResult{Report: report}, incomplete)
	var envelope resultEnvelope
	if err := json.Unmarshal(printed.Bytes(), &envelope); err != nil {
		t.Fatalf("`aforge run --json` did not print a parseable envelope (%v):\n%s", err, printed.String())
	}
	return envelope
}
