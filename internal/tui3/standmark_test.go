package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// standMarkLab is a conversation with the ambient side on: the gesture and the
// hint are both absent without one, so every test here has to say so.
func standMarkLab(t *testing.T) (*app, *fakeAgent, *standBand) {
	t.Helper()
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = 120
	band := &standBand{}
	band.wire(a)
	return a, agent, band
}

// typeDraft types into the box without sending.
func typeDraft(t *testing.T, a *app, line string) {
	t.Helper()
	for _, r := range line {
		drive(t, a, key(string(r)))
	}
}

// THE INVARIANT: A MARKED SEND IS NEVER ORDINARY WORK.
//
// The chord's whole reason to exist is that recognition fails silently, so the
// one thing it may never do is fall back to the road it was pressed to avoid.
func TestTheMarkedSendNeverGoesThroughTheOrdinaryDoor(t *testing.T) {
	a, agent, _ := standMarkLab(t)
	typeDraft(t, a, "always run the tests before you say you are done")
	drive(t, a, key(standMarkKey))

	if len(agent.marked) != 1 || agent.marked[0] != "always run the tests before you say you are done" {
		t.Fatalf("the marked door took %v, want the sentence", agent.marked)
	}
	if len(agent.sent) != 1 || agent.sent[0] != agent.marked[0] {
		t.Fatalf("the turn was not opened once on the person's own words: %v", agent.sent)
	}
	// AND THE BOX IS SPENT exactly as a plain enter spends it.
	if !a.input.empty() {
		t.Fatalf("the draft survived the send: %q", a.input.String())
	}
}

