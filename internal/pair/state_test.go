package pair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestC30PairingMergesMemoryUnlessReplaceWasExplicit proves C30.4 at both
// adoption doors: hand-edited personal lines survive a join; --replace clears
// shared memory/preferences while preserving machine-owned lines and paths.
func TestC30PairingMergesMemoryUnlessReplaceWasExplicit(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "replace"}[replace], func(t *testing.T) {
			home := t.TempDir()
			if _, err := identity.EnsureSolo(home); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"model.talk":"stub/local","machine.path":"/keep"}`), 0600); err != nil {
				t.Fatal(err)
			}
			brain, err := store.Open(filepath.Join(home, "graph.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer brain.Close()
			one, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "My hand-edited line."})
			if err != nil {
				t.Fatal(err)
			}
			local, err := brain.AddMemory(store.Memory{Owner: store.OwnerMachine, Text: "This machine uses local tools."})
			if err != nil {
				t.Fatal(err)
			}
			other, err := identity.Ensure(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adopt(Joining{Home: home, Replace: replace}, Grant{Identity: other}); err != nil {
				t.Fatal(err)
			}
			if replace {
				if err := brain.Rebuild(); err != nil {
					t.Fatal(err)
				}
			}
			_, found, err := brain.MemoryRecord(one.ID)
			if err != nil || found == replace {
				t.Fatalf("replace=%v personal memory found=%v %v", replace, found, err)
			}
			_, found, err = brain.MemoryRecord(local.ID)
			if err != nil || !found {
				t.Fatal("pairing lost machine memory")
			}
			prefs, err := config.ReadPreferences(home)
			if err != nil {
				t.Fatal(err)
			}
			if _, found := prefs[config.KeyChatModel]; found == replace {
				t.Fatal("preference replacement disagrees with memory")
			}
			raw, err := os.ReadFile(filepath.Join(home, "config.json"))
			if err != nil || !containsPath(raw) {
				t.Fatal("pairing removed machine path")
			}
		})
	}
}
func containsPath(raw []byte) bool { return strings.Contains(string(raw), `"/keep"`) }
