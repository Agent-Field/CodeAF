package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// ── THE EFFORT LADDER, SURFACE BY SURFACE ───────────────────────────────────
//
// One chord, three scopes, and the whole of what these hold shut is that the
// scope is the SURFACE A PERSON IS STANDING ON: the rung that moves is the rung
// that is drawn on the card in front of them, the setter it reaches is the one
// that owns that scope, and a surface the rule does not cover is left alone
// rather than given a fourth meaning.

// ── the wheel ───────────────────────────────────────────────────────────────

// THE WHEEL IS THE LADDER, CHEAPEST FIRST, AND ABSENCE IS ONLY A STARTING
// POINT. A rung a person paid for must not be cleared by pressing the key one
// time too many — clearing hands the scope back to whatever stands above it,
// which is a decision and not a thing a wheel does on its way past.
func TestTheWheelClimbsCheapestFirstAndNeverComesBackToAbsence(t *testing.T) {
	if got := effortNext(effort.None); got != effort.Low {
		t.Fatalf("the first press landed on %q, want the cheapest rung", got)
	}
	walk := []effort.Rung{effort.Low, effort.Medium, effort.High, effort.XHigh, effort.Max}
	at := effort.Low
	for i, want := range append(walk[1:], effort.Low) {
		at = effortNext(at)
		if at != want {
			t.Fatalf("step %d of the wheel landed on %q, want %q", i+1, at, want)
		}
		if at == effort.None {
			t.Fatal("the wheel came back to absence, which no number of presses may do")
		}
	}
	// A word this build does not know is absence and not a refusal: the wheel
	// starts it at the bottom rather than leaving a key that does nothing.
	if got := effortNext(effort.Rung("ultra")); got != effort.Low {
		t.Fatalf("a rung this build cannot parse cycled to %q, want the cheapest", got)
	}
}

// AND ABSENCE DRAWS NOTHING, which is the emptiness law: "nobody said" is not a
// rung to print, on any of the three cards.
func TestAScopeNobodyHasSetStatesNoRung(t *testing.T) {
	if got := effortClause(effort.None); got != "" {
		t.Fatalf("an unset scope drew %q, want nothing at all", got)
	}
	if got := effortClause(effort.XHigh); got != "thinking xhigh" {
		t.Fatalf("the clause reads %q", got)
	}
}

// ── home has no card about the machine, and no third scope ──────────────────

// THE THIRD SCOPE IS RETIRED AND THIS IS THE LAW THAT REPLACED IT.
//
// What used to be pinned here was "home at rest states the install's rung and
// ctrl+v moves it": the cursor walked up off the top row onto no row at all, the
// right-hand column became a card about the machine, and the chord wrote
// `config.WriteDefaultEffort`. That state is retired — `↑` off the top row
// reaches the TAB BAR now (pages.go's [barCursor]) — so the law it protected is
// restated as the thing that is still true: THE INSTALL'S RUNG HAS ONE WRITER ON
// THIS SURFACE, and home is not it.
//
// The invariant: home's cursor is on a row of its list at every moment, `ctrl+v`
// on a conversation row writes nothing to the profile, and the profile is left
// exactly where the settings panel put it.
func TestHomeNeverMovesTheInstallsRungBecauseTheMachineCardIsGone(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "aaaa000000000001", "one", lab.workspace("alpha"), time.Now())
	a := lab.app(transcript)
	a.profileDir = t.TempDir()
	a.openHome()
	a.width, a.height = 200, 30

	// THE CURSOR IS ON A ROW, AND WALKING UP OFF THE TOP DOES NOT TAKE IT OFF
	// ONE. It reaches the bar, which is a row of the FRAME rather than of the
	// list, and home's own cursor stays exactly where it was.
	a.frame()
	for i := 0; i < len(a.home.lines)+2; i++ {
		drive(t, a, key("up"))
	}
	if !a.bar.on {
		t.Fatal("walking up off the top row did not reach the tab bar")
	}
	if _, ok := a.home.focusedLine(); !ok {
		t.Fatal("home's own cursor came off its list, which is the state this wave retired")
	}
	if subject, ok := a.homeSubject(); !ok || subject.kind == bandKindItem {
		t.Fatalf("the card is not about the row under the cursor (ok=%v)", ok)
	}

	// AND THE CHORD WRITES NOTHING. The profile is untouched — no rung row is
	// created at all — because there is no machine card for the key to act on.
	before := config.DefaultEffortAt(a.profileDir)
	drive(t, a, key("esc"), key("ctrl+v"))
	if got := config.DefaultEffortAt(a.profileDir); got != before {
		t.Fatalf("ctrl+v on home moved the install's rung from %q to %q", before, got)
	}
}

