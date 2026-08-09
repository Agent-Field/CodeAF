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

// The fixture is a settled thread with both kinds of block in it: a plain
// message group, and a finished job's card. The card is the expensive one —
// it carries the deliverable, its parts and its outcome, and every word of all
// three is asked whether it names a file — and until this fixture had one, the
// reuse tests were watching the only half of the thread that was cached.
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
	started := now.Add(-9 * time.Minute)
	finished := now.Add(-8 * time.Minute)
	snapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "landed", Parent: store.RootID, Title: "the landed job", Status: store.Done,
			CreatedSeq: 1, StartedAt: started, FinishedAt: finished,
		},
		// An organizational root owns no card, so the answer anchored under it
		// stays in the thread and keeps its provenance chip.
		{ID: "box", Parent: store.RootID, Group: store.TerritoryGroup, CreatedSeq: 2},
		{ID: "job", Parent: "box", Title: "the first task", Status: store.Done, CreatedSeq: 3},
	}}
	backend := newCountingBackend(&fakeBackend{
		snapshot: snapshot,
		messages: []store.Message{
			{
				Seq: 1, SessionID: "reuse", Role: store.RoleAgent, NodeID: "job",
				Body: "wrote notes.md for you", Time: now.Add(-2 * time.Minute),
			},
			{
				Seq: 2, SessionID: "reuse", Role: store.RoleAgent, NodeID: "landed",
				Body: "the job is done and its answer is in notes.md", Time: finished,
			},
		},
	})
	model := NewWithCommander(backend, "reuse", commander)
	model.standingNow = func() time.Time { return now }
	model.setSize(100, 30)
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if card := model.cardByID("landed"); card == nil || card.State != cardSettled {
		t.Fatalf("fixture has no settled card: %+v", model.cards)
	}
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
		{
			Seq: 8, SessionID: "identical", Role: store.RoleAgent, Time: now,
			Body: "while you were away four things landed.",
			Brief: &store.Brief{Items: []store.BriefItem{
				{Kind: store.BriefDone, Body: "the report is written"},
				{Kind: store.BriefQuestion, Body: "one job is waiting on you"},
			}},
		},
		{
			Seq: 9, SessionID: "identical", Role: store.RoleAgent, NodeID: "landed", Time: now.Add(-5 * time.Minute),
			Body: "the landed job's answer is here, and it is " + long,
		},
	}
	backend := &fakeBackend{
		messages: messages,
		snapshot: store.Snapshot{Nodes: []store.Node{
			{ID: store.RootID},
			{ID: "job", Parent: store.RootID, Title: "the job"},
			{
				ID: "landed", Parent: store.RootID, Title: "the landed job", Status: store.Done,
				CreatedSeq: 2, StartedAt: now.Add(-9 * time.Minute), FinishedAt: now.Add(-5 * time.Minute),
			},
		}},
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
		coldCards := append([]cardRow(nil), model.chatCardRows...)
		coldParts := append([]cardPartRow(nil), model.cardPartRows...)
		coldCloses := append([]cardCloseRow(nil), model.cardCloseRows...)
		coldOptions := append([]cardOptionRow(nil), model.cardOptionRows...)
		warm := model.renderMessages()
		if warm != cold {
			t.Fatalf("%s: cached thread differs from a cold render:\n--- cold\n%q\n--- warm\n%q", stage, cold, warm)
		}
		if !reflect.DeepEqual(model.chatChipRows, coldChips) ||
			!reflect.DeepEqual(model.chatExpandRows, coldExpands) ||
			!reflect.DeepEqual(model.chatMessageRows, coldRows) ||
			!reflect.DeepEqual(model.chatCardRows, coldCards) ||
			!reflect.DeepEqual(model.cardPartRows, coldParts) ||
			!reflect.DeepEqual(model.cardCloseRows, coldCloses) ||
			!reflect.DeepEqual(model.cardOptionRows, coldOptions) {
			t.Fatalf("%s: cached thread moved its interactive rows", stage)
		}
	}

	// The thread has to hold one of each kind of block, or the comparison
	// below is only watching the half that was already cached.
	_ = model.renderMessages()
	if len(model.chatCardRows) == 0 {
		t.Fatalf("no settled card in the thread: %+v", model.cards)
	}
	briefRows := 0
	for _, row := range model.chatExpandRows {
		if row.action == chatExpandBrief {
			briefRows++
		}
	}
	if briefRows == 0 {
		t.Fatal("no arrival brief in the thread")
	}
	compare("settled")
	model.expandedMessages[5] = true
	compare("answer opened")
	model.learningExpanded[4] = true
	compare("learning opened")
	model.receiptsExpanded = true
	compare("receipts opened")
	model.cardExpanded["landed"] = true
	compare("settled card opened")
	model.briefExpanded[8] = true
	compare("brief opened")
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"still arriving`})
	model.advanceStream()
	compare("streaming tail")
	model.setSize(70, 30)
	compare("narrower pane")
}

