package exec

import (
	"fmt"
	"sort"
	"strings"
)

// Account is a worker's own account of the work it did, in the one shape every
// reader downstream needs and none of them can reconstruct.
//
// It exists because of a measured, expensive silence. A subharness that drives
// a whole pipeline behind a process boundary used to hand back one sentence and
// a bill, and when the pipeline ended without a verdict of its own the sentence
// was "it ended without saying how it went" — a void. Three readers then had to
// act on that void: the delivery gate judged it and reacted differently every
// time, the remainder judge could not tell finished work from unstarted work
// and added children to re-investigate what was already done, and the person
// reading the record was told nothing at all. Between 48% and 57% of a run's
// cost was measured going into that re-investigation.
//
// So the account is structural rather than prose: the files the work changed
// with the kind of change and its size, the checks it ran with what each one
// found, and the last thing the worker said for itself. Nothing here is
// interpreted — every field is a fact the worker observed and no reader further
// down could observe for itself — and nothing here is worker-specific. Any
// subharness that owns a verifier can fill it in, which is why it is named for
// what it is rather than for who writes it first.
//
// A nil Account is the ordinary case: a worker that photographs nothing leaves
// it empty, and an empty account reads as "no claim" everywhere it is rendered.
type Account struct {
	// Files is what the work changed, one row per path, merged across however
	// many times the worker touched it.
	Files []FileChange
	// Checks is what the worker's own verifier ran, as of the last time it ran.
	// It is replaced rather than appended by a repeated verification pass: a
	// suite that ran four times reports the state of the fourth run, and the
	// three before it are history rather than evidence.
	Checks []Check
	// Final is the last thing the worker said about the whole job, verbatim.
	Final string
}

// FileChange is one path the work touched: what kind of change it was, and how
// large. The line counts are the worker's own, taken from the patch it applied,
// rather than a diff computed afterwards by somebody who was not there.
type FileChange struct {
	Path    string
	Change  string
	Added   int
	Removed int
}

// The change kinds. They are the vocabulary a reader of the account sees, and
// they are deliberately the plain English words rather than git's letters: the
// account is read by a model and by a person, and neither of them should have
// to be told what "M" means.
const (
	ChangeAdded   = "added"
	ChangeChanged = "changed"
	ChangeMoved   = "moved"
	ChangeDeleted = "deleted"
)

// changeRank orders the kinds so a path touched several times keeps the honest
// word for what happened to it overall. A file created and then edited was
// added; a file edited and then deleted is gone, whatever was done to it on the
// way; a file moved and then edited moved.
func changeRank(change string) int {
	switch change {
	case ChangeDeleted:
		return 4
	case ChangeAdded:
		return 3
	case ChangeMoved:
		return 2
	case ChangeChanged:
		return 1
	}
	return 0
}

// Check is one command the worker's verifier ran and what the process said.
//
// Known is the one field that is not simply the exit status: a check that came
// back red and was red in exactly the same places before the work began has not
// been broken by this work. The worker is the only thing in the system that
// photographed the repository beforehand, so it is the only thing that can say
// so, and a reader that treated a known-red suite as this work's failure is the
// measured way correct work gets thrown away.
type Check struct {
	Command string
	Kind    string
	Passed  bool
	Known   bool
	Tail    string
}

// settled reports whether a check is evidence for the work rather than against
// it: it passed, or it failed in a place that was already failing.
func (c Check) settled() bool { return c.Passed || c.Known }

// Empty reports that there is nothing here to show. A nil account is empty, so
// every caller can ask without guarding first.
func (a *Account) Empty() bool {
	return a == nil || (len(a.Files) == 0 && len(a.Checks) == 0 && strings.TrimSpace(a.Final) == "")
}

// Verified reports that the worker ran checks and every one of them settled.
// It is the fact the remainder judge exists to be told: a node whose suite is
// green is finished, and a child added to re-run those tests is money spent
// proving something already proved.
//
// A worker that ran no checks answers no, because "nothing was checked" and
// "everything passed" are the two answers a silence used to collapse into.
func (a *Account) Verified() bool {
	if a == nil || len(a.Checks) == 0 {
		return false
	}
	for _, check := range a.Checks {
		if !check.settled() {
			return false
		}
	}
	return true
}

// Stat is the diff in three numbers: how many paths, and the lines added and
// removed across all of them.
func (a *Account) Stat() (files, added, removed int) {
	if a == nil {
		return 0, 0, 0
	}
	for _, file := range a.Files {
		added += file.Added
		removed += file.Removed
	}
	return len(a.Files), added, removed
}

// Note records one file the work touched, merging it with whatever is already
// known about that path. The line counts accumulate because each call is a
// separate edit that really did add and remove those lines; the kind takes the
// strongest word, which is what changeRank is for.
func (a *Account) Note(path, change string, added, removed int) {
	if a == nil {
		return
	}
	if path = strings.TrimSpace(path); path == "" {
		return
	}
	for index := range a.Files {
		if a.Files[index].Path != path {
			continue
		}
		a.Files[index].Added += added
		a.Files[index].Removed += removed
		if changeRank(change) > changeRank(a.Files[index].Change) {
			a.Files[index].Change = change
		}
		return
	}
	if change == "" {
		change = ChangeChanged
	}
	a.Files = append(a.Files, FileChange{Path: path, Change: change, Added: added, Removed: removed})
}

