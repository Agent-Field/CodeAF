package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// BARGE-IN: ONE GESTURE FOR "STOP — I MEANT THIS INSTEAD".
//
// The thing being pinned here is a pair of meanings on one keyboard, and the
// dangerous half of the pair is the one that ALREADY WORKED. Plain enter over a
// running answer now steers, while cmd+enter parks the sentence (park.go); every test in the
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

// CMD+ENTER WAITS. It parks, it does not send, and above all it
// does not stop the answer — on a terminal that can spell the chord just as
// much as on one that cannot, because the chord's existence must change nothing
// about the key beside it.
func TestCmdEnterOverAnAnswerWaits(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	parkLine(t, a, "no, the other file")

	if agent.stops != 0 {
		t.Fatalf("cmd+enter stopped the answer: %d stops", agent.stops)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("cmd+enter sent something: %q", agent.sent)
	}
	if len(a.parks) != 1 || a.parks[0].text != "no, the other file" {
		t.Fatalf("cmd+enter did not park the sentence: %+v", a.parks)
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
	// AND THE FRAME AGREES WITH THE ENTRIES, in the posture a person is actually
	// sitting in. A turn the chord stopped reached no answer, so it collapses
	// whole into its own chip (hierarchy.go's [app.cutTurn], workfold.go) — there
	// is no answer to leave standing under it, which is the point of the chip's
	// wording. What has to be true of the ORDER is that the chip belongs to the
	// turn above and the sentence opens the one below.
	body := plain(frame(a))
	stoppedAt := strings.Index(body, "stopped by you")
	saidAt := strings.LastIndex(body, "no, the other file")
	if stoppedAt < 0 {
		t.Fatalf("the stopped turn did not say so:\n%s", body)
	}
	if saidAt < 0 || saidAt < stoppedAt {
		t.Fatalf("the sentence is not drawn below the turn it stopped:\n%s", body)
	}
	// AND NOTHING WAS THROWN AWAY. Open the work and the words the answer managed
	// to say are on screen, above the sentence, where they happened — the chip is
	// a fold and never a deletion.
	a.workMode = config.WorkOpen
	a.touch()
	body = plain(frame(a))
	answerAt := strings.Index(body, "wrong answer")
	saidAt = strings.LastIndex(body, "no, the other file")
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
	if got := a.hintWord(); got != steerShortHint {
		t.Fatalf("hint = %q, want the plain-enter steer where the chord cannot work", got)
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
	// AND THE LINE IS THE WHOLE OF THAT STATE'S KEYS, which since the splice
	// landed is three of them rather than two: the chord that stops shares the
	// slot with the chord that steers, on a session and a terminal that have both
	// (steer.go's [app.typingHint]). What this asserts is that the slot is
	// composed rather than authored — a second copy of the sentence here would be
	// the test agreeing with itself.
	if got != a.typingHint() {
		t.Fatalf("hint = %q, want %q", got, a.typingHint())
	}
	if !strings.HasSuffix(got, bargeKey+" "+bargeSendWord) {
		t.Fatalf("hint = %q, want it to end with the chord this file is about", got)
	}
	// A session with no splice behind it reads exactly as this line always did,
	// and steer_test.go's own case asserts that.

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

// SOMEBODY PARKS DURING A STREAM, HITS ENTER TWICE, THEN THE STOP CHORD.
//
// The three presses are one message and two keys pressed at a box that is
// already empty, and the chord after them has nothing left to say. What must
// come out of that is: ONE message queued, ONE turn spent on it, no second copy
// of it, and no stop that the person did not ask for — because the chord over
// an empty box is the absent key, not a bare interrupt.
func TestAParkThenEmptyEntersAndTheChordSendTheMessageOnceAndLoseNothing(t *testing.T) {
	a, agent := bargeable(t, "reading the tree. ")
	parkLine(t, a, "no, the other file")
	drive(t, a, key("enter"), key("enter"))
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

// ── the gesture and the answer hierarchy, end to end ────────────────────────
//
// The two lanes that met here changed the same three seconds of a person's life
// from opposite sides: one made the stop pay at the key, the other decided which
// prose on the screen is an answer. Everything below is the whole gesture as it
// is actually lived — an answer going wrong, a correction typed over it, the
// chord — read off the screen at each step, because the two rules only meet on
// the screen and nowhere in the code.

// THE FULL GESTURE. The stopped turn folds as a turn nobody got an answer out
// of, and the sentence that stopped it opens a turn that promotes its answer
// exactly as any other turn does — the demotion is a fact about the turn that
// was cut and it must not spread to the one after it.
func TestABargedInTurnFoldsAsStoppedAndTheNextOneAnswersNormally(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{
		{text(session.EventTextDelta, "the wrong file is called parser.go and it ")},
		{
			text(session.EventTextDelta, "Let me open the other one."),
			toolBegin("read", "lexer.go"),
			toolEnd("read", ""),
			text(session.EventTextDelta, "It reads the length prefix twice."),
			{Kind: session.EventTurnDone},
		},
	}}
	a := newTestApp(agent)
	drive(t, a, tea.KeyboardEnhancementsMsg{Flags: 1})
	typeLine(t, a, "what is in this repository")
	drive(t, a, frameMsg{})

	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey))
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen}, frameMsg{})

	// The correction is away and its own turn is open, which is what the park
	// queue draining at the close is for.
	if len(agent.sent) != 2 || agent.sent[1] != "no, the other file" {
		t.Fatalf("the correction did not open a turn of its own: %q", agent.sent)
	}
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen}, frameMsg{})

	body := strings.Join(plainRows(a), "\n")
	// 1. THE STOPPED TURN SAYS WHO STOPPED IT, and leaves nothing standing that
	// claims to be an answer.
	if !strings.Contains(body, "▸ stopped by you") {
		t.Fatalf("the barged-in turn did not fold as a stopped one:\n%s", body)
	}
	if strings.Contains(body, "the wrong file is called parser.go") {
		t.Fatalf("the stopped turn left its half-sentence standing:\n%s", body)
	}
	// 2. AND THE TURN THE CORRECTION OPENED IS AN ORDINARY TURN. Its answer is
	// promoted: flush to the margin, at the body ink, with the work above it
	// folded into a chip of its own that says `worked` and not `stopped`.
	answer := rowWithText(t, a, "reads the length prefix twice")
	if strings.HasPrefix(plain(answer.text), " ") {
		t.Fatalf("the answer to the correction was demoted into the work column: %q", answer.text)
	}
	if strings.Contains(answer.text, sgrOf(a.pal.narr)) {
		t.Fatalf("the answer to the correction wears the working tier: %q", answer.text)
	}
	if !strings.Contains(body, "▸ worked") {
		t.Fatalf("the second turn's work did not fold as a finished one:\n%s", body)
	}
	// 3. AND THE NARRATION OF THAT TURN RECEDED, which is the hierarchy doing its
	// ordinary job on a turn that arrived by this gesture.
	if strings.Contains(body, "Let me open the other one") {
		t.Fatalf("the second turn's narration was left standing:\n%s", body)
	}
	// 4. AND THE ORDER IS THE ORDER IT HAPPENED IN.
	stoppedAt := strings.Index(body, "▸ stopped by you")
	saidAt := strings.Index(body, "no, the other file")
	answeredAt := strings.Index(body, "reads the length prefix twice")
	if !(stoppedAt < saidAt && saidAt < answeredAt) {
		t.Fatalf("the three turns are not in the order they happened:\n%s", body)
	}
}

