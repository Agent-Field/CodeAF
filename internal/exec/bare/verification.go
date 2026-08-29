package bare

// Two readings of the project's own verification, one on either side of the
// work.
//
// The bare worker is the whole-taker `aforge do` uses on the plain path, and
// until this existed it had no baseline at all: `Outcome.Baseline` was never
// set, and an empty Baseline reads downstream as NO CLAIM rather than as NOBODY
// LOOKED. Three graded runs in the 2026-08-28 sweep shipped patches that
// deleted attributes their repositories already had — `results_path` off
// igel's Igel, `_size_known` off textual's RichLog — and every one of the
// hidden tests failed on setup while the leaf's own narrow tests stayed green.
// They stayed green because THE LEAF WROTE THEM: a leaf's own new tests are the
// one signal that structurally cannot see a regression. Two measured readings
// of the project's own command are the only thing that can.
//
// Everything here is a MEASUREMENT and never a gate. It never fails the leaf,
// it never changes what the loop does, and every way it can go wrong — a
// project that declares no verification, a command that will not run, a
// ceiling that fires — leaves the outcome exactly as it would have been.

import (
	"context"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// The budget both readings are taken on, the floor under it and the arithmetic
// between them are verify.ReadingBudget. They used to be three constants in this
// file, where only this worker could reach them; the delivery gate weighs the
// same photograph and takes one of its own where none exists, and two readers of
// one cap is how a number in this repository drifts. See PERF.md, "The
// verification photograph's budget".

// photographBefore takes the first reading, if this leaf can afford one and
// this project says how it is checked.
//
// It runs BEFORE Workspace.WatchTree deliberately. A test runner leaves its own
// droppings — a .pytest_cache, a target/, a coverage file — and a reading taken
// after the tree was photographed would file every one of them as something
// this leaf produced. Taken first, they are part of the world the leaf arrived
// in, which is what they are.
func (b *Bare) photographBefore(ctx context.Context) verify.Reading {
	budget, affordable := verify.ReadingBudget(b.deadline)
	if !affordable {
		return verify.Reading{}
	}
	plan := verify.Discover(b.workspace.Root())
	result, ok := verify.RunTests(ctx, b.workspace.Root(), plan, budget)
	if !ok || result.TimedOut {
		// No test entrypoint, or a first reading that never finished. Either way
		// there is nothing to subtract a second reading from, and a subtraction
		// against an unknown baseline would name the repository's own
		// pre-existing reds as this change's doing.
		return verify.Reading{}
	}
	return verify.Reading{Plan: plan, Budget: budget, Before: result, Taken: true}
}

// photographAfter takes the second reading and writes what the two readings say
// onto the outcome.
//
// changed is the workspace's own account of whether this leaf moved anything —
// the before-and-after read of the tree it already took, not a fresh stat of
// the world. A leaf that changed nothing cannot have regressed anything, and
// re-running a suite to prove it costs an eighth of the wall for an answer that
// is known.
func photographAfter(
	ctx context.Context, reading verify.Reading, root string, changed bool, outcome *exec.Outcome,
) {
	if !reading.Taken || outcome == nil {
		return
	}
	// The pre-existing reds are owed to the judge whether or not this leaf
	// changed anything: it is the one worker that photographed the repository
	// before the work started, and the judge two processes away cannot rerun
	// anything.
	if len(reading.Before.Failing) > 0 {
		outcome.Baseline = append(outcome.Baseline, "`"+reading.Before.Entrypoint.Command+
			"` was ALREADY failing at this commit before the run touched the workspace ("+
			describeChecks(reading.Before.Failing)+"). This is the repository's pre-existing "+
			"state, not this change's doing.")
	}
	// The photograph rides the outcome whether or not a second reading was
	// taken, because WHAT WAS MEASURED AND WHAT NOBODY MEASURED ARE DIFFERENT
	// FACTS and only the worker that stood there before the work can tell them
	// apart. The delivery gate reads it: the roster it holds is what says which
	// checks exist, and its Taken flag is what stops the gate inventing a
	// finding out of a project that declares no verification at all.
	outcome.Verification = reading
	if !changed {
		return
	}
	after, ok := verify.RunTests(ctx, root, reading.Plan, reading.Budget)
	if !ok || after.TimedOut {
		return
	}
	reading.After, reading.AfterTaken = after, true
	outcome.Verification = reading
	outcome.Regressed = reading.Regressed()
}

// describeChecks names a bounded handful of checks for a reader. It is
// presentation and it is bounded for the same reason every other list a model
// reads is: a suite with two hundred reds says nothing more than a suite with
// eight and a count.
//
// codeaf's describeFailing is its sibling and spells the same eight-and-a-count
// rule. They are deliberately two, because one words a swepro auditor's prompt
// and this one words an outcome sentence; if a third ever appears, that is the
// moment the rule belongs in internal/verify instead of in each caller.
func describeChecks(names []string) string {
	if len(names) == 0 {
		return "no individually named tests"
	}
	if len(names) > 8 {
		return strings.Join(names[:8], ", ") + " and " + strconv.Itoa(len(names)-8) + " more"
	}
	return strings.Join(names, ", ")
}