// accountFileRows and accountTailBytes bound what an account may say.
//
// They are absolute rather than a share of anybody's window, for the reason
// Outcome.Ran's bounds are: this is not the worker's own memory but a
// fixed-size record handed to somebody else — a judge, a reviser, a person
// reading a job's summary — and what bounds it is that reader's patience. A
// refactor that touched four hundred files is a real outcome; four hundred rows
// in three dependents' contexts is not, and the count still tells the truth.
const (
	accountFileRows  = 20
	accountTailBytes = 200
)

// FileLines renders the changed files as evidence rows: the arithmetic first,
// then one row per path, then the count of whatever was left out. Empty when
// the work changed nothing, which is itself a fact and belongs to whoever is
// composing rather than being invented here.
func (a *Account) FileLines() []string {
	if a == nil || len(a.Files) == 0 {
		return nil
	}
	files := append([]FileChange(nil), a.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	count, added, removed := a.Stat()
	lines := []string{fmt.Sprintf("%s changed, %+d %+d lines", plural(count, "file"), added, -removed)}
	shown := files
	if len(shown) > accountFileRows {
		shown = shown[:accountFileRows]
	}
	for _, file := range shown {
		lines = append(lines, fmt.Sprintf("  %s %s (%+d %+d)",
			file.Change, file.Path, file.Added, -file.Removed))
	}
	if left := len(files) - len(shown); left > 0 {
		lines = append(lines, fmt.Sprintf("  and %d more, named in the run's own record", left))
	}
	return lines
}

// CheckLines renders the verification story as evidence rows: one line per
// command, the verdict first so a reader scanning the left edge sees what
// happened before it sees what was run.
//
// A failure that was already there before the work began says so on its own
// line rather than being dropped. Dropping it would make the account claim a
// green suite over a suite anyone can watch failing, which is the fastest way
// to teach a reader that the account cannot be trusted.
func (a *Account) CheckLines() []string {
	if a == nil || len(a.Checks) == 0 {
		return nil
	}
	lines := make([]string, 0, len(a.Checks))
	for _, check := range a.Checks {
		verdict := "failed"
		switch {
		case check.Passed:
			verdict = "passed"
		case check.Known:
			verdict = "failed, and was already failing before this work began"
		}
		line := "  " + verdict + ": " + check.Command
		if !check.Passed && strings.TrimSpace(check.Tail) != "" {
			line += " — " + snip(strings.TrimSpace(firstAccountLine(check.Tail)), accountTailBytes)
		}
		lines = append(lines, line)
	}
	return lines
}

// Lines is the whole account as evidence rows, headed so each block says what
// it is. This is what the delivery gate is handed: rows, in the same grammar as
// the run tail beside them, and never a paragraph explaining what to make of
// them — what to make of them is the gate's job and the gate's alone.
//
// The worker's own last word is included HERE and nowhere else, because the
// gate is often judging a deliverable that has been composed since — a
// synthesis, a repair pass, a parent's answer — and the sentence the worker
// signed off with may no longer be anywhere in it.
func (a *Account) Lines() []string { return a.rows(true) }

// Report is the same account as a block of text, for the leaf's own
// deliverable. It is the account and nothing else — no verdict, no advice —
// because the sentence above it already said how the run ended and this is the
// evidence for it.
//
// It leaves the worker's last word out for exactly that reason: the leaf's own
// text OPENS with it, and an account that repeated it would put the same
// paragraph twice into every context the deliverable travels through.
func (a *Account) Report() string {
	lines := a.rows(false)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// accountFinalBytes bounds the worker's last word where it is quoted. The whole
// of it is the deliverable itself; this is the copy that rides beside the diff.
const accountFinalBytes = 600

// rows is the one renderer both readers share, differing only in whether the
// worker's sign-off belongs in this particular copy.
func (a *Account) rows(signoff bool) []string {
	if a.Empty() {
		return nil
	}
	var lines []string
	if files := a.FileLines(); len(files) > 0 {
		lines = append(lines, "What the work changed:")
		lines = append(lines, files...)
	}
	if checks := a.CheckLines(); len(checks) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "What the work ran to check itself, and what each one found:")
		lines = append(lines, checks...)
	}
	if final := strings.TrimSpace(a.Final); signoff && final != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "What the work said for itself when it finished:",
			snip(final, accountFinalBytes))
	}
	return lines
}

// Summary is the account in one clause, for a reader with room for a clause
// rather than a block: the plan's own state view, where every node gets one
// line and thirty nodes share a budget.
func (a *Account) Summary() string {
	if a.Empty() {
		return ""
	}
	var parts []string
	if count, added, removed := a.Stat(); count > 0 {
		parts = append(parts, fmt.Sprintf("%s changed, %+d %+d lines",
			plural(count, "file"), added, -removed))
	}
	if len(a.Checks) > 0 {
		commands := make([]string, 0, len(a.Checks))
		for _, check := range a.Checks {
			commands = append(commands, check.Command)
		}
		if a.Verified() {
			parts = append(parts, "its own checks passed: "+strings.Join(commands, ", "))
		} else {
			parts = append(parts, "its own checks did not pass: "+strings.Join(commands, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

// plural writes a count with its noun, so no caller has to compose "1 files".
func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// firstAccountLine keeps a command's tail to its first line. A test runner's
// output is many lines and the row it goes into is one; the whole of it is in
// the run's own record for whoever needs it.
func firstAccountLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return line
}
