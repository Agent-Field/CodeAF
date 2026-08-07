package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// The sweep groups adjacent cells that share an ink into one render. The band
// it paints is unchanged: same text, same three inks, same positions.
func TestMatteSweepRunsPaintTheSameBand(t *testing.T) {
	text := "◐ compiling the plan · reading the repository"
	for frame := range 14 {
		model := &Model{shimmerFrame: frame}
		swept := model.matteSweep(text)
		if got := ansi.Strip(swept); got != text {
			t.Fatalf("frame %d changed the line: %q", frame, got)
		}
		center := sweepCenter(frame, utf8.RuneCountInString(text))
		var perRune strings.Builder
		for index, char := range []rune(text) {
			switch sweepBand(index - center) {
			case 2:
				perRune.WriteString(sweepCoreStyle.Render(string(char)))
			case 1:
				perRune.WriteString(sweepSoftStyle.Render(string(char)))
			default:
				perRune.WriteString(sweepBaseStyle.Render(string(char)))
			}
		}
		if lipgloss.Width(swept) != lipgloss.Width(perRune.String()) {
			t.Fatalf("frame %d changed the rendered width", frame)
		}
	}
}

// cardWorking is the default state, cleared only by a settled subtree. A job
// that has said nothing for ten minutes keeps its line on screen and stops
// asking for a repaint every 120ms.
func TestWedgedWorkStopsBreathing(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := New(&fakeBackend{}, "wedged")
	model.standingNow = func() time.Time { return now }
	model.cards = []jobCard{{
		ID: "job", RootID: "job", State: cardWorking, Title: "Long job", Latest: "still working",
	}}
	model.noteShimmerActivity()
	if !model.shimmerAnimating() {
		t.Fatal("fresh work did not breathe")
	}

	now = now.Add(shimmerStaleAfter + time.Minute)
	if model.shimmerAnimating() {
		t.Fatal("a status line that has not moved in ten minutes still breathes")
	}
	if !strings.Contains(ansi.Strip(model.renderShimmerLines(80)), "Long job") {
		t.Fatal("a stale job lost its line instead of only its sweep")
	}

	// News restarts it.
	model.cards[0].Latest = "found the failing test"
	model.noteShimmerActivity()
	if !model.shimmerAnimating() {
		t.Fatal("a moved status line did not resume breathing")
	}
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
