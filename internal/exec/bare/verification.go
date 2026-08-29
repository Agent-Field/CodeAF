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
// the baseline is looked up first: where this job already settled what its tree
// looked like — a reading, or the reason there could not be one — this leaf
// inherits that answer and spends nothing on reaching it again.
//
// A first reading of a tree runs BEFORE Workspace.WatchTree deliberately. A test
// runner leaves its own droppings — a .pytest_cache, a target/, a coverage
// file — and a reading taken after the tree was photographed would file every
// one of them as something this leaf produced. Taken first, they are part of the
// world the leaf arrived in, which is what they are.
//
// EVERY OUTCOME IS JOURNALED, including every way of having no reading. That is
// the whole repair of the s6 silence: the bare leaf spent five minutes and
// twenty-seven seconds on a reading that was killed at its ceiling, and the
// finished store held no row saying so, which read from outside exactly like a
// project that declares no verification at all.
func (b *Bare) photographBefore(ctx context.Context, task exec.Task) (reading verify.Reading, inherited bool) {
	job := verify.JobKey(task.Goal)
	pace := verify.Pace{}
	if held, ok := verify.BaselineFor(b.workspace.Root(), job); ok {
		// ONE ANSWER IS NOT INHERITED: a scoped reading killed at its ceiling
		// having named nothing. Every other refusal is a fact about the tree,
		// the project or the wall, and none of those move between rounds. That
		// one is a fact about a SIZE this program chose, and the cut measured
		// the pace that sizes it properly — so this round reads again over what
		// that pace affords rather than declining to look. textual s8 spent its
		// one reading on forty files, was cut naming nothing, and every round
		// after it inherited the silence.
		if !held.Retakeable() {
			b.journal(task, held, held.Before, "before the job's first change", true)
			return held, true
		}
		pace = held.Pace()
	}
	reading = verify.Photograph(ctx, b.workspace.Root(), b.deadline, focusOf(task), pace)
	verify.RememberBaseline(b.workspace.Root(), job, reading)
	b.journal(task, reading, reading.Before, "before the job's first change", false)
	return reading, false
}

