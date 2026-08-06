package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeBackend struct {
	mu       sync.Mutex
	messages []store.Message
	snapshot store.Snapshot
	posted   []store.Message
	postErr  error
}

type fakeCommander struct {
	models     []string
	current    map[string]string
	setRole    string
	setModel   string
	newSession string
	cancelled  []string
	database   string
	err        error
}

func (f *fakeCommander) Models() []string { return append([]string(nil), f.models...) }

func (f *fakeCommander) CurrentModel(role string) string { return f.current[role] }

func (f *fakeCommander) SetModel(role, slug string) error {
	if f.err != nil {
		return f.err
	}
	f.setRole, f.setModel = role, slug
	f.current[role] = slug
	return nil
}

func (f *fakeCommander) NewSession() (string, error) {
	return f.newSession, f.err
}

func (f *fakeCommander) Cancel(nodeID string) error {
	if f.err != nil {
		return f.err
	}
	f.cancelled = append(f.cancelled, nodeID)
	return nil
}

func (f *fakeCommander) DatabasePath() string { return f.database }

func (f *fakeBackend) Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []store.Message
	for _, message := range f.messages {
		if message.SessionID == sessionID && message.Seq > afterSeq {
			result = append(result, message)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakeBackend) PostMessage(message store.Message) (store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postErr != nil {
		return store.Message{}, f.postErr
	}
	message.Seq = int64(len(f.messages) + 1)
	message.Time = time.Now()
	f.posted = append(f.posted, message)
	f.messages = append(f.messages, message)
	return message, nil
}

func (f *fakeBackend) ActiveSnapshot() (store.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, nil
}

func TestMessageArrivalKeepsStickyBottomAndPreservesPinnedScroll(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "test-session")
	model.setSize(100, 18)

	messages := make([]store.Message, 14)
	for index := range messages {
		messages[index] = store.Message{
			Seq:       int64(index + 1),
			Time:      time.Now().Add(-time.Duration(index) * time.Minute),
			SessionID: "test-session",
			Role:      store.RoleAgent,
			Body:      "A calm but sufficiently long message to fill the viewport.",
		}
	}
	model.applyPoll(pollResultMsg{messages: messages})
	if !model.chat.AtBottom() {
		t.Fatal("new messages should pin an auto-scrolling viewport to the bottom")
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if model.autoScroll {
		t.Fatal("page up should unpin the viewport")
	}
	offset := model.chat.YOffset

	model.applyPoll(pollResultMsg{messages: []store.Message{{
		Seq:       15,
		Time:      time.Now(),
		SessionID: "test-session",
		Role:      store.RoleSystem,
		Body:      "A fold landed.",
	}}})
	if model.chat.YOffset != offset {
		t.Fatalf("arrival moved a pinned viewport: got offset %d, want %d", model.chat.YOffset, offset)
	}
	if model.newMessages != 1 {
		t.Fatalf("new message count = %d, want 1", model.newMessages)
	}
	if view := model.View(); !strings.Contains(view, "↓ new") {
		t.Fatalf("pinned view does not contain the new-message hint:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !model.autoScroll || model.newMessages != 0 || !model.chat.AtBottom() {
		t.Fatal("end should re-pin chat and clear the new-message count")
	}

	_, _ = model.Update(tea.MouseMsg{X: 2, Y: 3, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if model.autoScroll {
		t.Fatal("mouse wheel up should release chat auto-follow")
	}
	model.applyPoll(pollResultMsg{messages: []store.Message{{
		Seq: 16, Time: time.Now(), SessionID: "test-session", Role: store.RoleAgent, Body: "One more update.",
	}}})
	_, _ = model.Update(tea.MouseMsg{
		X: model.chatWidth - 2, Y: 2 + model.chatHeight - 2,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !model.autoScroll || model.newMessages != 0 || !model.chat.AtBottom() {
		t.Fatal("clicking the new-message pill should re-pin chat")
	}
}

func TestSlashPaletteOpensFiltersAndCycles(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "test-session", newFakeCommander())
	typeIntoModel(model, "/")
	if model.palette != paletteCommands {
		t.Fatalf("palette = %v, want command palette", model.palette)
	}
	view := model.View()
	for _, command := range []string{"「/model」", "「/session」", "「/quit」"} {
		if !strings.Contains(view, command) {
			t.Fatalf("command palette does not contain %q:\n%s", command, view)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if model.paletteSelected != 1 {
		t.Fatalf("tab selected row %d, want 1", model.paletteSelected)
	}
	typeIntoModel(model, "mo")
	view = model.View()
	if !strings.Contains(view, "「/model」") || strings.Contains(view, "「/session」") {
		t.Fatalf("fuzzy command filtering is wrong:\n%s", view)
	}
}

func TestModelCompletionAppliesAndShowsTransient(t *testing.T) {
	commander := newFakeCommander()
	model := NewWithCommander(&fakeBackend{}, "test-session", commander)
	typeIntoModel(model, "/model beta")
	if model.palette != paletteModelCompletion {
		t.Fatalf("palette = %v, want model completion", model.palette)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if value := model.input.Value(); value != "/model beta/model-two" {
		t.Fatalf("tab completion = %q", value)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if commander.setRole != "talk" || commander.setModel != "beta/model-two" {
		t.Fatalf("SetModel = (%q, %q)", commander.setRole, commander.setModel)
	}
	if !strings.Contains(model.View(), "talk model → beta/model-two") {
		t.Fatalf("transient model status missing:\n%s", model.View())
	}
}

func TestModelCompletionTargetsWorkRole(t *testing.T) {
	commander := newFakeCommander()
	model := NewWithCommander(&fakeBackend{}, "test-session", commander)
	typeIntoModel(model, "/model work beta")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if commander.setRole != "work" || commander.setModel != "beta/model-two" {
		t.Fatalf("SetModel = (%q, %q), want work beta/model-two", commander.setRole, commander.setModel)
	}
}

func TestCancelCompletionOnlyIncludesNonTerminalNodes(t *testing.T) {
	backend := &fakeBackend{}
	commander := newFakeCommander()
	model := NewWithCommander(backend, "test-session", commander)
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID, Status: store.Running},
		{ID: "active-node", Status: store.Running},
		{ID: "waiting-node", Status: store.Pending},
		{ID: "done-node", Status: store.Done},
		{ID: "failed-node", Status: store.Failed},
	}}
	typeIntoModel(model, "/cancel ")
	view := model.View()
	for _, nodeID := range []string{"active-node", "waiting-node"} {
		if !strings.Contains(view, nodeID) {
			t.Fatalf("cancel palette does not contain %q:\n%s", nodeID, view)
		}
	}
	for _, nodeID := range []string{"done-node", "failed-node"} {
		if strings.Contains(view, nodeID) {
			t.Fatalf("cancel palette contains terminal node %q:\n%s", nodeID, view)
		}
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(commander.cancelled) != 1 || commander.cancelled[0] != "active-node" {
		t.Fatalf("cancelled nodes = %v", commander.cancelled)
	}
	if command == nil {
		t.Fatal("cancel did not return the user-message post command")
	}
	_ = command()
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 || backend.posted[0].Body != "cancel active-node" {
		t.Fatalf("posted messages = %#v", backend.posted)
	}
}

func TestNewCommandSwitchesAndResetsSession(t *testing.T) {
	commander := newFakeCommander()
	commander.newSession = "fresh-session"
	model := NewWithCommander(&fakeBackend{}, "old-session", commander)
	model.messages = []store.Message{{Seq: 9, SessionID: "old-session", Body: "old"}}
	model.lastSeq = 9
	typeIntoModel(model, "/new")

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.sessionID != "fresh-session" || model.lastSeq != 0 || len(model.messages) != 0 {
		t.Fatalf("new session state = id %q, seq %d, messages %d", model.sessionID, model.lastSeq, len(model.messages))
	}
	if command == nil {
		t.Fatal("new session should poll its fresh tail immediately")
	}
	if !strings.Contains(model.View(), "new session → fresh-session") {
		t.Fatalf("new-session status missing:\n%s", model.View())
	}
}

func TestNodeGlyphsAndTreeRendering(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	tests := []struct {
		name   string
		node   store.Node
		glyphs []string
	}{
		{name: "done", node: store.Node{Status: store.Done}, glyphs: []string{"●"}},
		{name: "running", node: store.Node{Status: store.Running}, glyphs: []string{"●", spinnerFrames[0]}},
		{name: "pending", node: store.Node{Status: store.Pending}, glyphs: []string{"○"}},
		{name: "failed", node: store.Node{Status: store.Failed}, glyphs: []string{"●"}},
		{name: "cancelled", node: store.Node{Status: store.Cancelled}, glyphs: []string{"●"}},
		{name: "fold root", node: store.Node{Status: store.Done, FoldRoot: true}, glyphs: []string{"◆"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			glyph, _ := model.nodeGlyph(test.node)
			for _, expected := range test.glyphs {
				if !strings.Contains(glyph, expected) {
					t.Fatalf("glyph %q does not contain %q", glyph, expected)
				}
			}
		})
	}

	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "plan", Parent: store.RootID, Brief: "Plan the work", Status: store.Running, StartedAt: time.Now().Add(-3 * time.Second)},
		{ID: "research", Parent: "plan", Brief: "Research constraints", Status: store.Done},
		{ID: "write", Parent: store.RootID, Brief: "Write the answer\nwith extra detail", Status: store.Pending},
	}}
	tree := model.renderTree(50, 20)
	for _, expected := range []string{"├─", "╰─", "Plan the work", "Research constraints", "Write the answer", "elapsed"} {
		if !strings.Contains(tree, expected) {
			t.Fatalf("tree does not contain %q:\n%s", expected, tree)
		}
	}
}

func TestEnterPostsUserMessageAndClearsInput(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "session-42")
	model.input.SetValue("  please plan this  ")

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("enter returned no post command")
	}
	if value := model.input.Value(); value != "" {
		t.Fatalf("input was not cleared: %q", value)
	}
	result := command()
	if _, ok := result.(postResultMsg); !ok {
		t.Fatalf("post command returned %T, want postResultMsg", result)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(backend.posted))
	}
	posted := backend.posted[0]
	if posted.SessionID != "session-42" || posted.Role != store.RoleUser || posted.Body != "please plan this" {
		t.Fatalf("unexpected posted message: %#v", posted)
	}
}

func TestNarrowTerminalsStackPanes(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	_, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 30})
	if model.horizontal {
		t.Fatal("72-column terminal should stack panes")
	}
	if model.chatWidth != 72 || model.graphWidth != 72 {
		t.Fatalf("stacked widths are chat=%d graph=%d, want 72", model.chatWidth, model.graphWidth)
	}
}

