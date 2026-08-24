package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// BARGE-IN: ONE GESTURE FOR "STOP — I MEANT THIS INSTEAD".
//
// The thing being pinned here is a pair of meanings on one keyboard, and the
// dangerous half of the pair is the one that ALREADY WORKED. Plain enter over a
// running answer parks the sentence and waits (park.go); every test in the
// first block below exists to make sure the chord beside it never quietly
// became a second way to do that, or — very much worse — that enter never
// quietly became a way to stop a turn.
//
// Everything asserted is what a person would see or what the session was
// actually told: the agent's stop count, the words it received, the order the
// transcript ended up in, the line under the box.

// The draft is put in the box with [typeInto] (palette_test.go) rather than
// with [typeLine], which sends: the state this whole gesture is about is a
// sentence that has NOT been committed yet.

// bargeable is a streaming turn on a terminal that CAN spell the chord. The
// enhancement message is delivered rather than the field set, so the wiring
// from the terminal's answer to the feature's existence is under test too.
func bargeable(t *testing.T, first string) (*app, *fakeAgent) {
	t.Helper()
	a, agent := streaming(t, first)
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 1})
	if !a.keysDisambiguated {
		t.Fatal("the terminal's answer did not reach the surface")
	}
	return a, agent
}

// ── the safe default is sacred ──────────────────────────────────────────────

// PLAIN ENTER STILL ONLY WAITS. It parks, it does not send, and above all it
// does not stop the answer — on a terminal that can spell the chord just as
// much as on one that cannot, because the chord's existence must change nothing
// about the key beside it.
func TestPlainEnterOverAnAnswerStillOnlyWaits(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeLine(t, a, "no, the other file")

	if agent.stops != 0 {
		t.Fatalf("plain enter stopped the answer: %d stops", agent.stops)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("plain enter sent something: %q", agent.sent)
	}
	if len(a.parks) != 1 || a.parks[0].text != "no, the other file" {
		t.Fatalf("plain enter did not park the sentence: %+v", a.parks)
	}
	if a.state != stateWorking {
		t.Fatalf("state = %v, want the turn still running", a.state)
	}
}

// ── the chord ───────────────────────────────────────────────────────────────

// THE WHOLE GESTURE: the answer stops and the sentence opens the next turn.
func TestTheChordStopsTheAnswerAndSendsWhatWasTyped(t *testing.T) {
	a, agent := bargeable(t, "the first paragraph of the wrong answer. ")
	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey))

	if agent.stops != 1 {
		t.Fatalf("the chord did not stop the answer: %d stops", agent.stops)
	}
	if a.state != stateInterrupted {
		t.Fatalf("state = %v, want interrupted", a.state)
	}
	// The box is clear and the sentence is on the queue the close drains — it
	// has not been handed to the session yet, because the turn it is replacing
	// has not ended yet.
	if !a.input.empty() {
		t.Fatalf("the chord left the draft in the box: %q", a.input.String())
	}
	if len(agent.sent) != 1 {
		t.Fatalf("the chord sent before the turn ended: %q", agent.sent)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("the chord did not send the sentence: %q", agent.sent)
	}
}

// ORDERING HONESTY, READ OFF THE TRANSCRIPT. What the interrupted turn already
// said is kept, and the person's message opens the NEXT turn under it — no new
// claims, and nothing spliced into the answer it stopped.
func TestAfterTheChordTheTranscriptReadsInTheOrderItHappened(t *testing.T) {
	a, agent := bargeable(t, "the first paragraph of the wrong answer. ")
	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey))
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen}, frameMsg{})

	// The partial answer survived the stop whole — that is the engine's own
	// interrupted-turn rendering and this gesture must not disturb it.
	blocks := assistantBlocks(a)
	if len(blocks) != 1 || !strings.Contains(blocks[0], "first paragraph of the wrong answer") {
		t.Fatalf("the interrupted turn did not keep what it had said: %q", blocks)
	}
	// And the person's line is the LAST block, under it.
	last := a.entries[len(a.entries)-1]
	if last.kind != entryUser || last.text != "no, the other file" {
		t.Fatalf("the sentence did not open the next turn: %v %q", last.kind, last.text)
	}
	body := plain(frame(a))
	answerAt := strings.Index(body, "wrong answer")
	saidAt := strings.LastIndex(body, "no, the other file")
	if answerAt < 0 || saidAt < 0 || saidAt < answerAt {
		t.Fatalf("the sentence is not drawn below the answer it stopped:\n%s", body)
	}
}

// THE SEND WAITS FOR THE TURN'S TRUE END. Nothing reaches the session between
// the interrupt and the stream's close, which is the whole of why there is no
// race with a turn that is still in flight.
func TestTheChordSendsNothingUntilTheStoppedTurnHasActuallyClosed(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey))

	// Several frames go by with the stream still open — the session has been
	// told to stop and has not finished stopping.
	drive(t, a, frameMsg{}, frameMsg{}, frameMsg{})
	if len(agent.sent) != 1 {
		t.Fatalf("the sentence went before the turn closed: %q", agent.sent)
	}
	if len(a.parks) != 1 {
		t.Fatalf("the sentence is not waiting on the queue: %+v", a.parks)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 {
		t.Fatalf("the sentence did not go at the close: %q", agent.sent)
	}
}