// AND NOTHING BRIGHTENS ON THE WAY DOWN. The reply that was arriving wore the
// live tier — the one rung ABOVE the body ink, which means "still coming"
// (styles.go's [hueLive]) — and the moment the chord lands that claim is false.
// The two waves make it false in the same instant from two directions: the stop
// takes the spinner and the count-up off the line (render.go's [app.stateSegment]
// stands down outside [stateWorking]) and the cut demotes the block to the
// working tier, one rung BELOW the body. So the loudest thing on the screen goes
// quieter at the keypress and nothing on the frame moves until the turn is gone.
func TestNothingOnTheScreenBrightensWhileTheStoppedTurnWindsDown(t *testing.T) {
	a, _ := bargeable(t, "the first paragraph of the wrong answer. ")
	a.workMode = config.WorkOpen
	drive(t, a, frameMsg{})

	if !strings.Contains(frame(a), liveSGR()) {
		t.Fatal("the arriving reply was not at the live tier to begin with")
	}

	typeInto(t, a, "no, the other file")
	drive(t, a, key(bargeKey), frameMsg{})

	if !a.windingDown() {
		t.Fatal("the chord did not leave the turn winding down")
	}
	painted := frame(a)
	if strings.Contains(painted, liveSGR()) {
		t.Fatalf("something is still claiming to be arriving:\n%s", plain(painted))
	}
	// AND THE HALF-SENTENCE IS AT THE WORKING TIER, which is the demotion said in
	// the channel a person actually reads it in.
	partial := rowWithText(t, a, "first paragraph of the wrong answer")
	if !strings.Contains(partial.text, sgrOf(a.pal.narr)) {
		t.Fatalf("the stopped reply did not drop to the working tier: %q", partial.text)
	}
	// AND THE STATUS LINE IS STILL AND DIM. No spinner, no count-up, no colour:
	// winding down is the quietest thing this surface does.
	word, tinted := a.stateWord()
	if word != stoppingWord || tinted != a.pal.dim(stoppingWord) {
		t.Fatalf("the status line is not the dim stopping word: %q %q", word, tinted)
	}
	if shown, _ := a.stateSegment(); shown != stoppingWord {
		t.Fatalf("the status segment carries more than the word: %q", shown)
	}
	if line := plain(frame(a)); strings.ContainsAny(line, spinnerFrames()) {
		t.Fatalf("a spinner is still turning while the turn winds down:\n%s", line)
	}
}
