//go:build !windows

package app

import (
	"fmt"
	"strings"
)

// These strings are the run instruction: the first user message of a solo run,
// and the bounded continuations sent when a turn ends without a submission.
//
// They carry mechanics only -- the files the run uses, what senior-dev does on
// its own, and what ends the run. They do not tell the model when to edit, how
// much to explore, or how fast to move; those are its decisions, and a sentence
// spent on them is a sentence competing with the repository it is about to
// read. Keep every claim here true of this binary: a prompt that describes
// behaviour the code does not have is worse than a prompt that omits it.

// soloSystemPreamble frames the run instruction and nothing else. The detail is
// in the sections, and a long preamble is what a model under context pressure
// drops first.
const soloSystemPreamble = `You are implementing one change in this repository, by yourself, in one context.

What follows is how this run works: the files it uses, what senior-dev does,
and what ends it.`

// soloIntakeSection names the specification file. Intake writes .senior-dev/spec.md
// verbatim and compaction re-pins it from disk, so it is the one copy of the
// request that outlives the conversation.
const soloIntakeSection = `## The specification

.senior-dev/spec.md holds the request verbatim. It is the specification, and it is
re-pinned from that file whenever this context is compacted.`

// soloExploreSection names the pinned-command file. readPinnedCommand takes the
// first line of .senior-dev/pinned.txt and quotes it back in the nudge findings and
// in a later run's header, so the file has a reader even when the model forgets
// what it wrote there.
const soloExploreSection = `## The verification command

Write the build or test command you verify with to .senior-dev/pinned.txt, on one
line. senior-dev reads that first line and quotes it back to you if this run
needs a continuation.`

// soloImplementSection states what the workspace is and what leaves it. The
// exclusion list is seniorDevArtifactPathspecs: if the two disagree, submit
// accepts a tree whose only content is senior-dev's own bookkeeping.
const soloImplementSection = `## The workspace

The workspace is a git repository. Your tools are the ones declared with this
turn: a shell, file reading, editing, search, web access, and submit.

.senior-dev/ and git-ignored paths are excluded from the answer. Everything else in
the working tree, committed or not, is part of what you submit.`

// soloConformanceSection names the checklist file. soloFreeze refuses a
// submission when it is missing and records its item and tick counts when it is
// present, and soloChecklistItem matches both "[ ] x" and "- [ ] x".
const soloConformanceSection = `## The checklist

Write .senior-dev/checklist.md: one line per thing the request requires, each
starting "[ ] ", ticked to "[x]" when the code satisfies it. submit refuses if
this file does not exist, and records its item and tick counts.`

// soloSubmitSection is the completion protocol. It has to agree with the system
// prompt that the run ends by calling submit and by nothing else: when the two
// disagreed, the model followed the system prompt.
const soloSubmitSection = `## Ending the run

The run ends when you call the submit tool. Nothing else ends it: no status
tag, no report, no summary.

submit takes a reason, the evidence you verified with, and checklist_satisfied.
It refuses, naming the cause, when the tree is unchanged from the starting
commit, when .senior-dev/checklist.md does not exist, when reason or evidence is
empty, or when this run already submitted. A refusal does not end the run.

An accepted submit freezes the tree at that instant. senior-dev then runs this
project's own build and test entrypoints itself and records what they report;
that cannot change what ships, and neither can anything you edit afterwards — a
tree that moves after a submission is reverted to the frozen one.

A run that never submits is recorded as unsubmitted and leaves behind the tree
as it stands, except that a tree whose suite cannot start is restored to an
earlier one.`

// buildSoloPrompt assembles the run instruction. The request is repeated at the
// top verbatim: it travels through no paraphrase on its way to the model.
func buildSoloPrompt(goal string, pinned string, checklistPath string) string {
	sections := []string{
		soloSystemPreamble,
		soloIntakeSection,
		soloExploreSection,
		soloImplementSection,
		soloConformanceSection,
		soloSubmitSection,
	}
	body := strings.Join(sections, "\n\n")
	header := "# The request\n\n" + strings.TrimSpace(goal) +
		"\n\n(The same text is in .senior-dev/spec.md, which is the specification.)\n\n"
	if strings.TrimSpace(pinned) != "" {
		header += fmt.Sprintf(
			".senior-dev/pinned.txt already contains: %s\n\n", strings.TrimSpace(pinned),
		)
	}
	if checklistPath != "" {
		header += "Write your checklist to " + checklistPath + ".\n\n"
	}
	return header + "# How this run works\n\n" + body
}

// soloNudge is the bounded continuation for a run that stopped talking without
// submitting. It carries the facts senior-dev can see for itself rather than
// encouragement, and says what an unsubmitted ending actually does.
func soloNudge(attempt int, findings []string) string {
	return "You stopped without calling submit, so no submission has been captured.\n\n" +
		soloUnsubmittedBody(findings, attempt >= soloMaxNudges)
}

// soloLandingPrompt is the one bounded turn offered after the open work window
// closes or a turn dies. No model turn follows it, and it is reached without
// the model having stopped, so it opens on its own terms rather than soloNudge's.
func soloLandingPrompt(findings []string) string {
	return "This is the last turn of this run, and it is time-bounded. " +
		"No submission has been captured yet.\n\n" +
		soloUnsubmittedBody(findings, true)
}

// soloUnsubmittedBody is what every continuation says: the facts senior-dev
// observed, the one ending there is, and what an unsubmitted run actually leaves
// behind (which is the live tree, not nothing).
func soloUnsubmittedBody(findings []string, last bool) string {
	var builder strings.Builder
	if len(findings) > 0 {
		builder.WriteString("senior-dev checked the tree itself and found:\n\n")
		for _, finding := range findings {
			builder.WriteString("  - " + finding + "\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(
		"The run ends when you call submit and by nothing else. A run that never " +
			"calls it is recorded as unsubmitted and leaves behind the tree as it stands.",
	)
	if last {
		builder.WriteString(
			"\n\nThis is the last prompt you will get. senior-dev then checks that " +
				"final tree itself, and restores an earlier tree only if the suite cannot start.",
		)
	}
	return builder.String()
}

// soloToolLeakPrompt answers a response that carried provider markup as text
// instead of executing it. leakedToolCall detects it, and the correction is
// capped at soloMaxToolLeakCorrections.
func soloToolLeakPrompt() string {
	return "Your last response contained DSML tool-call markup as plain text, so no tool ran. " +
		"Make the intended call as a real tool call; this correction is offered at most twice."
}
