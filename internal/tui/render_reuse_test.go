package tui

import (
	"os"
	"path/filepath"
	"reflect"
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

func countingResolverModel(t *testing.T) (*Model, *countingResolver, *countingBackend) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	commander := &countingResolver{
		fakeCommander: &fakeCommander{current: map[string]string{}}, workspace: dir,
	}
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	backend := newCountingBackend(&fakeBackend{messages: []store.Message{{
		Seq: 1, SessionID: "reuse", Role: store.RoleAgent, NodeID: "job",
		Body: "wrote notes.md for you", Time: now.Add(-2 * time.Minute),
	}}})
	model := NewWithCommander(backend, "reuse", commander)
	model.standingNow = func() time.Time { return now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	return model, commander, backend
}

// The cache may not change one byte of the thread, nor one interactive row.
// Every rendered block is compared against the same block built cold, across
// the states that move under it: folds, receipts, a live stream, a new turn.
func TestCachedThreadIsByteIdenticalToAColdRender(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	long := strings.Repeat("a line of the answer\n", 30)
	messages := []store.Message{
		{Seq: 1, SessionID: "identical", Role: store.RoleUser, Body: "first ask", Time: now.Add(-time.Hour)},
		{Seq: 2, SessionID: "identical", Role: store.RoleAgent, Body: "the short reply", Time: now.Add(-time.Hour)},
		{Seq: 3, SessionID: "identical", Role: store.RoleSystem, Body: "read 3 files · $0.01", Time: now.Add(-30 * time.Minute)},
		{Seq: 4, SessionID: "identical", Role: store.RoleSystem, Body: "· reflected — kept two facts\nthe first fact\nthe second fact", Time: now.Add(-20 * time.Minute)},
		{Seq: 5, SessionID: "identical", Role: store.RoleAgent, NodeID: "job", Body: long, Time: now.Add(-10 * time.Minute)},
		{Seq: 6, SessionID: "identical", Role: store.RoleUser, Body: "and then?", Time: now.Add(-time.Minute)},
		{Seq: 7, SessionID: "identical", Role: store.RoleAgent, Body: "the last word", Time: now},
	}
	backend := &fakeBackend{
		messages: messages,
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: store.RootID}, {ID: "job", Parent: store.RootID, Title: "the job"}}},
	}
	model := New(backend, "identical")
	model.standingNow = func() time.Time { return now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))

	compare := func(stage string) {
		t.Helper()
		// Cold: the cache is retired, so every block is built from scratch.
		model.threadGen++
		cold := model.renderMessages()
		coldChips := append([]chatChipRow(nil), model.chatChipRows...)
		coldExpands := append([]chatExpandRow(nil), model.chatExpandRows...)
		coldRows := append([]chatMessageRow(nil), model.chatMessageRows...)
		warm := model.renderMessages()
		if warm != cold {
			t.Fatalf("%s: cached thread differs from a cold render:\n--- cold\n%q\n--- warm\n%q", stage, cold, warm)
		}
		if !reflect.DeepEqual(model.chatChipRows, coldChips) ||
			!reflect.DeepEqual(model.chatExpandRows, coldExpands) ||
			!reflect.DeepEqual(model.chatMessageRows, coldRows) {
			t.Fatalf("%s: cached thread moved its interactive rows", stage)
		}
	}

	compare("settled")
	model.expandedMessages[5] = true
	compare("answer opened")
	model.learningExpanded[4] = true
	compare("learning opened")
	model.receiptsExpanded = true
	compare("receipts opened")
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"still arriving`})
	model.advanceStream()
	compare("streaming tail")
	model.setSize(70, 30)
	compare("narrower pane")
}

// A settled message is immutable, so the thread it sits in is rendered once
// and replayed until the store says otherwise.
func TestSettledThreadIsRenderedOncePerChange(t *testing.T) {
	model, commander, backend := countingResolverModel(t)

	before := commander.resolves
	for range 5 {
		model.refreshChat()
		_ = model.View()
	}
	if commander.resolves != before {
		t.Fatalf("five frames re-rendered the settled thread %d times", commander.resolves-before)
	}

	// A quiet poll proves nothing moved, so the rendered blocks stand.
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if commander.resolves != before {
		t.Fatal("a quiet poll retired the rendered thread")
	}

	// Opening the answer in place is a change to that block, and only to it.
	model.expandedMessages[1] = true
	model.refreshChat()
	if commander.resolves == before {
		t.Fatal("opening a folded answer reused its collapsed block")
	}

	// So is a moved journal: it can rename the node the answer points at or
	// land the file it links.
	before = commander.resolves
	backend.bump()
	model.applyPoll(model.poll()().(pollResultMsg))
	if commander.resolves == before {
		t.Fatal("a moved journal did not retire the rendered thread")
	}
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
	model, _, _ := countingResolverModel(t)

	// A width only the relayout writes: if it survives the keystrokes, the
	// relayout did not run.
	model.self.Width = 999
	for _, char := range []rune{'h', 'e', 'l', 'l', 'o'} {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}
	if model.self.Width != 999 {
		t.Fatal("typing relayouted a frame whose shape had not changed")
	}
	if model.input.Value() != "hello" {
		t.Fatalf("draft = %q, want hello", model.input.Value())
	}

	// Opening the palette changes the frame's shape, so that keystroke must
	// relayout — the guard is about unchanged heights, not about typing.
	model.input.SetValue("")
	model.self.Width = 999
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !model.paletteOpen() {
		t.Fatal("a leading slash did not open the command palette")
	}
	if model.self.Width == 999 {
		t.Fatal("opening the palette skipped the relayout it needs")
	}
}
