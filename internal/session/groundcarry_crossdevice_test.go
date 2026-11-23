package session

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestResolveCollisionAcrossFilesystemsKeepsExactCopies(t *testing.T) {
	a, repo, id, _ := localLandingFixture(t, true)
	original := renameGroundCopy
	defer func() { renameGroundCopy = original }()
	moves := 0
	renameGroundCopy = func(from, to string) error {
		moves++
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionConflict, ID: id, Key: LandingYesKey}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt")); got != "task copy\n" {
		t.Fatalf("task did not land: %q", got)
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt.yours")); got != "my newer untracked copy\n\x00" {
		t.Fatalf("person's exact bytes lost: %q", got)
	}
	if moves < 2 || len(a.OpenQuestions()) != 0 {
		t.Fatalf("resolution did not move both copies and settle: moves=%d", moves)
	}
}

func TestResolveCollisionDoesNotUseSystemTemp(t *testing.T) {
	a, repo, id, _ := localLandingFixture(t, true)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	if err := a.ResolveQuestion(Answer{Kind: QuestionConflict, ID: id, Key: LandingYesKey}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt.yours")); got != "my newer untracked copy\n\x00" {
		t.Fatalf("person's exact bytes lost: %q", got)
	}
}

func TestCarryRefusalNamesTheMoveFailure(t *testing.T) {
	place, repo := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, repo, "ffff6666ffff6666", 1, "write collision")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "COLLISION.txt"), "task copy\n")
	writeFile(t, filepath.Join(repo, "COLLISION.txt"), "my copy\n")
	original := renameGroundCopy
	defer func() { renameGroundCopy = original }()
	renameGroundCopy = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EACCES}
	}
	merge, detail, _, _ := carryOnTheirWord(tree).comeHome("write collision", []string{"COLLISION.txt"}, gitSignature{})
	if cameHome(merge) || !strings.Contains(detail, syscall.EACCES.Error()) {
		t.Fatalf("refusal hides the move failure: merge=%q detail=%q", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "COLLISION.txt")); got != "my copy\n" {
		t.Fatalf("refusal changed person's bytes: %q", got)
	}
}

func TestCarryRestoreFailureRetainsThePersonsSavedBytes(t *testing.T) {
	place, repo := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, repo, "ffff6666ffff6666", 1, "write collision")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "COLLISION.txt"), "task copy\n")
	writeFile(t, filepath.Join(repo, "COLLISION.txt"), "my copy\n\x00")
	original := renameGroundCopy
	defer func() { renameGroundCopy = original }()
	var saved string
	renameGroundCopy = func(from, to string) error {
		if strings.HasSuffix(to, ".yours") {
			saved = from
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EACCES}
		}
		return original(from, to)
	}
	merge, detail, _, _ := carryOnTheirWord(tree).comeHome("write collision", []string{"COLLISION.txt"}, gitSignature{})
	if cameHome(merge) || saved == "" || !strings.Contains(detail, syscall.EACCES.Error()) || !strings.Contains(detail, filepath.Dir(saved)) {
		t.Fatalf("restore failure lost its recovery location: merge=%q detail=%q saved=%q", merge, detail, saved)
	}
	if got := readFile(t, saved); got != "my copy\n\x00" {
		t.Fatalf("saved bytes lost: %q", got)
	}
}