// A settled message is immutable, so the thread it sits in is rendered once
// and replayed until the store says otherwise. blockBuilds is the count of
// blocks this session has had to assemble — message groups, settled cards and
// briefs alike — so it says which halves of the thread were reused and which
// were not.
func TestSettledThreadIsRenderedOncePerChange(t *testing.T) {
	model, _, backend := countingResolverModel(t)

	before := model.blockBuilds
	for range 5 {
		model.refreshChat()
		_ = model.View()
	}
	if model.blockBuilds != before {
		t.Fatalf("five frames rebuilt %d thread blocks", model.blockBuilds-before)
	}

	// A quiet poll proves nothing moved, so the rendered blocks stand.
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if model.blockBuilds != before {
		t.Fatal("a quiet poll retired the rendered thread")
	}

	// A journal that moved but changed nothing this thread is made of keeps
	// every block: the keys name what a block depends on, so a poll that
	// touched none of it has nothing to retire.
	backend.bump()
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if model.blockBuilds != before {
		t.Fatalf("an irrelevant poll rebuilt %d thread blocks", model.blockBuilds-before)
	}

	// Opening the answer in place is a change to that block, and only to it.
	model.expandedMessages[1] = true
	model.refreshChat()
	if model.blockBuilds != before+1 {
		t.Fatalf("opening one folded answer rebuilt %d blocks", model.blockBuilds-before)
	}

	// Opening the settled card is a change to the card's block, and only it.
	before = model.blockBuilds
	model.cardExpanded["landed"] = true
	model.refreshChat()
	if model.blockBuilds != before+1 {
		t.Fatalf("opening one settled card rebuilt %d blocks", model.blockBuilds-before)
	}
}

// A poll that renames the node an answer points at must retire that answer's
// block — the chip carries the name — and a settled card whose contents moved
// must be redrawn.
func TestReferencedNodeChangeRetiresOnlyItsBlock(t *testing.T) {
	model, _, backend := countingResolverModel(t)
	_ = model.View()

	before := model.blockBuilds
	backend.fakeBackend.mu.Lock()
	nodes := append([]store.Node(nil), backend.fakeBackend.snapshot.Nodes...)
	for index := range nodes {
		if nodes[index].ID == "job" {
			nodes[index].Title = "the renamed task"
		}
	}
	backend.fakeBackend.snapshot = store.Snapshot{Nodes: nodes}
	backend.fakeBackend.mu.Unlock()
	backend.bump()
	model.applyPoll(model.poll()().(pollResultMsg))
	_ = model.View()
	if model.blockBuilds == before {
		t.Fatal("a renamed node kept the block whose chip names it")
	}
	if built := model.blockBuilds - before; built > 2 {
		t.Fatalf("a renamed node rebuilt %d blocks", built)
	}
}

