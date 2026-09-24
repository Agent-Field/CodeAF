package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE SPLICE FROM THE KEYBOARD: "and also this", said into an answer that is
// still coming.
//
// What is pinned here is a pair of doors onto one act and, above all, the two
// keys BESIDE them that must not have changed. Plain enter still only waits and
// `ctrl+shift+enter` still stops — a chord that quietly became a second way to do
// either would be worse than the gesture not existing, because both of those are
// keys people have already learned.
//
// Every assertion is what the session was actually told, what the box holds, or
// what the two lines on the screen say.

// ── the scripted agent's steer ──────────────────────────────────────────────

// Steer is [session.Agent.Steer] as the fake answers it: the words are recorded,
// and what comes back is whatever the test asked for. The default is a channel
// that is already closed, which is the shape of a steer whose turn ended with it
// consumed — the pump reads one closed channel and stops.
func (f *fakeAgent) Steer(words string) (<-chan session.Event, error) {
	f.steered = append(f.steered, words)
	if f.steerErr != nil {
		return nil, f.steerErr
	}
	if f.steerCh != nil {
		ch := f.steerCh
		f.steerCh = nil
		return ch, nil
	}
	done := make(chan session.Event)
	close(done)
	return done, nil
}

// blindAgent is a session with no steer at all — the capability law's own case,
// and the thing [app.steerable] exists to ask. It wraps the fake by VALUE
// forwarding rather than embedding the pointer, so the method set genuinely
// lacks Steer.
type blindAgent struct{ Agent }

// steerable is a streaming turn on a session that can steer and a terminal that
// can spell the chord. The enhancement message is delivered rather than the
// field set, so the wiring from the terminal's answer to the advertisement is
// under test too — [bargeable]'s own arrangement (bargein_test.go).
func steerableTurn(t *testing.T, first string) (*app, *fakeAgent) {
	t.Helper()
	a, agent := streaming(t, first)
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 1})
	if !a.keysDisambiguated {
		t.Fatal("the terminal's answer did not reach the surface")
	}
	return a, agent
}

// wirePress is one escape sequence as the real decoder reads it, checked against
// the name this surface binds it under on the way past. It is
// inputguard_test.go's idiom, and it exists for that file's reason: every key
// table in this package is written in the spelling ultraviolet's decoder
// produces, so a binding can be syntactically perfect and permanently
// unreachable with no test failing.
func wirePress(t *testing.T, seq, want string) tea.KeyPressMsg {
	t.Helper()
	var decoder uv.EventDecoder
	n, event := decoder.Decode([]byte(seq))
	press, ok := event.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("%q decoded to %#v, want a key press", seq, event)
	}
	if n != len(seq) {
		t.Fatalf("%q was read %d bytes deep, want %d", seq, n, len(seq))
	}
	if got := uv.Key(press).String(); got != want {
		t.Fatalf("%q arrives as %q, but this surface binds %q", seq, got, want)
	}
	return tea.KeyPressMsg(press)
}

func parkLine(t *testing.T, a *app, line string) {
	t.Helper()
	typeInto(t, a, line)
	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper))
}

// ── the wire ────────────────────────────────────────────────────────────────

