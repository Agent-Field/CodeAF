package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// WHAT A MESSAGE TYPED WHILE AN ANSWER IS STILL COMING DOES.
//
// The defect these pin came off a live session: an answer was streaming, the
// person typed "do much more of a deep resaerch please", pressed enter — and
// their sentence was drawn INTO THE MIDDLE of the reply, between two paragraphs
// of the same flowing answer. Two things were wrong and each is pinned on its
// own here, because either could come back without the other:
//
//	the render order  a person's line must never cut a streamed block in two
//	the key           enter waits for the answer instead of sending into it
//
// Every assertion below reads what a person would see or what the session was
// actually told, never the shape of the code between them.

// streaming is an app with one turn open and one paragraph of it already on
// screen. The agent holds the live channel, so it behaves exactly as a real one
// does mid-turn.
func streaming(t *testing.T, first string) (*app, *fakeAgent) {
	t.Helper()
	agent := &fakeAgent{turns: [][]session.Event{{text(session.EventTextDelta, first)}}}
	a := newTestApp(agent)
	typeLine(t, a, "what is in this repository")
	drive(t, a, frameMsg{})
	return a, agent
}

// assistantBlocks is every assistant block in the transcript, in order.
func assistantBlocks(a *app) []string {
	var out []string
	for i := range a.entries {
		if a.entries[i].kind == entryAssistant {
			out = append(out, a.entries[i].text)
		}
	}
	return out
}

// kindsAfter is the entry kinds from the first assistant block onward, which is
// the ordering the splice broke.
func kindsAfter(a *app) []entryKind {
	var out []entryKind
	started := false
	for i := range a.entries {
		if a.entries[i].kind == entryAssistant {
			started = true
		}
		if started {
			out = append(out, a.entries[i].kind)
		}
	}
	return out
}

// ── the render order ────────────────────────────────────────────────────────

// THE SPLICE, PINNED. A person's line put into the transcript while a reply is
// streaming must leave that reply WHOLE: one block, both paragraphs, and the
// line below it. This goes through [app.submit] directly rather than through
// enter, because enter now parks — the render-order fix has to stand on its own
// for every other door onto the transcript (a picked harness, a room's steer).
func TestAPersonsLineNeverCutsAStreamedAnswerInTwo(t *testing.T) {
	a, _ := streaming(t, "the first paragraph of the answer. ")
	a.submit("do much more of a deep research please")
	a.appendText("and the second paragraph of the same answer.")
	drive(t, a, frameMsg{})

	blocks := assistantBlocks(a)
	if len(blocks) != 1 {
		t.Fatalf("the streamed answer was cut into %d blocks: %q", len(blocks), blocks)
	}
	if !strings.Contains(blocks[0], "first paragraph") || !strings.Contains(blocks[0], "second paragraph") {
		t.Fatalf("the two paragraphs are not in one block: %q", blocks[0])
	}
	kinds := kindsAfter(a)
	if len(kinds) != 2 || kinds[0] != entryAssistant || kinds[1] != entryUser {
		t.Fatalf("the person's line did not land after the answer: %v", kinds)
	}
	// And the frame agrees with the entries: the sentence is below the answer,
	// which is the thing a reader complained about.
	body := plain(frame(a))
	answerAt := strings.Index(body, "second paragraph")
	saidAt := strings.Index(body, "deep research please")
	if answerAt < 0 || saidAt < 0 || saidAt < answerAt {
		t.Fatalf("the sentence is not drawn below the answer:\n%s", body)
	}
}

// The same rule over a room's own transcript, which is a second deck with the
// same shape (room.go's [app.roomSaid]).
func TestARoomsSteerNeverCutsTheNodesAnswerInTwo(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.room = &taskRoom{id: 3, title: "port the parser", unfolded: map[int]bool{}, live: -1, think: -1}
	a.roomAppend(entry{kind: entryAssistant, text: "the first half. "})
	a.room.live = 0

	a.roomSaid(entry{kind: entryUser, text: "use the other file"})
	a.room.entries[a.room.live].text += "the second half."

	if n := len(a.room.entries); n != 2 {
		t.Fatalf("the room holds %d blocks, want the answer and the line", n)
	}
	if got := a.room.entries[0].text; got != "the first half. the second half." {
		t.Fatalf("the node's answer was cut in two: %q", got)
	}
	if a.room.entries[1].kind != entryUser {
		t.Fatalf("the person's line is not the last block: %v", a.room.entries[1].kind)
	}
}

