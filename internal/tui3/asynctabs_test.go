package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── SEVERAL CONVERSATIONS, RUNNING AT ONCE, AND CLOSING A TAB THAT IS ────────
//
// These are the surface's half of the async wave. The engine's half — that the
// ordinary door can really hold three connections and three sessions — is
// asserted against a real host on a real socket in cmd/aforge's
// chatv3_beside_test.go; what is asserted here is what the surface does with
// them: that switching moves nothing but the screen, that a tab coming off the
// row keeps its conversation alive, and that the card offering to stop the work
// stops exactly one conversation's.

// asyncAgent is one conversation whose turn runs until this test finishes it,
// and which remembers what was done to it.
type asyncAgent struct {
	*fakeAgent
	turn    chan session.Event
	running bool
	closed  bool
	// nodes are the ids this conversation says are running, and cancelled is
	// what it was asked to stop.
	nodes     []session.TaskIndexEntry
	cancelled []string
}

func newAsyncAgent() *asyncAgent {
	return &asyncAgent{fakeAgent: &fakeAgent{model: "m"}}
}

// Attach is the door [app.attachConversation] and [behindWatch] both take onto a
// turn that is already in flight.
func (s *asyncAgent) Attach() (<-chan session.Event, bool, func()) {
	if !s.running {
		return nil, false, func() {}
	}
	return s.turn, true, func() {}
}

func (s *asyncAgent) Close() error { s.closed = true; return s.fakeAgent.Close() }

func (s *asyncAgent) TaskIndex() []session.TaskIndexEntry { return s.nodes }

func (s *asyncAgent) Cancel(id string) (string, error) {
	s.cancelled = append(s.cancelled, id)
	return "stopped " + id, nil
}

// begin puts a turn in flight in this conversation.
func (s *asyncAgent) begin() {
	s.turn = make(chan session.Event, 8)
	s.running = true
}

// asyncDoor is a door that opens a WHOLE conversation with an agent of its own
// every time it is asked — which is what the engine door does now that each
// conversation has its own connection (cmd/aforge's chatv3_beside.go).
type asyncDoor struct {
	t     *testing.T
	made  []*asyncAgent
	files []string
}

func (d *asyncDoor) start(workspace string) (Conversation, error) {
	agent := newAsyncAgent()
	d.made = append(d.made, agent)
	file := "/tmp/lab/beside-" + itoa(len(d.made)) + ".jsonl"
	d.files = append(d.files, file)
	return Conversation{
		Agent: agent, SessionFile: file, Workspace: "/tmp/lab",
		DraftFile: "/tmp/lab/draft.txt",
	}, nil
}

func (d *asyncDoor) open(workspace, transcript string) (Conversation, error) {
	agent := newAsyncAgent()
	d.made = append(d.made, agent)
	return Conversation{Agent: agent, SessionFile: transcript, Workspace: "/tmp/lab"}, nil
}

// asyncApp is a surface on a door that adds conversations rather than swapping
// them, with one conversation already in front and a turn running in it.
func asyncApp(t *testing.T) (*app, *asyncDoor, *asyncAgent) {
	t.Helper()
	first := newAsyncAgent()
	door := &asyncDoor{t: t}
	a := newApp(context.Background(), Options{
		Agent: first, Workspace: "/tmp/lab",
		Start: door.start, Open: door.open,
	})
	a.width, a.height = 120, 30
	a.file = "/tmp/lab/first.jsonl"
	a.stirs = make(chan string, stirDepth)
	emptyMachine(a)
	return a, door, first
}

// turning puts the surface into a turn on the conversation in front, the way a
// submitted message does, without going near a model.
func turning(a *app, agent *asyncAgent) {
	agent.begin()
	a.state = stateWorking
	a.stream = agent.turn
}

// ── THE CONVERSATIONS ───────────────────────────────────────────────────────

