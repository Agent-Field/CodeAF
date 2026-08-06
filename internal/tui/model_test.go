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
	commands []store.Command
	usage    store.TotalUsage
	jobUsage map[string]store.JobUsage
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
	facts      []store.Fact
	database   string
	trace      string
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

func (f *fakeCommander) Notebook(limit int) []store.Fact {
	facts := append([]store.Fact(nil), f.facts...)
	if limit > 0 && len(facts) > limit {
		facts = facts[:limit]
	}
	return facts
}

func (f *fakeCommander) DatabasePath() string { return f.database }

func (f *fakeCommander) NodeTrace(nodeID string, maxBytes int) string {
	if maxBytes > 0 && len(f.trace) > maxBytes {
		return f.trace[len(f.trace)-maxBytes:]
	}
	return f.trace
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

func (f *fakeBackend) Snapshot() (store.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, nil
}

func (f *fakeBackend) Node(id string) (store.Node, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, node := range f.snapshot.Nodes {
		if node.ID == id {
			return node, true, nil
		}
	}
	return store.Node{}, false, nil
}

func (f *fakeBackend) NodeMessages(nodeID string, afterSeq int64, limit int) ([]store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []store.Message
	for _, message := range f.messages {
		if message.NodeID == nodeID && message.Seq > afterSeq {
			result = append(result, message)
			if limit > 0 && len(result) == limit {
				break
			}
		}
	}
	return result, nil
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

func (f *fakeBackend) CommandBySeq(seq int64) (store.Command, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, command := range append(append([]store.Command(nil), f.commands...), f.pending...) {
		if command.Seq == seq {
			return command, true, nil
		}
	}
	return store.Command{}, false, nil
}

func (f *fakeBackend) Usage() (store.TotalUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usage, nil
}

func (f *fakeBackend) TopLevelJobUsage() (map[string]store.JobUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make(map[string]store.JobUsage, len(f.jobUsage))
	for jobID, usage := range f.jobUsage {
		result[jobID] = usage
	}
	return result, nil
}

func TestCardsDeriveFromSeededStore(t *testing.T) {
	graph, err := store.Open(t.TempDir() + "/cards.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()

	provenance := func(intent string) store.Provenance {
		return store.Provenance{Origin: store.OriginUser, SessionID: "cards", Intent: intent}
	}
	liveCommand, err := graph.RequestCommand(store.Command{
		SessionID: "cards", Kind: store.CommandSplice, Instruction: "compare the market",
	})
	if err != nil {
		t.Fatalf("request live command: %v", err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "live", Brief: "Compile the market report", Title: "Market report", Stage: 2, Needs: []store.Need{
			{NodeID: "live-a", Kind: store.FeedsInto}, {NodeID: "live-b", Kind: store.FeedsInto},
		}},
		{ID: "live-a", Parent: "live", Brief: "Collect prices", Title: "Collect prices", Stage: 1},
		{ID: "live-b", Parent: "live", Brief: "Compare vendors", Title: "Compare vendors", Stage: 1},
	}}, provenance("compare the market")); err != nil {
		t.Fatalf("splice live job: %v", err)
	}
	if err := graph.ResolveCommand(liveCommand.Seq, store.CommandApplied, "spliced 3 nodes"); err != nil {
		t.Fatalf("resolve live command: %v", err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleSystem, CommandSeq: liveCommand.Seq,
		Body: "Read the request as a market comparison.\nAssumed: prices are in Canadian dollars.",
	}); err != nil {
		t.Fatalf("post assumptions receipt: %v", err)
	}
	claimA, won, err := graph.Claim("live-a", "test")
	if err != nil || !won {
		t.Fatalf("claim live-a: won=%v err=%v", won, err)
	}
	if err := graph.Start(claimA); err != nil {
		t.Fatalf("start live-a: %v", err)
	}
	if err := graph.RecordUsage(store.NodeUsage{
		NodeID: "live-a", PromptTokens: 800, CompletionTokens: 200, Cost: 0.12,
	}); err != nil {
		t.Fatalf("record live usage: %v", err)
	}
	if err := graph.Complete(claimA, "Three prices collected"); err != nil {
		t.Fatalf("complete live-a: %v", err)
	}
	claimB, won, err := graph.Claim("live-b", "test")
	if err != nil || !won {
		t.Fatalf("claim live-b: won=%v err=%v", won, err)
	}
	if err := graph.Start(claimB); err != nil {
		t.Fatalf("start live-b: %v", err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleAgent, NodeID: "live",
		Body: "I have the price sample and I am comparing the vendors now.",
	}); err != nil {
		t.Fatalf("post narration: %v", err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleSystem, NodeID: "live-a",
		Body: "The price collection part landed.",
	}); err != nil {
		t.Fatalf("post part result: %v", err)
	}

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "settled", Brief: "Write the release note", Title: "Release note", Stage: 1,
	}}}, provenance("write the release note")); err != nil {
		t.Fatalf("splice settled job: %v", err)
	}
	settledClaim, won, err := graph.Claim("settled", "test")
	if err != nil || !won {
		t.Fatalf("claim settled: won=%v err=%v", won, err)
	}
	if err := graph.Start(settledClaim); err != nil {
		t.Fatalf("start settled: %v", err)
	}
	if err := graph.RecordUsage(store.NodeUsage{
		NodeID: "settled", PromptTokens: 400, CompletionTokens: 100, Cost: 0.08,
	}); err != nil {
		t.Fatalf("record settled usage: %v", err)
	}
	if err := graph.Complete(settledClaim, "Version 2 is ready to ship."); err != nil {
		t.Fatalf("complete settled: %v", err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleSystem, NodeID: "settled",
		Body: "Version 2 is ready to ship.\n\n- Faster startup\n- Clearer errors",
	}); err != nil {
		t.Fatalf("post deliverable: %v", err)
	}

	questionCommand, err := graph.RequestCommand(store.Command{
		SessionID: "cards", Kind: store.CommandSplice, Instruction: "deploy the service",
	})
	if err != nil {
		t.Fatalf("request question job: %v", err)
	}
	if err := graph.ResolveCommand(questionCommand.Seq, store.CommandRejected, "asked for a region"); err != nil {
		t.Fatalf("resolve question job: %v", err)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleAgent, CommandSeq: questionCommand.Seq,
		Body: "Which region should I deploy to?",
	}); err != nil {
		t.Fatalf("post question: %v", err)
	}
	pending, err := graph.RequestCommand(store.Command{
		SessionID: "cards", Kind: store.CommandSplice, Instruction: "audit the support queue",
	})
	if err != nil {
		t.Fatalf("request compiling job: %v", err)
	}

	model := New(graph, "cards")
	model.focus = focusChat
	model.inputFocused = false
	result, ok := model.poll()().(pollResultMsg)
	if !ok {
		t.Fatal("poll did not return a pollResultMsg")
	}
	model.applyPoll(result)
	if model.focus != focusInput || !model.inputFocused {
		t.Fatalf("question did not focus its answer box: focus=%v input=%v", model.focus, model.inputFocused)
	}
	if len(model.cards) != 4 {
		t.Fatalf("derived %d cards, want 4: %#v", len(model.cards), model.cards)
	}

	live := requireCard(t, model.cards, "live")
	if live.State != cardWorking || live.Done != 1 || live.Total != 3 {
		t.Fatalf("live card state/counts = %s %d/%d, want working 1/3", live.State, live.Done, live.Total)
	}
	if live.Usage.Cost != 0.12 || len(live.Messages) != 2 || len(live.Narration) != 1 ||
		!strings.Contains(live.Receipt, "Assumed: prices") ||
		!strings.Contains(live.Latest, "comparing the vendors") {
		t.Fatalf("live card did not group subtree messages and usage: %#v", live)
	}
	model.cardExpanded["live"] = true
	if expanded := model.renderCardDock(false); !strings.Contains(expanded, "Assumed: prices") ||
		!strings.Contains(expanded, "running summary") || !strings.Contains(expanded, "Collect prices") {
		t.Fatalf("expanded card is missing its receipt, narration, or parts:\n%s", expanded)
	}
	settled := requireCard(t, model.cards, "settled")
	if settled.State != cardSettled || settled.Done != 1 || settled.Total != 1 ||
		settled.Deliverable == nil || settled.Usage.Cost != 0.08 {
		t.Fatalf("settled card is incomplete: %#v", settled)
	}
	question := requireCard(t, model.cards, fmt.Sprintf("command:%d", questionCommand.Seq))
	if question.State != cardQuestion || !strings.Contains(question.Question, "Which region") {
		t.Fatalf("question card = %#v", question)
	}
	compiling := requireCard(t, model.cards, fmt.Sprintf("command:%d", pending.Seq))
	if compiling.State != cardCompiling || compiling.Ask != "audit the support queue" {
		t.Fatalf("compiling card = %#v", compiling)
	}
	if _, err := graph.PostMessage(store.Message{
		SessionID: "cards", Role: store.RoleUser, Body: "Use the Toronto region.",
	}); err != nil {
		t.Fatalf("post question answer: %v", err)
	}
	result = model.poll()().(pollResultMsg)
	model.applyPoll(result)
	if card := model.cardByID(fmt.Sprintf("command:%d", questionCommand.Seq)); card != nil {
		t.Fatalf("answered question card stayed active: %#v", card)
	}
}

