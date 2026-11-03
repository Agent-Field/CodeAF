package pair

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// L4-H2a: looking around creates bookkeeping, but no chat to lose on either road.
func TestL4LookedAroundCanPair(t *testing.T) {
	for _, link := range []bool{false, true} {
		h := t.TempDir()
		if _, err := identity.EnsureSolo(h); err != nil {
			t.Fatal(err)
		}
		if _, err := devname.Set(h, "desk-b"); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(h, "v3/projects/project/chat/.cell")
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "transcript.jsonl"), []byte("{\"type\":\"session\",\"id\":\"chat\"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		// Starting a chat writes its policy, while state.json records tool-created work.
		if err := os.WriteFile(filepath.Join(p, ".furrowpolicy"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		other, err := identity.Ensure(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		j := Joining{Home: h}
		if link {
			ready, err := (LinkJoining{Joining: j}).free()
			if err != nil {
				t.Fatal(err)
			}
			j = ready.Joining
		}
		if _, err := adopt(j, Grant{Identity: other}); err != nil {
			t.Fatalf("link=%v: %v", link, err)
		}
	}
}

// L4-H2b: the refusal counts submitted chats and explains the replacement truthfully.
func TestL4PairCountsChats(t *testing.T) {
	h := t.TempDir()
	if _, err := identity.EnsureSolo(h); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		p := filepath.Join(h, "v3/projects/project", name, ".cell")
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "transcript.jsonl"), []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := (LinkJoining{Joining: Joining{Home: h}}).free()
	if !errors.Is(err, ErrDifferentChats) || !strings.Contains(err.Error(), "already has 3 chats of its own") || !strings.Contains(err.Error(), "--replace") || !strings.Contains(err.Error(), "stay on this computer") {
		t.Fatalf("%v", err)
	}
}

// L4-H2b: one chat is counted in the singular, as the hand walk read it.
func TestL4PairCountsOneChat(t *testing.T) {
	h := t.TempDir()
	if _, err := identity.EnsureSolo(h); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(h, "v3/projects/project", "a", ".cell")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "transcript.jsonl"), []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (LinkJoining{Joining: Joining{Home: h}}).free()
	if !errors.Is(err, ErrDifferentChats) || !strings.Contains(err.Error(), "already has 1 chat of its own") || !strings.Contains(err.Error(), "that chat stays on this computer but no longer syncs") || strings.Contains(err.Error(), "those 1") {
		t.Fatalf("%v", err)
	}
}

// L4-H2d: the approver's startup instruction has no key-management vocabulary.
func TestL4ApprovalNeedsOneStart(t *testing.T) {
	if ErrNotJoined.Error() != "open codeaf on this computer once, then approve again" {
		t.Fatal(ErrNotJoined)
	}
}
