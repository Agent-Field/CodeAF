package pair

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

type preflightMailbox struct {
	pairbox.Box
	calls int
	err   error
}

func (b *preflightMailbox) Limits(context.Context) (pairbox.Limits, error) {
	b.calls++
	return pairbox.Limits{}, b.err
}

func preflightChat(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "v3/projects/project/chat/transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"session\",\"id\":\"chat\"}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"keep this turn\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// L4-C: A used solo home refuses before even checking the mailbox. Replacement,
// startup-only homes and previously paired homes retain their existing roads.
func TestL4Fix4SoloJoinRefusesBeforeContactingMailbox(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		used, paired, replace bool
	}{
		{name: "used", used: true},
		{name: "local work"},
		{name: "unused"},
		{name: "replace", used: true, replace: true},
		{name: "paired", used: true, paired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			original, err := identity.EnsureSolo(home)
			if err != nil {
				t.Fatal(err)
			}
			if tc.used {
				preflightChat(t, home)
			}
			if tc.name == "local work" {
				if err := os.WriteFile(filepath.Join(home, "my-notes"), []byte("keep me"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.paired {
				if err := identity.EndSolo(home); err != nil {
					t.Fatal(err)
				}
			}
			before := filesIn(t, home)
			box := &preflightMailbox{err: errors.New("mailbox reached")}
			_, err = Join(t.Context(), Mailbox{Box: box}, Joining{Home: home, Replace: tc.replace}, "42-715-302", newScreen())
			refuse := (tc.used || tc.name == "local work") && !tc.paired && !tc.replace
			if refuse {
				if !errors.Is(err, ErrDifferentChats) || box.calls != 0 {
					t.Fatalf("join = %v; mailbox calls = %d", err, box.calls)
				}
				if tc.used && !strings.Contains(err.Error(), "already has 1 chat of its own") {
					t.Fatal(err)
				}
			} else if !errors.Is(err, box.err) || box.calls != 1 {
				t.Fatalf("existing road = %v; mailbox calls = %d", err, box.calls)
			}
			same(t, before, filesIn(t, home))
			if held, err := identity.Load(home); err != nil || held.ID() != original.ID() {
				t.Fatal("preflight changed the local account", err)
			}
		})
	}
}