// A plain enter is untouched by any of this: the same sentence, sent the
// ordinary way, still goes the ordinary way.
func TestAPlainEnterIsStillAnOrdinarySend(t *testing.T) {
	a, agent, _ := standMarkLab(t)
	typeDraft(t, a, "always run the tests")
	drive(t, a, key("enter"))
	if len(agent.marked) != 0 {
		t.Fatalf("a plain enter marked the sentence: %v", agent.marked)
	}
	if len(agent.sent) != 1 {
		t.Fatalf("a plain enter sent %d messages", len(agent.sent))
	}
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN — and it says so rather
// than quietly sending the sentence as work.
func TestTheMarkedSendRefusesWhereNothingCanHoldOne(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.width = 120
	typeDraft(t, a, "always run the tests")
	drive(t, a, key(standMarkKey))

	if len(agent.sent) != 0 || len(agent.marked) != 0 {
		t.Fatalf("a surface with no ambient side sent it anyway: %v", agent.sent)
	}
	if !strings.Contains(plain(frame(a)), standMarkNowhere) {
		t.Fatalf("the refusal was never said:\n%s", plain(frame(a)))
	}
}

// AND A BOX HOLDING SOMETHING ELSE IS REFUSED RATHER THAN SENT. A picture and a
// picked shape of work are both deliberate arrangements the gesture is not
// about, and falling back to an ordinary send there is the silent failure again.
func TestTheMarkedSendRefusesABoxThatIsHoldingSomethingElse(t *testing.T) {
	a, agent, _ := standMarkLab(t)
	a.harnChip = "review"
	typeDraft(t, a, "always run the tests")
	drive(t, a, key(standMarkKey))

	if len(agent.sent) != 0 || len(agent.marked) != 0 {
		t.Fatalf("a picked shape of work was sent as a standing order: %v", agent.sent)
	}
	if a.input.String() != "always run the tests" {
		t.Fatalf("a refused mark spent the draft: %q", a.input.String())
	}
	if !strings.Contains(plain(frame(a)), standMarkWordsOnly) {
		t.Fatalf("the refusal was never said:\n%s", plain(frame(a)))
	}
}

// A SLASH COMMAND IS SAID TO THIS SURFACE, so the chord is the ordinary enter
// there: there is no sentence to keep true and nothing to refuse.
func TestTheMarkedSendOnACommandIsJustTheCommand(t *testing.T) {
	a, agent, _ := standMarkLab(t)
	typeDraft(t, a, "/standing")
	drive(t, a, key(standMarkKey))
	if len(agent.marked) != 0 || len(agent.sent) != 0 {
		t.Fatalf("a command was sent to the model: marked=%v sent=%v", agent.marked, agent.sent)
	}
}

// THE MARK WAITS WITH THE WORDS. A sentence typed over a running answer is
// parked, and what goes when the answer ends is still marked.
func TestAMarkedSendParkedOverAnAnswerIsStillMarkedWhenItGoes(t *testing.T) {
	a, agent, _ := standMarkLab(t)
	a.state = stateWorking
	typeDraft(t, a, "never commit straight to main here")
	drive(t, a, key(standMarkKey))
	if len(a.parks) != 1 || !a.parks[0].standing {
		t.Fatalf("the mark did not travel with the parked message: %+v", a.parks)
	}
	a.state = stateIdle
	drive(t, a, runCmd(a.sendParked())...)
	if len(agent.marked) != 1 {
		t.Fatalf("the parked message went through the ordinary door: %v", agent.sent)
	}
}

// ── the self-teaching hint ──────────────────────────────────────────────────

// THE HINT APPEARS ON WHAT LOOKS LIKE A CONDITION AND VANISHES ON WHAT DOES NOT.
func TestTheHintNamesTheChordOnlyWhileTheDraftLooksStanding(t *testing.T) {
	conditions := []string{
		"always run the tests before you say you are done",
		"never commit straight to main here",
		"every Monday draft the weekly update",
		"whenever I push, run the suite",
		"each time CI goes red tell me",
		"from now on use tabs",
		"remind me at 6 to leave",
		"keep an eye on the build",
		"make sure the public API never changes",
	}
	ordinary := []string{
		"fix the parser",
		"what time is it?",
		"however you like",
		"make sure it compiles",
		"read the delivery notes",
	}
	for _, line := range conditions {
		if !looksStanding(line) {
			t.Errorf("no hint offered for %q", line)
		}
	}
	for _, line := range ordinary {
		if looksStanding(line) {
			t.Errorf("a hint was offered for %q", line)
		}
	}
	if looksStanding("") {
		t.Error("an empty draft was offered the hint")
	}
}

// AND IT REACHES THE SLOT, AND LEAVES IT AGAIN.
func TestTheHintReachesTheSlotAndVanishesWithTheDraft(t *testing.T) {
	a, _, _ := standMarkLab(t)
	if strings.Contains(plain(frame(a)), standMarkHint) {
		t.Fatal("an empty box was offered the chord")
	}
	typeDraft(t, a, "always run the tests")
	if !strings.Contains(plain(frame(a)), standMarkHint) {
		t.Fatalf("the hint never appeared:\n%s", plain(frame(a)))
	}
	// AND THE BOX DID NOT MOVE. The hint rides the legend, which is on the frame
	// in every state, so a draft that starts looking like a rule changes one word
	// at the end of a line and nothing else about the geometry.
	height := a.inputHeight()
	for range "always run the tests" {
		drive(t, a, key("backspace"))
	}
	if strings.Contains(plain(frame(a)), standMarkHint) {
		t.Fatalf("the hint outlived the draft:\n%s", plain(frame(a)))
	}
	if a.inputHeight() != height {
		t.Fatalf("the box changed height with the hint: %d then %d", height, a.inputHeight())
	}
}

// AND IT IS NEVER OFFERED WHERE THE CHORD WOULD REFUSE — the two lists are the
// same list read from both ends.
func TestTheHintIsAbsentWhereTheChordWouldRefuse(t *testing.T) {
	bare := newTestApp(&fakeAgent{model: "m"})
	bare.width = 120
	typeDraft(t, bare, "always run the tests")
	if bare.standMarkOffered() {
		t.Error("a surface with no ambient side offered the chord")
	}

	a, _, _ := standMarkLab(t)
	typeDraft(t, a, "always run the tests")
	if !a.standMarkOffered() {
		t.Fatal("the ordinary case did not offer the chord")
	}
	a.harnChip = "review"
	if a.standMarkOffered() {
		t.Error("a picked shape of work still offered the chord")
	}
}

// ── the visible door ────────────────────────────────────────────────────────

// standDoorLab is a conversation with one order standing over it: the segment
// is drawn, and the page behind the door has a row to show.
func standDoorLab(t *testing.T) (*app, *standBand) {
	t.Helper()
	item := bandItem("one", "remind me on Fridays", "/tmp/lab", standing.WhenEvery, "Fridays")
	agent := &standPageFake{fakeAgent: &fakeAgent{model: "m"}, stand: []standing.Item{item}}
	a := newTestApp(agent)
	a.width = 120
	a.workspace = "/tmp/lab"
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	band := &standBand{items: []standing.Item{item}}
	band.wire(a)
	return a, band
}

// THE SEGMENT IS A DOOR. Pressing `keeping an eye on N` opens /standing, which
// is the page a person reading that number is trying to find.
func TestPressingTheKeepingSegmentOpensTheStandingPage(t *testing.T) {
	a, _ := standDoorLab(t)
	x, y := standDoorAt(t, a)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.at(pageStanding) {
		t.Fatalf("the door did not open the page:\n%s", plain(frame(a)))
	}
}

// A press anywhere else on that row falls through, because the rest of it is
// telemetry — figures, not controls.
func TestPressingBesideTheKeepingSegmentOpensNothing(t *testing.T) {
	a, _ := standDoorLab(t)
	_, y := standDoorAt(t, a)
	drive(t, a, tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	if a.at(pageStanding) {
		t.Fatal("the whole status row acted as the door")
	}
}

// AND THERE IS NO DOOR WHERE THERE IS NO SEGMENT — the emptiness law applied to
// a hit box: nothing stands here, so nothing on that row is pressable.
func TestThereIsNoDoorWhileNothingStandsHere(t *testing.T) {
	a, _, _ := standMarkLab(t)
	a.workspace = "/tmp/lab"
	frame(a)
	if a.keepSpan.pressable() {
		t.Fatalf("a row with no keeping segment recorded a door: %+v", a.keepSpan)
	}
}

// AND THE DOOR BREATHES ONLY WHILE A PASS HAS ONE OF THIS PLACE'S ORDERS IN ITS
// HANDS. Still and calm otherwise, which is the same treatment the rail gives
// live work — and the whole of the movement this wave adds.
func TestTheDoorBreathesOnlyWhileAnOrderIsInHand(t *testing.T) {
	a, band := standDoorLab(t)
	now := a.now()
	a.clock = func() time.Time { return now }
	stale := func() { now = now.Add(keepEvery + time.Second) }

	still := standWaitGlyph + homeKeepingWord + "1"
	stale()
	if a.keepingWord() != still {
		t.Fatalf("a quiet door is not still: %q", a.keepingWord())
	}
	band.running = map[string]standing.RunningMark{
		"one": {PID: 1, Since: now, What: standing.RunningFiring},
	}
	stale()
	if a.keepingWord() == still {
		t.Fatal("a door with an order in hand is not breathing")
	}
	// AND IT IS STILL THE SAME DOOR, at the same width: only the glyph moves.
	if !strings.HasSuffix(a.keepingWord(), homeKeepingWord+"1") {
		t.Fatalf("the breathing door lost its count: %q", a.keepingWord())
	}
}

// standDoorAt paints one frame and answers where the door landed, failing rather
// than guessing when it was not drawn.
func standDoorAt(t *testing.T, a *app) (int, int) {
	t.Helper()
	frame(a)
	if !a.keepSpan.pressable() {
		t.Fatalf("the keeping segment was never drawn:\n%s", plain(frame(a)))
	}
	_, height := a.size()
	for y := height - 1; y >= 0; y-- {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeStatus && mark.index == a.keepRow {
			return a.keepSpan.from, y
		}
	}
	t.Fatal("the status row the door was drawn on is not on the frame")
	return 0, 0
}