// ── the capability law ──────────────────────────────────────────────────────

// ON A TERMINAL THAT CANNOT SPELL THE CHORD THE FEATURE IS ABSENT, NOT BROKEN —
// and absent includes the advertisement. Nothing happens, nothing is lost, and
// the line under the box never names a key this terminal will never deliver.
func TestTheChordIsAbsentOnATerminalThatCannotSpellIt(t *testing.T) {
	// No tea.KeyboardEnhancementsMsg: this terminal never answered the query.
	a, agent := streaming(t, "reading the tree. ")
	typeInto(t, a, "no, the other file")

	if a.bargeOffered() {
		t.Fatal("the chord is offered on a terminal that never said it could send it")
	}
	if got := a.hintWord(); got != "esc interrupt" {
		t.Fatalf("hint = %q, want the plain interrupt where the chord cannot work", got)
	}
	if strings.Contains(plain(frame(a)), bargeKey) {
		t.Fatal("the frame named a chord this terminal cannot deliver")
	}

	drive(t, a, key(bargeKey))
	if agent.stops != 0 {
		t.Fatalf("the absent chord stopped the answer: %d stops", agent.stops)
	}
	// AND IT COST THE PERSON NOTHING. The draft is byte for byte what it was —
	// the chord carries no text, so falling through the router types nothing.
	if got := a.input.String(); got != "no, the other file" {
		t.Fatalf("the absent chord changed the draft: %q", got)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the absent chord parked something: %+v", a.parks)
	}
}

// ── discoverability ─────────────────────────────────────────────────────────

// THE HINT TEACHES BOTH MEANINGS, AND ONLY IN THE ONE STATE THEY ARE BOTH TRUE.
func TestTheHintTeachesBothMeaningsOnlyWhileThereIsSomethingToSend(t *testing.T) {
	a, _ := bargeable(t, "reading the tree. ")

	// A running turn with an EMPTY box: nothing to send, so the slot keeps the
	// plain interrupt. This is the emptiness law on the line itself.
	if got := a.hintWord(); got != "esc interrupt" {
		t.Fatalf("an empty box while working = %q, want the plain interrupt", got)
	}

	typeInto(t, a, "no, the other file")
	got := a.hintWord()
	if !strings.Contains(got, "enter") || !strings.Contains(got, bargeKey) {
		t.Fatalf("hint = %q, want both meanings named", got)
	}
	if got != bargeHint {
		t.Fatalf("hint = %q, want %q", got, bargeHint)
	}

	// And at rest there is nothing to stop, so the line is gone again.
	drive(t, a, key(bargeKey))
	a.state = stateIdle
	if strings.Contains(a.hintWord(), bargeKey) {
		t.Fatalf("the chord is still advertised at rest: %q", a.hintWord())
	}
}

// ONE SOURCE OF TRUTH FOR ONE ACT. The block above the box and the line under
// it say the same three words about the same thing, because `esc stops and
// sends` and `shift+enter stops and sends` are one sentence with two keys in it.
func TestTheChordAndTheParkedBlockSayTheSameThreeWords(t *testing.T) {
	if !strings.HasSuffix(parkedHint[1], bargeSendWord) {
		t.Fatalf("the parked block no longer says %q: %q", bargeSendWord, parkedHint[1])
	}
	if !strings.HasSuffix(bargeHint, bargeKey+" "+bargeSendWord) {
		t.Fatalf("the hint no longer names the chord and what it does: %q", bargeHint)
	}
}

// ── the impatient user ──────────────────────────────────────────────────────

// SOMEBODY TYPES DURING A STREAM, HITS ENTER THREE TIMES, THEN THE CHORD.
//
// The three presses are one message and two keys pressed at a box that is
// already empty, and the chord after them has nothing left to say. What must
// come out of that is: ONE message queued, ONE turn spent on it, no second copy
// of it, and no stop that the person did not ask for — because the chord over
// an empty box is the absent key, not a bare interrupt.
func TestThreeEntersThenTheChordSendTheMessageOnceAndLoseNothing(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeInto(t, a, "no, the other file")

	drive(t, a, key("enter"), key("enter"), key("enter"))
	if len(a.parks) != 1 {
		t.Fatalf("%d messages queued, want the one that was typed: %+v", len(a.parks), a.parks)
	}
	if a.followWaiting() != 0 {
		t.Fatalf("the count above the box claims %d queued", a.followWaiting())
	}

	drive(t, a, key(bargeKey))
	if agent.stops != 0 {
		t.Fatalf("the chord over an empty box stopped the answer: %d stops", agent.stops)
	}
	if len(a.parks) != 1 {
		t.Fatalf("the chord over an empty box disturbed the queue: %+v", a.parks)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("the message was not sent exactly once: %q", agent.sent)
	}
	if len(a.parks) != 0 {
		t.Fatalf("%d messages left over", len(a.parks))
	}
}