// ── a standing item's card: that item's own rung ─────────────────────────────

// effortBand is [standBand] with the one door this lane adds, recording every
// rung the store was asked to write.
type effortBand struct {
	*standBand
	rungs   []effort.Rung
	refusal error
}

func (b *effortBand) wire(a *app) {
	b.standBand.wire(a)
	a.stands.SetEffort = func(id string, rung effort.Rung) error {
		if b.refusal != nil {
			return b.refusal
		}
		b.rungs = append(b.rungs, rung)
		for i := range b.standBand.items {
			if b.standBand.items[i].ID == id {
				b.standBand.items[i].Does.Effort = rung.String()
			}
		}
		return nil
	}
}

// itemHome is home with one standing item a cursor can be put on.
//
// THE ITEM IS FIRING, AND IT HAS TO BE. Home at rest is one flat ranked list and
// a watch earns a row on it only while it is asking somebody something or is
// actually running ([readSwitcher] — a watch that is merely set is not news and
// is reached from the standing place or by typing its name). The rung is a fact
// about the item and not about the pass it is on, so a running mark is the
// cheapest honest way to put the cursor on one.
func itemHome(t *testing.T) (*app, *effortBand) {
	t.Helper()
	lab := newHomeLab(t)
	work := lab.workspace("alpha")
	transcript := lab.session("alpha", "aaaa000000000001", "one", work, time.Now())
	band := &effortBand{standBand: &standBand{
		items: []standing.Item{
			bandItem("one", "remind me on Fridays", work, standing.WhenEvery, "Fridays"),
		},
		running: map[string]standing.RunningMark{
			"one": {PID: 4242, Since: time.Now().Add(-time.Minute), What: "reading the calendar"},
		},
	}}
	a := lab.app(transcript)
	band.wire(a)
	a.openHome()
	a.home.pointItemForTest("one")
	return a, band
}

// AN ITEM'S CARD STATES ITS RUNG AND CTRL+V MOVES IT THROUGH THE STORE'S OWN
// DOOR — never through the whole-document write the pause and stop keys use,
// which would undo whatever the ticker has moved since the card was drawn.
func TestAStandingItemsCardStatesItsRungAndCtrlVMovesIt(t *testing.T) {
	a, band := itemHome(t)

	// A SENTINEL SAYS NOTHING UNTIL SOMEBODY RAISES IT. Its firings run on the
	// standing floor and "nobody said" is not a rung to print.
	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindItem {
		t.Fatalf("the cursor is not on an item: %+v", subject)
	}
	before := plain(strings.Join(a.drawHomeBands(bandContext{subject: subject, width: 40,
		now: time.Now(), pal: a.pal})[0], "\n"))
	if strings.Contains(before, "thinking") {
		t.Fatalf("an item nobody has dialled drew a rung:\n%s", before)
	}

	drive(t, a, key("ctrl+v"))
	if len(band.rungs) != 1 || band.rungs[0] != effort.Low {
		t.Fatalf("the store was asked for %v, want one call with the cheapest rung", band.rungs)
	}
	if !strings.Contains(a.home.msg, "thinking low") ||
		!strings.Contains(a.home.msg, "remind me on Fridays") {
		t.Fatalf("home said %q about the change", a.home.msg)
	}
	subject, _ = a.homeSubject()
	after := itemCardText(a, subject)
	if !strings.Contains(after, "thinking low") {
		t.Fatalf("the item's card did not follow the write:\n%s", after)
	}
	if !strings.Contains(after, effortKeyClause) {
		t.Fatalf("the item's card names no way to move it:\n%s", after)
	}

	// AND THE ENGINE'S OWN SENTENCE IS KEPT on a refusal, so a person is not
	// left pressing the same key at a store that will not take it.
	band.refusal = errors.New("that item is gone")
	drive(t, a, key("ctrl+v"))
	if a.home.msg != "that item is gone" {
		t.Fatalf("a refused write said %q", a.home.msg)
	}
}

