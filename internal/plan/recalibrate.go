package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// recalibratePrompt rewrites the ruler from tasks that actually ran.
//
// It asks for three examples and nothing else, because the ruler was always
// three examples — what changes is that they stop being invented. Feeding real
// task descriptions rather than fitted numbers is deliberate: the loop from
// anchors to plans to measurements back to anchors is confounded, since changing
// the ruler changes what tasks get produced and therefore what gets measured
// next. Describing observed work keeps that loop descriptive instead of letting
// it chase its own tail.
//
// Its second paragraph names the worker in its own words rather than reusing
// workerPremise, which is the source of truth for the shared fact. The sentence
// here is doing something else: it says whose envelope is being redrawn.
const recalibratePrompt = `You rewrite the ruler used to judge whether a task is the right size for one agent.

That agent works alone and in order, with tools. Below are tasks it really ran,
with how many tool-using turns each took. Turns are the cost: a task that took
three turns was too small to be worth handing over, and one that ran out of
budget before finishing was too big to be one task.

Write three reference examples in exactly the shape given, drawn from the
evidence:

TOO SMALL — one task that finished in very few turns. Say what made it trivial.
RIGHT — one task near the middle of the range that finished cleanly. Say what
  made it a sensible single job.
TOO BIG — one task that ran out of budget or took far longer than the rest. Say
  what made it too much for one agent.

Use the real task descriptions, generalised slightly so they read as examples
rather than as one project's specifics. Keep each to two or three lines. End
with the same guidance the current ruler ends with: judge by the breadth of the
subject rather than the volume of material, since a great deal of material on one
subject is a single job while a little across five unrelated subjects is not.`

// boundaryPreamble introduces the worker's own notes about its fit. It is a
// separate paragraph because the notes are a different kind of evidence from
// the turn counts: a count says what the work cost, a note says whether the
// work belonged here at all.
const boundaryPreamble = "\n\nWhat the worker itself noticed about its fit for the " +
	"work it was given (a run that says it was far inside its envelope is evidence the " +
	"ruler's TOO SMALL example is set too low; one that says it was at the top of its " +
	"envelope is evidence the RIGHT example is set too high):\n\n"

var recalibrateSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "anchors": { "type": "string" }
  },
  "required": ["anchors"],
  "additionalProperties": false
}`)

// calibrationEvidence renders the three measured bands in one fixed order.
//
// It was a range over a map literal, which handed Go's randomized iteration
// order straight into a prompt: the same evidence produced a different document
// on every run, so the pass was not reproducible and its text could never match
// a cached prefix. Small to large is the ruler's own order and the order the
// prompt's own instructions read in.
func calibrationEvidence(small, middle, large []profile.Record) string {
	var evidence strings.Builder
	for _, band := range []struct {
		label   string
		records []profile.Record
	}{
		{"finished quickly", small},
		{"finished in the middle of the range", middle},
		{"ran out of budget", large},
	} {
		for _, record := range band.records {
			appendCalibrationEvidence(&evidence, band.label, record)
		}
	}
	return evidence.String()
}

// Recalibrate rewrites the ruler from measured work, returning the new anchors
// and whether anything changed. It is a no-op unless the profile both has enough
// evidence and disagrees with the ruler in force.
func Recalibrate(ctx context.Context, client Completer, store *profile.Profile) (string, string, Usage, error) {
	var usage Usage
	needed, reason := store.NeedsRecalibration()
	if !needed {
		return "", reason, usage, nil
	}

	small, middle, large := store.Evidence(3)
	evidence := calibrationEvidence(small, middle, large)
	if strings.TrimSpace(evidence) == "" {
		return "", "no usable evidence", usage, nil
	}

	messages := []ai.Message{
		systemMessage(recalibratePrompt),
		userMessage("The ruler currently in force:\n\n" + Anchors()),
		userMessage("Tasks this model actually ran:\n\n" + evidence + boundaryEvidence(store)),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanRecalibrate)
	var decoded struct {
		Anchors string `json:"anchors"`
	}
	response, err := structured(ctx, client, messages, recalibrateSchema, &decoded)
	usage.Add(usageOf(response))
	if err != nil {
		return "", reason, usage, fmt.Errorf("recalibrate: %w", err)
	}
	anchors := strings.TrimSpace(decoded.Anchors)
	// A ruler shorter than a sentence per anchor is not a ruler. Refusing it
	// keeps a bad call from replacing a working prior with nothing.
	if len(anchors) < 200 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return "", "the rewritten ruler came back too thin to use", usage, nil
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return anchors, reason, usage, nil
}

func appendCalibrationEvidence(evidence *strings.Builder, label string, record profile.Record) {
	fmt.Fprintf(evidence, "- [%s] %s — %s (%d turns", label, record.Title, record.Summary, record.Turns)
	if record.HasSourceCount() {
		fmt.Fprintf(evidence, ", %d sources", record.Sources)
	}
	fmt.Fprintf(evidence, ", %dk tokens)\n", record.Tokens/1000)
	appendBoundaryNotes(evidence, "  ", record)
}

// boundaryEvidence is the fit half of the ruler's evidence, rendered only when
// there is any. A profile with no notes renders none, and the recalibrate call
// it makes is the call it has always made.
//
// Records already shown in the three bands carry their notes inline; this
// section exists because the bands are picked by turn count and a record that
// says the most about the boundary is frequently not the cheapest, the most
// median or the one that overran.
func boundaryEvidence(store *profile.Profile) string {
	records := store.BoundaryEvidence(boundaryEvidenceCount)
	if len(records) == 0 {
		return ""
	}
	var section strings.Builder
	section.WriteString(boundaryPreamble)
	for _, record := range records {
		fmt.Fprintf(&section, "- %s (%d turns, %dk tokens)\n", record.Title, record.Turns, record.Tokens/1000)
		appendBoundaryNotes(&section, "  ", record)
	}
	return section.String()
}

// boundaryEvidenceCount bounds the fit section at the same handful the three
// bands are allowed. A ruler is rewritten from examples, not from a corpus.
const boundaryEvidenceCount = 6

func appendBoundaryNotes(evidence *strings.Builder, indent string, record profile.Record) {
	for _, note := range record.Calibration {
		if note = strings.TrimSpace(note); note != "" {
			fmt.Fprintf(evidence, "%s· %s\n", indent, note)
		}
	}
}
