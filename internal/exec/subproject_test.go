package exec

// The reading is taken where the PROJECT is, and a corpus task does not clone its
// project at the workspace root. awilix lands under ./repo, bandit under
// ./bandit, and a reading taken at the bare root found no manifest, no Makefile
// and no script there — so the errand ran no check at all, and awilix shipped a
// src/awilix.ts that a build would have caught.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/verify"
)

// A PROJECT CLONED INTO A SUBDIRECTORY IS STILL THE PROJECT THE READING IS OF.
//
// The workspace root here declares nothing: the one file that says how this
// project is checked lives in ./repo. The reading must find it there and name it,
// rather than answering that this project declares no way of checking itself.
func TestAReadingIsTakenAtAProjectClonedIntoASubdirectory(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "Makefile"),
		[]byte("test:\n\t@echo 'ok 1 - a check'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{Goal: "Fix src/awilix.ts so the project's own check passes."}

	opening := PhotographBefore(context.Background(), workspace, nil, time.Hour, task)
	if !opening.Reading.Taken {
		t.Fatalf("a project cloned into ./repo declared no way of checking itself: %q",
			opening.Reading.Unread)
	}
	if strings.TrimSpace(opening.Reading.Unread) != "" {
		t.Errorf("a reading that was taken carries a reason for not being: %q",
			opening.Reading.Unread)
	}
	// The command runs at the discovered project, spelled workspace-relative, so
	// the second reading and every other reader reaches the same place.
	if opening.Reading.Strategy.Workdir != "repo" {
		t.Errorf("the reading was taken at %q, want the discovered project root \"repo\"",
			opening.Reading.Strategy.Workdir)
	}
	if !strings.Contains(opening.Reading.Before.Strategy.Command, "make test") {
		t.Errorf("the reading did not name the project's own check: %q",
			opening.Reading.Before.Strategy.Command)
	}
}

// AND A WORKSPACE ROOT THAT DECLARES ITS OWN CHECK IS UNCHANGED by any of that:
// the subdirectory walk is a fallback and never a redirection.
func TestAReadingAtTheRootIgnoresASubdirectory(t *testing.T) {
	verify.ForgetBaselines()
	t.Cleanup(verify.ForgetBaselines)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"),
		[]byte("test:\n\t@echo 'ok 1 - the root check'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "Makefile"),
		[]byte("test:\n\t@echo 'ok 1 - the nested check'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	opening := PhotographBefore(context.Background(), workspace, nil, time.Hour,
		Task{Goal: "Change nothing."})
	if !opening.Reading.Taken {
		t.Fatalf("a root that declares its own check took no reading: %q", opening.Reading.Unread)
	}
	if opening.Reading.Strategy.Workdir != "" {
		t.Errorf("a root with its own check was redirected to %q",
			opening.Reading.Strategy.Workdir)
	}
}
