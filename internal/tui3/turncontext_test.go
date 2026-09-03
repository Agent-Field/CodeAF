package tui3

// THE TURN SAYS WHAT IT IS PART OF (turncontext.go).
//
// A sentence typed into a subharness being designed can rewrite that page, and
// the transcript drew it exactly as it draws a remark made to nobody in
// particular. These tests hold the three halves of the fix: the mark is drawn on
// a turn the engine routed into a named context, it is drawn on NOTHING else,
// and every word of it comes off the engine rather than out of this package.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// designRoomAt is a surface standing in the room of a node the engine has named
// a working context for — the state a person is in while a subharness is being
// designed. The context word is the ENGINE'S, handed in whole, because that is
// the one thing this mechanism is not allowed to invent.
func designRoomAt(t *testing.T, context string) *app {
	t.Helper()
	agent := &designingRoomAgent{roomFake: &roomFake{
		taskFake: &taskFake{fakeAgent: &fakeAgent{model: "m"}},
		lanes:    map[uint64]chan session.Event{},
	}}
	a := newTestApp(agent)
	a.width, a.height = 100, 30
	a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
		Kind:    session.TaskKindHarness,
		Doing:   session.HarnessPhaseAsking,
		Context: context,
	}))
	a.room = a.newRoom(4, "harness · flake triage")
	return a
}

// THE MARK IS DRAWN, AND IT IS THE ENGINE'S OWN WORD. Nothing is retyped here:
// what the row must carry is built out of the string the notice arrived with and
// the divider the file spells once.
func TestALineSteeredIntoADesignNamesTheContextItRanIn(t *testing.T) {
	const word = "designing subharness flake-triage"
	a := designRoomAt(t, word)
	a.input.setText("use models dynamically in it")
	a.steer()

	drawn := roomText(a)
	if !strings.Contains(drawn, "use models dynamically in it"+turnContextSep+word) {
		t.Fatalf("the steered line does not name the context it ran in:\n%s", drawn)
	}
}

// AND IT IS FROZEN ON THE BLOCK. What the surface drew has to be what the engine
// said at the moment the words were taken, so the block itself is asked rather
// than the frame — a mark re-derived on every paint would re-label a whole
// history the moment somebody walked into a different room.
func TestTheContextIsTakenFromTheSessionAtTheMomentTheTurnStarts(t *testing.T) {
	const word = "designing subharness flake-triage"
	a := designRoomAt(t, word)
	a.input.setText("make it run the linter first")
	a.steer()

	said := a.room.entries[len(a.room.entries)-1]
	// It is an ELBOW in a room — a correction to work already running, not a new
	// question (steerelbow.go, #252) — and the mark rides it exactly as it rides
	// the person's own block out in the conversation.
	if said.kind != entrySteer {
		t.Fatalf("the last block in the room is %v, want the person's own correction", said.kind)
	}
	if said.context != word {
		t.Fatalf("the block carries %q, want the context the engine published", said.context)
	}
	// AND IT IS THE NODE'S, not something this package assembled: the surface's
	// own answer to "what will the next turn run inside" has to be the same string.
	if got := a.turnContext(); got != word {
		t.Fatalf("turnContext is %q, want the node's own %q", got, word)
	}
}

// THE MARK IS QUIET. It is a fact about where a sentence went, never a summons,
// so it is written in the dim tier and never spends the accent the person's own
// words are painted in (docs/DESIGN-LANGUAGE.md's accent budget).
func TestTheContextMarkIsDimAndNeverSpendsTheAccent(t *testing.T) {
	const word = "designing subharness flake-triage"
	a := designRoomAt(t, word)
	a.input.setText("shorter, two steps")
	a.steer()

	var painted string
	for _, r := range a.roomRows(a.bodyWidth()) {
		if strings.Contains(plain(r.text), word) {
			painted = r.text
			break
		}
	}
	if painted == "" {
		t.Fatalf("no row carries the mark:\n%s", roomText(a))
	}
	if !strings.Contains(painted, a.pal.dim(turnContextSep+word)) {
		t.Fatalf("the mark is not painted in the dim tier:\n%q", painted)
	}
	if strings.Contains(painted, a.pal.accent(word)) {
		t.Fatalf("the mark spent the accent:\n%q", painted)
	}
}