// THE DEFECT, AS THE SURFACE SEES IT. Opening a second and a third conversation
// leaves the first two running, keeps three separate transcripts, and says
// nothing about a connection holding one conversation at a time.
func TestThreeConversationsOpenBesideEachOtherAndAllThreeStayAlive(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)

	for i := 0; i < 2; i++ {
		cmd, refusal := a.startBeside("/tmp/lab")
		if refusal != "" {
			t.Fatalf("opening a conversation beside was refused: %s", refusal)
		}
		drain(t, a, cmd)
		turning(a, door.made[len(door.made)-1])
	}

	if a.openCount() != 3 {
		t.Fatalf("this window holds %d conversations, and three were opened", a.openCount())
	}
	if first.closed || door.made[0].closed {
		t.Fatal("opening a conversation beside closed one that was already open")
	}
	if !first.running || !door.made[0].running {
		t.Fatal("a conversation stopped running when another was opened beside it")
	}
	// THREE TRANSCRIPTS AND NOT ONE. Two tabs on one journal is the shape a
	// shared handle produced, and it is the one this must never be.
	files := map[string]bool{a.file: true}
	for key := range a.behind {
		files[key] = true
	}
	if len(files) != 3 {
		t.Fatalf("three conversations are writing %d transcripts: %v", len(files), files)
	}
	// AND NOBODY WAS TOLD THAT A CONNECTION HOLDS ONE AT A TIME.
	for i := range a.entries {
		if a.entries[i].kind == entryNote && strings.Contains(a.entries[i].text, oneConversationWord) {
			t.Fatalf("the surface said %q on a door that opens conversations beside", a.entries[i].text)
		}
	}
}

// SWITCHING MOVES THE SCREEN AND NOTHING ELSE: every conversation keeps its
// agent, its turn and its transcript, and going back finds the one that was left.
func TestSwitchingThroughTabsCancelsNothingAndCrossRoutesNothing(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	cmd, refusal := a.startBeside("/tmp/lab")
	if refusal != "" {
		t.Fatalf("opening beside: %s", refusal)
	}
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)

	// Back to the first, and forward again.
	drain(t, a, a.lastConversation())
	if a.agent != Agent(first) || a.file != "/tmp/lab/first.jsonl" {
		t.Fatalf("tab landed on %q", a.file)
	}
	drain(t, a, a.lastConversation())
	if a.agent != Agent(second) || a.file != door.files[0] {
		t.Fatalf("tab back landed on %q", a.file)
	}
	for i, agent := range []*asyncAgent{first, second} {
		if agent.closed || agent.stops != 0 {
			t.Fatalf("conversation %d was closed %v or interrupted %d times by a switch", i, agent.closed, agent.stops)
		}
		if !agent.running {
			t.Fatalf("conversation %d stopped running across a switch", i)
		}
	}
	// AND THE STREAM THE SURFACE IS PUMPING IS THIS CONVERSATION'S. A surface
	// still holding the other one's channel would draw one reply in two places.
	if a.stream != nil && a.stream != (<-chan session.Event)(second.turn) {
		t.Fatal("the surface came back holding another conversation's turn")
	}
}

// GOING HOME IS A PLACE AND NOT A CLOSE: everything this window holds is still
// held and still running when the person comes back.
func TestGoingHomeLeavesEveryConversationRunning(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)

	drain(t, a, a.showPage(pageHome))
	if a.openCount() != 2 {
		t.Fatalf("home left %d conversations open", a.openCount())
	}
	for i, agent := range []*asyncAgent{first, second} {
		if agent.closed || !agent.running {
			t.Fatalf("conversation %d did not survive a trip home", i)
		}
	}
}

// ── THE CLOSE-A-TAB CARD ────────────────────────────────────────────────────

// A TAB WITH NOTHING IN FLIGHT CLOSES ON THE FIRST PRESS. There is nothing to
// decide, and a question with one useful answer is friction.
func TestClosingAnIdleTabAsksNothing(t *testing.T) {
	a, _, _ := asyncApp(t)
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	// The strip has to have been laid out for the row to know where a closed
	// tab hands the surface on to (chattabs.go's [app.lastVisibleTab]).
	_ = a.tabsRow(a.width)

	before := a.file
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if a.closingTab() {
		t.Fatal("closing an idle tab raised a question")
	}
	if a.file == before {
		t.Fatal("closing an idle tab did nothing at all")
	}
}

