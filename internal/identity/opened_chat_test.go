package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/identity/identitytest"
)

func TestAComputerThatOnlyOpenedAndQuitAChatCanJoin(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "borrowed project", true: "owned project"}[owned], func(t *testing.T) {
			home := t.TempDir()
			place := identitytest.OpenAndQuit(t, home, owned)
			if ok, err := identity.Joinable(home); !ok || err != nil {
				t.Fatalf("an opened and quit chat cannot join: %v, %v", ok, err)
			}
			for _, name := range []string{"memories.jsonl", "state.json", "env/notes.txt"} {
				path := filepath.Join(place.Dir, ".cell", name)
				if err := os.WriteFile(path, []byte("keep my work"), 0o600); err != nil {
					t.Fatal(err)
				}
				if ok, err := identity.Joinable(home); ok || err != nil {
					t.Fatalf("%s must keep the home from joining: %v, %v", name, ok, err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(place.Dir, ".cell", "env/inventory.json")
			for _, body := range []string{
				`{"V":1,"platform":{},"annotations":{"note":{"text":"keep"}}}`,
				`{"V":1,"platform":{"notes":"keep"}}`,
				`{"V":2,"platform":{}}`,
				`{"V":1,"platform":{}} {"notes":"keep"}`,
			} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if ok, err := identity.Joinable(home); ok || err != nil {
					t.Fatalf("inventory notes or unknown contents must remain work: %v, %v: %s", ok, err, body)
				}
			}
		})
	}
}
