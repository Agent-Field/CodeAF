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

// standDoorLab is a conversation with one order standing over it: the line is
// drawn at the foot of the task column, and the page behind the door has a row
// to show.
//
// THE FRAME IS THE FULL COLUMN'S because that is where the line lives now
// (task.go's [app.railFootRows]): a column too narrow or too short for a footer
// draws no footer, which is a fact about the frame and not about this door.
func standDoorLab(t *testing.T) (*app, *standBand) {
	t.Helper()
	item := bandItem("one", "remind me on Fridays", "/tmp/lab", standing.WhenEvery, "Fridays")
	agent := &standingPlaceFake{fakeAgent: &fakeAgent{model: "m"}, stand: []standing.Item{item}}
	a := newTestApp(agent)
	a.width, a.height = 140, 24
	a.workspace = "/tmp/lab"
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	band := &standBand{items: []standing.Item{item}}
	band.wire(a)
	return a, band
}

// THE COUNT IS A DOOR. Pressing `◦ 1 standing order` at the foot of the task
// column opens /standing, which is the page a person reading that number is
// trying to find.
func TestPressingTheStandingCountOpensTheStandingPage(t *testing.T) {
	a, _ := standDoorLab(t)
	_, y := standDoorAt(t, a)
	pressMargin(t, a, y)
	if !a.at(pageStanding) {
		t.Fatalf("the door did not open the page:\n%s", plain(frame(a)))
	}
}

// AND IT SAYS SO UNDER THE POINTER, which is this column's own spelling of
// "this line answers to a click": what LIGHTS has to be what the press acts on
// (hover.go's law).
func TestTheStandingCountBrightensUnderThePointer(t *testing.T) {
	a, _ := standDoorLab(t)
	_, y := standDoorAt(t, a)
	still := a.railStandingLine()
	drive(t, a, tea.MouseMotionMsg{X: a.railLeft() + 3, Y: y})
	if !a.hoveringRailStanding() {
		t.Fatalf("the pointer on the count is not on the door: %+v", a.hot)
	}
	if lit := a.railStandingLine(); lit == still {
		t.Fatalf("the count did not brighten under the pointer: %q", plain(lit))
	}
	if plain(a.railStandingLine()) != plain(still) {
		t.Fatalf("brightening changed the words: %q", plain(a.railStandingLine()))
	}
	// AND THE ROW'S GROUND COMES UP WITH IT — the other half of the law, which the
	// column applies in its own layout pass (task.go's [app.railRows]): the ground
	// is what tells a hand that the whole line answers.
	var grounded bool
	for _, row := range a.railRows(a.viewHeight()) {
		if strings.Contains(plain(row), homeKeepingWord) && strings.Contains(row, hoverBg()) {
			grounded = true
		}
	}
	if !grounded {
		t.Fatalf("the count under the pointer wears no ground:\n%s",
			strings.Join(a.railRows(a.viewHeight()), "\n"))
	}
}

// A press on the counts above it opens nothing. Those lines are a tally —
// figures, not controls — exactly as the rest of the status row was.
func TestPressingTheTallyAboveTheStandingCountOpensNothing(t *testing.T) {
	a, _ := standDoorLab(t)
	_, y := standDoorAt(t, a)
	pressMargin(t, a, y-1)
	if a.at(pageStanding) {
		t.Fatal("the whole footer acted as the door")
	}
}

// AND THERE IS NO LINE WHERE NOTHING STANDS — the emptiness law applied to a
// hit box: nothing stands here, so the footer grows no row for it.
func TestThereIsNoStandingLineWhileNothingStandsHere(t *testing.T) {
	a, _, _ := standMarkLab(t)
	a.width, a.height = 140, 24
	a.workspace = "/tmp/lab"
	frame(a)
	view, _ := a.railView(a.viewHeight())
	for _, line := range view {
		if line.keeping {
			t.Fatalf("a column with nothing standing drew a count: %q", plain(line.text))
		}
	}
}

// AND IT IS NOT ON THE STATUS ROW ANY MORE. The row is the conversation's own
// numbers; what stands over the project is the column's (foot.go).
func TestTheStandingCountIsNotOnTheStatusRow(t *testing.T) {
	a, _ := standDoorLab(t)
	if got := a.keepingSegment(); got == "" {
		t.Fatal("the fixture has nothing standing over it")
	}
	if line := plain(a.status(200)); strings.Contains(line, "standing order") {
		t.Fatalf("the standing count is back on the status row:\n%s", line)
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

	// ONE ORDER IS SAID IN THE SINGULAR: `◦ 1 standing order` (homestanding.go).
	still := standWaitGlyph + " 1" + homeKeepingWord
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
	if !strings.HasSuffix(a.keepingWord(), " 1"+homeKeepingWord) {
		t.Fatalf("the breathing door lost its count: %q", a.keepingWord())
	}
}

// standDoorAt paints one frame and answers where the door landed — the line
// itself, and the SCREEN ROW a press has to land on, which only the layout
// knows (margin_test.go's [marginLine] asks the column the same question).
func standDoorAt(t *testing.T, a *app) (railLine, int) {
	t.Helper()
	frame(a)
	return marginLine(t, a, func(line railLine) bool { return line.keeping })
}
