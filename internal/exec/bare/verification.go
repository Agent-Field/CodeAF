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
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// The budget both readings are taken on, the floor under it and the arithmetic
// between them are verify.ReadingBudget. They used to be three constants in this
// file, where only this worker could reach them; the delivery gate weighs the
// same photograph and takes one of its own where none exists, and two readers of
// one cap is how a number in this repository drifts. See PERF.md, "The
// verification photograph's budget".

// photographBefore is the reading the whole comparison is subtracted from, and
// it is the JOB's reading rather than this leaf's.
//
// A repair round is a new leaf, in a new workspace object, standing in a tree
// its own job has already changed. A leaf that photographed what IT found took
// the broken tree as its baseline, so every check an earlier round turned red
// subtracted to nothing and was never a finding again — textual's s5 run walked
// twenty project checks down to one across four rounds and raised nothing. So
// the baseline is looked up first: where this job already took one, this leaf
// inherits it, spends no suite run at all, and is measured against the tree as
// it stood before the job's first change. See verify's baseline.go.
//
// A first reading of a tree runs BEFORE Workspace.WatchTree deliberately. A test
// runner leaves its own droppings — a .pytest_cache, a target/, a coverage
// file — and a reading taken after the tree was photographed would file every
// one of them as something this leaf produced. Taken first, they are part of the
// world the leaf arrived in, which is what they are.
func (b *Bare) photographBefore(ctx context.Context, task exec.Task) (reading verify.Reading, inherited bool) {
	job := verify.JobKey(task.Goal)
	if held, ok := verify.BaselineFor(b.workspace.Root(), job); ok {
		b.journal(task, held.Before, "before the job's first change", true)
		return held, true
	}
	budget, affordable := verify.ReadingBudget(b.deadline)
	if !affordable {
		return verify.Reading{}, false
	}
	plan := verify.Discover(b.workspace.Root())
	result, ok := verify.RunTests(ctx, b.workspace.Root(), plan, budget)
	if !ok || result.TimedOut {
		// No test entrypoint, or a first reading that never finished. Either way
		// there is nothing to subtract a second reading from, and a subtraction
		// against an unknown baseline would name the repository's own
		// pre-existing reds as this change's doing.
		return verify.Reading{}, false
	}
	reading = verify.Reading{Plan: plan, Budget: budget, Before: result, Taken: true}
	verify.RememberBaseline(b.workspace.Root(), job, reading)
	b.journal(task, result, "before the job's first change", false)
	return reading, false
}

// photographAfter takes the second reading and writes what the two readings say
// onto the outcome.
//
// changed is the workspace's own account of whether THIS leaf moved anything.
// inherited says an earlier round of the same job already did. Either is reason
// enough to take the second reading: a continuation that only rewrote its
// account still hands over a tree an earlier round may have broken, and the
// whole reason the baseline is the job's is so that breakage is still visible
// here. A first leaf that changed nothing cannot have regressed anything, and
// re-running a suite to prove it costs an eighth of the wall for an answer that
// is already known.
func (b *Bare) photographAfter(
	ctx context.Context, task exec.Task, reading verify.Reading,
	changed, inherited bool, outcome *exec.Outcome,
) {
	if !reading.Taken || outcome == nil {
		return
	}
	// The pre-existing reds are owed to the judge whether or not this leaf
	// changed anything: this job photographed the repository before the work
	// started, and the judge two processes away cannot rerun anything.
	if len(reading.Before.Failing) > 0 {
		outcome.Baseline = append(outcome.Baseline, "`"+reading.Before.Entrypoint.Command+
			"` was ALREADY failing at this commit before the run touched the workspace ("+
			describeChecks(reading.Before.Failing)+"). This is the repository's pre-existing "+
			"state, not this change's doing.")
	}
	// The photograph rides the outcome whether or not a second reading was
	// taken, because WHAT WAS MEASURED AND WHAT NOBODY MEASURED ARE DIFFERENT
	// FACTS and only the run that stood there before the work can tell them
	// apart. The delivery gate reads it: the roster it holds is what says which
	// checks exist, and its Taken flag is what stops the gate inventing a
	// finding out of a project that declares no verification at all.
	outcome.Verification = reading
	if !changed && !inherited {
		return
	}
	// The SAME strategy the baseline was taken with, pinned rather than
	// re-derived. Two readings taken with two different commands subtract to
	// noise, and re-deriving would hand a worker that edited its own test
	// script the power to choose what the after reading measures — which is the
	// tamper the photograph exists to catch.
	after, ok := verify.RunReading(ctx, b.workspace.Root(), reading.Before.Strategy, reading.Budget)
	if !ok || after.TimedOut {
		return
	}
	reading.After, reading.AfterTaken = after, true
	outcome.Verification = reading
	outcome.Regressed = reading.Regressed()
	b.journal(task, after, "on the finished tree", false)
}

// journal writes one reading into the run's own record.
//
// A FAIL-SAFE THAT LEAVES NO RECORD CANNOT BE AUTOPSIED (FAILSAFE.md clause 4),
// and this one left none: the photograph lived in memory from the worker that
// took it to the gate that weighed it, so a finished run held no row saying
// whether a reading had happened at all. Five graded runs were read back with no
// way to tell a project that declares no verification from a reading that ran
// and named nothing — the two opposite diagnoses.
//
// It is a measurement of the run and never a gate: a store that refuses the row
// changes nothing about what the leaf does.
func (b *Bare) journal(task exec.Task, result verify.Result, when string, inherited bool) {
	if b.history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	sample := result.Reported
	if len(sample) > store.VerificationSample {
		sample = sample[:store.VerificationSample]
	}
	_ = b.history.RecordVerification(task.StoreNodeID, store.VerificationReading{
		When:        when,
		Command:     result.Strategy.Command,
		Declared:    result.Strategy.Declared,
		Runner:      result.Strategy.Runner,
		Read:        string(result.Strategy.Read),
		Source:      result.Strategy.Source,
		ReadAsPlain: result.ReadAsPlain,
		Exit:        result.Exit,
		TimedOut:    result.TimedOut,
		Named:       len(result.Reported),
		Red:         len(result.Failing),
		Sample:      append([]string{}, sample...),
		Inherited:   inherited,
	})
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
