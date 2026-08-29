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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// verificationWallShare is the denominator of the leaf's own wall that ONE
// reading of the project's own verification may spend. See PERF.md, "The
// verification photograph's budget", which is where this arithmetic is stated
// for a person.
//
// It is a share and not a duration because the thing being bounded is not a
// suite — it is the fraction of a leaf's life spent measuring instead of
// working. An eighth each way is a quarter of the wall at the very worst, and
// the worst is rare: the second reading is taken only when the tree changed.
const verificationWallShare = 8

// shortestUsefulReading is the floor under that share, and it is the refusal
// this file exists to state as a law: A LEAF WHOSE WALL CANNOT AFFORD A REAL
// READING TAKES NO READING AT ALL, rather than spending an eighth of its life
// on a command that will be killed before it says anything.
//
// One minute is derived from the fastest whole suite measured in the sweep this
// work comes from — igel's two passing project tests, "2 passed in 27.86s" —
// doubled to leave room for an interpreter, an import graph and a compile. A
// budget under that cannot hold even the cheapest observed project suite, so a
// reading taken with it would time out, name nothing, and cost the leaf an
// eighth of its wall for a Result that says nothing at all.
const shortestUsefulReading = time.Minute

// verificationBudget is what one reading of the project's own verification may
// spend, given the whole wall the leaf was granted.
//
// ok is false when the leaf is too short to afford the measurement, which is
// the refusal above. A ninety-minute wall affords 11m15s a reading; a
// sixty-second wall affords 7.5s, which is under the floor, so it photographs
// nothing. The shortest wall that photographs at all is eight minutes.
func verificationBudget(wall time.Duration) (time.Duration, bool) {
	if wall <= 0 {
		return 0, false
	}
	budget := wall / verificationWallShare
	if budget < shortestUsefulReading {
		return 0, false
	}
	return budget, true
}

// verificationReading is the before half of the comparison, held across the
// loop. A zero value — which is what an unaffordable wall, an undiscoverable
// entrypoint or a hung first reading all produce — is a reading that was never
// taken, and the after half declines to run against it.
type verificationReading struct {
	plan   verify.Plan
	budget time.Duration
	before verify.Result
	taken  bool
}

// photographBefore takes the first reading, if this leaf can afford one and
// this project says how it is checked.
//
// It runs BEFORE Workspace.WatchTree deliberately. A test runner leaves its own
// droppings — a .pytest_cache, a target/, a coverage file — and a reading taken
// after the tree was photographed would file every one of them as something
// this leaf produced. Taken first, they are part of the world the leaf arrived
// in, which is what they are.
func (b *Bare) photographBefore(ctx context.Context) verificationReading {
	budget, affordable := verificationBudget(b.deadline)
	if !affordable {
		return verificationReading{}
	}
	plan := verify.Discover(b.workspace.Root())
	result, ok := verify.RunTests(ctx, b.workspace.Root(), plan, budget)
	if !ok || result.TimedOut {
		// No test entrypoint, or a first reading that never finished. Either way
		// there is nothing to subtract a second reading from, and a subtraction
		// against an unknown baseline would name the repository's own
		// pre-existing reds as this change's doing.
		return verificationReading{}
	}
	return verificationReading{plan: plan, budget: budget, before: result, taken: true}
}

// photographAfter takes the second reading and writes what the two readings say
// onto the outcome.
//
// changed is the workspace's own account of whether this leaf moved anything —
// the before-and-after read of the tree it already took, not a fresh stat of
// the world. A leaf that changed nothing cannot have regressed anything, and
// re-running a suite to prove it costs an eighth of the wall for an answer that
// is known.
func (reading verificationReading) photographAfter(
	ctx context.Context, root string, changed bool, outcome *exec.Outcome,
) {
	if !reading.taken || outcome == nil {
		return
	}
	// The pre-existing reds are owed to the judge whether or not this leaf
	// changed anything: it is the one worker that photographed the repository
	// before the work started, and the judge two processes away cannot rerun
	// anything.
	if len(reading.before.Failing) > 0 {
		outcome.Baseline = append(outcome.Baseline, "`"+reading.before.Entrypoint.Command+
			"` was ALREADY failing at this commit before the run touched the workspace ("+
			describeChecks(reading.before.Failing)+"). This is the repository's pre-existing "+
			"state, not this change's doing.")
	}
	if !changed {
		return
	}
	after, ok := verify.RunTests(ctx, root, reading.plan, reading.budget)
	if !ok || after.TimedOut {
		return
	}
	outcome.Regressed = verify.NewFailures(reading.before.Failing, after.Failing)
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