func TestCompilingCardKeepsDisclosureWhenItsRootAppears(t *testing.T) {
	command := store.Command{
		Seq: 7, SessionID: "cards", Kind: store.CommandSplice,
		Instruction: "compare the market", Status: store.CommandPending,
	}
	model := New(&fakeBackend{}, "cards")
	model.commands[command.Seq] = command
	model.pending = []store.Command{command}
	model.cardSnapshot = store.Snapshot{Nodes: []store.Node{{ID: store.RootID}}}
	model.rebuildCards()
	compilingID := fmt.Sprintf("command:%d", command.Seq)
	model.cardExpanded[compilingID] = true
	model.selectedCardID = compilingID

	command.Status = store.CommandApplied
	model.commands[command.Seq] = command
	model.pending = nil
	model.cardSnapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "job", Parent: store.RootID, Brief: "Compare the market", CreatedSeq: 9,
			Status: store.Pending, Provenance: store.Provenance{
				SessionID: "cards", Intent: "compare the market",
			},
		},
	}}
	model.rebuildCards()

	card := requireCard(t, model.cards, "job")
	if card.CommandSeq != command.Seq || !model.cardExpanded["job"] || model.selectedCardID != "job" {
		t.Fatalf("compiling-to-working transition lost disclosure state: card=%#v expanded=%v selected=%q",
			card, model.cardExpanded, model.selectedCardID)
	}
}