// Every whitespace-separated token of every answer on screen is a question for
// the resolver, and in production each one is a query and a stat. The same
// question is asked of the store once.
func TestWorkspaceResolutionIsAskedOncePerQuestion(t *testing.T) {
	model, commander, _ := countingResolverModel(t)

	before := commander.resolves
	for range 5 {
		model.threadGen++
		model.refreshChat()
	}
	if commander.resolves != before {
		t.Fatalf("five cold renders asked the resolver %d more times", commander.resolves-before)
	}

	// The clock's own repaint is where a file that landed under a node nobody
	// has touched since finally gets to become a link.
	model.forgetWorkspaceLinks()
	model.threadGen++
	model.refreshChat()
	if commander.resolves == before {
		t.Fatal("dropping the remembered links did not ask the resolver again")
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

// The card lookups are a map read now, and a map read has to answer exactly
// what the walk answered: the first card that owns the node or the command,
// including after a card is edited where it lies.
func TestCardLookupsAnswerWhatTheWalkAnswered(t *testing.T) {
	model := New(&fakeBackend{}, "lookups")
	model.cards = []jobCard{
		{ID: "first", CommandSeq: 7, Parts: []cardPart{{NodeID: "shared"}, {NodeID: "one"}}},
		{ID: "second", CommandSeq: 7, Parts: []cardPart{{NodeID: "shared"}, {NodeID: "two"}}},
	}
	if card := model.cardForNodeID("shared"); card == nil || card.ID != "first" {
		t.Fatalf("shared node resolved to %v, want the first card", card)
	}
	if card := model.cardForNodeID("two"); card == nil || card.ID != "second" {
		t.Fatalf("second card's own node resolved to %v", card)
	}
	if card := model.cardForNodeID("absent"); card != nil {
		t.Fatalf("unknown node resolved to %q", card.ID)
	}
	message := store.Message{CommandSeq: 7}
	if card := model.cardForMessage(message); card == nil || card.ID != "first" {
		t.Fatalf("shared command resolved to %v, want the first card", card)
	}

	// A card edited in place keeps its position, so the index still finds it.
	model.cards[1].State = cardSettled
	if card := model.cardForNodeID("two"); card == nil || card.State != cardSettled {
		t.Fatal("a card edited where it lies was no longer findable")
	}

	// A replaced list is a different list.
	model.cards = []jobCard{{ID: "only", Parts: []cardPart{{NodeID: "shared"}}}}
	if card := model.cardForNodeID("shared"); card == nil || card.ID != "only" {
		t.Fatalf("a rebuilt card list resolved to %v", card)
	}
	if card := model.cardForMessage(message); card != nil {
		t.Fatalf("a retired command still resolved to %q", card.ID)
	}
}

// The whole-graph sweep is kept beside the snapshot it swept, and it answers
// what the separate walks answered — including the charter definition subtree
// that neither the counts nor the presence line may see.
func TestGraphFactsAreSweptOncePerSnapshot(t *testing.T) {
	model := New(&fakeBackend{}, "facts")
	definition := store.Node{ID: "watch", Parent: store.RootID, Group: charterGroupMarker}
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		definition,
		{ID: "sentinel", Parent: "watch", Status: store.Running},
		{ID: "work", Parent: store.RootID, Status: store.Running},
		{ID: "queued", Parent: store.RootID, Status: store.Pending},
		{ID: "broken", Parent: store.RootID, Status: store.Failed},
		{ID: "mine", Parent: store.RootID, Status: store.Running,
			Provenance: store.Provenance{Origin: store.OriginSelf}},
		{ID: "practice", Parent: store.RootID, Group: store.PracticeGroup,
			Title: "practice writing", Status: store.Running, CreatedSeq: 9,
			Provenance: store.Provenance{Origin: store.OriginSelf}},
	}}
	running, queued, failed := model.taskCounts()
	if running != 1 || queued != 1 || failed != 1 {
		t.Fatalf("counts = %d/%d/%d, want 1/1/1", running, queued, failed)
	}
	if text := model.residentPresenceText(); !strings.Contains(text, "practicing: writing") {
		t.Fatalf("presence line = %q", text)
	}

	// The same snapshot is not swept again.
	facts := model.graphFacts.of(model.snapshot.Nodes)
	if !facts.definitions["sentinel"] {
		t.Fatal("the charter definition subtree was not marked")
	}
	facts.definitions["\x00mark"] = true
	if again := model.graphFacts.of(model.snapshot.Nodes); !again.definitions["\x00mark"] {
		t.Fatal("the same snapshot was swept twice")
	}

	// A replaced snapshot is swept for itself.
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{ID: "work", Parent: store.RootID, Status: store.Done},
	}}
	if running, queued, failed = model.taskCounts(); running != 0 || queued != 0 || failed != 0 {
		t.Fatalf("a replaced snapshot counted %d/%d/%d", running, queued, failed)
	}
	if text := model.residentPresenceText(); text != "" {
		t.Fatalf("a replaced snapshot kept the presence line %q", text)
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
