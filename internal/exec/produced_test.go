package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The defect this file exists for: the artifact registry was fed by the write
// family alone, so a file a subprocess created under the sh tool was invisible
// to everything downstream — the files footer, the delivery gate's evidence, a
// continuation node's inputs. A run rendered a 204KB plot, delivered without
// mentioning it, and was failed for a message that "did not contain the
// script".
func TestASubprocessCreatedFileIsRecordedAsAnArtifact(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "7", nil)

	// Nothing here types the file into a tool call: a program the leaf ran
	// writes it, which is how a chart, a build output or a converted document
	// actually arrives in a workspace.
	result := tools.Execute(t.Context(), "sh",
		`{"cmd":"printf 'plot bytes' > stochastic_fit_plot.png"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}

	artifacts := space.Artifacts("7")
	if !slices.Contains(artifacts, "stochastic_fit_plot.png") {
		t.Fatalf("a file created by the command is not in the node's artifacts: %v", artifacts)
	}
}

func TestMutationRevisionAdvancesWhenTheSameArtifactIsEditedAgain(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "answer.md")
	space.Record("7", path)
	first := space.MutationCount("7")
	space.Record("7", path)
	second := space.MutationCount("7")

	if second <= first {
		t.Fatalf("mutation revision stayed at %d after a second edit to the same path", second)
	}
	if artifacts := space.Artifacts("7"); len(artifacts) != 1 {
		t.Fatalf("one repeatedly edited path became %d artifacts: %v", len(artifacts), artifacts)
	}
}

// A file the command only read is not a thing the command produced, or every
// leaf would deliver its own inputs back to the person who supplied them.
func TestAnUntouchedFileIsNotClaimedAsProduced(t *testing.T) {
	space := workspace(t)
	given := filepath.Join(space.Root(), "given.csv")
	if err := os.WriteFile(given, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Older than the slack the scan allows for coarse filesystem timestamps,
	// so this stands in for material that was in the workspace all along.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(given, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	tools := NewToolbox(space, "4", nil)

	if result := tools.Execute(t.Context(), "sh", `{"cmd":"wc -l given.csv"}`); result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}

	if artifacts := space.Artifacts("4"); len(artifacts) != 0 {
		t.Fatalf("a command that read a file claimed it as output: %v", artifacts)
	}
}

// The harness's own machinery must never surface as a deliverable. The spill
// directory is written by the very call that would otherwise record it, so this
// is not a hypothetical: without the dot-file rule every large sh result would
// name its own spill file as an artifact of the job.
func TestTheHarnessesOwnFilesAreNotDeliverables(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "5", nil)

	result := tools.Execute(t.Context(), "sh",
		`{"cmd":"printf 'LINE%s\\n' 1 2 3 4 5 6 7 8 9 10 | awk '{for(i=0;i<200;i++) print}'"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, obsDir) {
		t.Fatalf("this test needs a spilled result to be meaningful: %q", result.Content)
	}

	for _, path := range space.Artifacts("5") {
		if strings.HasPrefix(path, ".") {
			t.Errorf("machinery named as a deliverable: %q", path)
		}
	}
}