// cmd+enter TRAVELS BY TWO ROADS AND BOTH OF THEM ARRIVE.
//
// This is the test the whole binding hangs on. A modified enter reaches
// ultraviolet through one of two readers and they disagree about what the ninth
// modifier is called: the kitty protocol's `CSI 13;9u` is read against the
// kitty table, where bit 8 is `super`, and xterm's modifyOtherKeys form
// `CSI 27;9;13~` is read against the static table, where the ninth column is
// `meta`. One hand, one keystroke, two names — the same split that left cmd+←
// bound as `super+left` and dead on every terminal there is.
//
// So the bytes are driven through the real decoder into the real router, and
// what is asked is that the SESSION was told the sentence.
func TestBothWireSpellingsOfCmdEnterParkTheDraft(t *testing.T) {
	for _, tc := range []struct{ seq, name, road string }{
		{"\x1b[13;9u", steerKeySuper, "the kitty keyboard protocol"},
		{"\x1b[27;9;13~", steerKeyMeta, "xterm's modifyOtherKeys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent := steerableTurn(t, "reading the tree. ")
			typeInto(t, a, "no, the OTHER file")
			drive(t, a, wirePress(t, tc.seq, tc.name))

			if len(agent.steered) != 0 {
				t.Fatalf("%s steered instead of parking: %q", tc.road, agent.steered)
			}
			// NOTHING WAS STOPPED AND NOTHING WAS STARTED. That is the whole
			// difference between this secondary chord and plain enter: the answer
			// is still coming, and no second turn was opened for the correction.
			if agent.stops != 0 {
				t.Fatalf("the park stopped the answer: %d stops", agent.stops)
			}
			if len(agent.sent) != 1 {
				t.Fatalf("the park started a second turn: %q", agent.sent)
			}
			if a.state != stateWorking {
				t.Fatalf("state = %v, want the turn still running", a.state)
			}
			// THE BOX IS CLEARED AND THE MESSAGE IS VISIBLE ABOVE IT. The secondary
			// gesture exists precisely to preserve this waiting choice.
			if !a.input.empty() {
				t.Fatalf("the chord left the draft in the box: %q", a.input.String())
			}
			if len(a.parks) != 1 || a.parks[0].text != "no, the OTHER file" {
				t.Fatalf("the sentence did not wait: %+v", a.parks)
			}
		})
	}
}

// ── at rest, and with nothing to say ────────────────────────────────────────

// AT REST THE CHORD DOES NOTHING AT ALL, which is `ctrl+shift+enter`'s own answer to
// the same question (bargein.go): plain enter already sends, so a second chord
// meaning the same thing would teach a gesture nobody needs — and one that meant
// something else would be a key with two readings a hand cannot tell apart.
func TestAtRestCmdEnterDoesNothingAtAll(t *testing.T) {
	agent, a := wired(nil)
	typeInto(t, a, "what is in this repository")
	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper))

	if len(agent.steered) != 0 {
		t.Fatalf("the chord steered with nothing running: %q", agent.steered)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("the chord sent at rest: %q", agent.sent)
	}
	if got := a.input.String(); got != "what is in this repository" {
		t.Fatalf("the chord disturbed the draft at rest: %q", got)
	}
}

// AND AN EMPTY BOX DOES NOTHING, running or not: there is no sentence to hold
// for the next turn.
func TestCmdEnterOverAnEmptyBoxDoesNothing(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper))

	if len(agent.steered) != 0 {
		t.Fatalf("an empty box steered: %q", agent.steered)
	}
	if len(a.parks) != 0 {
		t.Fatalf("an empty box parked something: %+v", a.parks)
	}
}

// A SESSION THAT CANNOT STEER STILL HAS THE WAITING DOOR. Plain enter and the
// secondary chord both take the ordinary park road instead of advertising a
// capability with nothing behind it.
func TestASessionThatCannotSteerNeitherTakesTheChordNorNamesIt(t *testing.T) {
	agent := &fakeAgent{turns: [][]session.Event{{text(session.EventTextDelta, "reading. ")}}}
	a := newTestApp(blindAgent{Agent: agent})
	typeLine(t, a, "what is in this repository")
	drive(t, a, frameMsg{}, tea.KeyboardEnhancementsMsg{Flags: 1})
	typeInto(t, a, "no, the other file")

	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper))
	if len(agent.steered) != 0 {
		t.Fatalf("a session with no steer was steered: %q", agent.steered)
	}
	if len(a.parks) != 1 || a.parks[0].text != "no, the other file" {
		t.Fatalf("the secondary key did not park the draft: %+v", a.parks)
	}
	// AND THE LINE READS EXACTLY AS IT DID BEFORE THE SPLICE EXISTED. The chord
	// beside it is still true here — the terminal answered, the turn is running,
	// there is a sentence — so this is the whole of what a session without the
	// verb loses: the one clause.
	if got := a.hintWord(); got != parkedHint[1] {
		t.Fatalf("a session with no steer reads %q, want %q", got, parkedHint[1])
	}
}

// ── the primary gesture acts now ───────────────────────────────────────────

