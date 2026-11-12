package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE @ COMPLETES A TYPED PATH FROM EXACTLY WHERE IT SAYS: a relative path
// from the agent's workspace, `~/` from home, an absolute path from itself,
// and no agent at all from home. A folder that is not there is said, and an
// agent nobody knows is not_found.
func TestAppCompletePathFromTheWorkspaceHomeAndAnywhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	work := agent.Workspace
	for _, dir := range []string{filepath.Join(work, "internal", "parse"), filepath.Join(home, "Downloads")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(work, "internal", "beat.go"), filepath.Join(home, "notes.md")} {
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	in, err := backend.CompletePath(agent.ID, "internal/", 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []appwire.PathEntry{{Name: "parse", Path: "internal/parse/", Dir: true}, {Name: "beat.go", Path: "internal/beat.go"}}
	if in.Dir != filepath.Join(work, "internal") || in.State != "" || len(in.Entries) != 2 || in.Entries[0] != want[0] || in.Entries[1] != want[1] {
		t.Fatalf("internal/ from the workspace = %+v", in)
	}

	tilde, err := backend.CompletePath(agent.ID, "~/Do", 50)
	if err != nil || tilde.Dir != home || len(tilde.Entries) != 1 || tilde.Entries[0].Path != "~/Downloads/" {
		t.Fatalf("~/Do = %+v, %v", tilde, err)
	}
	abs, err := backend.CompletePath(agent.ID, filepath.Join(work, "internal", "b"), 50)
	if err != nil || len(abs.Entries) != 1 || abs.Entries[0].Path != filepath.Join(work, "internal", "beat.go") {
		t.Fatalf("an absolute path = %+v, %v", abs, err)
	}
	none, err := backend.CompletePath("", "no", 50)
	if err != nil || none.Dir != home || len(none.Entries) != 1 || none.Entries[0].Path != "notes.md" {
		t.Fatalf("no agent = %+v, %v", none, err)
	}
	gone, err := backend.CompletePath(agent.ID, "nowhere/", 50)
	if err != nil || gone.State != appwire.PathMissing || gone.Entries == nil || len(gone.Entries) != 0 {
		t.Fatalf("a missing folder = %+v, %v", gone, err)
	}
	var wire *appwire.Error
	if _, err := backend.CompletePath("no-such-agent", "x/", 50); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("an unknown agent answered %v", err)
	}
}