func TestCardsDockWhileActiveAndSettleAtBirth(t *testing.T) {
	model := New(&fakeBackend{}, "cards")
	delivery := store.Message{Seq: 12, Role: store.RoleSystem, Body: "The landed deliverable."}
	model.messages = []store.Message{
		{Seq: 1, Role: store.RoleUser, Body: "First ask"},
		{Seq: 20, Role: store.RoleUser, Body: "Later conversation"},
	}
	model.cards = []jobCard{
		{ID: "active", RootID: "active", State: cardWorking, Title: "Docked work", BirthSeq: 2, Done: 1, Total: 3},
		{
			ID: "settled", RootID: "settled", State: cardSettled, Title: "Settled work",
			BirthSeq: 10, Done: 1, Total: 1, Outcome: "The landed deliverable.", Deliverable: &delivery,
		},
	}

	active, settled := placeJobCards(model.cards)
	if len(active) != 1 || active[0].ID != "active" || len(settled) != 1 || settled[0].ID != "settled" {
		t.Fatalf("card placement = active %#v settled %#v", active, settled)
	}
	thread := model.renderMessages()
	first := strings.Index(thread, "First ask")
	landed := strings.Index(thread, "Settled work")
	later := strings.Index(thread, "Later conversation")
	// Active work appears exactly once as the rail-closed shimmer, never as a settled card.
	if first < 0 || landed < first || later < landed || strings.Count(thread, "Docked work") != 1 {
		t.Fatalf("settled card did not land at birth while active card stayed docked:\n%s", thread)
	}
	dock := model.renderActivityBar()
	if !strings.Contains(dock, "Docked work") || strings.Contains(dock, "Settled work") {
		t.Fatalf("dock placement is wrong: %s", dock)
	}

	for index := 0; index < 3; index++ {
		model.cards = append(model.cards, jobCard{
			ID: fmt.Sprintf("extra-%d", index), State: cardWorking, Title: fmt.Sprintf("Extra %d", index),
		})
	}
	model.focus = focusInput
	if collapsed := model.renderActivityBar(); !strings.Contains(collapsed, "4 running") {
		t.Fatalf("four-card dock did not collapse: %s", collapsed)
	}
	model.focusCardDock()
	if expanded := model.renderActivityBar(); !strings.Contains(expanded, "Extra 2") {
		t.Fatalf("focused dock did not expand:\n%s", expanded)
	}
}

func TestCardsKeepProgressOutOfTheAttentionStream(t *testing.T) {
	model := New(&fakeBackend{}, "cards")
	model.cardSnapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "job", Parent: store.RootID, Status: store.Pending},
		{ID: "done-part", Parent: "job", Status: store.Done},
		{ID: "failed-part", Parent: "job", Status: store.Failed},
	}}
	model.cards = []jobCard{{
		ID: "job", RootID: "job", State: cardQuestion,
		Parts: []cardPart{
			{NodeID: "job"}, {NodeID: "done-part", Status: store.Done},
			{NodeID: "failed-part", Status: store.Failed},
		},
	}}
	model.messages = []store.Message{
		{Seq: 1, Role: store.RoleAgent, NodeID: "job", Body: "Quiet narration mutates the card."},
		{Seq: 2, Role: store.RoleSystem, NodeID: "done-part", Body: "Ambient part progress."},
		{Seq: 3, Role: store.RoleSystem, NodeID: "failed-part", Body: "A worker failed visibly."},
		{Seq: 4, Role: store.RoleAgent, NodeID: "job", Body: "Which region should I use?"},
	}

	rendered := model.renderMessages()
	for _, hidden := range []string{"Quiet narration", "Ambient part progress"} {
		if strings.Contains(rendered, hidden) {
			t.Fatalf("ambient progress leaked into the stream:\n%s", rendered)
		}
	}
	for _, interruption := range []string{"A worker failed visibly.", "Which region should I use?"} {
		if !strings.Contains(rendered, interruption) {
			t.Fatalf("stream lost interruption %q:\n%s", interruption, rendered)
		}
	}
}