// PLAIN ENTER STEERS AND ctrl+shift+enter STILL STOPS.
func TestEnterSteersAndCtrlShiftEnterStillStops(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	typeLine(t, a, "no, the other file")
	if len(agent.steered) != 1 || agent.steered[0] != "no, the other file" {
		t.Fatalf("plain enter did not steer: %q", agent.steered)
	}
	if len(a.parks) != 0 {
		t.Fatalf("plain enter parked the sentence: %+v", a.parks)
	}

	typeInto(t, a, "and the tests")
	drive(t, a, key(bargeKey))
	if len(agent.steered) != 1 {
		t.Fatalf("the barge steered instead of stopping: %q", agent.steered)
	}
	if agent.stops != 1 {
		t.Fatalf("the barge did not stop the answer: %d stops", agent.stops)
	}
}

// ── the arrow over the waiting message ──────────────────────────────────────

// → PROMOTES A WAITING MESSAGE, AND ONLY WHERE IT HAD NO MEANING.
//
// The gesture exists in one narrow state — a turn running, a message parked, an
// empty box — because that is exactly the state in which the arrow was doing
// nothing a person could want. The moment there is a sentence to move a caret
// through, it is the caret's again.
func TestTheArrowPromotesAWaitingMessageOnlyOverAnEmptyBox(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	parkLine(t, a, "do much more of a deep research please")
	if len(a.parks) != 1 {
		t.Fatalf("the message did not park: %+v", a.parks)
	}

	// With a sentence in the box the arrow is the caret's, and the waiting
	// message is left exactly where it is.
	a.input.setText("abc")
	a.input.cursor = 1
	drive(t, a, key("right"))
	if a.input.cursor != 2 {
		t.Fatalf("→ over a sentence did not move the caret: cursor %d", a.input.cursor)
	}
	if len(agent.steered) != 0 {
		t.Fatalf("→ over a sentence steered: %q", agent.steered)
	}
	if len(a.parks) != 1 {
		t.Fatalf("→ over a sentence disturbed the queue: %+v", a.parks)
	}

	// And over an empty box it goes in.
	a.input.reset()
	drive(t, a, key("right"))
	if len(agent.steered) != 1 || agent.steered[0] != "do much more of a deep research please" {
		t.Fatalf("→ did not promote the waiting message: %q", agent.steered)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the promoted message is still waiting: %+v", a.parks)
	}
	if agent.stops != 0 {
		t.Fatalf("the promotion stopped the answer: %d stops", agent.stops)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("the promotion started a second turn: %q", agent.sent)
	}
}

// AND WITH NOTHING WAITING → IS THE NAVIGATION IT HAS ALWAYS BEEN. The promotion
// is read above the step into the work, so this is the assertion that it stands
// down where there is nothing to promote.
func TestTheArrowStillNavigatesWithNothingWaiting(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, key("right"))
	if !a.roomOpen() {
		t.Fatal("→ over an empty box with nothing waiting stopped opening a room")
	}
}

// A MESSAGE THAT CANNOT BE STEERED IS LEFT WAITING, and the line does not offer
// the key. Pictures reach a running turn by their own door, and a sentence
// somebody marked as something to keep true is bound for a different door
// entirely — so both wait and go the way they were always going to go.
func TestAWaitingMessageThatCannotBeSteeredIsLeftAloneAndNotAdvertised(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold parked
	}{
		{"a message with pictures", parked{text: "what is this", chips: []chip{{}}}},
		{"a message marked to keep true", parked{text: "always run the tests", standing: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent := steerableTurn(t, "reading the tree. ")
			a.parks = append(a.parks, tc.hold)

			drive(t, a, key("right"))
			if len(agent.steered) != 0 {
				t.Fatalf("%s was steered: %q", tc.name, agent.steered)
			}
			if len(a.parks) != 1 {
				t.Fatalf("%s left the queue: %+v", tc.name, a.parks)
			}
			if strings.Contains(plain(frame(a)), steerArrowWord) {
				t.Fatalf("the strip offered a key that would decline %s:\n%s", tc.name, plain(frame(a)))
			}
		})
	}
}

// ── the strip's door ────────────────────────────────────────────────────────

