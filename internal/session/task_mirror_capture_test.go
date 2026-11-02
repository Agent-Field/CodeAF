package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func TestExplicitPlainMirrorFromGitChatCarriesItsIndependentBaseline(t *testing.T) {
	folder, place, parent := familyOnAFolder(t)
	writeFile(t, filepath.Join(parent.dir, "result.md"), "completed result\n")
	gitOut(t, parent.dir, "add", "result.md")
	gitOut(t, parent.dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "result")
	_ = partOf(t, place, parent, 8, "child holds parent baseline")
	otherProject := t.TempDir()
	gitOut(t, otherProject, "init", "-q")
	gitOut(t, otherProject, "-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "unrelated Git chat")
	at := cell.Cell{ID: place.ID(), Root: place.Dir}
	if err := (taskcopy.Carry{}).ComposeWorkspace(at, otherProject); err != nil {
		t.Fatal(err)
	}
	results, err := taskcopy.RestoreCompletedResults(at, folder, t.TempDir(), []taskcopy.ResultSelection{{ID: "7", Paths: []string{"result.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(results["7"], "result.md"))
	if err != nil || string(body) != "completed result\n" {
		t.Fatalf("independent result missing: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); !os.IsNotExist(err) {
		t.Fatal("plain project was modified")
	}
}