func TestCardDisclosureLadderClimbsBackOneRungAtATime(t *testing.T) {
	activeSnapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "job", Parent: store.RootID, Brief: "The job", Status: store.Pending},
	}}
	cardSnapshot := store.Snapshot{Nodes: append(append([]store.Node(nil), activeSnapshot.Nodes...),
		store.Node{ID: "part", Parent: "job", Brief: "One part", Status: store.Running},
	)}
	model := New(&fakeBackend{snapshot: cardSnapshot}, "cards")
	model.snapshot = activeSnapshot
	model.cardSnapshot = cardSnapshot
	model.cards = []jobCard{{
		ID: "job", RootID: "job", State: cardWorking, Title: "The job", Total: 2,
		Parts: []cardPart{{NodeID: "job", Title: "The job"}, {NodeID: "part", Title: "One part", Status: store.Running}},
	}}
	model.focusCardDock()

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.cardExpanded["job"] || model.graphOpen {
		t.Fatalf("first enter did not expand card: expanded=%v graph=%v", model.cardExpanded["job"], model.graphOpen)
	}
	_ = model.View()
	var partRow cardPartRow
	for _, row := range model.cardPartRows {
		if row.dock && row.nodeID == "part" {
			partRow = row
			break
		}
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.activityBarBounds.x + 2, Y: model.activityBarBounds.y + partRow.line,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.nodeViewID != "part" {
		t.Fatalf("clicking an expanded part opened %q, want its flight recorder", model.nodeViewID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.nodeViewID != "" || model.focus != focusCards || !model.cardExpanded["job"] {
		t.Fatalf("escaping a clicked part did not return to its expanded card")
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.graphOpen || model.graphScopeID != "job" || model.focus != focusGraph {
		t.Fatalf("second enter did not open scoped graph: open=%v scope=%q focus=%v",
			model.graphOpen, model.graphScopeID, model.focus)
	}
	if tree := model.renderTree(60, 0); !strings.Contains(tree, "One part") {
		t.Fatalf("scoped graph did not use the job's full subtree:\n%s", tree)
	}
	_ = model.openNodeByID("part")
	if model.nodeViewID != "part" {
		t.Fatalf("part did not open flight recorder: %q", model.nodeViewID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.nodeViewID != "" || model.focus != focusGraph || model.graphScopeID != "job" {
		t.Fatalf("first escape did not return to scoped graph: node=%q focus=%v scope=%q",
			model.nodeViewID, model.focus, model.graphScopeID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.graphOpen || model.focus != focusCards || !model.cardExpanded["job"] {
		t.Fatalf("second escape did not return to expanded card: graph=%v focus=%v expanded=%v",
			model.graphOpen, model.focus, model.cardExpanded["job"])
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.cardExpanded["job"] {
		t.Fatal("third escape did not collapse the card")
	}
}

func requireCard(t *testing.T, cards []jobCard, id string) jobCard {
	t.Helper()
	for _, card := range cards {
		if card.ID == id {
			return card
		}
	}
	t.Fatalf("card %q not found in %#v", id, cards)
	return jobCard{}
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
		X: model.chatWidth - 4, Y: 2 + model.chatHeight - 1,
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
	if !strings.Contains(view, "/graph · /tasks · /node · /notebook · /help") || strings.Contains(view, "「/model」") {
		t.Fatalf("bare slash did not render the one-line completion hint:\n%s", view)
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
	for _, expected := range []string{
		"「/notebook」", "you ask · aforge answers", "v toggles receipts", "alt+g toggles the rail",
		"chips jump to the task", "enter to steer", "mouse",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("help does not contain %q:\n%s", expected, view)
		}
	}
}

func TestGraphSelectionMovesAcrossNodesAndSkipsPlanningRows(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.pending = []store.Command{{Kind: store.CommandSplice, Instruction: "planning placeholder"}}
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "first", Parent: store.RootID, Brief: "First node", Status: store.Pending},
		{ID: "second", Parent: store.RootID, Brief: "Second node", Status: store.Pending},
	}}
	model.refreshGraph()

	model.toggleGraph()
	if model.selectedNodeID != "first" {
		t.Fatalf("initial graph selection = %q, want first", model.selectedNodeID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.selectedNodeID != "second" {
		t.Fatalf("down selected %q, want second", model.selectedNodeID)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.selectedNodeID != "first" {
		t.Fatalf("up selected %q, want first", model.selectedNodeID)
	}
	if strings.Contains(model.selectedNodeID, "planning") {
		t.Fatalf("planning placeholder became selectable: %q", model.selectedNodeID)
	}
}

func TestEnterOpensNodeViewAndEscapeClosesIt(t *testing.T) {
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "worker", Parent: store.RootID, Brief: "Inspect this worker\nFull brief", Status: store.Running},
	}}}
	model := New(backend, "test-session")
	model.snapshot = backend.snapshot
	model.refreshGraph()
	model.toggleGraph()

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nodeViewID != "worker" || !strings.Contains(model.input.Placeholder, "steer this worker") {
		t.Fatalf("node view did not open: id=%q placeholder=%q", model.nodeViewID, model.input.Placeholder)
	}
	if view := model.View(); !strings.Contains(view, "Inspect this worker") || !strings.Contains(view, "BRIEF") {
		t.Fatalf("node view did not replace the chat/graph row:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.nodeViewID != "" || model.focus != focusGraph {
		t.Fatalf("escape did not return to graph: id=%q focus=%v", model.nodeViewID, model.focus)
	}
}

func TestSteeringPostsNodeAnchoredUserMessageAndShowsImmediately(t *testing.T) {
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "worker", Parent: store.RootID, Brief: "Running worker", Status: store.Running},
	}}}
	model := New(backend, "session-42")
	model.snapshot = backend.snapshot
	model.selectedNodeID = "worker"
	_ = model.openSelectedNode()
	typeIntoModel(model, "please check the edge case")

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("steering enter returned no post command")
	}
	if feed := model.renderActivityFeed(80); !strings.Contains(feed, "please check the edge case") {
		t.Fatalf("steer was not shown optimistically:\n%s", feed)
	}
	result := command()
	_, _ = model.Update(result)

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(backend.posted))
	}
	posted := backend.posted[0]
	if posted.SessionID != "session-42" || posted.Role != store.RoleUser || posted.NodeID != "worker" ||
		posted.Body != "please check the edge case" {
		t.Fatalf("unexpected steering message: %#v", posted)
	}
}

func TestGroupedRenderingMergesAndSplitsByVoiceAndTime(t *testing.T) {
	now := time.Now()
	model := New(&fakeBackend{}, "test-session")
	model.messages = []store.Message{
		{Time: now, Role: store.RoleAgent, Body: "first aforge line"},
		{Time: now.Add(time.Minute), Role: store.RoleSystem, NodeID: "result", Body: "node result"},
		{Time: now.Add(2 * time.Minute), Role: store.RoleUser, Body: "first user line"},
		{Time: now.Add(4 * time.Minute), Role: store.RoleUser, Body: "same user group"},
		{Time: now.Add(8 * time.Minute), Role: store.RoleUser, Body: "later user group"},
	}

	rendered := model.renderMessages()
	groups := groupMessages(model.messages)
	if len(groups) != 3 || len(groups[0].messages) != 2 || len(groups[1].messages) != 2 || len(groups[2].messages) != 1 {
		t.Fatalf("unexpected message groups: %#v", groups)
	}
	if count := strings.Count(rendered, "you  now"); count != 2 {
		t.Fatalf("user messages rendered %d labels, want 2 after time split:\n%s", count, rendered)
	}
	for _, body := range []string{"first aforge line", "node result", "first user line", "same user group", "later user group"} {
		if !strings.Contains(rendered, body) {
			t.Fatalf("grouped rendering lost %q:\n%s", body, rendered)
		}
	}
}

