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
	pending  []store.Command
	usage    store.TotalUsage
	posted   []store.Message
	postErr  error
}

type fakeCommander struct {
	models     []string
	catalog    []ModelChoice
	current    map[string]string
	setRole    string
	setModel   string
	newSession string
	cancelled  []string
	database   string
	err        error
}

func (f *fakeCommander) Models() []string { return append([]string(nil), f.models...) }

func (f *fakeCommander) Catalog() []ModelChoice { return append([]ModelChoice(nil), f.catalog...) }

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

func (f *fakeBackend) PendingCommands(limit int) ([]store.Command, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	commands := append([]store.Command(nil), f.pending...)
	if limit > 0 && len(commands) > limit {
		commands = commands[:limit]
	}
	return commands, nil
}

func (f *fakeBackend) Usage() (store.TotalUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage, nil
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

func TestMessagesRenderAsYouAndAforge(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.messages = []store.Message{
		{Time: time.Now(), Role: store.RoleUser, Body: "Please investigate this."},
		{Time: time.Now(), Role: store.RoleAgent, Body: "I am on it."},
		{Time: time.Now(), Role: store.RoleSystem, NodeID: "answer-node", Body: "Here is the completed answer."},
	}

	rendered := model.renderMessages()
	for _, expected := range []string{"you", "aforge", "Here is the completed answer."} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("messages do not contain %q:\n%s", expected, rendered)
		}
	}
	for _, unwanted := range []string{"USER", "AGENT", "SYSTEM", "◇"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("messages contain retired role treatment %q:\n%s", unwanted, rendered)
		}
	}
	if _, label := messagePresentation(model.messages[0]); label != "you" {
		t.Fatalf("user label = %q, want you", label)
	}
	for _, message := range model.messages[1:] {
		if _, label := messagePresentation(message); label != "aforge" {
			t.Fatalf("aforge label = %q", label)
		}
	}
}

