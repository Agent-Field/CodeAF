package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestTheBackgroundCopierLeavesAFolderThatHoldsCodeafsOwnState counts copy
// attempts for a chat whose folder holds codeaf's home, as a chat started in a
// home directory does. Codeaf writes its own state there while it runs, so a
// whole-folder seal never settles; each attempt planned the folder eight times
// and failed. A chat in any other folder is still copied.
func TestTheBackgroundCopierLeavesAFolderThatHoldsCodeafsOwnState(t *testing.T) {
	user := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(user, ".codeaf"))
	chat := func(id, workspace string) string {
		dir := filepath.Join(home.Dir(), "v3", "projects", "p", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(`{"type":"session","version":1,"id":"`+id+`"}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(dir, session.Meta{ID: id, LastUserAt: time.Now(), Workspace: workspace}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	inHome := chat("in-home", user)
	elsewhere := chat("elsewhere", t.TempDir())

	var copied []string
	chats := olderChats{copy: func(_ context.Context, dir string) (cell.Cell, error) {
		copied = append(copied, filepath.Base(dir))
		return cell.Cell{Root: dir}, nil
	}}
	chats.visit(t.Context(), inHome)
	chats.visit(t.Context(), elsewhere)
	if len(copied) != 1 || copied[0] != "elsewhere" {
		t.Fatalf("copy attempts = %v, want only the chat outside codeaf's home", copied)
	}
}