func TestResizeReflowsToTerminalHeight(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "test-session", newFakeCommander())
	for _, size := range []tea.WindowSizeMsg{
		{Width: 100, Height: 30}, {Width: 72, Height: 30}, {Width: 100, Height: 20}, {Width: 72, Height: 20},
		{Width: 72, Height: 15},
	} {
		_, _ = model.Update(size)
		if height := lipgloss.Height(model.View()); height != size.Height {
			t.Fatalf("view at %dx%d is %d rows (chat %d/%d, graph %d/%d, input %d)",
				size.Width, size.Height, height,
				lipgloss.Height(model.renderChatPane()), model.chatHeight,
				lipgloss.Height(model.renderGraphPane()), model.graphHeight,
				lipgloss.Height(model.renderInput()),
			)
		}
	}
	typeIntoModel(model, "/")
	if height := lipgloss.Height(model.View()); height != model.height {
		t.Fatalf("view with command palette is %d rows, want %d", height, model.height)
	}
}

func TestInputSoftWrapsToThreeLines(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.setSize(36, 24)
	model.input.SetValue(strings.Repeat("long input ", 20))
	model.setSize(36, 24)
	if lines := model.input.LineCount(); lines != 3 {
		t.Fatalf("input line count = %d, want 3", lines)
	}
	if value := model.input.Value(); len(value) != len(strings.Repeat("long input ", 20)) {
		t.Fatal("soft wrapping discarded input text")
	}
}

