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

// ── home, the cursor at rest: the install's own rung ─────────────────────────

// restingHome is home open with the cursor on no row at all — the state whose
// card is the machine's own (homemachine.go) — and a profile to write into.
func restingHome(t *testing.T) (*app, string) {
	t.Helper()
	lab := newHomeLab(t)
	transcript := lab.session("alpha", "aaaa000000000001", "one", lab.workspace("alpha"), time.Now())
	a := lab.app(transcript)
	a.profileDir = t.TempDir()
	a.openHome()
	a.home.cursor, a.home.picked = homeRest, false
	a.home.build()
	if !a.home.resting() {
		t.Fatal("the cursor is not at rest, so the card is not the machine's")
	}
	return a, a.profileDir
}

// THE MACHINE'S CARD STATES THE INSTALL'S RUNG AND CTRL+V MOVES IT, and the
// write goes to the profile rather than to anything this window holds.
func TestHomeAtRestStatesTheInstallsRungAndCtrlVMovesIt(t *testing.T) {
	a, dir := restingHome(t)

	// An install nobody has touched reads at the shipped rung, and the card says
	// so as a quiet clause.
	card := machineCardText(a, 40)
	if !strings.Contains(card, "thinking "+effort.Ship.String()) {
		t.Fatalf("the machine card does not state the install's rung:\n%s", card)
	}
	// AND THE CARD NAMES THE KEY, because a chord with no visible door beside it
	// is the one thing docs/DESIGN-LANGUAGE.md refuses outright.
	if !strings.Contains(card, effortKeyClause) {
		t.Fatalf("the machine card names no way to move it:\n%s", card)
	}

	drive(t, a, key("ctrl+v"))
	if got := config.DefaultEffortAt(dir); got != effort.XHigh {
		t.Fatalf("ctrl+v wrote %q to the profile, want the next rung up from %q", got, effort.Ship)
	}
	if card := machineCardText(a, 40); !strings.Contains(card, "thinking xhigh") {
		t.Fatalf("the card did not follow the write:\n%s", card)
	}
	if !strings.Contains(a.home.msg, "thinking xhigh") {
		t.Fatalf("home said %q about the change", a.home.msg)
	}
}

// A WINDOW WITH NO PROFILE DRAWS NO RUNG AND NAMES NO KEY. A capability that
// cannot work is absent, not broken — and a legend advertising a keystroke that
// would refuse is the broken half.
func TestAWindowWithNoProfileSaysNothingAboutThinking(t *testing.T) {
	a, _ := restingHome(t)
	a.profileDir = ""
	card := machineCardText(a, 40)
	if strings.Contains(card, "thinking") || strings.Contains(card, "ctrl+v") {
		t.Fatalf("a window with nowhere to write still drew the rung:\n%s", card)
	}
	drive(t, a, key("ctrl+v"))
	if a.home.msg != homeEffortNoProfile {
		t.Fatalf("home said %q, want %q", a.home.msg, homeEffortNoProfile)
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

func itemHome(t *testing.T) (*app, *effortBand) {
	t.Helper()
	lab := newHomeLab(t)
	work := lab.workspace("alpha")
	transcript := lab.session("alpha", "aaaa000000000001", "one", work, time.Now())
	band := &effortBand{standBand: &standBand{items: []standing.Item{
		bandItem("one", "remind me on Fridays", work, standing.WhenEvery, "Fridays"),
	}}}
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
	a, dir := restingHome(t)
	band := &effortBand{standBand: &standBand{}}
	band.wire(a)
	drive(t, a, key("down"))
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
		a, dir := restingHome(t)
		a.sheet.open = true
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
		a.taskSheet.open = true
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
