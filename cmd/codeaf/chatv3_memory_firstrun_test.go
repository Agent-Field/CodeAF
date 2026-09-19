package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The first launch on a machine that has never run codeaf must come up holding
// a brain.
//
// It did not, for as long as the state root was assumed rather than made:
// SQLite creates a database file but never the directory around it, so the very
// first `codeaf` on a new machine printed a complaint about memory it did not
// have and then carried the whole of that person's use with nothing remembered.
// The fix is in [store.Open]; this is the door proving it from where the person
// stands.
func TestTheFirstLaunchOnANewMachineOpensABrain(t *testing.T) {
	fresh := t.TempDir()
	t.Setenv("HOME", fresh)
	t.Setenv(home.EnvVar, filepath.Join(fresh, ".codeaf"))

	profile := t.TempDir()
	brain := v3Memory(profile)
	if brain == nil {
		t.Fatal("a first run on an empty home opened no brain")
	}
	defer brain.Close()

	// Not merely opened: the file is on the disk where the rest of this binary
	// looks for it, so the next launch and every other surface find the same
	// memories.
	if _, err := os.Stat(defaultChatDB()); err != nil {
		t.Fatalf("the brain is not at %s: %v", defaultChatDB(), err)
	}

	// And it answers: an empty shelf and no error is what a new machine holds,
	// which is what the memory place teaches over rather than apologising for.
	shelves, err := brain.MemorySnapshot(20)
	if err != nil {
		t.Fatalf("reading a brand new brain: %v", err)
	}
	if shelves.Held != 0 {
		t.Fatalf("a brand new brain already holds %d", shelves.Held)
	}
}

// A store that genuinely cannot be opened still costs the person only their
// memory, and the sentence they get names the file and the real reason. This
// pins the reason, because the wording SQLite supplies for every one of these —
// "out of memory (14)" — is a lie about the machine.
func TestAnUnopenableBrainSaysWhichFileAndWhy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so there is nothing here to refuse")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, locked)

	profile := t.TempDir()
	if brain := v3Memory(profile); brain != nil {
		_ = brain.Close()
		t.Fatal("a brain in a directory nobody may write to opened anyway")
	}
	// The door prints the sentence to stderr and hands back nothing, so the
	// sentence itself is asked of the same call the door makes.
	opened, err := store.Open(defaultChatDB())
	if err == nil {
		opened.Close()
		t.Fatal("expected the open to fail")
	}
	said := err.Error()
	for _, want := range []string{"could not open ", "graph.db", "permission denied"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence is missing %q: %q", want, said)
		}
	}
	if strings.Contains(said, "out of memory") {
		t.Fatalf("the sentence still blames memory: %q", said)
	}
}

// A18: memory off leaves remember and reflex absent and still opens graph.db
// for search_conversations and the search place.
func TestTheDoorOpensHistoryWhenMemoryIsOff(t *testing.T) {
	fresh := t.TempDir()
	t.Setenv("HOME", fresh)
	t.Setenv(home.EnvVar, filepath.Join(fresh, ".codeaf"))

	profile := t.TempDir()
	registry := config.NewSettings(config.SettingsOptions{
		ProfileDir: profile,
		ModelValue: func(slot string) string { return slot + "/model" },
		SetModel:   func(string, string) error { return nil },
		SplitPct:   func() int { return 0 },
	})
	row, found := registry.Row(config.KeyMemoryEnabled)
	if !found {
		t.Fatalf("no settings row %q", config.KeyMemoryEnabled)
	}
	if err := row.Apply(config.MemoryOff); err != nil {
		t.Fatalf("turning memory off: %v", err)
	}
	if brain := v3Memory(profile); brain != nil {
		_ = brain.Close()
		t.Fatal("the door opened a brain with memory turned off")
	}
	history := v3History()
	if history == nil {
		t.Fatal("the door opened no history with memory turned off")
	}
	defer history.Close()
	if got := historyStore(session.Config{ConversationHistory: history}); got != history {
		t.Fatal("the search place did not follow ConversationHistory")
	}
}