func TestGraphViewportScrollsOnlyWhenFocused(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.setSize(100, 15)
	model.snapshot.Nodes = append(model.snapshot.Nodes, store.Node{ID: store.RootID})
	for index := range 12 {
		model.snapshot.Nodes = append(model.snapshot.Nodes, store.Node{
			ID: fmt.Sprintf("node-%02d", index), Parent: store.RootID, Brief: "graph row", Status: store.Pending,
		})
	}
	model.refreshGraph()
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	chatOffset := model.chat.YOffset
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if model.graph.YOffset == 0 {
		t.Fatal("focused graph did not page down")
	}
	if model.chat.YOffset != chatOffset {
		t.Fatal("graph paging changed the chat viewport")
	}
}

func TestEscapeClearsBeforeQuitting(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	typeIntoModel(model, "draft")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil || model.input.Value() != "" {
		t.Fatal("first escape should clear a non-empty input without quitting")
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command == nil {
		t.Fatal("escape on an empty input should quit")
	}
}

func newFakeCommander() *fakeCommander {
	return &fakeCommander{
		models: []string{"alpha/model-one", "beta/model-two", "gamma/model-three"},
		current: map[string]string{
			"talk": "alpha/model-one",
			"work": "gamma/model-three",
		},
		database: "/tmp/aforge-test.db",
	}
}

func typeIntoModel(model *Model, text string) {
	for _, char := range text {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}
}