// THE LINE PRINTS A GESTURE, SO THE LINE ANSWERS TO IT. `→ steers it in` is
// pressable, which is what lets the clause stay on the line with a sentence in
// the box — the key half is conditional and the pointer half is not, exactly as
// `↑ or click to edit` beside it.
func TestClickingTheStripsSteerWordPromotesTheWaitingMessage(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	a.width = 90
	parkLine(t, a, "do much more of a deep research please")
	drive(t, a, frameMsg{})

	// The frame is what writes the span, exactly as it is for every other
	// right-placed door on this surface.
	frame(a)
	if !a.steerDoor.pressable() {
		t.Fatal("the strip drew the steer clause without recording where it landed")
	}
	row := -1
	for y := 0; y < a.height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeParkedHint {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("the strip's dim line carries no mark for the pointer to find")
	}
	cmd, took := a.steerDoorPress(a.steerDoor.from, row)
	if !took {
		t.Fatal("a press on the steer word was not taken")
	}
	drive(t, a, runCmd(cmd)...)

	if len(agent.steered) != 1 || agent.steered[0] != "do much more of a deep research please" {
		t.Fatalf("the click did not promote the waiting message: %q", agent.steered)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the promoted message is still waiting: %+v", a.parks)
	}
}

// AND A PRESS ON THE REST OF THAT LINE DOES NOTHING. Three of its four clauses
// are statements, and a line where every phrase quietly acted as a door would be
// the surface answering a gesture nobody made.
func TestAPressOnTheRestOfTheStripsDimLineDoesNothing(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	a.width = 90
	parkLine(t, a, "do much more of a deep research please")
	drive(t, a, frameMsg{})
	frame(a)

	row := -1
	for y := 0; y < a.height; y++ {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeParkedHint {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("the strip's dim line carries no mark")
	}
	if _, took := a.steerDoorPress(0, row); took {
		t.Fatal("a press on the first clause acted as the steer door")
	}
	if len(agent.steered) != 0 || len(a.parks) != 1 {
		t.Fatalf("a press off the door moved something: %q %+v", agent.steered, a.parks)
	}
}

// ── the refusal ─────────────────────────────────────────────────────────────

// THE RACE IS ANSWERED BY DELIVERING THE WORDS, NOT BY A NOTE. The turn ended
// between the frame that offered the key and the press, so the engine refuses
// with [session.ErrNothingToSteer] — and what the person asked for was that
// these words be delivered, so they are, the ordinary way.
func TestASteerRefusedBecauseTheTurnEndedFallsBackToTheOrdinarySend(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	agent.steerErr = session.ErrNothingToSteer
	typeInto(t, a, "no, the other file")

	// The turn really does end under the press: the stream closes on the way
	// through, which is the shape of the window this fallback is about.
	drive(t, a, key("enter"))
	if len(agent.steered) != 1 {
		t.Fatalf("the chord did not reach the session: %q", agent.steered)
	}
	// The words are back on the waiting queue, at the head, where the queue's own
	// machinery sends them when the turn ends.
	if len(a.parks) != 1 || a.parks[0].text != "no, the other file" {
		t.Fatalf("the refused steer did not fall back to the queue: %+v", a.parks)
	}
	// AND NOTHING WAS SAID ABOUT IT. Nobody made a mistake, so there is nothing
	// to report.
	if note := plain(frame(a)); strings.Contains(note, "steer failed") {
		t.Fatalf("the refusal was surfaced as an error:\n%s", note)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("the refused steer was never delivered: %q", agent.sent)
	}
}

// AND WITH NOTHING BEING PUMPED IT GOES AT ONCE. There is no close coming to
// drain the queue, so the fallback sends it where it stands — followup.go's own
// argument about a message queued into an idle session.
func TestARefusedSteerWithNothingRunningIsSentImmediately(t *testing.T) {
	agent, a := wired(nil)
	a.parks = nil
	drive(t, a, steeredMsg{words: "no, the other file", err: session.ErrNothingToSteer})

	if len(agent.sent) != 1 || agent.sent[0] != "no, the other file" {
		t.Fatalf("the refused steer was not sent: %q", agent.sent)
	}
	if len(a.parks) != 0 {
		t.Fatalf("the refused steer stayed on the queue: %+v", a.parks)
	}
}

// EVERY OTHER FAILURE IS SAID OUT LOUD, because every other one means something
// a person needs to know.
func TestASteerThatFailedForAnyOtherReasonIsReported(t *testing.T) {
	_, a := wired(nil)
	drive(t, a, steeredMsg{words: "no", err: errors.New("agent is closed")}, frameMsg{})
	if body := plain(frame(a)); !strings.Contains(body, "steer failed: agent is closed") {
		t.Fatalf("the failure was swallowed:\n%s", body)
	}
}

// ── the stream ──────────────────────────────────────────────────────────────

// A STEER THAT FELL THROUGH KEEPS ITS CHANNEL, AND THE TURN IT STARTS IS DRAWN.
//
// The engine lifts a steer that never reached a boundary onto its own follow-up
// queue and carries the stream across with it, so the turn those words then
// start speaks on that channel and nowhere else. Left unread it would be a whole
// turn running with nothing on the screen about it.
//
// THE ACCEPTANCE COMES FIRST ON THE WIRE AND IT IS SCRIPTED HERE FOR THAT
// REASON. A steer's stream is an ordinary subscriber to the turn's hub, so it
// carries every other steer's news too, and the lane that reads it tells its own
// steer from theirs by the FIRST ACCEPTANCE IT SEES (steerelbow.go's
// [waitSteerLane]). That is sound because the engine adopts the stream and sends
// the acceptance under one lock, in that order ([session.Agent.Steer]), so a
// fall-through can never be the first thing on a real steer's channel — a fake
// that put one there would be testing against an engine that does not exist.
func TestASteerThatFellThroughBecomesTheNextMessagesTurn(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	lane := make(chan session.Event, 4)
	agent.steerCh = lane
	typeInto(t, a, "no, the other file")
	lane <- session.Event{
		Kind:  session.EventSteerAccepted,
		Steer: &session.SteerNote{ID: 1, Words: "no, the other file"},
	}
	lane <- session.Event{
		Kind:  session.EventSteerFellThrough,
		Steer: &session.SteerNote{ID: 1, Words: "no, the other file"},
	}
	drive(t, a, key("enter"))

	if len(a.follows) != 1 || a.follows[0].text != "no, the other file" {
		t.Fatalf("the fall-through did not become a waiting turn: %+v", a.follows)
	}
	// The turn being steered is still the one being pumped, so the carried
	// stream waits for it exactly as a ctrl+q message's does.
	if a.stream == nil {
		t.Fatal("the fall-through took the running turn's stream")
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if a.stream != lane {
		t.Fatal("the carried stream did not become the next turn's")
	}
	last := a.entries[len(a.entries)-1]
	if last.kind != entryUser || last.text != "no, the other file" {
		t.Fatalf("the fallen-through sentence did not open the next turn: %v %q", last.kind, last.text)
	}
}

// A PERSON'S SENTENCE IS ON THE SCREEN ONCE, AND A FALL-THROUGH IS WHERE THAT IS
// EASIEST TO GET WRONG.
//
// The words arrive at the surface twice by two roads and they must produce one
// row between them: the TURN'S OWN STREAM hangs them off the question as an
// elbow (steerelbow.go's [app.steerAccepted]) and takes that elbow off again
// when the turn ends without them ([app.steerFellThrough]), while the STEER'S
// OWN LANE carries them onto the waiting queue, where the drain draws them as
// the question they have become ([app.steerFell], followup.go's
// [app.startFollow]). Draw both and the transcript says a person asked the same
// thing twice; draw neither and their correction is gone.
//
// This is the merge seam the two lanes met at, so it is pinned end to end: one
// chord, both roads, one row.
func TestAFallenThroughCorrectionIsOnTheScreenExactlyOnce(t *testing.T) {
	const words = "no, the other file"
	a, agent := steerableTurn(t, "reading the tree. ")
	lane := make(chan session.Event, 4)
	agent.steerCh = lane
	lane <- session.Event{Kind: session.EventSteerAccepted, Steer: &session.SteerNote{ID: 1, Words: words}}
	lane <- session.Event{Kind: session.EventSteerFellThrough, Steer: &session.SteerNote{ID: 1, Words: words}}

	typeInto(t, a, words)
	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper))
	// The turn's own stream says the same two things, which is how the elbow is
	// drawn at all and how it comes off again.
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerAcceptedEvent(1, words)})
	drive(t, a, streamEventMsg{gen: a.gen, ev: steerFellEvent(1, words)})

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen}, frameMsg{})

	var asked, elbows int
	for _, line := range plainRows(a) {
		if strings.HasPrefix(line, "› "+words) {
			asked++
		}
		if strings.HasPrefix(line, glyphSteer+words) {
			elbows++
		}
	}
	if asked != 1 || elbows != 0 {
		t.Fatalf("the correction is drawn %d times as a question and %d times as an elbow:\n%s",
			asked, elbows, strings.Join(plainRows(a), "\n"))
	}
}