func TestMouseClickSelectsGraphRowFromTrackedBounds(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.setSize(120, 30)
	model.toggleGraph()
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "first", Parent: store.RootID, Brief: "First", Status: store.Pending},
		{ID: "second", Parent: store.RootID, Brief: "Second", Status: store.Pending},
	}}
	model.refreshGraph()
	_ = model.View()

	var target graphRow
	for _, row := range model.graphRows {
		if row.nodeID == "second" {
			target = row
		}
	}
	_, _ = model.Update(tea.MouseMsg{
		X:      model.graphRowsBounds.x,
		Y:      model.graphRowsBounds.y + target.line - model.graph.YOffset,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if model.selectedNodeID != "second" || model.focus != focusGraph {
		t.Fatalf("mouse selected %q with focus %v, want second/graph", model.selectedNodeID, model.focus)
	}
}

func TestNodeTraceWithoutCommanderHasNoSectionAndDoesNotPanic(t *testing.T) {
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "worker", Parent: store.RootID, Brief: "No trace commander", Status: store.Running},
	}}}
	model := New(backend, "test-session")
	model.snapshot = backend.snapshot
	model.selectedNodeID = "worker"
	poll := model.openSelectedNode()
	if poll == nil {
		t.Fatal("opening a node should request an immediate poll")
	}
	_, _ = model.Update(poll())
	if view := model.View(); strings.Contains(view, "TRACE TAIL") {
		t.Fatalf("nil Commander rendered a trace section:\n%s", view)
	}
}

func TestMemoryWithoutCommanderDegradesGracefully(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	_ = model.executeSlash("/memory")
	if model.palette != paletteNone || !strings.Contains(model.View(), "memory unavailable — no Commander") {
		t.Fatalf("memory without a Commander did not degrade gracefully:\n%s", model.View())
	}
}

func TestMemoryPanelGroupsScopesRendersKindsAndCloses(t *testing.T) {
	commander := newFakeCommander()
	commander.facts = []store.Fact{
		{Scope: "file:/repo/main.go", Kind: store.FactQuirk, Body: "Generated section must stay last."},
		{Scope: "user", Kind: store.FactPreference, Body: "Keep status updates compact."},
		{Scope: "file:/repo/main.go", Kind: store.FactPlain, Body: "The entry point is runMain."},
		{Scope: "repo:/repo", Kind: store.FactLesson, Body: "Run race tests after TUI changes."},
	}
	model := NewWithCommander(&fakeBackend{}, "test-session", commander)
	_ = model.executeSlash("/memory")
	if model.palette != paletteMemory {
		t.Fatalf("palette = %v, want memory", model.palette)
	}
	view := model.View()
	for _, expected := range []string{
		"file:/repo/main.go", "user", "repo:/repo",
		"▲", "Generated section must stay last.",
		"◆", "Keep status updates compact.",
		"·", "The entry point is runMain.",
		"●", "Run race tests after TUI changes.",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("memory panel does not contain %q:\n%s", expected, view)
		}
	}
	if count := strings.Count(view, "file:/repo/main.go"); count != 1 {
		t.Fatalf("file scope rendered %d times, want one group header:\n%s", count, view)
	}
	fileHeader := strings.Index(view, "file:/repo/main.go")
	fileQuirk := strings.Index(view, "Generated section must stay last.")
	fileFact := strings.Index(view, "The entry point is runMain.")
	userHeader := strings.Index(view, "user")
	if !(fileHeader < fileQuirk && fileQuirk < fileFact && fileFact < userHeader) {
		t.Fatalf("memory facts are not grouped under their scope:\n%s", view)
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil || model.palette != paletteNone {
		t.Fatal("escape should close the memory panel without quitting")
	}
}

func TestMemoryPanelScrolls(t *testing.T) {
	commander := newFakeCommander()
	for index := range 14 {
		commander.facts = append(commander.facts, store.Fact{
			Scope: "repo:/repo", Kind: store.FactPlain, Body: fmt.Sprintf("memory-%02d", index),
		})
	}
	model := NewWithCommander(&fakeBackend{}, "test-session", commander)
	model.setSize(72, 15)
	_ = model.executeSlash("/memory")
	before := strings.Join(model.paletteLines(68), "\n")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	after := strings.Join(model.paletteLines(68), "\n")
	if model.paletteSelected == 0 || before == after || !strings.Contains(after, "memory-") {
		t.Fatalf("page down did not scroll memory: offset %d\nbefore:\n%s\nafter:\n%s", model.paletteSelected, before, after)
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

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(commander.cancelled) != 1 || commander.cancelled[0] != "active-node" {
		t.Fatalf("cancelled nodes = %v", commander.cancelled)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 0 {
		t.Fatalf("cancel leaked to the head: %#v", backend.posted)
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
	if bar := model.renderActivityBar(); !strings.Contains(bar, "compiling") {
		t.Fatalf("activity bar does not show planning: %s", bar)
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
	spend := model.renderSpend()
	if !strings.Contains(spend, "1.2k tok") || !strings.Contains(spend, "$0.88") {
		t.Fatalf("usage spend is wrong: %s", spend)
	}
	if strings.Contains(spend, "done") || strings.Contains(spend, "failed") {
		t.Fatalf("spend line should not carry status tallies: %s", spend)
	}

	model.usage = store.TotalUsage{}
	if spend := model.renderSpend(); spend != "" {
		t.Fatalf("zero usage should hide spend entirely: %s", spend)
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

func TestGraphCollapsesToActivityBarAndTogglesOpen(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "running", Parent: store.RootID, Brief: "Active work", Status: store.Running},
		{ID: "waiting", Parent: store.RootID, Brief: "Queued work", Status: store.Pending},
	}}
	_, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 30})
	if model.horizontal {
		t.Fatal("72-column terminal should not qualify for the side rail")
	}
	if model.graphOpen {
		t.Fatal("tasks should start collapsed — chat is the primary surface")
	}
	if model.chatWidth != 72 {
		t.Fatalf("collapsed chat width = %d, want the full 72", model.chatWidth)
	}
	bar := model.renderActivityBar()
	for _, want := range []string{"1 working", "1 queued", "alt+g tasks"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("activity bar missing %q: %s", want, bar)
		}
	}
	if view := model.View(); strings.Contains(view, "Active work") {
		t.Fatalf("collapsed view should not render the task tree:\n%s", view)
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}, Alt: true})
	if !model.graphOpen || model.focus != focusGraph {
		t.Fatalf("alt+g did not open tasks with focus: open=%v focus=%v", model.graphOpen, model.focus)
	}
	if view := model.View(); !strings.Contains(view, "Active work") || !strings.Contains(view, "tasks") {
		t.Fatalf("open tasks pane should render the tree:\n%s", view)
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.graphOpen || !model.inputFocused {
		t.Fatalf("esc did not close tasks back to the input: open=%v", model.graphOpen)
	}
}

