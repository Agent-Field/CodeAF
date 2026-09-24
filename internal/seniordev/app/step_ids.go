//go:build !windows

package app

import (
	"path/filepath"
	"strings"
)

// The steps of senior-dev's own process, as a `step` record names the one an
// action served. They are the parts of the one model context the run
// instruction lays out (solo_prompt.go) — read the spec, explore, pin a check,
// list the requirements, implement, submit — and the independent check the run
// makes of the tree itself afterwards.
//
// THEY ARE A REPORT, NEVER A PLAN. Inside the model context the order is the
// model's own: it may explore after it has pinned, and write its checklist last.
// A step id says which part of the process an action serves, so an id recurs
// whenever the model comes back to that part; nothing here moves the model from
// one to the next, and nothing the model sees depends on them.
//
// ONE SOURCE OF TRUTH: these constants are the ids on the wire, the classifier
// below answers only them, and senior-dev's page words are keyed by them
// (internal/seniordev's actions.go).
const (
	// StepBrief is reading the spec the brief was written down as.
	StepBrief = "brief"
	// StepExplore is reading, searching and running commands before the first
	// change to a project file.
	StepExplore = "explore"
	// StepPin is writing down the command that shows the work passes.
	StepPin = "pin"
	// StepChecklist is listing the request's requirements, and ticking them.
	StepChecklist = "checklist"
	// StepImplement is every change to a project file, and whatever the model
	// reads or runs once it has made one.
	StepImplement = "implement"
	// StepSubmit is the submit tool, and anything after an accepted submit.
	StepSubmit = "submit"
	// StepVerify is senior-dev running the project's own build and tests itself,
	// with no model: after the hand-in, and when it checks the tree mid-run.
	StepVerify = "verify"
)

// Steps is every step id, in the order a run first reaches them when its
// model works through the process as the instruction lays it out.
var Steps = []string{StepBrief, StepExplore, StepPin, StepChecklist, StepImplement, StepSubmit, StepVerify}

// The files senior-dev keeps its own records in, inside the folder it works in
// (seniorDevArtifactPathspecs keeps them out of the answer). An action on one
// of them serves that record's step, whatever tool it took.
const (
	seniorDevSpec      = seniorDevDataDirectory + "/spec.md"
	seniorDevPinned    = seniorDevDataDirectory + "/pinned.txt"
	seniorDevChecklist = seniorDevDataDirectory + "/checklist.md"
)

// stepProgress is what the step classifier knows about the run so far: whether
// an edit tool has changed a project file, and whether a submit was accepted.
// It only ever moves forward. A finished tool call moves the first (stepOf);
// only the freeze's own stage record moves the second (afterStage).
type stepProgress struct {
	changed   bool
	submitted bool
}

// The stage record the freeze writes once it has captured the tree, and at no
// other time (solo.go's soloFreezeWithContext): the one record that says a
// submit was accepted.
const (
	frozenStage  = "submit"
	frozenStatus = "frozen"
)

// afterStage is the run's progress after a stage record. Only the freeze's
// record moves it: from then on the tree is frozen, and everything is the
// submit step.
func (progress stepProgress) afterStage(stage, status string) stepProgress {
	if stage == frozenStage && status == frozenStatus {
		progress.submitted = true
	}
	return progress
}

// stepAction is one finished tool call as the classifier reads it: the tool,
// what it was aimed at — the file a file tool named, a shell's command, a
// patch's text — and whether it failed.
type stepAction struct {
	tool   string
	target string
	failed bool
}

// editTools are the tools that change a file. Only their success moves a run
// from exploring to implementing: a shell command may change files too, but a
// reader of the command cannot tell which, and the instruction's own line
// between the two parts is the first edit.
var editTools = map[string]bool{"edit": true, "write": true, "apply_patch": true}

// stepOf is the step a finished tool call served, and the run's progress after
// it. It is pure: the same call on the same progress answers the same step.
//
//   - Once a submit has been accepted, everything is the submit step: the
//     tree is frozen and the run is handing in.
//   - The submit tool is the submit step, accepted or refused, and it moves
//     nothing. A refused submit tells its model why and lets it keep working,
//     so it settles as a completed call exactly as an accepted one does
//     (tool/submit.go): the call cannot say which it was, and the freeze's
//     own stage record, which comes first, does (afterStage).
//   - An action on one of senior-dev's own records is that record's step: the
//     spec (brief), the pinned check (pin), the checklist.
//   - A successful edit to a project file is the first change, and it and
//     everything after it is implementing; before it, exploring.
//
// `question` is always refused in a run nobody attends (runtime.go), so it
// changes nothing and is read as whichever part the model was in when it
// asked.
func stepOf(action stepAction, progress stepProgress) (string, stepProgress) {
	if progress.submitted {
		return StepSubmit, progress
	}
	if action.tool == "submit" {
		return StepSubmit, progress
	}
	if record := seniorDevRecordStep(action); record != "" {
		return record, progress
	}
	if editTools[action.tool] {
		if !action.failed {
			progress.changed = true
		}
		return StepImplement, progress
	}
	if progress.changed {
		return StepImplement, progress
	}
	return StepExplore, progress
}

// seniorDevRecordStep is the step of an action on one of senior-dev's own
// records, and "" for an action on anything else. A patch counts as one only
// when every file it touches is one of them: a patch that also changes a
// project file is implementing.
func seniorDevRecordStep(action stepAction) string {
	target := filepath.ToSlash(action.target)
	if action.tool == "apply_patch" {
		files := patchFiles(target)
		if len(files) == 0 {
			return ""
		}
		step := ""
		for _, file := range files {
			record := recordStepOf(file)
			if record == "" {
				return ""
			}
			if step == "" {
				step = record
			}
		}
		return step
	}
	return recordStepOf(target)
}

// recordStepOf names the record a path or a command mentions, first the spec,
// then the pinned check, then the checklist.
func recordStepOf(text string) string {
	for _, record := range []struct{ file, step string }{
		{seniorDevSpec, StepBrief},
		{seniorDevPinned, StepPin},
		{seniorDevChecklist, StepChecklist},
	} {
		if mentionsPath(text, record.file) {
			return record.step
		}
	}
	return ""
}

// mentionsPath reports whether text names the file at a path boundary: the
// relative path itself, or the same path at the end of a longer one — so
// `/copy/.senior-dev/spec.md` is the spec and `my.senior-dev/spec.md` is not.
func mentionsPath(text, file string) bool {
	for from := 0; ; {
		at := strings.Index(text[from:], file)
		if at < 0 {
			return false
		}
		at += from
		if at == 0 || strings.ContainsRune("/ \t\n'\"=<>(;&|", rune(text[at-1])) {
			return true
		}
		from = at + len(file)
	}
}

// patchFiles are the files an apply_patch text touches, from its own headers.
func patchFiles(text string) []string {
	var files []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, header := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if name, ok := strings.CutPrefix(line, header); ok && strings.TrimSpace(name) != "" {
				files = append(files, strings.TrimSpace(name))
			}
		}
	}
	return files
}
