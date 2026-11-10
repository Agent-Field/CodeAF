package cellstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
)

// A CHAT WHOSE FOLDER IS THE CODEAF HOME SAVES ONLY ITS OWN FOLDER. The home
// is codeaf's state — the graph database, the factory's clones, the vault this
// guard writes — so a chat opened there, or in a folder above it, never seals
// or sweeps it. A folder inside it (a factory job's own checkout) is a project
// and is still saved. This is what stops the chats that already name the home
// from sweeping it, whichever door reopens them.
func TestTheCodeafHomeIsNeverSavedWithAChat(t *testing.T) {
	state := filepath.Join(t.TempDir(), "codeaf-home")
	t.Setenv(home.EnvVar, state)
	job := filepath.Join(state, "v3", "factory", "work", "ledger-12")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	if !holdsHome(state) || !holdsHome(filepath.Dir(state)) {
		t.Fatal("the codeaf home, or the folder above it, would be saved with a chat")
	}
	if holdsHome(job) {
		t.Fatal("a job's own checkout inside the codeaf home would not be saved")
	}
	c := cell.Cell{Root: t.TempDir()}
	if got := EngineFor(state).workspace(c); got != "" {
		t.Fatalf("a chat in the codeaf home seals %q; want only its own folder", got)
	}
	if got := EngineFor(job).workspace(c); got != job {
		t.Fatalf("a chat in a job's checkout seals %q; want %q", got, job)
	}
}
