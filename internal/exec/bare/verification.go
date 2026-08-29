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
// THE MEASUREMENT IS THE RUN'S AND NOT THIS WORKER'S, so its whole
// implementation now lives one package up, in exec.PhotographBefore and
// exec.PhotographAfter, where every belt can reach it. It was here alone for one
// wave and that was long enough to prove the cost: the generalist belt in
// linear.go, which is what an unrouted node actually gets, took no reading at
// all and left a store with nothing in it to autopsy. What remains here are the
// two seams this worker calls through — the wall, the workspace and the journal
// it holds, handed over under the names the shared reading expects.
//
// Everything below is a MEASUREMENT and never a gate. It never fails the leaf,
// it never changes what the loop does, and every way it can go wrong — a
// project that declares no verification, a command that will not run, a
// ceiling that fires — leaves the outcome exactly as it would have been.

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// photographBefore is the reading the whole comparison is subtracted from, and
// it is the JOB's reading rather than this leaf's. exec.PhotographBefore holds
// the whole of why — the inherited baseline, the one refusal that is retaken,
// and the ordering against Workspace.WatchTree that this worker's Run obeys.
func (b *Bare) photographBefore(ctx context.Context, task exec.Task) (verify.Reading, bool) {
	return exec.PhotographBefore(ctx, b.workspace, b.history, b.deadline, task)
}

// photographAfter takes the second reading and writes what the two readings say
// onto the outcome. changed is this loop's own answer about whether the tree
// moved, which the workspace has already compared; exec.PhotographAfter holds
// what is done with it.
func (b *Bare) photographAfter(
	ctx context.Context, task exec.Task, reading verify.Reading,
	changed, inherited bool, outcome *exec.Outcome,
) {
	exec.PhotographAfter(ctx, b.workspace, b.history, b.deadline, task, reading,
		changed, inherited, outcome)
}