// A READ-ONLY HOME SAYS SO rather than pretending, in the same sentence the
// pause and stop keys already say it in.
func TestAnItemCardWithNoDoorRefusesInTheWordsItAlreadyHas(t *testing.T) {
	a, band := itemHome(t)
	a.stands.SetEffort = nil
	drive(t, a, key("ctrl+v"))
	if len(band.rungs) != 0 {
		t.Fatalf("a window with no door still wrote %v", band.rungs)
	}
	if a.home.msg != homeItemNoStore {
		t.Fatalf("home said %q, want %q", a.home.msg, homeItemNoStore)
	}
	subject, _ := a.homeSubject()
	if card := itemCardText(a, subject); strings.Contains(card, effortKeyClause) {
		t.Fatalf("a card that cannot move the rung still named the key:\n%s", card)
	}
}

// itemCardText is one subject's bands as a reader sees them.
func itemCardText(a *app, subject bandSubject) string {
	var out []string
	for _, band := range a.drawHomeBands(bandContext{subject: subject, width: 40,
		now: time.Now(), pal: a.pal}) {
		out = append(out, band...)
	}
	return plain(strings.Join(out, "\n"))
}

// ── a task: the rung its workers run at ──────────────────────────────────────

// effortFake is [roomFake] widened by the one door [taskEffortDoor] asserts.
type effortFake struct {
	*roomFake
	rungs   map[uint64]string
	asked   []string
	refusal error
}

func (f *effortFake) TaskEffort(id uint64) string { return f.rungs[id] }

func (f *effortFake) SetTaskEffort(id uint64, rung string) error {
	f.asked = append(f.asked, taskIDWord(id)+":"+rung)
	if f.refusal != nil {
		return f.refusal
	}
	if f.rungs == nil {
		f.rungs = map[uint64]string{}
	}
	f.rungs[id] = rung
	return nil
}

// effortTaskApp is [roomApp] with the rung door open, the roster holding the
// keyboard and its cursor on the one running node.
func effortTaskApp(t *testing.T) (*app, *effortFake) {
	t.Helper()
	base, room, _ := roomApp(t)
	agent := &effortFake{roomFake: room, rungs: map[uint64]string{}}
	base.agent = agent
	base.railTake(true)
	base.railWhere = railSpot{id: 7}
	base.touch()
	return base, agent
}

// THE ROSTER'S CURSOR IS A TASK YOU ARE STANDING ON, and ctrl+v moves that
// node's rung, writes the decision down, and says honestly when it lands.
func TestCtrlVOnTheFocusedTaskMovesThatTasksRung(t *testing.T) {
	a, agent := effortTaskApp(t)

	// The legend names the key while the row under the cursor can take it.
	if hint := a.railHoldHintWord(); !strings.Contains(hint, effortKeyClause) {
		t.Fatalf("the roster's legend does not name the chord: %q", hint)
	}

	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 1 || agent.asked[0] != "task 7:low" {
		t.Fatalf("the engine was asked %v, want one call setting task 7 to the cheapest rung", agent.asked)
	}
	// A RUNNING NODE IS TOLD WHEN, and the note never claims the call on the
	// wire changed: the worker keeps the rung it started on.
	want := "task 7 · thinking · low · " + taskEffortNextCallWord
	found := false
	for _, e := range a.entries {
		found = found || (e.kind == entryNote && e.text == want)
	}
	if !found {
		t.Fatalf("the change left no note reading %q in the conversation:\n%s", want, taskText(a))
	}

	// AND THE ROOM'S HEADER STATES IT, beside the model, where the page already
	// says what is true of this work right now.
	drive(t, a, key("enter"))
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatalf("enter did not open the focused node's room")
	}
	if head := plain(a.roomHeadWord(120)); !strings.Contains(head, "thinking low") {
		t.Fatalf("the room's header does not state the rung: %q", head)
	}
	// And the chord means the same thing from inside the page it opened.
	a.railTake(false)
	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 2 || agent.asked[1] != "task 7:medium" {
		t.Fatalf("the room's ctrl+v asked %v", agent.asked)
	}
}

// A REFUSED RUNG KEEPS THE ENGINE'S OWN SENTENCE — a node that settled in the
// instant between the frame and the press is told, never ignored.
func TestARefusedTaskRungSaysWhatTheEngineSaid(t *testing.T) {
	a, agent := effortTaskApp(t)
	agent.refusal = errors.New("task 7 is done, not running")
	drive(t, a, key("ctrl+v"))
	if !strings.Contains(taskText(a), "task 7 is done, not running") {
		t.Fatalf("the engine's refusal never reached the conversation:\n%s", taskText(a))
	}
}