// AN ORDINARY TURN DRAWS NOTHING AT ALL — the emptiness law, which is what makes
// the mark mean something on the turns that do carry it. This is the half that
// would fail loudest if the mechanism ever started guessing.
func TestAnOrdinaryTurnDrawsNoContextMark(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 100, 30
	if got := a.turnContext(); got != "" {
		t.Fatalf("the conversation named a context: %q", got)
	}
	a.said(entry{kind: entryUser, text: "what is in this repo", turn: 1, context: a.turnContext()})

	drawn := taskText(a)
	if !strings.Contains(drawn, "what is in this repo") {
		t.Fatalf("the message is not on the page:\n%s", drawn)
	}
	if strings.Contains(drawn, turnContextSep) {
		t.Fatalf("an ordinary turn drew a context clause:\n%s", drawn)
	}
}

// AND A ROOM ON A NODE THAT NAMES NO CONTEXT DRAWS NOTHING EITHER. An ordinary
// task is work you handed over, not a place you are inside, so the mechanism has
// to stay silent there — otherwise every steered line on the surface would grow a
// clause and the mark would stop being news.
func TestAnOrdinaryTasksRoomNamesNoContext(t *testing.T) {
	a := designRoomAt(t, "")
	a.input.setText("try the other parser")
	a.steer()

	if got := a.turnContext(); got != "" {
		t.Fatalf("an unnamed node named a context: %q", got)
	}
	// The row says what the sending did and NOTHING about where the words went:
	// the delivery receipt is every clause an ordinary node's correction wears.
	if drawn := elbowRowIn(a, "try the other parser"); drawn !=
		glyphSteer+"try the other parser"+steerClauseSep+session.SteerDelivered(false) {
		t.Fatalf("an ordinary node's room drew something other than the receipt: %q", drawn)
	}
}

// THE NAME ARRIVES LATE AND THE MARK TAKES IT. A design says "designing a
// subharness" from the moment it is admitted and names the page the moment one
// exists, both while the node is running — so the update carrying the real name
// has to survive the roster's de-dup, which throws away everything that is not
// news about a node in the state it is already in.
func TestABetterContextNameReachesTheNode(t *testing.T) {
	const first = "designing a subharness"
	const named = "designing subharness flake-triage"
	a := designRoomAt(t, first)
	if got := a.turnContext(); got != first {
		t.Fatalf("the room opened on %q, want %q", got, first)
	}
	a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
		Kind:    session.TaskKindHarness,
		Doing:   session.HarnessPhaseAsking,
		Context: named,
	}))
	if got := a.turnContext(); got != named {
		t.Fatalf("the node kept %q after the page was named, want %q", got, named)
	}
	// AND AN UPDATE THAT SAYS NOTHING ABOUT IT HAS NOT TAKEN THE PERSON OUT OF THE
	// CONTEXT: the word is a fact about the node, kept like its kind and its branch.
	a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
		Kind:  session.TaskKindHarness,
		Doing: session.HarnessPhaseAsking,
	}))
	if got := a.turnContext(); got != named {
		t.Fatalf("a quiet update cleared the context: %q", got)
	}
}

// AND THE HISTORY IS MARKED TOO. The journal records what was said and never
// where it went, so a room opened on a design would otherwise read back as an
// ordinary chat — the exact defect this mechanism exists to close, one session
// later.
func TestARoomsHistoryIsMarkedWithTheContextItHappenedIn(t *testing.T) {
	const word = "designing subharness flake-triage"
	a := designRoomAt(t, word)
	a.room.entries = []entry{
		{kind: entryUser, text: "Design a reusable subharness for this: chase a flaky test", turn: 1},
		{kind: entryAssistant, text: "The page is written.", turn: 1, settled: true},
	}
	a.markRoomContext(a.room)

	if got := a.room.entries[0].context; got != word {
		t.Fatalf("the opening line carries %q, want %q", got, word)
	}
	if got := a.room.entries[1].context; got != "" {
		t.Fatalf("an answer was marked with a context: %q", got)
	}
}
