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
	// A correction creates its replacement under today's write key. The old row
	// is superseded through the owner's permission list, never silently moved.
	fresh, _, _, err := agent.keepLine("Use the current client contract", "project", store.OriginPerson, &saved.Memory, false)
	if err != nil || fresh.Owner != store.OwnerProject(key) {
		t.Fatalf("scoped correction: %+v (%v)", fresh, err)
	}
	if receipt, err := agent.forgetLine("current client contract"); err != nil || !strings.HasPrefix(receipt, "forgot") {
		t.Fatalf("scoped forget: %s (%v)", receipt, err)
	}
	if receipt, err := agent.forgetLine("unrelated"); err != nil || !strings.HasPrefix(receipt, "nothing remembered matches") {
		t.Fatalf("unrelated scope: %s (%v)", receipt, err)
	}
}
