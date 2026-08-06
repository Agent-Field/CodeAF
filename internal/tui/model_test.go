package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeBackend struct {
	mu       sync.Mutex
	messages []store.Message
	snapshot store.Snapshot
	posted   []store.Message
	postErr  error
}

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