func TestWideLayoutKeepsChatAtEightyPercentAndWrapsMessages(t *testing.T) {
	model := New(&fakeBackend{}, "test-session")
	model.setSize(120, 30)
	model.toggleGraph()
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
	model.toggleGraph()
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

func TestActivityFeedParsesTraceIntoGlyphs(t *testing.T) {
	trace := "contract in force: verify before finishing\n" +
		"── turn 1  finish=tool_calls  in=1372 out=45 ──\n" +
		"call sh {\"cmd\":\"ls -la clips/\"}\n" +
		"  → 1438B: total 3984⏎drwx------\n" +
		"── turn 2  finish=tool_calls  in=2007 out=58  [nudge] ──\n" +
		"text: The handoff says the video is incomplete.⏎Checking generation state.\n" +
		"call web {\"q\":\"ffmpeg concat mp4\"}\n" +
		"  → 902B ERROR: exa 503: upstream\n" +
		"steered: focus on scene 10 only\n"
	model := New(&fakeBackend{}, "feed-test")
	model.nodeTraceText = trace
	feed := model.renderActivityFeed(80)
	for _, want := range []string{
		"turn 1 · 45 tok", "$ ls -la clips/", "→ 1.4KB", "turn 2 · 58 tok · nudge",
		"✳ ", "⌕ ffmpeg concat mp4", "ERROR", "▸ you", "focus on scene 10 only",
	} {
		if !strings.Contains(feed, want) {
			t.Fatalf("feed missing %q:\n%s", want, feed)
		}
	}
}

func TestProvenanceChipNamesTheTaskAndOpensItOnClick(t *testing.T) {
	backend := &fakeBackend{snapshot: store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "task-1", Parent: store.RootID, Brief: "long brief text", Title: "Speed up parser", Status: store.Done},
	}}}
	model := New(backend, "test-session")
	model.setSize(100, 30)
	model.snapshot = backend.snapshot
	model.messages = []store.Message{
		{Seq: 1, Time: time.Now(), Role: store.RoleSystem, NodeID: "task-1", Body: "The parser is now twice as fast."},
	}
	model.refreshChat()

	rendered := model.renderMessages()
	if !strings.Contains(rendered, "↳ Speed up parser") {
		t.Fatalf("task-anchored answer is missing its provenance chip:\n%s", rendered)
	}
	if len(model.chatChipRows) != 1 {
		t.Fatalf("chip rows = %d, want 1", len(model.chatChipRows))
	}

	_ = model.View()
	chip := model.chatChipRows[0]
	_, _ = model.Update(tea.MouseMsg{
		X: model.chatBounds.x + 1, Y: model.chatBounds.y + chip.line - model.chat.YOffset,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.nodeViewID != "task-1" {
		t.Fatalf("chip click opened %q, want task-1", model.nodeViewID)
	}
}

func TestRailShowsTitlesGroupsAndDependencyWaits(t *testing.T) {
	snapshot := store.Snapshot{
		Nodes: []store.Node{
			{ID: store.RootID},
			{ID: "job", Parent: store.RootID, Brief: "long goal text", Title: "Nighttime podcast", Status: store.Pending},
			{ID: "gather", Parent: "job", Brief: "collect sources", Title: "Collect sources", Group: "Research", Status: store.Running, StartedAt: time.Now()},
			{ID: "mix", Parent: "job", Brief: "mix the audio", Title: "Mix audio", Group: "Production", Status: store.Pending},
		},
		Edges: []store.Edge{{From: "gather", To: "mix", Kind: store.FeedsInto}},
	}
	model := New(&fakeBackend{snapshot: snapshot}, "test-session")
	model.snapshot = snapshot
	model.selectedNodeID = "mix"
	tree := model.renderTree(80, 0)
	for _, want := range []string{"Nighttime podcast", "┄ Research", "┄ Production", "Mix audio", "◌", "waits: Collect sources"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("rail missing %q:\n%s", want, tree)
		}
	}
	if strings.Contains(tree, "long goal text") {
		t.Fatalf("rail shows brief where a title exists:\n%s", tree)
	}
}