func TestReceiptsCollapseAndExpandTogether(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.messages = []store.Message{
		{Time: time.Now(), Role: store.RoleAgent, Body: "I have enough context to continue."},
		{
			Time:       time.Now(),
			Role:       store.RoleSystem,
			CommandSeq: 42,
			Body:       "Read the compiled request.\nAssumed: the local branch is authoritative.\nAssumed: no schema changes.",
		},
		{
			Time: time.Now(),
			Role: store.RoleSystem,
			Body: "Background sync complete.\nThis is secondary plumbing.",
		},
	}
	model.refreshChat()

	collapsed := model.renderMessages()
	if !strings.Contains(collapsed, "· reading + 2 assumptions — v to expand") {
		t.Fatalf("collapsed receipt summary is missing:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "Read the compiled request.") {
		t.Fatalf("collapsed receipt exposed its body:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "· Background sync complete. — v to expand") ||
		strings.Contains(collapsed, "This is secondary plumbing.") {
		t.Fatalf("rare system message did not collapse to its first line:\n%s", collapsed)
	}
	if count := strings.Count(collapsed, "aforge"); count != 1 {
		t.Fatalf("receipt created another speaker block: found %d aforge labels\n%s", count, collapsed)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if !model.receiptsExpanded {
		t.Fatal("v from chat focus did not expand receipts")
	}
	expanded := model.renderMessages()
	for _, expected := range []string{
		"Read the compiled request.", "Assumed: no schema changes.", "This is secondary plumbing.", "v to collapse",
	} {
		if !strings.Contains(expanded, expected) {
			t.Fatalf("expanded receipt does not contain %q:\n%s", expected, expanded)
		}
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

func TestHintsDescribeReceiptsGraphViewAndTwoVoices(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "test-session", newFakeCommander())
	if view := model.View(); !strings.Contains(view, "v receipts") {
		t.Fatalf("bottom hint does not mention receipt toggling:\n%s", view)
	}

	_ = model.executeSlash("/help")
	view := model.View()
	for _, expected := range []string{"you ask · aforge answers", "v toggles receipts", "tab focus / graph view"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("help does not contain %q:\n%s", expected, view)
		}
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

func TestModelCatalogPickerFiltersAndAppliesCurrentRole(t *testing.T) {
	commander := newFakeCommander()
	commander.catalog = []ModelChoice{
		{Slug: "openai/gpt-text", Name: "GPT Text", Price: "$1/M in · $2/M out"},
		{Slug: "openai/gpt-vision", Name: "GPT Vision", Price: "$3/M in · $4/M out"},
		{Slug: "anthropic/claude", Name: "Claude Sonnet", Price: "$5/M in · $6/M out"},
	}
	model := NewWithCommander(&fakeBackend{}, "test-session", commander)
	command := model.executeSlash("/model work")
	if command == nil {
		t.Fatal("opening the model picker did not start the lazy catalog fetch")
	}
	if view := model.View(); !strings.Contains(view, "fetching full catalog…") {
		t.Fatalf("picker does not show the fallback loading state:\n%s", view)
	}

	_, _ = model.Update(command())
	typeIntoModel(model, "vision")
	view := model.View()
	for _, expected := range []string{"filter: vision", "openai/gpt-vision", "GPT Vision", "$3/M in · $4/M out"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("filtered picker does not contain %q:\n%s", expected, view)
		}
	}
	for _, excluded := range []string{"openai/gpt-text", "anthropic/claude", "fetching full catalog…"} {
		if strings.Contains(view, excluded) {
			t.Fatalf("filtered picker still contains %q:\n%s", excluded, view)
		}
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if commander.setRole != "work" || commander.setModel != "openai/gpt-vision" {
		t.Fatalf("SetModel = (%q, %q), want work openai/gpt-vision", commander.setRole, commander.setModel)
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

func TestPendingSpliceRendersPlanningPlaceholderAndTally(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.applyPoll(pollResultMsg{
		snapshot: store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}},
		pending: []store.Command{{
			Seq:         9,
			Time:        time.Now().Add(-4 * time.Second),
			Kind:        store.CommandSplice,
			Instruction: "Investigate the graph scheduling delay in detail",
		}},
	})

	tree := model.renderTree(80, 20)
	for _, expected := range []string{"planning…", "Investigate the graph schedul", "elapsed"} {
		if !strings.Contains(tree, expected) {
			t.Fatalf("planning placeholder does not contain %q:\n%s", expected, tree)
		}
	}
	if tally := model.renderTally(); !strings.Contains(tally, "1 planning") || !strings.Contains(tally, "0 done") {
		t.Fatalf("planning tally is wrong: %s", tally)
	}
}

func TestRunningSpinnerAdvancesOnlyOnAnimationTicks(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "running", Parent: store.RootID, Brief: "Active work", Status: store.Running, StartedAt: time.Now()},
	}}
	model.refreshGraph()
	initial := model.spinnerFrame

	_, _ = model.Update(pollTickMsg(time.Now()))
	if model.spinnerFrame != initial {
		t.Fatalf("data-poll tick advanced spinner from %d to %d", initial, model.spinnerFrame)
	}
	_, _ = model.Update(animationTickMsg(time.Now()))
	first := model.spinnerFrame
	firstGlyph, _ := model.nodeGlyph(model.snapshot.Nodes[1])
	_, _ = model.Update(animationTickMsg(time.Now().Add(animationInterval)))
	second := model.spinnerFrame
	secondGlyph, _ := model.nodeGlyph(model.snapshot.Nodes[1])
	if first == initial || second == first || firstGlyph == secondGlyph {
		t.Fatalf("animation ticks did not advance spinner: frames %d, %d, %d; glyphs %q, %q",
			initial, first, second, firstGlyph, secondGlyph)
	}
}

func TestUsageSpendFormatsAndHidesAtZero(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "done-1", Status: store.Done},
		{ID: "done-2", Status: store.Done},
	}}
	model.usage = store.TotalUsage{Nodes: 2, PromptTokens: 1_000, CompletionTokens: 234, Cost: 0.876}
	if tally := model.renderTally(); !strings.Contains(tally, "2 done  ·  1.2k tok · $0.88") {
		t.Fatalf("usage tally is wrong: %s", tally)
	}

	model.usage = store.TotalUsage{}
	tally := model.renderTally()
	if strings.Contains(tally, "tok") || strings.Contains(tally, "$") {
		t.Fatalf("zero usage should hide spend entirely: %s", tally)
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

func TestNarrowTerminalsCollapseGraphToStripAndCycleFullPane(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "running", Parent: store.RootID, Brief: "Active work", Status: store.Running},
		{ID: "waiting", Parent: store.RootID, Brief: "Queued work", Status: store.Pending},
	}}
	_, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 30})
	if model.horizontal {
		t.Fatal("72-column terminal should collapse the graph rail")
	}
	if model.chatWidth != 72 || model.graphWidth != 72 {
		t.Fatalf("full-pane widths are chat=%d graph=%d, want 72", model.chatWidth, model.graphWidth)
	}
	strip := model.renderGraphStrip()
	if !strings.Contains(strip, "● 1 running · ○ 1 waiting — tab to view") {
		t.Fatalf("narrow graph strip is wrong: %s", strip)
	}
	if view := model.View(); !strings.Contains(view, "CHAT") || strings.Contains(view, "GRAPH") {
		t.Fatalf("narrow default view should show only chat:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if view := model.View(); !strings.Contains(view, "GRAPH") || strings.Contains(view, "CHAT") {
		t.Fatalf("graph focus should show the full-pane graph:\n%s", view)
	}
}

func TestWideLayoutKeepsChatAtEightyPercentAndWrapsMessages(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.setSize(120, 30)
	if !model.horizontal {
		t.Fatal("120-column terminal should show the graph rail")
	}
	available := model.width - 2
	percentage := model.chatWidth * 100 / available
	if percentage < 78 || percentage > 80 {
		t.Fatalf("chat width = %d%% of available width, want 78-80%%", percentage)
	}
	model.messages = []store.Message{{
		Time: time.Now(), Role: store.RoleAgent,
		Body: strings.Repeat("a long answer should wrap cleanly inside the primary chat pane ", 8),
	}}
	for _, line := range strings.Split(model.renderMessages(), "\n") {
		if width := lipgloss.Width(line); width > model.chat.Width {
			t.Fatalf("wrapped message line is %d columns, viewport is %d:\n%s", width, model.chat.Width, line)
		}
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
		catalog: []ModelChoice{
			{Slug: "alpha/model-one"},
			{Slug: "beta/model-two"},
			{Slug: "gamma/model-three"},
		},
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
