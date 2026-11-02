package pair

import (
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

// L4-H2d: the approver's startup instruction has no key-management vocabulary.
func TestL4ApprovalNeedsOneStart(t *testing.T) {
	if ErrNotJoined.Error() != "open codeaf on this computer once, then approve again" {
		t.Fatal(ErrNotJoined)
	}
}

// A solo computer with chats of its own may join: app/v3 merges its chats into
// the shared set instead of refusing or replacing them (1c4ec8a25, #2031), and
// the joined line says so. Dev's count-and-refuse wording (#2087) does not
// apply on this branch.
func TestL4PairKeepsAComputersOwnChats(t *testing.T) {
	for _, chats := range []int{1, 3} {
		h := t.TempDir()
		if _, err := identity.EnsureSolo(h); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < chats; i++ {
			p := filepath.Join(h, "v3/projects/project", string(rune('a'+i)), ".cell")
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "transcript.jsonl"), []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := (LinkJoining{Joining: Joining{Home: h}}).free(); err != nil {
			t.Fatalf("%d chats: a computer with its own chats was refused: %v", chats, err)
		}
		if !strings.Contains(JoinedLine, "Your chats are kept") {
			t.Fatal(JoinedLine)
		}
	}
}
