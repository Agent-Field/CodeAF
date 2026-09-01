package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE MODEL, WHILE A ROOM IS OPEN ─────────────────────────────────────────
//
// The bug these tests hold shut: a conversation on one model, a task launched
// on another, the task genuinely running on the one it was given — and the only
// always-visible model name on the screen still saying the CONVERSATION's, from
// inside the task's own room. A person read it and concluded their task had run
// on the default.
//
// The law is the window's own, applied rather than excepted: while a room is
// open the window IS that task. Where the law lives moved with the header: the
// status row's identity cluster is the TOP BAR's now (topbar.go), so at wide
// width the room's model is the bar's model segment — the one fact on the bar
// that is also a door. At phone width nothing moved: the deck's model chip is
// still row 2, and the sheet still says both models.

// roomModelApp is a room open on a node that was launched with this model, on a
// session running something else — the exact shape of the night's bug. An empty
// model is the other half of it: a node nobody published one for.
//
// The model rides the node's FIRST update, which is where the engine settles it
// (task.go: the contract is frozen at admission, and a second update carrying
// nothing new is thrown out as the duplicate it is).
func roomModelApp(t *testing.T, model string) (*app, *roomFake) {
	t.Helper()
	a, fake, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Ship the parser fix",
		session.TaskRunning, session.TaskNotice{Model: model})})
	if got := a.tasks[9].model; got != model {
		t.Fatalf("the node was admitted on %q, want %q", got, model)
	}
	a.openRoom(9, "Ship the parser fix")
	a.touch()
	return a, fake
}

// AT WIDE WIDTH THE TOP BAR NAMES THE ROOM'S NODE, and it says whose model it
// is: the task's, led by the word, and never the conversation's.
func TestTheTopBarNamesTheOpenRoomsModel(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")

	bar := plain(a.topBarWord(a.width))
	for _, want := range []string{"Ship the parser fix", roomModelLead + "glm-5.2"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("the top bar is missing %q while the room is open:\n%q", want, bar)
		}
	}
	// THE CONVERSATION'S MODEL IS NOT ON THE BAR while somebody is standing in a
	// room that runs on something else. This is the whole bug.
	if strings.Contains(bar, "deepseek") {
		t.Fatalf("the room's bar still names the conversation's model:\n%q", bar)
	}
	// It is the BASENAME, which is the surface's own law about its scarce width —
	// the vendor is nine cells that never vary.
	if strings.Contains(bar, "z-ai/") {
		t.Fatalf("the task's model is drawn as a routing address, not a name:\n%q", bar)
	}

	// AND ESC GIVES EVERYTHING BACK. The window is the conversation again, so the
	// name on the bar is the conversation's again — its basename, which is the
	// bar's own law about its scarce width.
	a.closeRoom()
	bar = plain(a.topBarWord(a.width))
	if !strings.Contains(bar, "deepseek-v4-flash") {
		t.Fatalf("closing the room did not restore the conversation's model:\n%q", bar)
	}
	if strings.Contains(bar, roomModelLead) || strings.Contains(bar, "glm-5.2") {
		t.Fatalf("the closed room's model is still on the bar:\n%q", bar)
	}
}

// AND AT PHONE WIDTH IT IS ROW 2 OF THE DECK, under a row 1 that has already
// renamed itself to the task (statusdeck.go) — the phone tier has no top bar,
// and the deck is where the crumb and the model live there.
func TestTheDecksModelChipNamesTheOpenRoomsModel(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 44, 20
	a.touch()

	deck := deckRowsOf(a)
	if got := len(deck); got != deckHeight {
		t.Fatalf("the deck is %d rows in a room, want %d", got, deckHeight)
	}
	if !strings.Contains(deck[1], roomModelLead+"glm-5.2") {
		t.Fatalf("the deck's model chip does not name the room's node:\n%q", deck[1])
	}
	if strings.Contains(strings.Join(deck, "\n"), "deepseek") {
		t.Fatalf("the deck still names the conversation's model in a room:\n%q", deck)
	}

	a.closeRoom()
	if deck := deckRowsOf(a); !strings.Contains(deck[1], "deepseek-v4-flash") {
		t.Fatalf("closing the room did not restore the deck's model chip:\n%q", deck[1])
	}
}

// A NODE THAT PUBLISHED NO MODEL SAYS NOTHING, at either end of the width range,
// and the session's id is NOT the fallback: the engine reads an empty model as
// "the conversation's own" at the moment the node's agent is minted, and the
// dial has been movable ever since — so an unpublished model and a child that
// ran on something the session has since left are the same thing from here
// (room.go's [app.roomModelWord]).
func TestANodeWithNoPublishedModelNamesNoModelAtAll(t *testing.T) {
	a, _ := roomModelApp(t, "")

	// The wide bar.
	if bar := plain(a.topBarWord(a.width)); strings.Contains(bar, roomModelLead) ||
		strings.Contains(bar, "deepseek") {
		t.Fatalf("the bar borrowed the session's model as a fallback:\n%q", bar)
	}

	// And the phone deck.
	a.width, a.height = 44, 20
	a.touch()
	deck := deckRowsOf(a)
	if strings.Contains(strings.Join(deck, "\n"), roomModelLead) ||
		strings.Contains(strings.Join(deck, "\n"), "deepseek") {
		t.Fatalf("the deck borrowed the session's model as a fallback:\n%q", deck)
	}
}

