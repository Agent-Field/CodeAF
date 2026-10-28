package taskcopy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletedResultsMaterializeCommittedAndDirtyFilesWithoutLiveCopies(t *testing.T) {
	m := newMachine(t)
	write(t, filepath.Join(m.project, "large-input.dat"), strings.Repeat("unchanged input\n", 1<<17))
	run(t, m.project, "git", "add", "large-input.dat")
	run(t, m.project, "git", "commit", "-qm", "large input baseline")
	tree := m.task("1")
	write(t, filepath.Join(tree, "committed.txt"), "committed result\n")
	run(t, tree, "git", "add", "committed.txt")
	run(t, tree, "git", "commit", "-qm", "result")
	write(t, filepath.Join(tree, "dirty.txt"), "dirty result\n")
	if err := os.Remove(filepath.Join(tree, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	_ = m.task("2")
	m.seal()
	to := newMachineNoRepo(t)
	m.moveTo(to)
	dest := filepath.Join(to.cell.Root, ".cell", "fork-history", "fork", "results")
	results, err := RestoreCompletedResults(to.cell, to.project, dest, []ResultSelection{{ID: "1", Paths: []string{"committed.txt", "dirty.txt", "keep.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"committed.txt": "committed result\n", "dirty.txt": "dirty result\n"} {
		raw, err := os.ReadFile(filepath.Join(results["1"], name))
		if err != nil || string(raw) != want {
			t.Fatalf("result %s=%q %v", name, raw, err)
		}
	}
	for _, name := range []string{"keep.txt", "README.md", "large-input.dat", ".git"} {
		if _, err := os.Lstat(filepath.Join(results["1"], name)); !os.IsNotExist(err) {
			t.Fatalf("result copied deleted/input/Git path %s: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dest, "2")); !os.IsNotExist(err) {
		t.Fatal("active task copied")
	}
	if entries, _ := os.ReadDir(liveRoot(to.cell)); len(entries) != 0 {
		t.Fatal("results created runnable working copies")
	}
}

func TestReportOnlyResultsNeverMaterializeTheProject(t *testing.T) {
	m := newMachine(t)
	_ = m.task("1")
	m.seal()
	dest := filepath.Join(t.TempDir(), "results")
	results, err := RestoreCompletedResults(m.cell, m.project, dest, []ResultSelection{{ID: "1"}})
	if err != nil || len(results) != 0 {
		t.Fatalf("report-only task materialized files: %v %v", results, err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatal("report-only task made a resident output tree")
	}
}

func TestHistoricalOutputSelectionCannotLeaveItsFolder(t *testing.T) {
	m := newMachine(t)
	_ = m.task("1")
	m.seal()
	for _, selection := range []ResultSelection{{ID: "../outside", Paths: []string{"README.md"}}, {ID: "1", Paths: []string{"../../outside"}}, {ID: "1", Paths: []string{".git/config"}}} {
		if _, err := RestoreCompletedResults(m.cell, m.project, filepath.Join(t.TempDir(), "results"), []ResultSelection{selection}); err == nil {
			t.Fatalf("unsafe output accepted: %+v", selection)
		}
	}
}

func TestCompletedResultsPreserveLiteralDanglingSymlinks(t *testing.T) {
	m := newMachine(t)
	tree := m.task("1")
	if err := os.Symlink("missing-result-target", filepath.Join(tree, "result-link")); err != nil {
		t.Fatal(err)
	}
	m.seal()
	to := newMachineNoRepo(t)
	m.moveTo(to)
	dest := filepath.Join(to.cell.Root, ".cell", "fork-history", "fork", "results")
	results, err := RestoreCompletedResults(to.cell, to.project, dest, []ResultSelection{{ID: "1", Paths: []string{"result-link"}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(results["1"], "result-link"))
	if err != nil || got != "missing-result-target" {
		t.Fatalf("retained link=%q: %v", got, err)
	}
}