// ── the overlays above ──────────────────────────────────────────────────────

// AN OVERLAY THAT HAS TAKEN THE KEYBOARD KEEPS BOTH KEYS. Copy mode is read
// above the plain switch, and while it is up the surface is a reader: a chord
// that reached past it would send a sentence out of a box nobody is looking at.
func TestAnOverlayAboveKeepsBothSteerKeys(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	parkLine(t, a, "do much more of a deep research please")
	a.input.setText("no, the other file")
	a.enterCopy()
	if !a.copy.on {
		t.Fatal("copy mode did not open")
	}

	drive(t, a, wirePress(t, "\x1b[13;9u", steerKeySuper), key("right"))
	if len(agent.steered) != 0 {
		t.Fatalf("a key reached past copy mode and steered: %q", agent.steered)
	}
	if len(a.parks) != 1 {
		t.Fatalf("a key reached past copy mode and moved the queue: %+v", a.parks)
	}
	// And the slot names neither, because neither would do anything.
	if got := a.hintWord(); strings.Contains(got, parkKey) || strings.Contains(got, steerSendWord) {
		t.Fatalf("the hint named the steer while copy mode held the keyboard: %q", got)
	}
}

// ── the two lines that teach it ─────────────────────────────────────────────

// H2 and H3: the one running-turn line names every deliverable key in its fixed
// order, and a basic terminal retains plain enter while losing its chords.
func TestTheHintUnderTheBoxTeachesTheSteerWhereTheChordCanBeDelivered(t *testing.T) {
	a, _ := steerableTurn(t, "reading the tree. ")
	typeInto(t, a, "no, the other file")

	want := "enter " + steerSendWord + " · " + bargeKey + " " + bargeSendWord + " · esc interrupt"
	if got := a.hintWord(); got != want {
		t.Fatalf("the hint slot reads %q, want %q", got, want)
	}

	// A TERMINAL THAT NEVER ANSWERED THE QUERY IS STILL TOLD ABOUT PLAIN ENTER,
	// because steering does not need a modified-key protocol. The unavailable
	// secondary chords are the only clauses removed.
	a.keysDisambiguated = false
	if got := a.hintWord(); got != steerShortHint+" · esc interrupt" {
		t.Fatalf("a basic terminal lost the plain-enter steer: %q", got)
	}
}

