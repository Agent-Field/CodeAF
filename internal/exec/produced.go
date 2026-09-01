package exec

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// What a command left behind, and why the write tools alone could never see it.
//
// The artifact registry used to be populated by the write family only — write,
// edit, and the media tools — which encodes the assumption that a file arrives
// in the workspace by being typed into a tool call. Most of the useful ones do
// not. A script run under `sh` renders a chart, a build writes a binary, a
// converter emits a document: the leaf did the work, the file is on disk, and
// nothing in the product ever learned it existed. One measured run saved a
// 204KB plot the person had asked to see, never named it, was failed by the
// delivery gate for a message that "did not contain the script", and spent a
// whole continuation node retyping a file already sitting in the workspace.
//
// The first repair asked the CLOCK: note the instant the call began, walk the
// tree once afterwards, keep whatever carried a later write time. That is
// evidence sourced from the component being checked rather than from the world,
// which is the exact shape FAILSAFE.md rule 2 was written about, and it was
// wrong in three ordinary cases:
//
//   - a file written inside the filesystem's own timestamp granularity of the
//     mark carries a stamp fractionally BEFORE it and was silently dropped —
//     TestABareLeafFilesTheFilesItsToolsLeaveBehind failed about one run in four
//     on a machine whose tmpfs answers in whole milliseconds;
//   - a file a tool put there with its timestamp preserved (cp -p, git checkout,
//     tar, rsync -t) claims to predate a mark it postdates, and the deliverable
//     is never named;
//   - a file rewritten with identical bytes moved the clock and nothing else,
//     and was named as though the call had produced something.
//
// So the sweep now asks the WORLD, in the same words the whole-leaf evidence
// record asks it: [Workspace.Snapshot] photographs the tree before the call and
// again after it, and [diffTrees] — ONE implementation, shared with
// [Workspace.WatchTree] and [Workspace.RecordChanges] — says what the two
// sightings prove. Files created, changed or deleted between them are this
// call's products whatever any clock says, and the files it finds go into the
// same registry the write tools use, under the same node identity, so
// everything downstream — the files footer, the gate's evidence, a
// continuation's inputs — is fixed by this one record.
//
// The cost of asking the world is a second bounded walk per tool call rather
// than one; see [Workspace.Snapshot] for the bounds and PERF.md for the budget.

const (
	// producedScanLimit bounds one snapshot. A workspace is usually a handful of
	// files, but a leaf that ran a build or unpacked an archive can have tens of
	// thousands, and this runs at both ends of every shell call — so the walk
	// stops rather than walking a tree whose size is nobody's plan.
	producedScanLimit = 6000

	// producedPerCall bounds what one command may claim. A command that touched
	// hundreds of files did something whose output is a tree rather than a
	// deliverable, and a footer naming three hundred paths names nothing.
	producedPerCall = 24

	// snapshotDigestFileLimit is the largest file a snapshot will read to know
	// its bytes. Above it the stamp keeps size, mode and write time, which is
	// the answer the sweep gave before digests existed. A deliverable bigger
	// than a megabyte is nearly always bigger than its predecessor too, so the
	// size alone still catches it; what is given up is the rewrite-in-place of
	// a large file at exactly its old length, which the tool-sourced record
	// covers and which is why the two accounts are layered rather than one
	// chosen over the other.
	snapshotDigestFileLimit = 1 << 20

	// snapshotDigestBudget is the total bytes ONE snapshot will read. Past it
	// the remaining stamps are digestless and fall back on the clock. It bounds
	// the honest worst case, which the walk's own limit does not: six thousand
	// files just under the per-file limit would be six gigabytes of reading on
	// the leaf's critical path, twice per tool call.
	//
	// Eight megabytes is chosen against the real workspace — a report, a chart,
	// a script — and against the cost of being wrong about it. WHAT DEGRADES
	// PAST THE BUDGET IS ONLY THE REWRITE CASE: created and deleted files are
	// decided by whether the tree holds the path at all, which needs no bytes
	// and is the half that was actually broken. A tree big enough to exhaust
	// this gets the older, narrower size-and-clock answer for its tail, never a
	// wrong one. The walk is lexical, so both snapshots spend the budget on the
	// same files and compare like with like.
	snapshotDigestBudget = 8 << 20

	// producedSlack moves a snapshot's own timestamp back far enough that a
	// coarse filesystem cannot hide a file behind it. It is the last resort of
	// a PARTIAL snapshot and of a file too large to digest — see fileStamp.same
	// and diffTrees — and never the first question asked about a file.
	producedSlack = time.Second
)

