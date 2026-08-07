package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// countingResolver counts one workspace lookup per rendered answer body, which
// makes "did the thread re-render?" an observable fact rather than a guess.
type countingResolver struct {
	*fakeCommander
	workspace string
	resolves  int
}

func (c *countingResolver) ResolveWorkspacePath(_ string, relative string) (string, bool) {
	c.resolves++
	target := filepath.Join(c.workspace, filepath.Clean(relative))
	info, err := os.Stat(target)
	return target, err == nil && !info.IsDir()
}

func countingResolverModel(t *testing.T) (*Model, *countingResolver) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	commander := &countingResolver{
		fakeCommander: &fakeCommander{current: map[string]string{}}, workspace: dir,
	}
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	backend := &fakeBackend{messages: []store.Message{{
		Seq: 1, SessionID: "reuse", Role: store.RoleAgent, NodeID: "job",
		Body: "wrote notes.md for you", Time: now.Add(-2 * time.Minute),
	}}}
	model := NewWithCommander(backend, "reuse", commander)
	model.standingNow = func() time.Time { return now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	return model, commander
}

// A character in the draft changes the draft. It does not change the thread,
// the rail, or the employee file — so it must not rebuild them.
func TestTypingDoesNotRelayoutTheWholeFrame(t *testing.T) {
	model, commander := countingResolverModel(t)

	before := commander.resolves
	for _, char := range []rune{'h', 'e', 'l', 'l', 'o'} {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}
	if commander.resolves != before {
		t.Fatalf("five characters rebuilt the thread %d times", commander.resolves-before)
	}
	if model.input.Value() != "hello" {
		t.Fatalf("draft = %q, want hello", model.input.Value())
	}

	// Opening the palette changes the frame's shape, so that keystroke must
	// relayout — the guard is about unchanged heights, not about typing.
	model.input.SetValue("")
	before = commander.resolves
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !model.paletteOpen() {
		t.Fatal("a leading slash did not open the command palette")
	}
	if commander.resolves == before {
		t.Fatal("opening the palette skipped the relayout it needs")
	}
}