// H2 and H3 also apply when the draft is only a picture on the tray. The tray
// is message content, so plain enter waits even though the text box is empty,
// and the stop-and-send chord appears only on a terminal that can spell it.
func TestAPictureOnTheTrayKeepsThePlainEnterHint(t *testing.T) {
	a, _ := steerableTurn(t, "reading the tree. ")
	a.chips = []chip{{path: "/tmp/shot.png"}}

	want := enterWaitHint + " · " + bargeKey + " " + bargeSendWord + " · esc interrupt"
	if got := a.hintWord(); got != want {
		t.Fatalf("the tray-only hint reads %q, want %q", got, want)
	}

	a.keysDisambiguated = false
	if got := a.hintWord(); got != enterWaitHint+" · esc interrupt" {
		t.Fatalf("a basic terminal lost the tray's plain-enter hint: %q", got)
	}
}

// H5: the narrow ladder drops one clause from the right on every rung, stops at
// one clause, and still draws that clause at the 70-column floor.
func TestANarrowFrameKeepsTheShorterHintRatherThanLosingTheSlot(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	a.agent = &promotingAgent{fakeAgent: agent, answer: session.BashPromotedLead + "3"}
	runningBash(t, a, "c1", "go test ./...")
	typeInto(t, a, "no, the other file")

	// Wide enough for all three: the line is the whole sentence.
	a.width = 120
	full := a.runHint()
	whole := full
	if body := plain(frame(a)); !strings.Contains(body, full) {
		t.Fatalf("a wide frame did not draw the whole hint:\n%s", body)
	}

	want := []string{
		"enter " + steerSendWord + " · " + bargeKey + " " + bargeSendWord + " · ctrl+g backgrounds",
		"enter " + steerSendWord + " · " + bargeKey + " " + bargeSendWord,
		steerShortHint,
		"",
	}
	for i, expected := range want {
		full = a.hintShorter(full)
		if full != expected {
			t.Fatalf("ladder rung %d = %q, want %q", i+1, full, expected)
		}
	}

	// The narrowest frame that carries a hint slot at all keeps the leftmost
	// clause rather than dropping the running-turn slot.
	a.width = hudTight
	body := plain(frame(a))
	if !strings.Contains(body, steerShortHint) {
		t.Fatalf("the narrow frame lost the whole hint slot:\n%s", body)
	}
	if strings.Contains(body, whole) || strings.Contains(body, "esc interrupt") {
		t.Fatalf("the narrow ladder did not drop from the right:\n%s", body)
	}
}