// A SESSION WITH NO DOOR ONTO THE LADDER SAYS SO, and its roster names no key.
func TestATaskSurfaceWithNoDoorNamesNoKeyAndSaysSo(t *testing.T) {
	a, _, _ := roomApp(t)
	a.railTake(true)
	a.railWhere = railSpot{id: 7}
	if hint := a.railHoldHintWord(); strings.Contains(hint, "ctrl+v") {
		t.Fatalf("a roster with no door still named the chord: %q", hint)
	}
	drive(t, a, key("ctrl+v"))
	if !strings.Contains(taskText(a), taskEffortUnavailableWord) {
		t.Fatalf("the surface did not say the door is missing:\n%s", taskText(a))
	}
}

// ── the surfaces the rule does not cover ────────────────────────────────────

// A CONVERSATION'S ROW ON HOME IS NOT ONE OF THE THREE SCOPES. Its rung belongs
// to the window that session is open in, and a list must not reach into it.
func TestCtrlVOnAConversationRowChangesNothing(t *testing.T) {
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "aaaa000000000001", "one", lab.workspace("alpha"), time.Now())
	a := lab.app(transcript)
	dir := t.TempDir()
	a.profileDir = dir
	band := &effortBand{standBand: &standBand{}}
	band.wire(a)
	a.openHome()
	line, ok := a.home.previewLine()
	if !ok || line.kind != homeSession {
		t.Fatalf("the cursor is not on a conversation row: %+v", line)
	}
	drive(t, a, key("ctrl+v"))
	if got := config.DefaultEffortAt(dir); got != effort.Ship {
		t.Fatalf("a press on a conversation row moved the install's rung to %q", got)
	}
	if len(band.rungs) != 0 {
		t.Fatalf("a press on a conversation row wrote an item's rung: %v", band.rungs)
	}
	if a.home.msg != "" {
		t.Fatalf("a key with no door under it explained itself: %q", a.home.msg)
	}
	// AND THE CARD NAMES NO KEY FOR IT, which is the same law read rather than
	// pressed: a chord with a visible door beside it is a promise, and this
	// surface cannot keep that one.
	//
	// THE LAW THAT DIED IS "THE CARD STATES NO RUNG". It used to be asserted here
	// that `thinking` appeared nowhere on a conversation's card, on the argument
	// that printing the machine's default beside a chat would be advertising a
	// fact about the install as a fact about the chat. SCREEN 1d overrules it: it
	// spells the facts line of a CONVERSATION'S card `spent $1.63 · 3.6M tokens ·
	// thinking high`, and the owner ordered the design followed exactly
	// (FIDELITY.md item 8). So the clause is there, it is the INSTALL'S rung —
	// what work started from this card would think at — and place_home.go's
	// [app.homeCardFacts] says so in as many words. What survives untouched is the
	// half this test is really about: no key is offered, because there is nothing
	// here the key could honestly write.
	card := strings.Join(homeCardFor(t, a, a.file), "\n")
	if !strings.Contains(card, "thinking "+effort.Ship.String()) {
		t.Fatalf("the card does not state the rung work started here would think at:\n%s", card)
	}
	if strings.Contains(card, effortKeyClause) || strings.Contains(card, "ctrl+v") {
		t.Fatalf("a conversation's card named a key for a rung it cannot move:\n%s", card)
	}
}

// AND THE CONVERSATION ITSELF IS NOT, EITHER — not from this lane. With no
// roster hold and no room, the chord reaches nothing that would write.
func TestCtrlVWithNoTaskUnderTheCursorAsksTheEngineNothing(t *testing.T) {
	a, agent := effortTaskApp(t)
	a.railTake(false)
	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 0 {
		t.Fatalf("a press with nothing under the cursor asked the engine %v", agent.asked)
	}
}

// ── the guards ──────────────────────────────────────────────────────────────

