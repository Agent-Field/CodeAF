package taskcopy

import (
	"os"
	"path/filepath"
	"testing"
)

func nestedPlainFixture(t *testing.T) *machine {
	t.Helper()
	source := plainMirrorFixture(t)
	parent := filepath.Join(liveRoot(source.cell), "1")
	child := filepath.Join(liveRoot(source.cell), "2")
	run(t, parent, "git", "worktree", "add", "-qb", "task/child", child)
	write(t, filepath.Join(child, "child.txt"), "nested committed result\n")
	run(t, child, "git", "add", "child.txt")
	run(t, child, "git", "commit", "-qm", "child output")
	write(t, filepath.Join(child, "child-dirty.txt"), "nested dirty result\n")
	source.seal()
	return source
}

func TestExplicitParentRestoresNestedPlainMirrorAsLinkedWorktree(t *testing.T) {
	source := nestedPlainFixture(t)
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	restored, err := restorer.RestoreWithParents(receiver.cell, receiver.project, map[string]string{"2": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 || restored[0] != "1" || restored[1] != "2" {
		t.Fatalf("wrong restoration order: %v", restored)
	}
	child := filepath.Join(liveRoot(receiver.cell), "2")
	info, err := os.Stat(filepath.Join(child, ".git"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("linked child became independent: %v", err)
	}
	for name, want := range map[string]string{"child.txt": "nested committed result\n", "child-dirty.txt": "nested dirty result\n"} {
		raw, err := os.ReadFile(filepath.Join(child, name))
		if err != nil || string(raw) != want {
			t.Fatalf("%s=%q %v", name, raw, err)
		}
	}
	assertPlainProjectUnchanged(t, receiver.project)
}

func TestNestedCompletedResultsReconstructParentOnlyInTemporaryScratch(t *testing.T) {
	source := nestedPlainFixture(t)
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	destination := filepath.Join(receiver.cell.Root, ".cell", "history", "results")
	results, err := RestoreCompletedResultsWithParents(receiver.cell, receiver.project, destination, []ResultSelection{{ID: "2", Paths: []string{"child.txt", "child-dirty.txt"}}}, map[string]string{"2": "1"})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"child.txt": "nested committed result\n", "child-dirty.txt": "nested dirty result\n"} {
		raw, err := os.ReadFile(filepath.Join(results["2"], name))
		if err != nil || string(raw) != want {
			t.Fatalf("%s=%q %v", name, raw, err)
		}
	}
	if entries, _ := os.ReadDir(liveRoot(receiver.cell)); len(entries) != 0 {
		t.Fatal("historical result reconstruction instantiated active parent/child copies")
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != "2" {
		t.Fatalf("parent scratch was retained: %v %v", entries, err)
	}
	assertPlainProjectUnchanged(t, receiver.project)
}