// focusOf is what this job is about, as paths: the files the person's own request
// names, and the files the work this leaf continues left behind.
//
// It is what decides HOW MUCH of the project a reading covers and WHERE it is
// taken — the checks next to the change rather than the whole repository, and
// the package of a monorepo the change is in rather than the fan-out at its root
// (verify.Focus, verify.Members). Both of those were the difference between a
// reading and no reading at all: textual's whole-repository suite is 793 seconds
// against a budget of 5m30s, and happy-dom's root command dies inside turbo
// having named no check of any package.
//
// It reads the request for NAMES rather than only for paths. Most requests
// spell no path at all: happy-dom's says "Implement `observe()`, `unobserve()`,
// `disconnect()` and `takeRecords()`" and names `IntersectionObserver`, and a
// focus built from paths alone was empty — so no package was chosen, the whole
// reading was taken at the repository root, and it was killed at its ceiling.
// verify.Locate matches those names, whole, against files the workspace holds.
//
// IT IS DERIVED FROM THE REQUEST AND NOT FROM THE DIFF, and that is forced
// rather than chosen. The baseline is the tree BEFORE the job's first change, so
// at the moment it is taken there is no diff to read; the request is the only
// account of what the work is about that exists yet. It is also the right one —
// the request is what every round of the job shares, so the scope it decides is
// the scope every round inherits, which is exactly what the comparison rule
// needs (verify.Reading.comparable). A continuation's inputs are read too,
// because the files an earlier round left behind are the same job's own record
// of where it has been working.
func focusOf(task exec.Task) verify.Focus {
	focus := verify.Focus(verify.NamedSubjects(
		strings.Join([]string{task.Title, task.Goal, task.Brief}, "\n")))
	for _, input := range task.Inputs {
		focus = append(focus, input.Artifacts...)
	}
	return focus
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
	if outcome == nil {
		return
	}
	// The photograph rides the outcome whether or not a reading was taken,
	// because WHAT WAS MEASURED AND WHAT NOBODY MEASURED ARE DIFFERENT FACTS
	// and only the run that stood there before the work can tell them apart.
	// A reading that could not be taken carries the sentence saying why, so
	// the gate is handed a reason rather than a void.
	outcome.Verification = reading
	if !reading.Taken {
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
	if !changed && !inherited {
		return
	}
	// The SAME rung of the ladder the baseline was taken on, pinned rather than
	// re-derived. Two readings taken with two different commands subtract to
	// noise, and re-deriving would hand a worker that edited its own test
	// script the power to choose what the after reading measures — which is the
	// tamper the photograph exists to catch.
	after, ok := verify.RunReading(ctx, b.workspace.Root(), reading.Before.Strategy, reading.Budget)
	switch {
	case !ok:
		reading.Unread = "the finished tree could not be read: `" +
			reading.Before.Strategy.Command + "` could not be started a second time"
	case after.TimedOut:
		reading.Unread = "the finished tree was not read: `" + after.Strategy.Command +
			"` was killed at its ceiling without finishing"
	default:
		reading.After, reading.AfterTaken = after, true
		outcome.Verification = reading
		outcome.Regressed = reading.Regressed()
		b.journal(task, reading, after, "on the finished tree", false)
		return
	}
	// Not taken, and said so. The before half stands and the outcome keeps it;
	// what is lost is the subtraction, and a run that cannot say a check went
	// red must not be able to say one did not either.
	outcome.Verification = reading
	b.journal(task, reading, verify.Result{Strategy: reading.Before.Strategy}, "on the finished tree", false)
}

// journal writes one reading — or one reading that could not be taken — into the
// run's own record.
//
// A FAIL-SAFE THAT LEAVES NO RECORD CANNOT BE AUTOPSIED (FAILSAFE.md clause 4).
// Every way of having no reading is written down here, because the absence of
// the event used to be the only spelling of four different facts: a project that
// declares no verification, a wall that could not afford a reading, a shell that
// could not run one, and a command killed at its ceiling. They cost a run
// nothing, nothing, nothing and five and a half minutes respectively, and an
// autopsy could not tell which had happened.
//
// It is a measurement of the run and never a gate: a store that refuses the row
// changes nothing about what the leaf does.
func (b *Bare) journal(
	task exec.Task, reading verify.Reading, result verify.Result, when string, inherited bool,
) {
	if b.history == nil || strings.TrimSpace(task.StoreNodeID) == "" {
		return
	}
	// The rung that was actually reached. A taken reading carries it on its own
	// result; a refused one carries only the rung it got to.
	strategy := result.Strategy
	if strategy.Empty() {
		strategy = reading.Strategy
	}
	taken := reading.Taken
	if when == "on the finished tree" {
		taken = reading.AfterTaken
	}
	sample := result.Reported
	if len(sample) > store.VerificationSample {
		sample = sample[:store.VerificationSample]
	}
	_ = b.history.RecordVerification(task.StoreNodeID, store.VerificationReading{
		When:     when,
		Read:     taken,
		Why:      reading.Unread,
		Command:  strategy.Command,
		Declared: strategy.Declared,
		Runner:   strategy.Runner,
		Format:   string(strategy.Read),
		Source:   strategy.Source,
		// WHERE and HOW MUCH, beside WHAT and HOW. A roster of forty means one
		// thing for a small project read whole and another for a large one read
		// next to the change, and the row could not say which.
		Scope:   strategy.Scope,
		Package: strategy.Workdir,
		// And what a cut reading learned before it was cut: that its roster
		// stops where the clock did, and how long it took to get that far. The
		// second is the only thing this run ever measures about the PACE of the
		// machine it is on, and a ceiling derived from a wall knows nothing
		// about that until a reading is cut and says so.
		Partial:     reading.Partial && when == "before the job's first change",
		Elapsed:     reading.CutAfter,
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