func TestGraphBindingRoutesAltGAndLeavesCtrlGAlone(t *testing.T) {
	model := New(&fakeBackend{}, "bindings")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if model.graphOpen {
		t.Fatal("ctrl+g still toggles the graph rail")
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}, Alt: true})
	if !model.graphOpen || model.focus != focusGraph {
		t.Fatalf("alt+g did not route to the rail: open=%v focus=%v", model.graphOpen, model.focus)
	}
	if view := model.View(); !strings.Contains(view, "alt+g hide") || strings.Contains(view, "^g") {
		t.Fatalf("binding hints are not sourced from alt+g:\n%s", view)
	}
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{
		X: model.graphBounds.x, Y: model.graphBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.graphOpen {
		t.Fatal("clicking the rail header did not close it")
	}
}

func TestSlashCommandsAreConsumedAndRouteLocally(t *testing.T) {
	backend := &fakeBackend{}
	commander := newFakeCommander()
	commander.facts = []store.Fact{{Scope: "user", Kind: store.FactPreference, Body: "Keep it concise."}}
	model := NewWithCommander(backend, "slash", commander)

	model.input.SetValue("/not-a-command")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || model.input.Value() != "" || !strings.Contains(model.status, "not a command") {
		t.Fatalf("unknown slash command was not consumed quietly: command=%v input=%q status=%q",
			command != nil, model.input.Value(), model.status)
	}

	model.input.SetValue("/graph")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.graphOpen {
		t.Fatal("/graph did not toggle the rail")
	}

	model.cards = []jobCard{{ID: "job", RootID: "job", State: cardWorking, Title: "Active task"}}
	model.input.SetValue("/tasks")
	model.focus = focusInput
	model.inputFocused = true
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.graphOpen || model.focus != focusCards {
		t.Fatalf("/tasks did not focus the dock: graph=%v focus=%v", model.graphOpen, model.focus)
	}

	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "node-alpha", Parent: store.RootID, Brief: "Alpha node", Status: store.Running},
	}}
	model.cardSnapshot = model.snapshot
	model.focus = focusInput
	model.inputFocused = true
	model.selectedNodeID = "node-alpha"
	model.input.SetValue("/node")
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nodeViewID != "node-alpha" || command == nil {
		t.Fatalf("/node did not use the clicked selection: node=%q command=%v", model.nodeViewID, command != nil)
	}
	model.closeNodeView()

	model.focus = focusInput
	model.inputFocused = true
	model.selectedNodeID = ""
	model.input.SetValue("/node node-a")
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nodeViewID != "node-alpha" || command == nil {
		t.Fatalf("/node prefix did not open the flight recorder: node=%q command=%v", model.nodeViewID, command != nil)
	}

	model.input.SetValue("/notebook")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.palette != paletteMemory || !strings.Contains(model.renderPalette(), "Keep it concise") {
		t.Fatalf("/notebook did not open the scrollable notebook pane:\n%s", model.renderPalette())
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 0 {
		t.Fatalf("slash commands leaked %d messages to the head: %#v", len(backend.posted), backend.posted)
	}
}

func TestShimmerLinesOnlyShowForRailClosedActiveJobs(t *testing.T) {
	model := New(&fakeBackend{}, "shimmer")
	model.messages = []store.Message{{Seq: 1, Role: store.RoleUser, Body: "Run these jobs"}}
	for index := 0; index < 4; index++ {
		model.cards = append(model.cards, jobCard{
			ID: fmt.Sprintf("job-%d", index), State: cardWorking,
			Title: fmt.Sprintf("Job %d", index), Latest: fmt.Sprintf("Narrating %d", index),
		})
	}

	shimmer := model.renderShimmerLines(80)
	for _, want := range []string{"Job 0", "Narrating 1", "Job 2", "1 more running"} {
		if !strings.Contains(shimmer, want) {
			t.Fatalf("closed-rail shimmer is missing %q:\n%s", want, shimmer)
		}
	}
	if strings.Contains(shimmer, "Job 3") {
		t.Fatalf("shimmer exceeded the three-job line budget:\n%s", shimmer)
	}
	thread := model.renderMessages()
	if strings.Index(thread, "Job 0") < strings.Index(thread, "Run these jobs") {
		t.Fatalf("shimmer did not render under the last thread message:\n%s", thread)
	}

	model.toggleGraph()
	if got := model.renderShimmerLines(80); got != "" {
		t.Fatalf("open rail duplicated the shimmer:\n%s", got)
	}
	model.toggleGraph()
	model.cards = nil
	if got := model.renderShimmerLines(80); got != "" {
		t.Fatalf("idle thread rendered a shimmer:\n%s", got)
	}
}

