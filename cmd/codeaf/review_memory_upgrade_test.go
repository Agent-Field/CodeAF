package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The launch has already replaced the raw spelling before it assembles the
// config. The old conversation citation must still recover its memory owner.
func TestReviewResumedConversationKeepsPathMemory(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	oldBucket := homeProjectForReview(alias)
	old := writeV3Session(t, oldBucket, "00000000000000f3", "remember this project's contract", time.Now())
	meta, err := session.LoadMeta(old)
	if err != nil {
		t.Fatal(err)
	}
	meta.Workspace = alias
	if err := session.SaveMeta(old, meta); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	oldOwner := store.OwnerProject(hex.EncodeToString(sum[:16]))
	brain, err := store.Open(defaultChatDB())
	if err != nil {
		t.Fatal(err)
	}
	const words = "Use the archived client contract for this project"
	saved, err := brain.Write(store.WriteRequest{Owner: oldOwner, Text: words, SourceSession: "existing-session"})
	if err != nil {
		t.Fatal(err)
	}
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	project, owned := v3Workspace(alias, alias)
	if got := doMemory(project); !strings.Contains(got, words) {
		t.Fatalf("headless project's memory: %q", got)
	}
	brain, err = store.Open(defaultChatDB())
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	found, err := v3ResolveSession("", project, alias, owned)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := v3PointAt(session.Config{Workspace: project, Memory: brain, Model: "test/model", APIKey: "unused", BaseURL: "http://127.0.0.1:1", ProfileDir: t.TempDir()}, found.Place)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	rows, err := agent.Memories("")
	if err != nil || len(rows) != 1 || rows[0].ID != saved.Memory.ID {
		t.Fatalf("resumed project memories: %+v (%v)", rows, err)
	}
	// Compatibility is a read permission, not a silent durable reownership.
	oldRows, err := brain.MemoriesFor([]string{oldOwner})
	if err != nil || len(oldRows) != 1 {
		t.Fatalf("opening changed historical ownership: %v (%v)", oldRows, err)
	}
}

func homeProjectForReview(workspace string) string {
	return filepath.Join(os.Getenv("CODEAF_HOME"), "v3", "projects", encodeWorkspace(workspace))
}
