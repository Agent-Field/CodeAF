package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

func TestReviewPathMemoryAliasesPermitScopedEdits(t *testing.T) {
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	key, err := gitidentity.ProjectKey(alias)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	oldOwner := store.OwnerProject(hex.EncodeToString(sum[:16]))
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = alias; c.MemoryProjectKey = key })
	saved, err := brain.Write(store.WriteRequest{Owner: oldOwner, Text: "Use the archived client contract", SourceSession: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := brain.Write(store.WriteRequest{Owner: store.OwnerProject("unrelated"), Text: "Never show the unrelated project's rule"}); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.refreshMemoryLocked()
	block := agent.memoryText
	agent.mu.Unlock()
	if !strings.Contains(block, saved.Memory.Text) || strings.Contains(block, "unrelated") {
		t.Fatalf("project memory block: %q", block)
	}
	// Editing a historical line retains its owner, preserving the store graph.
	// A new independently remembered line uses today's canonical write key.
	fresh, _, _, err := agent.keepLine("Use the current client contract", "project", store.OriginPerson, &saved.Memory, false)
	if err != nil || fresh.Owner != oldOwner {
		t.Fatalf("scoped correction: %+v (%v)", fresh, err)
	}
	if _, err := agent.Remember("Keep fresh project notes"); err != nil {
		t.Fatal(err)
	}
	current, err := brain.MemoriesFor([]string{store.OwnerProject(key)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range current {
		if row.Text == "Keep fresh project notes" {
			found = true
		}
	}
	if !found {
		t.Fatal("new line was not kept under the canonical owner")
	}
	if receipt, err := agent.forgetLine("current client contract"); err != nil || !strings.HasPrefix(receipt, "forgot") {
		t.Fatalf("scoped forget: %s (%v)", receipt, err)
	}
	if receipt, err := agent.forgetLine("unrelated"); err != nil || !strings.HasPrefix(receipt, "nothing remembered matches") {
		t.Fatalf("unrelated scope: %s (%v)", receipt, err)
	}
}