// Bounded, because this runs after every shell call and a workspace's size is
// nobody's plan: a command that unpacked a dependency tree must not turn into
// three hundred named deliverables.
func TestProducedFilesAreBounded(t *testing.T) {
	space := workspace(t)
	before := space.Snapshot()
	outputs := filepath.Join(space.Root(), "out")
	if err := os.MkdirAll(outputs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for index := range producedPerCall + 10 {
		name := filepath.Join(outputs, fmt.Sprintf("part-%03d.txt", index))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Files under a dependency tree are somebody else's, however new they are.
	skipped := filepath.Join(space.Root(), "node_modules", "left-pad", "index.js")
	if err := os.MkdirAll(filepath.Dir(skipped), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skipped, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	space.RecordProducedSince("8", before)

	produced := space.Artifacts("8")
	if len(produced) > producedPerCall {
		t.Fatalf("the sweep filed %d files, cap is %d", len(produced), producedPerCall)
	}
	for _, path := range produced {
		if strings.Contains(path, "node_modules") {
			t.Errorf("a dependency tree was claimed as output: %q", path)
		}
	}
}

// The defect the snapshot diff exists for: a file whose write time is at or
// before the moment the call began is still a file the tree did not hold and
// now does.
//
// The clock was wrong about this in two ordinary ways at once. A filesystem
// whose timestamps are coarser than the gap between the mark and the write
// stamps the new file fractionally BEFORE the mark — that is what made
// TestABareLeafFilesTheFilesItsToolsLeaveBehind fail about one run in four —
// and a tool that preserves the timestamp it copied (cp -p, git checkout, tar,
// rsync -t) backdates it on purpose. Chtimes stands in for both, an hour deep,
// so no amount of slack can rescue a clock-sourced answer.
//
// FAILSAFE.md rule 2: source evidence from the world.
func TestAFileTheClockCallsOldIsStillProduced(t *testing.T) {
	space := workspace(t)
	before := space.Snapshot()

	path := touch(t, space, "delivered.md", "the work")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	space.RecordProducedSince("11", before)

	if artifacts := space.Artifacts("11"); !slices.Contains(artifacts, "delivered.md") {
		t.Fatalf("artifacts = %v, want the file the call left behind", artifacts)
	}
}

// A rewrite that puts the same bytes back is not something the call produced,
// however far it moved the clock. A formatter run over a file it has already
// formatted, an idempotent generator re-run, `touch` on somebody's input: each
// of them used to name that file as this call's output.
func TestARewriteWithIdenticalBytesIsNotProduced(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "given.csv", "a,b\n1,2\n")
	before := space.Snapshot()

	// A whole second later, so the write time has certainly moved on every
	// filesystem this runs on.
	later := time.Now().Add(time.Second)
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	space.RecordProducedSince("12", before)

	if artifacts := space.Artifacts("12"); len(artifacts) != 0 {
		t.Fatalf("artifacts = %v, want nothing: the bytes never changed", artifacts)
	}
}

// The other half of the same rule: bytes that DID change are the call's output
// even when the file is exactly as long as it was and the clock says nothing.
// Length plus write time cannot see this at all, and it is the ordinary shape
// of an edit — a one-character fix, a flipped flag, a swapped identifier.
func TestARewriteAtTheSameLengthAndClockIsStillProduced(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "config.toml", "mode = \"draft\"\n")
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	before := space.Snapshot()

	if err := os.WriteFile(path, []byte("mode = \"final\"\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// The tool preserved the timestamp, and the replacement is the same length.
	// Nothing but the bytes can tell the two files apart.
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	space.RecordProducedSince("13", before)

	if artifacts := space.Artifacts("13"); !slices.Contains(artifacts, "config.toml") {
		t.Fatalf("artifacts = %v, want the file the call rewrote", artifacts)
	}
}

// A command that removed a file leaves that fact in the evidence record and
// nowhere else — the same place, and for the same reason, that the whole-leaf
// diff keeps a deletion. Artifacts is a list of things to open, and a path that
// no longer exists sends every reader of it to nothing.
func TestACommandsDeletionIsEvidenceButNotSomethingToOpen(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "draft.md", "superseded")
	before := space.Snapshot()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	space.RecordProducedSince("14", before)

	fact, found := factFor(space.ArtifactFacts("14"), "draft.md")
	if !found {
		t.Fatal("the call removed draft.md and the evidence record does not say so")
	}
	if !fact.Observed || fact.Change != ArtifactDeleted {
		t.Fatalf("draft.md fact = %+v, want observed and deleted", fact)
	}
	if artifacts := space.Artifacts("14"); slices.Contains(artifacts, "draft.md") {
		t.Fatalf("artifacts = %v, want a file that no longer exists left out", artifacts)
	}
}

// A scratch file the leaf made and then removed is never handed to anyone.
//
// Two sweeps see the two halves — one call creates it, a later call removes it
// — and the later sighting wins, so the path leaves the list of things to open
// exactly as a deletion of somebody else's file would. Before the tree diff the
// creating call filed it as a deliverable and nothing ever retracted that: the
// files footer named a temp file, and a dependent following the pointer found
// nothing there.
func TestAScratchFileMadeAndRemovedIsNotHandedToAnyone(t *testing.T) {
	space := workspace(t)

	first := space.Snapshot()
	path := touch(t, space, "tmp-working.json", "{}")
	space.RecordProducedSince("15", first)

	second := space.Snapshot()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	space.RecordProducedSince("15", second)

	if artifacts := space.Artifacts("15"); len(artifacts) != 0 {
		t.Fatalf("artifacts = %v, want nothing to open: the file is gone", artifacts)
	}
	fact, found := factFor(space.ArtifactFacts("15"), "tmp-working.json")
	if !found || fact.Change != ArtifactDeleted {
		t.Fatalf("tmp-working.json fact = %+v (found=%v), want the deletion recorded", fact, found)
	}
}