// AND THE OTHER SHAPE OF IMPATIENCE: the chord pressed three times over one
// sentence. The first takes the message and stops the turn; the two after it
// are the absent key over an empty box, and neither of them spends a second
// stop on a session that is already stopping.
func TestTheChordPressedThreeTimesStopsOnceAndSendsOnce(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeInto(t, a, "no, the other file")

	drive(t, a, key(bargeKey), key(bargeKey), key(bargeKey))
	if agent.stops != 1 {
		t.Fatalf("%d stops, want the one the person asked for", agent.stops)
	}
	if len(a.parks) != 1 {
		t.Fatalf("%d messages queued, want one: %+v", len(a.parks), a.parks)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 {
		t.Fatalf("the sentence went more than once: %q", agent.sent)
	}
}

// A QUEUED FOLLOW-UP AND A BARGE-IN IN ONE TURN. ctrl+q hands its message to
// the SESSION, and the session drops its queue on an interrupt — so the surface
// must say the follow-up is gone and must still send the sentence that did the
// stopping. Anything else is a count above the box for a turn that will never
// run, or a message silently eaten by the stop.
func TestTheChordDropsTheQueuedFollowUpAndStillSendsItsOwnSentence(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeInto(t, a, "and then write the tests")
	drive(t, a, key("ctrl+q"))
	if a.followWaiting() != 1 {
		t.Fatalf("ctrl+q did not queue: %d waiting", a.followWaiting())
	}

	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey))

	if a.followWaiting() != 0 {
		t.Fatalf("the interrupt left %d follow-ups counted", a.followWaiting())
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("the barged sentence did not go: %q", agent.sent)
	}
}

// ── what the chord refuses to stop for ──────────────────────────────────────

// A SLASH COMMAND IS SAID TO THIS SURFACE, NOT TO THE MODEL, so it runs at once
// and the running turn is left alone. A chord that stopped a turn on its way to
// opening a panel would be an irreversible act as a side effect of a
// navigational one.
func TestTheChordDoesNotStopATurnToRunASlashCommand(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	typeInto(t, a, "/help")
	drive(t, a, key(bargeKey))

	if agent.stops != 0 {
		t.Fatalf("the chord stopped the answer to run a command: %d stops", agent.stops)
	}
	if len(a.parks) != 0 {
		t.Fatalf("a slash command was queued as a message: %+v", a.parks)
	}
	if a.state != stateWorking {
		t.Fatalf("state = %v, want the turn still running", a.state)
	}
}

// ── one ideology, every chat surface ────────────────────────────────────────

// INSIDE A TASK ROOM THE GESTURE IS HONESTLY ABSENT, and the reason is that
// there is nothing there for it to mean: the box steers a NODE, enter sends it
// there and then with no queue to jump, and the room's own way of ending work
// is `x` and a card that asks first (room.go, stop.go). So the chord must not
// reach past the room and stop the conversation's turn behind it.
func TestTheChordIsAbsentInsideATaskRoom(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	a.room = &taskRoom{id: 3, title: "port the parser", unfolded: map[int]bool{}, live: -1, think: -1}
	typeInto(t, a, "use the other file")

	if a.bargeOffered() {
		t.Fatal("the chord is offered inside a room")
	}
	if strings.Contains(a.hintWord(), bargeKey) {
		t.Fatalf("the room's hint slot named the chord: %q", a.hintWord())
	}

	drive(t, a, key(bargeKey))
	if agent.stops != 0 {
		t.Fatalf("the chord reached past the room and stopped the conversation: %d stops", agent.stops)
	}
	if got := a.input.String(); got != "use the other file" {
		t.Fatalf("the chord disturbed the room's draft: %q", got)
	}
}

// ── the chord at rest ───────────────────────────────────────────────────────

// WITH NOTHING RUNNING THE CHORD DOES NOTHING AT ALL. There is no turn to stop,
// plain enter already sends, and a chord that quietly became a second send
// would be a key teaching a gesture nobody needs — and one press away from
// sending a half-typed sentence.
func TestTheChordAtRestDoesNothingAtAll(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 1})
	typeInto(t, a, "a half-finished sentence")

	drive(t, a, key(bargeKey))
	if got := a.input.String(); got != "a half-finished sentence" {
		t.Fatalf("the chord at rest changed the draft: %q", got)
	}
	if a.state == stateWorking {
		t.Fatal("the chord at rest started a turn")
	}
}

// And the enhancement report is read for what it says rather than merely for
// having arrived: a terminal that answered with no flags at all cannot
// disambiguate, and the feature is absent there too.
func TestATerminalThatReportsNoFlagsCannotSpellTheChord(t *testing.T) {
	a, _ := streaming(t, "reading the tree. ")
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 0})
	typeInto(t, a, "no, the other file")

	if a.keysDisambiguated || a.bargeOffered() {
		t.Fatal("an empty enhancement report was read as a capability")
	}
}

// A last belt-and-braces reading of the same law from the other side: the
// events the surface is driven with are the real ones, so a scripted turn that
// never streams still behaves.
var _ = session.EventTextDelta