// A TAB THAT IS WORKING RAISES THE CARD, AND THE CARD CHANGES NOTHING UNTIL IT
// IS ANSWERED.
func TestClosingAWorkingTabAsksBeforeAnythingMoves(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	before := a.file

	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if !a.closingTab() {
		t.Fatal("closing a working tab took it off the row without asking")
	}
	if a.file != before {
		t.Fatal("the card moved the surface before it was answered")
	}
	if first.stops != 0 || first.closed {
		t.Fatal("raising the card touched the work")
	}
	// THE CURSOR OPENS ON THE ANSWER THAT LOSES NOTHING.
	if a.tabClose.pick != tabCloseKeepAt {
		t.Fatalf("the card opened on answer %d", a.tabClose.pick)
	}
	rows := a.tabCloseRows(a.width)
	if len(rows) != 3 {
		t.Fatalf("the card drew %d rows", len(rows))
	}
	said := plain(strings.Join(rows, "\n"))
	for _, want := range []string{"keep running", "stop work", "cancel", "Close this tab?"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the card does not say %q:\n%s", want, said)
		}
	}
}

// ESC LEAVES THE WORK AND THE TAB EXACTLY AS THEY WERE.
func TestCancellingTheCloseLeavesTheTabAndTheWorkAlone(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	before := a.file
	drain(t, a, a.tabDismiss(a.frontChatTab()))

	drive(t, a, key("esc"))
	if a.closingTab() {
		t.Fatal("esc left the card up")
	}
	if a.file != before {
		t.Fatalf("esc moved the surface to %q", a.file)
	}
	if first.stops != 0 || first.closed || !first.running {
		t.Fatalf("esc touched the work: stops=%d closed=%v running=%v", first.stops, first.closed, first.running)
	}
	if a.tabShut[a.convKey(before)] {
		t.Fatal("esc took the tab off the row")
	}
}

// KEEP RUNNING TAKES THE TAB OFF AND LEAVES EVERYTHING ELSE: the agent runs on,
// this window still holds it, and it is still on the switcher.
func TestKeepRunningHidesTheTabAndKeepsTheConversation(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)
	// The card is about the conversation IN FRONT, which is the second one.
	front := a.file
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if !a.closingTab() {
		t.Fatal("a working tab was closed without asking")
	}

	drive(t, a, key("enter"))
	if a.closingTab() {
		t.Fatal("enter left the card up")
	}
	if second.closed || second.stops != 0 || !second.running {
		t.Fatalf("keep running stopped the work: closed=%v stops=%d running=%v",
			second.closed, second.stops, second.running)
	}
	if !a.tabShut[a.convKey(front)] {
		t.Fatal("keep running left the tab on the row")
	}
	if !a.holding(front) {
		t.Fatal("keep running let go of the conversation")
	}
	// AND IT IS STILL FINDABLE. `Chats` opens the switcher over every
	// conversation on the machine, and this one is one of them.
	a.hopOpenAll()
	found := false
	for _, row := range a.hop.rows {
		if row.file == front {
			found = true
		}
	}
	if !found {
		t.Fatalf("a conversation whose tab was hidden left the switcher: %+v", a.hop.rows)
	}
}