// ── parking ─────────────────────────────────────────────────────────────────

// ENTER WHILE AN ANSWER IS COMING DOES NOT SEND. The session is told nothing,
// the transcript grows nothing, and the words are held where the person can see
// them.
func TestEnterWhileAnAnswerIsStreamingParksTheMessage(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	before := len(agent.sent)
	typeLine(t, a, "do much more of a deep research please")

	if len(agent.sent) != before {
		t.Fatalf("the message was sent mid-answer: %q", agent.sent)
	}
	if len(a.parks) != 1 || a.parks[0].text != "do much more of a deep research please" {
		t.Fatalf("nothing was parked: %+v", a.parks)
	}
	for i := range a.entries {
		if a.entries[i].kind == entryUser && strings.Contains(a.entries[i].text, "deep research") {
			t.Fatal("a parked message was written into the transcript before it was sent")
		}
	}
	// The box is clear again, so the person can keep typing.
	if !a.input.empty() {
		t.Fatalf("the box kept the parked sentence: %q", a.input.String())
	}
}

// AND IT IS DRAWN, in the person's own hue, with a dim line saying what it is
// waiting for and which keys change that.
func TestTheParkedBlockSaysWhatItIsWaitingForAndWhichKeysMoveIt(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	// A frame with the room for the whole line, so the trim is not what is under
	// test here — it has a test of its own directly below.
	a.width = 90
	typeLine(t, a, "do much more of a deep research please")
	drive(t, a, frameMsg{})

	body := plain(frame(a))
	if !strings.Contains(body, "do much more of a deep research please") {
		t.Fatalf("the parked message is not on screen:\n%s", body)
	}
	for _, want := range parkedHint {
		if !strings.Contains(body, want) {
			t.Fatalf("the parked block did not say %q:\n%s", want, body)
		}
	}
	// The sentence wears the accent, because it is the person's own — the same
	// law the transcript's own user block is under (render.go).
	drawn := a.parkedRows(a.width)
	if len(drawn) < 2 || !strings.Contains(drawn[0], sgr256(hueAccent)) {
		t.Fatalf("the parked sentence is not in the person's hue: %q", drawn)
	}
	if !strings.Contains(drawn[len(drawn)-1], sgr256(hueDim)) {
		t.Fatalf("the line under the block is not dim: %q", drawn[len(drawn)-1])
	}
}

// ONE MESSAGE IS NOT COUNTED. The emptiness law over a number that says nothing
// the block above it does not.
func TestOneWaitingMessageIsNotCounted(t *testing.T) {
	if got := parkedWord(1, 200); strings.HasPrefix(got, "1 ") {
		t.Fatalf("one waiting message was counted: %q", got)
	}
	if got := parkedWord(2, 200); !strings.HasPrefix(got, "2 wait for this answer") {
		t.Fatalf("two waiting messages were not counted: %q", got)
	}
}

// A narrow frame drops the pieces from the right and never wraps the line.
func TestTheParkedLineTrimsFromTheRightOnANarrowFrame(t *testing.T) {
	full := parkedWord(1, 200)
	if full != strings.Join(parkedHint, " · ") {
		t.Fatalf("the whole line is not the whole hint: %q", full)
	}
	tight := parkedWord(1, ansi.StringWidth(parkedHint[0]+" · "+parkedHint[1]))
	if tight != parkedHint[0]+" · "+parkedHint[1] {
		t.Fatalf("the line did not drop its last piece: %q", tight)
	}
	if got := parkedWord(1, 4); got != parkedHint[0] {
		t.Fatalf("the narrowest line is not what the message is doing: %q", got)
	}
}

// ── the answer finishing ────────────────────────────────────────────────────