func TestRealProviderDeltasAccumulateAtTypewriterPace(t *testing.T) {
	model := New(&fakeBackend{}, "real-stream")
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"This arrives`})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: ` in provider deltas.","command":null}`})
	if model.streamTarget != "This arrives in provider deltas." || model.streamShown != "" {
		t.Fatalf("real deltas were not buffered for pacing: target=%q shown=%q", model.streamTarget, model.streamShown)
	}

	_, _ = model.Update(animationTickMsg(time.Now()))
	if model.streamShown == "" || model.streamShown == model.streamTarget ||
		!strings.HasPrefix(model.streamTarget, model.streamShown) {
		t.Fatalf("first real-delta tick did not grow a prefix: shown=%q target=%q", model.streamShown, model.streamTarget)
	}
	for index := 0; index < 20 && model.streamShown != model.streamTarget; index++ {
		_, _ = model.Update(animationTickMsg(time.Now()))
	}
	if model.streamShown != model.streamTarget {
		t.Fatalf("real stream never accumulated completely: shown=%q", model.streamShown)
	}

	model.applyPoll(pollResultMsg{messages: []store.Message{{
		Seq: 1, SessionID: "real-stream", Role: store.RoleAgent, Body: model.streamTarget,
	}}})
	if model.streamMode != streamReal || model.streamSeq != 1 {
		t.Fatalf("durable poll racing the terminal stream event lost the real preview: mode=%v seq=%d",
			model.streamMode, model.streamSeq)
	}
	model.applyStreamEvent(StreamEvent{Kind: StreamFinished})
	if model.streamMode != streamNone || !strings.Contains(model.renderMessages(), "This arrives in provider deltas.") {
		t.Fatalf("durable real-stream landing did not replace the preview cleanly: mode=%v\n%s",
			model.streamMode, model.renderMessages())
	}

	if partial, _ := partialJSONReply(`{"reply":"emoji \uD83D`); partial != "emoji " {
		t.Fatalf("partial surrogate leaked a replacement rune: %q", partial)
	}
	if complete, _ := partialJSONReply(`{"reply":"emoji \uD83D\uDE80"}`); complete != "emoji 🚀" {
		t.Fatalf("completed surrogate pair decoded as %q", complete)
	}
}

func TestStreamStartAdoptsAnAlreadyPolledHeadReply(t *testing.T) {
	message := store.Message{
		Seq: 1, SessionID: "raced-stream", Role: store.RoleAgent, Body: "A reply that landed first.",
	}
	model := New(&fakeBackend{}, "raced-stream")
	model.applyPoll(pollResultMsg{messages: []store.Message{message}})
	if model.streamMode != streamSimulated || model.streamSeq != message.Seq {
		t.Fatalf("early durable reply did not enter fallback pacing: mode=%v seq=%d", model.streamMode, model.streamSeq)
	}

	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	if model.streamMode != streamReal || model.streamSeq != message.Seq {
		t.Fatalf("provider start did not adopt the landed reply: mode=%v seq=%d", model.streamMode, model.streamSeq)
	}
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"A reply that landed first."}`})
	model.applyStreamEvent(StreamEvent{Kind: StreamFinished})
	for index := 0; index < 20 && model.streamMode != streamNone; index++ {
		_, _ = model.Update(animationTickMsg(time.Now()))
	}
	if model.streamMode != streamNone || !strings.Contains(model.renderMessages(), message.Body) {
		t.Fatalf("adopted provider stream did not settle cleanly: mode=%v\n%s",
			model.streamMode, model.renderMessages())
	}
}

func TestSimulatedStreamsQueueWithoutExposingLaterAnswer(t *testing.T) {
	first := store.Message{Seq: 1, Role: store.RoleSystem, NodeID: "job-a", Body: "First answer lands."}
	second := store.Message{Seq: 2, Role: store.RoleSystem, NodeID: "job-b", Body: "Second answer waits its turn."}
	model := New(&fakeBackend{}, "queued-streams")
	model.messages = []store.Message{first, second}
	model.startSimulatedStream(first)
	model.queueSimulatedStream(second)

	if rendered := model.renderAnswer(second, 80); strings.Contains(rendered, second.Body) {
		t.Fatalf("queued answer appeared before its reveal turn: %q", rendered)
	}
	for index := 0; index < 10 && model.streamSeq == first.Seq; index++ {
		_, _ = model.Update(animationTickMsg(time.Now()))
	}
	if model.streamMode != streamSimulated || model.streamSeq != second.Seq || model.streamShown == second.Body {
		t.Fatalf("second answer did not enter paced reveal: mode=%v seq=%d shown=%q",
			model.streamMode, model.streamSeq, model.streamShown)
	}
	for index := 0; index < 20 && model.streamMode != streamNone; index++ {
		_, _ = model.Update(animationTickMsg(time.Now()))
	}
	if model.streamMode != streamNone {
		t.Fatalf("queued stream did not settle: mode=%v", model.streamMode)
	}
}

func TestNonStreamingDeliverableUsesSimulatedTypewriter(t *testing.T) {
	body := "A landed deliverable grows one token at a time in the thread."
	snapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "job", Parent: store.RootID, Status: store.Done, Summary: body, CreatedSeq: 1,
			Provenance: store.Provenance{SessionID: "sim-stream", Intent: "deliver it"}},
	}}
	delivery := store.Message{Seq: 2, SessionID: "sim-stream", Role: store.RoleSystem, NodeID: "job", Body: body}
	model := New(&fakeBackend{}, "sim-stream")
	model.applyPoll(pollResultMsg{messages: []store.Message{delivery}, snapshot: snapshot, cardSnapshot: snapshot})
	if model.streamMode != streamSimulated || model.streamShown == "" || model.streamShown == body {
		t.Fatalf("landed deliverable did not enter simulated streaming: mode=%v shown=%q", model.streamMode, model.streamShown)
	}
	first := model.streamShown
	_, _ = model.Update(animationTickMsg(time.Now()))
	if len(model.streamShown) <= len(first) || !strings.HasPrefix(body, model.streamShown) {
		t.Fatalf("simulated typewriter did not accumulate: before=%q after=%q", first, model.streamShown)
	}
	for index := 0; index < 30 && model.streamMode != streamNone; index++ {
		_, _ = model.Update(animationTickMsg(time.Now()))
	}
	if model.streamMode != streamNone || !strings.Contains(model.renderMessages(), body) {
		t.Fatalf("simulated stream did not settle to the full deliverable: mode=%v\n%s",
			model.streamMode, model.renderMessages())
	}
}