// STOP WORK IS EXPLICIT, AND IT REACHES EXACTLY ONE CONVERSATION.
func TestStopWorkEndsOnlyTheConversationItsCardIsAbout(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)
	second.nodes = []session.TaskIndexEntry{
		{ID: "3", Status: string(session.TaskRunning)},
	}
	// The roster the surface itself holds for the conversation in front, which
	// is what a stop walks (tabclose.go's [app.stopFrontNodes]).
	a.tasks = map[uint64]*taskNode{3: {id: 3, state: session.TaskRunning}}
	a.taskOrder = []uint64{3}

	drain(t, a, a.tabDismiss(a.frontChatTab()))
	drive(t, a, key(tabCloseStopKey))

	if second.stops == 0 {
		t.Fatal("stop work did not stop the turn it was about")
	}
	if len(second.cancelled) != 1 || second.cancelled[0] != session.CancelTask+":3" {
		t.Fatalf("stop work asked for %v", second.cancelled)
	}
	// AND THE OTHER CONVERSATION IS UNTOUCHED.
	if first.stops != 0 || first.closed || !first.running || len(first.cancelled) != 0 {
		t.Fatalf("stop work reached another conversation: stops=%d closed=%v cancelled=%v",
			first.stops, first.closed, first.cancelled)
	}
	// THE TRANSCRIPT IS NOT DELETED AND THE CONVERSATION IS STILL HELD: stopping
	// work is not closing a conversation.
	if second.closed {
		t.Fatal("stop work closed the conversation as well as its work")
	}
}

// CLOSING ONE CHAT WHILE ANOTHER RUNS TOUCHES ONLY THE ONE CLOSED.
func TestClosingOneChatWhileAnotherRunsLeavesTheOtherRunning(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)

	drain(t, a, a.tabDismiss(a.frontChatTab()))
	drive(t, a, key(tabCloseStopKey))

	if !first.running || first.closed || first.stops != 0 {
		t.Fatal("stopping one chat stopped the one behind it")
	}
	if a.openCount() != 2 {
		t.Fatalf("this window holds %d conversations after one tab was closed", a.openCount())
	}
}

// A CONVERSATION THAT FINISHES WHILE THE CARD IS UP IS STILL ANSWERABLE, both
// ways, and the card does not vanish under the hand about to answer it.
func TestTheCardSurvivesTheWorkFinishingUnderIt(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	if !a.closingTab() {
		t.Fatal("no card")
	}

	// The turn lands while the question is on screen.
	first.running = false
	a.state = stateIdle
	a.stream = nil
	a.touch()
	if !a.closingTab() {
		t.Fatal("the card took itself down when the work finished")
	}
	if rows := a.tabCloseRows(a.width); len(rows) != 3 {
		t.Fatalf("the card stopped drawing: %v", rows)
	}
	// AND STOP WORK ON IT IS A STOP THAT FINDS NOTHING, never a failure.
	drive(t, a, key(tabCloseStopKey))
	if a.closingTab() {
		t.Fatal("the card is still up after being answered")
	}
	if first.closed {
		t.Fatal("answering a card about a finished conversation closed it")
	}
}

// THE POINTER ANSWERS THE SAME CARD THE KEYBOARD DOES, on the answers' own row.
func TestAPressOnAnAnswerAnswersTheCard(t *testing.T) {
	a, _, first := asyncApp(t)
	turning(a, first)
	drain(t, a, a.tabDismiss(a.frontChatTab()))
	// Lay the card out so its answers record where they landed.
	rows := a.tabCloseRows(a.width)
	if len(rows) != 3 || len(a.tabClose.spans) != 3 {
		t.Fatalf("the card drew %d rows and %d answers", len(rows), len(a.tabClose.spans))
	}
	stopSpan := a.tabClose.spans[tabCloseStopAt]

	// The card sits in the guard's slot; the frame is what knows where that is.
	_ = a.View()
	row := -1
	for y := 0; y < a.height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeTabClose && mark.index == 1 {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("the frame drew no pressable row for the card")
	}
	var answered tea.Cmd
	if !a.tabClosePress(stopSpan.from, row, &answered) {
		t.Fatal("a press on the stop answer was not taken")
	}
	drain(t, a, answered)
	if a.closingTab() {
		t.Fatal("the press did not answer the card")
	}
	if first.stops == 0 {
		t.Fatal("the press on stop work did not stop anything")
	}
}