// AN OPEN OVERLAY ABOVE THE SURFACE WINS, and it wins because the chord is read
// from inside each surface's own handler rather than above them — which is the
// precedence law input.go states, tested from the keystroke rather than from
// the reading of it (inputguard_test.go's rule).
func TestAnOverlayAboveTheSurfaceKeepsTheChord(t *testing.T) {
	t.Run("the settings panel over home", func(t *testing.T) {
		lab := newHomeLab(t)
		transcript := lab.session("alpha", "aaaa000000000001", "one", lab.workspace("alpha"), time.Now())
		a := lab.app(transcript)
		dir := t.TempDir()
		a.profileDir = dir
		a.openHome()
		a.raisePlace(pageSettings)
		drive(t, a, key("ctrl+v"))
		if got := config.DefaultEffortAt(dir); got != effort.Ship {
			t.Fatalf("a chord under an open settings panel moved the install's rung to %q", got)
		}
	})

	t.Run("home over the roster", func(t *testing.T) {
		a, agent := effortTaskApp(t)
		a.openHome()
		drive(t, a, key("ctrl+v"))
		if len(agent.asked) != 0 {
			t.Fatalf("a chord under home reached the roster behind it: %v", agent.asked)
		}
	})

	t.Run("the task page over the room", func(t *testing.T) {
		a, agent := effortTaskApp(t)
		a.railTake(false)
		drive(t, a, key("enter"))
		a.railTake(true)
		a.railWhere = railSpot{id: 7}
		a.raisePlace(pageTasks)
		drive(t, a, key("ctrl+v"))
		if len(agent.asked) != 0 {
			t.Fatalf("a chord under the task page reached the work behind it: %v", agent.asked)
		}
	})

	t.Run("an approval question over everything", func(t *testing.T) {
		a, agent := effortTaskApp(t)
		drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
			Kind: session.EventConsentRequest, ID: 1, Tool: "bash", Hint: "rm -rf build",
		}})
		if !a.asking() {
			t.Fatal("the question is not up, so this proves nothing")
		}
		drive(t, a, key("ctrl+v"))
		if len(agent.asked) != 0 {
			t.Fatalf("a chord under an approval question reached the work behind it: %v", agent.asked)
		}
	})
}

// ── 5. the four surfaces share one chord, and the nearest one wins ───────────

// THE OVERLAY ON TOP IS THE SURFACE YOU ARE STANDING ON. The chord means "move
// the rung of the thing you are standing on", and the conversation's own ladder
// drawn over the message box (effortchip.go) is nearer than the roster behind
// it — so with that list up, ctrl+v walks the LIST and no task's rung moves.
//
// It is pinned because the two surfaces bind one chord in two routers read one
// after the other (app.go: railKey, then roomKey, then input.go's plain switch),
// and the roster's guard did not know the chooser existed: the chord moved a
// task while the conversation's five rungs sat open on screen, and ↑↓, enter and
// esc were taken off the list as well.
func TestTheOpenThinkingLadderKeepsTheChordFromTheRosterBehindIt(t *testing.T) {
	a, agent := effortTaskApp(t)
	a.effPick.start(effort.Low)

	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 0 {
		t.Fatalf("the chord reached a task while the conversation's ladder was open: %v", agent.asked)
	}
	if !a.effPick.open {
		t.Fatal("the chooser closed under a chord that belongs to it")
	}
	if a.effPick.cursor != 1 {
		t.Fatalf("the chord left the ladder's cursor at %d, want one step down", a.effPick.cursor)
	}
	// AND THE REST OF THE LIST'S OWN KEYS COME WITH IT, which is the whole of
	// what "modal" means here: a list a person can see and cannot drive is worse
	// than no list at all.
	drive(t, a, key("down"))
	if a.effPick.cursor != 2 {
		t.Fatalf("↓ left the ladder's cursor at %d, want two steps down", a.effPick.cursor)
	}
	drive(t, a, key("esc"))
	if a.effPick.open {
		t.Fatal("esc did not close the ladder it was aimed at")
	}
	// And with the list away the roster has the chord back, unchanged.
	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 1 || agent.asked[0] != "task 7:low" {
		t.Fatalf("the roster did not get the chord back: %v", agent.asked)
	}
}

// AND A ROOM STANDS DOWN FOR IT ON THE SAME TERMS, because a room is a page onto
// one node and the ladder over the box is nearer than the page behind it.
func TestTheOpenThinkingLadderKeepsTheChordFromTheRoomBehindIt(t *testing.T) {
	a, agent := effortTaskApp(t)
	drive(t, a, key("enter"))
	if !a.roomOpen() {
		t.Fatal("enter did not open the focused node's room")
	}
	a.railTake(false)
	a.effPick.start(effort.Low)

	drive(t, a, key("ctrl+v"))
	if len(agent.asked) != 0 {
		t.Fatalf("the chord reached the room's node while the ladder was open: %v", agent.asked)
	}
	if a.effPick.cursor != 1 {
		t.Fatalf("the chord left the ladder's cursor at %d, want one step down", a.effPick.cursor)
	}
}