// THE DIAL IS THE CONVERSATION'S. The reasoning level is spliced onto the
// conversation's own model and never onto a task's — a knob the person turned
// for this session, printed on a node that was never run with it, is a fact
// invented on screen. What a task's word may carry is its own effort clause
// (taskeffort.go), which is the node's fact, not this dial.
func TestATaskModelNeverWearsTheConversationsReasoningSuffix(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	fake.levels = map[string]string{"deepseek/deepseek-v4-flash": "high"}
	settleLevels(a, "deepseek/deepseek-v4-flash")

	bar := plain(a.topBarWord(a.width))
	if strings.Contains(bar, ":high") {
		t.Fatalf("the task's model is wearing the conversation's reasoning level:\n%q", bar)
	}
	// And the level is real: it is on the bar the moment the window is the
	// conversation again, which is what makes the absence above a decision.
	a.closeRoom()
	if bar := plain(a.topBarWord(a.width)); !strings.Contains(bar, "deepseek-v4-flash:high") {
		t.Fatalf("the conversation's own level went missing with the room:\n%q", bar)
	}
}

// THE SEGMENT IS A DOOR ONTO WHAT IT NAMES, and inside a RUNNING node's room
// what it names is that node: the picker it opens moves that task and nothing
// else, and the conversation's own model is untouched by it.
func TestPressingARunningTasksModelRetargetsThatTaskAlone(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	a.touch()

	// The bar's draw is what records the span, so it is drawn before it is read.
	bar := plain(a.topBarWord(a.width))
	if !a.modelSpan.pressable() {
		t.Fatal("a running node's room recorded no press target for its model")
	}
	// The columns the bar recorded are the columns the name is actually drawn
	// on, which is what makes the press a press on the thing and not on a number.
	if at := strings.Index(bar, "glm-5.2"); at < 0 || !a.modelSpan.holds(at) {
		t.Fatalf("the span %+v does not cover the task's model on the bar:\n%q", a.modelSpan, bar)
	}
	drive(t, a, tea.MouseClickMsg{X: a.modelSpan.from + 1, Y: 0, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.modelSpan.from + 1, Y: 0, Button: tea.MouseLeft})
	if !a.pick.open {
		t.Fatal("pressing a running task's model opened nothing")
	}
	if a.pick.task != 9 {
		t.Fatalf("the picker opened on task %d, want the room's node 9", a.pick.task)
	}
	// It opens ON THE NODE'S MODEL rather than the session's, so enter confirms.
	if a.pick.current != "z-ai/glm-5.2" {
		t.Fatalf("the picker marks %q as current, want the node's own model", a.pick.current)
	}

	// Choosing goes to the node's own door, and the conversation stays where it is.
	before := a.model
	a.pick.cursor = 0
	chosen, _ := a.pick.choice()
	drive(t, a, key("enter"))
	if len(fake.retargeted) != 1 || fake.retargeted[0] != (modelPick{id: 9, model: chosen.ID}) {
		t.Fatalf("the pick did not reach the node's door: %+v", fake.retargeted)
	}
	if a.model != before {
		t.Fatalf("retargeting a task moved the conversation's model to %q", a.model)
	}
	// And it is written down where every other model change is.
	want, found := "task 9 · model · "+chosen.ID, false
	for _, e := range a.entries {
		if e.kind == entryNote && e.text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the retarget left no note reading %q in the conversation", want)
	}
}

// AND OUT IN THE CONVERSATION THE SAME GESTURE IS THE SESSION'S, unchanged: one
// esc from a room and the name on the bar is the conversation's again, and
// pressing it opens the picker with no task on it.
func TestPressingTheConversationsModelStillOpensTheSessionsPicker(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	a.closeRoom()
	_ = plain(a.topBarWord(a.width))

	if !a.modelSpan.pressable() {
		t.Fatal("closing the room did not give the model segment its columns back")
	}
	drive(t, a, tea.MouseClickMsg{X: a.modelSpan.from + 1, Y: 0, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.modelSpan.from + 1, Y: 0, Button: tea.MouseLeft})
	if !a.pick.open {
		t.Fatal("the conversation's model stopped opening the picker after a room closed")
	}
	if a.pick.task != 0 {
		t.Fatalf("the conversation's picker is pointed at task %d", a.pick.task)
	}
	drive(t, a, key("enter"))
	if len(fake.retargeted) != 0 {
		t.Fatalf("choosing in the conversation's picker retargeted a task: %+v", fake.retargeted)
	}
}

