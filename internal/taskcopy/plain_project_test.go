package taskcopy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func plainMirrorFixture(t *testing.T) *machine {
	t.Helper()
	m := unsealedPlainMirrorFixture(t)
	m.seal()
	return m
}

func unsealedPlainMirrorFixture(t *testing.T) *machine {
	t.Helper()
	m := newMachineNoRepo(t)
	write(t, filepath.Join(m.project, "README.md"), "plain project input\n")
	write(t, filepath.Join(m.project, "keep.txt"), "baseline\n")
	tree := filepath.Join(m.cell.Root, cell.TreesDir, "1")
	write(t, filepath.Join(tree, "README.md"), "plain project input\n")
	write(t, filepath.Join(tree, "keep.txt"), "baseline\n")
	run(t, tree, "git", "init", "-q", "-b", "main")
	run(t, tree, "git", "add", ".")
	run(t, tree, "git", "commit", "-qm", "private mirror baseline")
	write(t, filepath.Join(tree, "committed.txt"), "committed mirror output\n")
	run(t, tree, "git", "add", "committed.txt")
	run(t, tree, "git", "commit", "-qm", "output")
	write(t, filepath.Join(tree, "dirty.txt"), "dirty mirror output\n")
	if err := os.Remove(filepath.Join(tree, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPlainProjectPrivateMirrorRestoresWithoutChangingBorrowedProject(t *testing.T) {
	source := plainMirrorFixture(t)
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	restored, err := restorer.Restore(receiver.cell, receiver.project)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0] != "1" {
		t.Fatalf("mirror not restored: %v", restored)
	}
	for name, want := range map[string]string{"committed.txt": "committed mirror output\n", "dirty.txt": "dirty mirror output\n", "README.md": "plain project input\n"} {
		body, ok := receiver.read("1", name)
		if !ok || body != want {
			t.Fatalf("%s=%q present=%v", name, body, ok)
		}
	}
	if _, err := os.Stat(filepath.Join(liveRoot(receiver.cell), "1", "keep.txt")); !os.IsNotExist(err) {
		t.Fatal("deleted mirror file returned")
	}
	assertPlainProjectUnchanged(t, receiver.project)
	if receiver.branch("1") != "main" {
		t.Fatal("mirror branch identity changed")
	}
}

func TestPlainProjectCompletedMirrorResultsRemainReadable(t *testing.T) {
	source := plainMirrorFixture(t)
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	results, err := RestoreCompletedResults(receiver.cell, receiver.project, filepath.Join(receiver.cell.Root, ".cell", "history", "results"), []ResultSelection{{ID: "1", Paths: []string{"committed.txt", "dirty.txt", "keep.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"committed.txt": "committed mirror output\n", "dirty.txt": "dirty mirror output\n"} {
		raw, err := os.ReadFile(filepath.Join(results["1"], name))
		if err != nil || string(raw) != want {
			t.Fatalf("%s=%q %v", name, raw, err)
		}
	}
	assertPlainProjectUnchanged(t, receiver.project)
}

func assertPlainProjectUnchanged(t *testing.T, project string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(project, ".git")); !os.IsNotExist(err) {
		t.Fatal("borrowed plain project was made into a Git repository")
	}
	raw, err := os.ReadFile(filepath.Join(project, "keep.txt"))
	if err != nil || string(raw) != "baseline\n" {
		t.Fatal("borrowed project was modified")
	}
	if _, err := os.Stat(filepath.Join(project, "committed.txt")); !os.IsNotExist(err) {
		t.Fatal("mirror output was written to borrowed project")
	}
}

func TestPlainProjectIncompleteChildBundleRefusesAndPreservesArchive(t *testing.T) {
	source := plainMirrorFixture(t)
	parent := filepath.Join(liveRoot(source.cell), "1")
	child := filepath.Join(liveRoot(source.cell), "2")
	run(t, parent, "git", "worktree", "add", "-qb", "task/child", child)
	write(t, filepath.Join(child, "child.txt"), "nested committed result\n")
	run(t, child, "git", "add", "child.txt")
	run(t, child, "git", "commit", "-qm", "nested output")
	source.seal()
	receiver := newMachineNoRepo(t)
	source.moveTo(receiver)
	archive := filepath.Join(carriedRoot(receiver.cell), "2", bundleName)
	original, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restorer.Restore(receiver.cell, receiver.project)
	if err == nil {
		t.Fatal("partial child bundle invented its missing parent baseline")
	}
	if len(restored) != 1 || restored[0] != "1" {
		t.Fatalf("complete parent mirror did not restore independently: %v %v", restored, err)
	}
	after, readErr := os.ReadFile(archive)
	if readErr != nil || string(after) != string(original) {
		t.Fatal("failed nested restore changed its original archive")
	}
	if _, err := RestoreCompletedResults(receiver.cell, receiver.project, filepath.Join(receiver.cell.Root, ".cell", "history", "results"), []ResultSelection{{ID: "2", Paths: []string{"child.txt"}}}); err == nil {
		t.Fatal("completed child results invented a missing baseline")
	}
	assertPlainProjectUnchanged(t, receiver.project)
}