// THE WHOLE POINT: it goes on its own, as a turn, the moment the answer is over.
func TestAParkedMessageSendsItselfWhenTheAnswerIsFinished(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	typeLine(t, a, "do much more of a deep research please")

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	if len(agent.sent) != 2 || agent.sent[1] != "do much more of a deep research please" {
		t.Fatalf("the parked message was not sent when the answer ended: %q", agent.sent)
	}
	if len(a.parks) != 0 {
		t.Fatalf("%d messages are still parked after the answer ended", len(a.parks))
	}
	// It is an ordinary message from here: it is in the transcript, in the
	// person's own block, and it opened a turn.
	last := a.entries[len(a.entries)-1]
	if last.kind != entryUser || last.text != "do much more of a deep research please" {
		t.Fatalf("the sent message is not the last block: %v %q", last.kind, last.text)
	}
	if a.state != stateWorking {
		t.Fatalf("the parked message did not open a turn: %v", a.state)
	}
}

// TWO MESSAGES GO ONE AT A TIME, in the order they were typed — the session's
// own law for its follow-up queue, said about this one.
func TestParkedMessagesGoOneAtATimeInTheOrderTheyWereTyped(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	typeLine(t, a, "first correction")
	typeLine(t, a, "second correction")
	if len(a.parks) != 2 {
		t.Fatalf("%d messages parked, want two", len(a.parks))
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 || agent.sent[1] != "first correction" {
		t.Fatalf("the older message did not go first: %q", agent.sent)
	}
	if len(a.parks) != 1 {
		t.Fatalf("both messages went at once: %d left", len(a.parks))
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 3 || agent.sent[2] != "second correction" {
		t.Fatalf("the second message did not go at the next turn's end: %q", agent.sent)
	}
}

// ── esc ─────────────────────────────────────────────────────────────────────

// ESC WITH A MESSAGE WAITING STOPS THE ANSWER AND SENDS IT.
func TestEscWithAMessageWaitingStopsTheAnswerAndSendsItNow(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	typeLine(t, a, "no, the other file")

	drive(t, a, key("esc"))
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	if agent.stops != 1 {
		t.Fatalf("esc did not stop the answer: %d stops", agent.stops)
	}
	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("esc did not send the waiting message: %q", agent.sent)
	}
}

// ESC WITH NOTHING WAITING IS EXACTLY WHAT IT WAS: a stop, and no message.
func TestEscWithNothingWaitingIsStillJustTheInterrupt(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	drive(t, a, key("esc"))

	if agent.stops != 1 {
		t.Fatalf("esc did not stop the answer: %d stops", agent.stops)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("esc sent something nobody typed: %q", agent.sent)
	}
	if a.state != stateInterrupted {
		t.Fatalf("state = %v, want interrupted", a.state)
	}
}

// And while something is waiting, the line under the box says what the next esc
// will do rather than the plain stop it used to promise.
func TestTheHintSaysWhatEscDoesWhileAMessageIsWaiting(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	if got := a.hintWord(); got != "esc interrupt" {
		t.Fatalf("a plain running turn = %q, want the interrupt", got)
	}
	typeLine(t, a, "no, the other file")
	if got := a.hintWord(); got != "esc stops and sends" {
		t.Fatalf("hint = %q, want what esc now does", got)
	}
}

// ── editing ─────────────────────────────────────────────────────────────────

// ↑ OVER AN EMPTY BOX TAKES THE MESSAGE BACK. It is a typo you are fixing, so
// what comes back is exactly what went in, and the block goes with it.
func TestUpPullsAWaitingMessageBackIntoTheBox(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	typeLine(t, a, "do much more of a deep resaerch please")

	drive(t, a, key("up"))
	if got := a.input.String(); got != "do much more of a deep resaerch please" {
		t.Fatalf("the box holds %q, want the parked sentence back", got)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the message is still parked as well as in the box: %+v", a.parks)
	}
	if strings.Contains(plain(frame(a)), parkedHint[0]) {
		t.Fatalf("the block is still drawn after being taken back:\n%s", plain(frame(a)))
	}
	// And enter parks the edited sentence again, once — not twice, which is what
	// a ↑ that walked the history instead would have produced.
	drive(t, a, key("enter"))
	if len(a.parks) != 1 || a.parks[0].text != "do much more of a deep resaerch please" {
		t.Fatalf("re-parking the edited sentence gave %+v", a.parks)
	}
}

// ↑ WITH SOMETHING TYPED IS STILL THE HISTORY WALK. The parked message is only
// read over an empty box, which is the same rule every other ↑ meaning is under.
func TestUpWithASentenceInTheBoxDoesNotTakeTheWaitingMessage(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	typeLine(t, a, "the parked one")
	typeInto(t, a, "half a thought")

	drive(t, a, key("up"))
	if len(a.parks) != 1 {
		t.Fatalf("↑ over a sentence took the parked message: %+v", a.parks)
	}
}

// A CLICK ON THE BLOCK IS THE SAME GESTURE, because the block prints it.
func TestClickingAWaitingMessageTakesItBackIntoTheBox(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	typeLine(t, a, "no, the other file")
	drive(t, a, frameMsg{})

	y := parkedRowY(t, a)
	drive(t, a, press(2, y))
	if got := a.input.String(); got != "no, the other file" {
		t.Fatalf("the click put %q in the box", got)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the click left the message parked: %+v", a.parks)
	}
}

// The dim line under the block belongs to no message, so a press on it does
// nothing rather than taking a sentence the pointer was not over.
func TestClickingTheLineUnderTheBlockTakesNothing(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	typeLine(t, a, "no, the other file")
	drive(t, a, frameMsg{})

	drive(t, a, press(2, parkedRowY(t, a)+1))
	if len(a.parks) != 1 {
		t.Fatalf("a press on the dim line took the message: %+v", a.parks)
	}
}

// parkedRowY is the screen row the first parked message is drawn on.
func parkedRowY(t *testing.T, a *app) int {
	t.Helper()
	_, height := a.size()
	for y := 0; y < height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeParked && mark.index == 0 {
			return y
		}
	}
	t.Fatal("the parked block is not on any row of the frame")
	return -1
}

