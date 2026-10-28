package taskcopy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestColdPlainMirrorCarriesCompleteBaselineBeforeFirstSeal(t *testing.T) {
	source := unsealedPlainMirrorFixture(t)
	parent := filepath.Join(liveRoot(source.cell), "1")
	child := filepath.Join(liveRoot(source.cell), "2")
	run(t, parent, "git", "worktree", "add", "-qb", "task/child", child)
	write(t, filepath.Join(child, "child.txt"), "child output\n")
	run(t, child, "git", "add", "child.txt")
	run(t, child, "git", "commit", "-qm", "child")
	carry := Carry{Project: source.project}
	if err := carry.PrepareCold(source.cell, parent); err != nil {
		t.Fatal(err)
	}
	verify := func() {
		t.Helper()
		rec, err := readRecord(filepath.Join(carriedRoot(source.cell), "1"))
		if err != nil || !rec.FullHistory {
			t.Fatalf("plain baseline completeness not recorded: %+v %v", rec, err)
		}
		empty := t.TempDir()
		run(t, empty, "git", "init", "-q")
		run(t, empty, "git", "bundle", "verify", filepath.Join(carriedRoot(source.cell), "1", bundleName))
	}
	verify()
	// A later preparation without source context must not downgrade its known
	// self-contained archive, including after the mirrored parent commits again.
	write(t, filepath.Join(parent, "later.txt"), "later output\n")
	run(t, parent, "git", "add", "later.txt")
	run(t, parent, "git", "commit", "-qm", "later")
	if err := (Carry{}).PrepareCold(source.cell, parent); err != nil {
		t.Fatal(err)
	}
	verify()
	if err := (Carry{}).Compose(source.cell); err != nil {
		t.Fatal(err)
	}
	verify()
	assertPlainProjectUnchanged(t, source.project)
}

func TestPlainWorkspaceCaptureIncludesBaselineDespiteChildReferences(t *testing.T) {
	source := unsealedPlainMirrorFixture(t)
	parent := filepath.Join(liveRoot(source.cell), "1")
	child := filepath.Join(liveRoot(source.cell), "2")
	run(t, parent, "git", "worktree", "add", "-qb", "task/child", child)
	if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(carriedRoot(source.cell), "1", bundleName)
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("parent has no independent baseline: %v", err)
	}
	empty := t.TempDir()
	run(t, empty, "git", "init", "-q")
	run(t, empty, "git", "bundle", "verify", archive)
}