// producedMark is an instant moved back far enough that a coarse filesystem
// cannot hide a file behind it.
func producedMark(now time.Time) time.Time { return now.Add(-producedSlack) }

// producedBound orders a per-call change set newest first and cuts it to what
// one command may claim. Newest first, so that when the cap bites it keeps what
// the command most recently produced rather than whatever sorts early in the
// alphabet; ties break on the path, so the same diff is the same list twice.
//
// A deleted path is dated by the snapshot that still held it, which is the last
// moment the world was seen to have it.
func producedBound(changes map[string]ArtifactChange, before, after *TreeSnapshot) []string {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	when := func(path string) time.Time {
		if stamp, ok := after.files[path]; ok {
			return stamp.modified
		}
		return before.files[path].modified
	}
	sort.Slice(paths, func(i, j int) bool {
		left, right := when(paths[i]), when(paths[j])
		if left.Equal(right) {
			return paths[i] < paths[j]
		}
		return left.After(right)
	})
	if len(paths) > producedPerCall {
		paths = paths[:producedPerCall]
	}
	return paths
}

// producedSkipDir names the trees that are somebody else's files sitting in
// this workspace: dependency installs, caches, and version-control storage. A
// leaf that ran `pip install` or `npm install` produced thousands of files and
// delivered none of them.
func producedSkipDir(name string) bool { return verify.SkipTree(name) }

// recordProduced files everything this call changed in the workspace under this
// leaf's node identity — the same registry, keyed the same way, as a write.
func (t *Toolbox) recordProduced(before *TreeSnapshot) {
	t.workspace.RecordProducedSince(t.leaf, before)
}

// RecordProducedSince files what the workspace gained, changed or lost between
// before — a [Workspace.Snapshot] taken at the top of one tool call — and now,
// under leaf, in the same registry a write goes into.
//
// It is the ONE door for an executor whose tools do not report their own writes
// — a shell command, or a tool ported from elsewhere that knows a directory and
// nothing of this workspace — because what the delivery gate is later shown is
// this registry and nothing else: a leaf that wrote the file and never filed it
// is convicted of not writing it, and a second leaf is spliced in to write it
// again. A belt whose tools file nothing paid that twice on every run until it
// went through this door.
//
// The findings go to BOTH accounts, and deliberately. A created or changed file
// is recorded as a deliverable, which is what the footer and the gate read and
// is the behaviour every caller already depends on; and every finding, deletions
// included, is recorded as OBSERVED, because a before-and-after read of the tree
// is the world's own testimony whether it spans a whole leaf or one call. A
// deletion cannot be a deliverable — there is nothing to open — so it is kept
// only in the evidence record, exactly where [Workspace.RecordChanges] keeps
// one.
func (w *Workspace) RecordProducedSince(leaf string, before *TreeSnapshot) {
	if w == nil || before == nil || strings.TrimSpace(leaf) == "" {
		return
	}
	after := w.Snapshot()
	changes := diffTrees(before, after)
	if len(changes) == 0 {
		return
	}
	paths := producedBound(changes, before, after)
	w.noteObserved(leaf, changes, paths)
	for _, path := range paths {
		if changes[path] == ArtifactDeleted {
			continue
		}
		w.Record(leaf, filepath.Join(w.root, path))
	}
}