// ── nothing changes at rest ─────────────────────────────────────────────────

// AN IDLE ENTER STILL SENDS AT ONCE. Parking is a thing that happens to a
// message typed at a turn, and nothing else.
func TestEnterWithNothingRunningStillSendsAtOnce(t *testing.T) {
	agent := &fakeAgent{turns: [][]session.Event{{text(session.EventTextDelta, "hello")}}}
	a := newTestApp(agent)
	typeLine(t, a, "what is in this repository")

	if len(a.parks) != 0 {
		t.Fatalf("an idle message was parked: %+v", a.parks)
	}
	if len(agent.sent) != 1 || agent.sent[0] != "what is in this repository" {
		t.Fatalf("an idle message was not sent: %q", agent.sent)
	}
	if a.parkedHeight() != 0 {
		t.Fatal("the parked block took rows in a conversation with nothing parked")
	}
}

// ── the wait for the provider ───────────────────────────────────────────────

// PARKING DOES NOT TOUCH THE PROVIDER WAIT. A message held above the box says
// nothing about whether a request is outstanding, so the anchor the wait is
// measured from must survive it untouched (app.go's [app.awaited]).
func TestParkingLeavesTheProviderWaitWhereItWas(t *testing.T) {
	a, _ := streaming(t, "")
	anchored := time.Now().Add(-12 * time.Second)
	a.awaited = anchored
	typeLine(t, a, "do much more of a deep research please")

	if !a.awaited.Equal(anchored) {
		t.Fatalf("parking moved the wait's anchor to %v, want %v", a.awaited, anchored)
	}
	line, ok := a.ellipsis()
	if !ok || !strings.Contains(plain(line), "waiting") {
		t.Fatalf("the wait stopped being drawn while a message was parked: %q", plain(line))
	}
}

// And the turn a parked message opens anchors its own wait, exactly as a typed
// one does: the request went out with nothing back from it.
func TestTheTurnAParkedMessageOpensAnchorsItsOwnWait(t *testing.T) {
	a, agent := streaming(t, "reading the tree. ")
	typeLine(t, a, "do much more of a deep research please")
	a.awaited = time.Time{}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	if a.awaited.IsZero() {
		t.Fatal("the turn a parked message opened is waiting on nothing")
	}
}

// ── the conversation being replaced ─────────────────────────────────────────

// A MESSAGE PARKED AT A CONVERSATION THAT IS BEING CLOSED IS SAID OUT LOUD
// rather than dropped in silence: the person typed those words.
func TestDroppingAWaitingMessageSaysSo(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.parks = []parked{{text: "one"}, {text: "two"}}
	a.dropParked()

	last := plain(a.entries[len(a.entries)-1].text)
	if !strings.Contains(last, "2 waiting messages dropped") {
		t.Fatalf("the drop said %q", last)
	}
	if len(a.parks) != 0 {
		t.Fatal("the messages were not dropped")
	}
}