// A NODE THAT IS PAST BEING MOVED KEEPS THE NAME AND LOSES THE DOOR — absent
// affordance, never a failing one. The engine refuses a settled node, so the
// bar records no columns and the press falls through to the row it landed on.
func TestASettledTasksModelIsNotPressable(t *testing.T) {
	for _, state := range []session.TaskState{
		session.TaskDone, session.TaskFailed, session.TaskUnverified, session.TaskQueued,
	} {
		a, fake := roomModelApp(t, "z-ai/glm-5.2")
		a.width, a.height = 120, 24
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Ship the parser fix",
			state, session.TaskNotice{Model: "z-ai/glm-5.2"})})
		bar := plain(a.topBarWord(a.width))

		if a.modelSpan.pressable() {
			t.Fatalf("a %s node's model is still a press target: %+v", state, a.modelSpan)
		}
		// The name is still there to be read — this is a door removed, not a fact.
		at := strings.Index(bar, "glm-5.2")
		if at < 0 {
			t.Fatalf("a %s node stopped naming its model at all:\n%q", state, bar)
		}
		drive(t, a, tea.MouseClickMsg{X: at + 1, Y: 0, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: at + 1, Y: 0, Button: tea.MouseLeft})
		if a.pick.open {
			t.Fatalf("pressing a %s node's model opened the picker", state)
		}
		if len(fake.retargeted) != 0 {
			t.Fatalf("pressing a %s node's model reached the door: %+v", state, fake.retargeted)
		}
	}
}

// THE SET THAT LIGHTS IS THE SET THE PRESS ACTS ON (hover.go). The model
// segment is pressable at both subjects, so it lights at both — and where it is
// only a fact, it does not.
func TestTheTopBarsModelSegmentLightsUnderThePointer(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	_ = plain(a.topBarWord(a.width))

	a.setHover(a.modelSpan.from+1, 0)
	if !a.hoveringStatusModel() {
		t.Fatal("the running node's model segment does not light under the pointer")
	}
	// One cell to the left of the span is the separator, which is not a control.
	a.setHover(a.modelSpan.from-1, 0)
	if a.hoveringStatusModel() {
		t.Fatal("the model segment lights from outside its own columns")
	}

	// Out in the conversation, the same segment and the same light.
	a.closeRoom()
	_ = plain(a.topBarWord(a.width))
	a.setHover(a.modelSpan.from+1, 0)
	if !a.hoveringStatusModel() {
		t.Fatal("the conversation's model segment does not light under the pointer")
	}
	// And the hovered bar is drawn differently from the resting one, which is
	// what a person actually sees.
	hot := a.topBarWord(a.width)
	a.dropHover()
	if cold := a.topBarWord(a.width); hot == cold {
		t.Fatal("hovering the model segment changed nothing on the bar")
	}

	// A node past being moved has no span, so nothing lights over its name.
	a2, _ := roomModelApp(t, "z-ai/glm-5.2")
	a2.width, a2.height = 120, 24
	drive(t, a2, streamEventMsg{gen: a2.gen, ev: update(9, "Ship the parser fix",
		session.TaskDone, session.TaskNotice{Model: "z-ai/glm-5.2"})})
	bar := plain(a2.topBarWord(a2.width))
	at := strings.Index(bar, "glm-5.2")
	a2.setHover(at+1, 0)
	if a2.hoveringStatusModel() {
		t.Fatal("a settled node's model lights under the pointer with no door behind it")
	}
}

// At phone width the deck answers every press on its two rows, so an inert chip
// is not a press that falls through — it is a press that lands on the SHEET,
// which names the conversation's model and the task's on two labelled lines.
// That is the honest destination: it says both, and only the conversation's is a
// door (statusdeck.go's [app.deckItems]).
func TestTheDecksTaskChipOpensTheSheetAndNotThePicker(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 44, 20
	a.touch()
	_ = frame(a)

	if a.modelSpan.pressable() {
		t.Fatalf("the deck recorded a press target for the task's model: %+v", a.modelSpan)
	}
	drive(t, a, clickAt(len(deckPad)+1, a.height-1))
	if a.pick.open {
		t.Fatal("pressing the deck's task chip opened the conversation's picker")
	}
	if !a.deck.open {
		t.Fatal("pressing the deck's task chip opened nothing at all")
	}

	// The sheet is where both are recorded, whole address and all.
	body := plain(strings.Join(a.statusSheetLines(), "\n"))
	for _, want := range []string{"deepseek/deepseek-v4-flash", "task model", "z-ai/glm-5.2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the sheet never says %q:\n%s", want, body)
		}
	}
	// And the only line a tap acts on is still the conversation's.
	for _, item := range a.deckItems() {
		if item.act == deckActModel && item.label != "model" {
			t.Fatalf("the %q line is a door to the conversation's picker", item.label)
		}
	}

	// The picker is still one keypress away from the line that names it, which is
	// what makes the chip's inertness a redirection rather than a removal.
	items := a.deckItems()
	for i, item := range items {
		if item.act == deckActModel {
			a.deck.cursor = i
		}
	}
	drive(t, a, key("enter"))
	if !a.pick.open {
		t.Fatal("the sheet's model line stopped opening the picker inside a room")
	}
}

// statusSheetLines is the status sheet as it is drawn, for a test that wants to
// read what it says.
func (a *app) statusSheetLines() []string {
	lines, _, _, _ := a.deckSheetFrame(a.width, a.height)
	return lines
}
