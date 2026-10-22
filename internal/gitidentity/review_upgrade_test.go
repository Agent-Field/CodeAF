package gitidentity_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

func TestReviewExistingPathOwnedMemoryRemainsVisible(t *testing.T) {
	for _, git := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "git-without-origin"}[git], func(t *testing.T) { reviewPathMemory(t, git) })
	}
}

func reviewPathMemory(t *testing.T, git bool) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	target := t.TempDir()
	if git {
		if out, err := exec.Command("git", "init", target).CombinedOutput(); err != nil {
			t.Fatalf("git init: %s: %v", out, err)
		}
	}
	alias := filepath.Join(t.TempDir(), "notes")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(alias)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(absolute))
	oldKey := hex.EncodeToString(sum[:16])
	path := filepath.Join(t.TempDir(), "brain.db")
	brain, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	written, err := brain.Write(store.WriteRequest{Owner: store.OwnerProject(oldKey), Text: "Use the archived client contract for this project", SourceSession: "existing-session"})
	if err != nil {
		t.Fatal(err)
	}
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	brain, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	key, err := gitidentity.ProjectKey(alias)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := session.New(session.Config{Workspace: alias, Model: "test/model", APIKey: "unused", BaseURL: "http://127.0.0.1:1", Memory: brain, MemoryProjectKey: key, ProfileDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	rows, err := agent.Memories("")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("old owner=%s, current owner=%s, visible=%d", store.OwnerProject(oldKey), store.OwnerProject(key), len(rows))
	for _, row := range rows {
		if row.ID == written.Memory.ID {
			// Editing in project scope must reach the historical row.
			if _, err := agent.MoveMemory(row.ID, "project"); err != nil {
				t.Fatal(err)
			}
			if _, err := agent.Remember("Keep new project notes under the current owner"); err != nil {
				t.Fatal(err)
			}
			current, err := brain.MemoriesFor(session.MemoryOwners(key))
			if err != nil || len(current) != 2 {
				t.Fatalf("canonical project writes: %v (%v)", current, err)
			}
			return
		}
	}
	t.Fatalf("existing project memory %s disappeared after reopening unchanged workspace %q", written.Memory.ID, alias)
}