// AND THE WAITING MESSAGE'S OWN LINE CARRIES THE ARROW, unconditionally as far
// as the terminal is concerned: an arrow key reaches every terminal there is, so
// there is nothing to gate the clause on but whether the act itself is possible.
func TestTheStripNamesTheArrowAndEscDropsTheWholeWaitingBlock(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	a.width = 90
	parkLine(t, a, "do much more of a deep research please")
	drive(t, a, frameMsg{})

	body := plain(frame(a))
	for _, want := range parkedHint {
		if !strings.Contains(body, want) {
			t.Fatalf("the strip did not say %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, steerArrowWord) {
		t.Fatalf("the strip did not offer the steer:\n%s", body)
	}

	// ESC drops the queue at the keypress, so a winding-down turn has neither a
	// stale message nor an arrow that claims it can still cross a boundary.
	drive(t, a, key("esc"), frameMsg{})
	if !a.windingDown() {
		t.Fatal("the surface is not winding down after esc")
	}
	body = plain(frame(a))
	if strings.Contains(body, steerArrowWord) || strings.Contains(body, parkedHint[0]) ||
		strings.Contains(body, "do much more of a deep research please") {
		t.Fatalf("the dropped waiting block is still on the winding-down frame:\n%s", body)
	}
	// And the key is inert with it.
	drive(t, a, key("right"))
	if len(agent.steered) != 0 {
		t.Fatalf("→ steered into a turn that was winding down: %q", agent.steered)
	}
	_ = agent
}

// THE NARROW LINE DROPS FROM THE RIGHT, and the steer outranks the edit: a
// message that can still go now is worth more cells than one you can change.
func TestTheStripDropsTheEditBeforeTheSteer(t *testing.T) {
	full := parkedWord(1, 200, true, true)
	if full != strings.Join(parkedHint, " · ") {
		t.Fatalf("the whole line is not the whole hint: %q", full)
	}
	head := parkedHint[0] + " · " + parkedHint[1] + " · " + parkedHint[2]
	if got := parkedWord(1, ansi.StringWidth(head), true, true); got != head {
		t.Fatalf("the line did not drop the edit first: %q", got)
	}
	// And a session with nothing to promote never draws the clause at all.
	without := parkedWord(1, 200, true, false)
	if strings.Contains(without, steerArrowWord) {
		t.Fatalf("the line offered a steer nothing could take: %q", without)
	}
	if !strings.Contains(without, parkedHint[3]) {
		t.Fatalf("dropping the steer took the edit with it: %q", without)
	}
}
