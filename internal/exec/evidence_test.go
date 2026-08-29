package exec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The defect this file exists for, in one sentence: on 2026-08-28 a leaf wrote
// out.txt with a shell command, the delivery gate's audit said "the record shows
// nothing named out.txt was left behind" while the file sat on disk, and a
// correct deliverable was convicted on evidence that came from the component
// being checked rather than from the world. FAILSAFE.md rule 2.

// touch writes a file straight onto disk, with nothing in the harness told about
// it. It stands in for every way a file actually arrives in a workspace that is
// not a write tool: a shell redirect, a script, a build, a converter.
func touch(t *testing.T, space *Workspace, name, body string) string {
	t.Helper()
	path := filepath.Join(space.Root(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func factFor(facts []ArtifactFact, path string) (ArtifactFact, bool) {
	for _, fact := range facts {
		if fact.Path == path {
			return fact, true
		}
	}
	return ArtifactFact{}, false
}

// A file no tool ever heard about is still a file the run left behind.
func TestAFileWrittenWithoutAWriteToolIsAnArtifact(t *testing.T) {
	space := workspace(t)
	space.WatchTree("7")
	touch(t, space, "out.txt", "the deliverable")
	space.RecordChanges("7")

	if artifacts := space.Artifacts("7"); !slices.Contains(artifacts, "out.txt") {
		t.Fatalf("artifacts = %v, want the file the shell wrote", artifacts)
	}
	fact, found := factFor(space.ArtifactFacts("7"), "out.txt")
	if !found {
		t.Fatal("out.txt is missing from the evidence record entirely")
	}
	if fact.Claimed {
		t.Fatal("out.txt is claimed, but no tool ever recorded it")
	}
	if !fact.Observed || fact.Change != ArtifactCreated {
		t.Fatalf("out.txt fact = %+v, want observed and created", fact)
	}
}

// The two accounts do not double-count, and the tool's claim survives the merge:
// the diff says the file moved, the tool says it is what the work was for, and
// both facts are readable off one row.
func TestAFileBothClaimedAndObservedIsReportedOnceAndStillDeliverable(t *testing.T) {
	space := workspace(t)
	space.WatchTree("7")
	path := touch(t, space, "report.md", "# findings")
	space.Record("7", path)
	space.RecordChanges("7")

	artifacts := space.Artifacts("7")
	if len(artifacts) != 1 || artifacts[0] != "report.md" {
		t.Fatalf("artifacts = %v, want report.md exactly once", artifacts)
	}
	fact, _ := factFor(space.ArtifactFacts("7"), "report.md")
	if !fact.Claimed || !fact.Observed || fact.Change != ArtifactCreated {
		t.Fatalf("report.md fact = %+v, want claimed and observed", fact)
	}
}

// A file the leaf changed rather than created is evidence too — a run that edits
// one file in place used to leave the same trace as a run that did nothing.
func TestAChangedFileIsObserved(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "notes.md", "before")
	space.WatchTree("7")
	if err := os.WriteFile(path, []byte("before, and then rather a lot more"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	space.RecordChanges("7")

	fact, found := factFor(space.ArtifactFacts("7"), "notes.md")
	if !found || fact.Change != ArtifactChanged {
		t.Fatalf("notes.md fact = %+v (found=%v), want a changed file", fact, found)
	}
	if artifacts := space.Artifacts("7"); !slices.Contains(artifacts, "notes.md") {
		t.Fatalf("artifacts = %v, want the file the leaf edited", artifacts)
	}
}

// A file that was there before and was not touched is nobody's deliverable. The
// diff has to be able to say "this was already here", or every leaf would report
// back the material it was given.
func TestAnUntouchedFileIsNotEvidenceOfAnything(t *testing.T) {
	space := workspace(t)
	touch(t, space, "given.csv", "a,b\n1,2\n")
	space.WatchTree("7")
	space.RecordChanges("7")

	if facts := space.ArtifactFacts("7"); len(facts) != 0 {
		t.Fatalf("evidence = %+v, want nothing: the leaf touched nothing", facts)
	}
}

// A deletion is a fact about the run and NOT a path to hand anyone.
//
// Artifacts is a list of things to open — it becomes the head's files line, a
// dependent's "(files: …)" pointer, the gate's roll call of what is on disk —
// and a path that no longer exists sends every one of those readers to nothing.
// So the deletion is kept where the readers are asking what happened, and left
// out where they are asking what to read.
func TestADeletedFileIsEvidenceButNotSomethingToOpen(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "draft.md", "superseded")
	space.WatchTree("7")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	space.RecordChanges("7")

	fact, found := factFor(space.ArtifactFacts("7"), "draft.md")
	if !found {
		t.Fatal("the run removed draft.md and the evidence record does not say so")
	}
	if !fact.Observed || fact.Change != ArtifactDeleted {
		t.Fatalf("draft.md fact = %+v, want observed and deleted", fact)
	}
	if artifacts := space.Artifacts("7"); slices.Contains(artifacts, "draft.md") {
		t.Fatalf("artifacts = %v, want a file that no longer exists left out", artifacts)
	}
}

// The harness's own files stay out of the answer however they are seen. A job
// log recorded internally must not be smuggled back in by the tree diff, because
// the diff has no idea whose file it is.
func TestTheHarnessOwnFilesStayOutOfTheAnswer(t *testing.T) {
	space := workspace(t)
	space.WatchTree("7")
	log := touch(t, space, "job-1.log", "starting…")
	space.RecordInternal("7", log)
	touch(t, space, "answer.md", "the work")
	space.RecordChanges("7")

	artifacts := space.Artifacts("7")
	if len(artifacts) != 1 || artifacts[0] != "answer.md" {
		t.Fatalf("artifacts = %v, want only the deliverable", artifacts)
	}
	if _, found := factFor(space.ArtifactFacts("7"), "job-1.log"); found {
		t.Fatal("the job log reached the evidence record through the tree diff")
	}
}

// Dependency trees and machinery directories are somebody else's files. The
// snapshot skips exactly what the per-command sweep skips, from the same
// predicate, so the two accounts cannot disagree about what a deliverable is.
func TestTheSnapshotSkipsMachineryAndDependencyTrees(t *testing.T) {
	space := workspace(t)
	space.WatchTree("7")
	touch(t, space, "node_modules/left-pad/index.js", "module.exports = 1")
	touch(t, space, ".obs/spill-1.txt", "spilled")
	touch(t, space, "site-packages/numpy/__init__.py", "")
	touch(t, space, "kept.md", "the work")
	space.RecordChanges("7")

	artifacts := space.Artifacts("7")
	if len(artifacts) != 1 || artifacts[0] != "kept.md" {
		t.Fatalf("artifacts = %v, want only the file that is a deliverable", artifacts)
	}
}

// A leaf that was never watched keeps exactly the answer it always had. The
// observed account is an addition, and a caller that never opened one must not
// be handed a worse list than before.
func TestAnUnwatchedLeafKeepsTheToolSourcedAnswer(t *testing.T) {
	space := workspace(t)
	path := touch(t, space, "report.md", "# findings")
	space.Record("9", path)
	touch(t, space, "stray.txt", "written by nobody in particular")
	space.RecordChanges("9")

	artifacts := space.Artifacts("9")
	if len(artifacts) != 1 || artifacts[0] != "report.md" {
		t.Fatalf("artifacts = %v, want the tool-sourced list unchanged", artifacts)
	}
}
