package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE STATUS LINE'S MODEL, WHILE A ROOM IS OPEN ───────────────────────────
//
// The bug these tests hold shut: a conversation on one model, a task launched
// on another, the task genuinely running on the one it was given — and the only
// always-visible model name on the screen still saying the CONVERSATION's, from
// inside the task's own room. A person read it and concluded their task had run
// on the default.
//
// The law is render.go's own, applied rather than excepted: the status row is
// about THE WINDOW, and while a room is open the window IS that task.

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

// statusText is the status line as a reader sees it, drawn through the frame's
// own door (view.go's [app.statusRow]) so the reasoning splice is on it.
func statusText(a *app) string {
	return plain(strings.Join(a.statusRow(a.width), "\n"))
}

// AT WIDE WIDTH THE SEGMENT NAMES THE ROOM'S NODE, and it says whose model it
// is: the task's, led by the word, and never the conversation's.
func TestTheStatusLineNamesTheOpenRoomsModel(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")

	line := statusText(a)
	for _, want := range []string{"Ship the parser fix", roomModelLead + "glm-5.2"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the status line is missing %q while the room is open:\n%q", want, line)
		}
	}
	// THE CONVERSATION'S MODEL IS NOT ON THE LINE while somebody is standing in a
	// room that runs on something else. This is the whole bug.
	if strings.Contains(line, "deepseek") {
		t.Fatalf("the room's status line still names the conversation's model:\n%q", line)
	}
	// It is the BASENAME, which is the row's own law about its scarce width — the
	// vendor is nine cells that never vary (render.go's [app.identity]).
	if strings.Contains(line, "z-ai/") {
		t.Fatalf("the task's model is drawn as a routing address, not a name:\n%q", line)
	}

	// AND ESC GIVES EVERYTHING BACK. The window is the conversation again, so the
	// chip goes off the row entirely — and the conversation's own model is where
	// it always is out of a room: on the seam above the box, which is the only
	// place it is written since 2026-09-09 (foot.go's [app.seamIdentity]).
	a.closeRoom()
	line = statusText(a)
	if strings.Contains(line, roomModelLead) || strings.Contains(line, "glm-5.2") {
		t.Fatalf("the closed room's model is still on the line:\n%q", line)
	}
	if strings.Contains(line, "deepseek") {
		t.Fatalf("the conversation's model moved onto the status row:\n%q", line)
	}
	if seam := plain(a.legend(a.width)); !strings.Contains(seam, "deepseek-v4-flash") {
		t.Fatalf("closing the room did not restore the conversation's model to the seam:\n%q", seam)
	}
}

// AND AT PHONE WIDTH IT IS ROW 2 OF THE DECK, under a row 1 that has already
// renamed itself to the task (statusdeck.go).
func TestTheDecksModelChipNamesTheOpenRoomsModel(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 44, 20
	a.touch()

	deck := deckRowsOf(t, a)
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
	if deck := deckRowsOf(t, a); !strings.Contains(deck[1], "deepseek-v4-flash") {
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

	for _, width := range []int{200, 44} {
		a.width = width
		a.touch()
		line := statusText(a)
		if strings.Contains(line, roomModelLead) {
			t.Fatalf("at %d columns a node with no model still led one:\n%q", width, line)
		}
		if strings.Contains(line, "deepseek") {
			t.Fatalf("at %d columns the room borrowed the session's model as a fallback:\n%q",
				width, line)
		}
	}
}

// THE DIAL IS THE CONVERSATION'S. The reasoning level rides the conversation's
// own model wherever that is written — the seam's word builds it (foot.go's
// [app.seamIdentity]) and the phone deck's row is lent it (view.go's
// [app.statusRow]) — and a task model must never wear it: a knob the person
// turned for this session, printed on a node that was never run with it, is a
// fact invented on screen.
func TestATaskModelNeverWearsTheConversationsReasoningSuffix(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	fake.levels = map[string]string{"deepseek/deepseek-v4-flash": "high"}
	settleLevels(a, "deepseek/deepseek-v4-flash")

	line := statusText(a)
	if strings.Contains(line, ":high") {
		t.Fatalf("the task's model is wearing the conversation's reasoning level:\n%q", line)
	}
	// And the level is real: it is the conversation's, held against the
	// conversation's own model, and the sheet spells it there whole
	// (statusdeck.go). THE SEAM DOES NOT SPELL IT AT ALL SINCE 2026-09-09 — that
	// line carries one thinking rung and it is the resolved one, which this level
	// is folded into (effortchip.go) — so the absence above is a decision about
	// the ROOM's model rather than about the level having gone.
	a.closeRoom()
	if got := a.reasoningFor("deepseek/deepseek-v4-flash"); got != "high" {
		t.Fatalf("the conversation's own level went missing with the room: %q", got)
	}
	if seam := plain(a.legend(a.width)); strings.Contains(seam, ":high") {
		t.Fatalf("the seam still spells a level onto the model id:\n%q", seam)
	}
}

// THE SEGMENT IS A DOOR ONTO WHAT IT NAMES, and inside a RUNNING node's room
// what it names is that node: the picker it opens moves that task and nothing
// else, and the conversation's own model is untouched by it.
func TestPressingARunningTasksModelRetargetsThatTaskAlone(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	a.touch()

	// The frame is what records the columns, so it is drawn before they are read.
	rows := strings.Split(plain(frame(a)), "\n")
	if !a.modelSpan.pressable() {
		t.Fatal("a running node's room recorded no press target for its model")
	}
	// The columns the render recorded are the columns the name is actually drawn
	// on, which is what makes the press a press on the thing and not on a number.
	line := rows[len(rows)-1]
	if at := strings.Index(line, "glm-5.2"); at < 0 || !a.modelSpan.holds(at) {
		t.Fatalf("the span %+v does not cover the task's model on the row:\n%q", a.modelSpan, line)
	}
	drive(t, a, tea.MouseClickMsg{X: a.modelSpan.from + 1, Y: a.height - 1, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.modelSpan.from + 1, Y: a.height - 1, Button: tea.MouseLeft})
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
// esc from a room and the name on the line is the conversation's again, and
// pressing it opens the picker with no task on it.
func TestPressingTheConversationsModelStillOpensTheSessionsPicker(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	a.closeRoom()
	_ = frame(a)

	// Out of a room the door is the SEAM's, at the left of the legend above the
	// box (foot.go's [app.legendModelPress]).
	if !a.seamModelSpan.pressable() {
		t.Fatal("closing the room did not give the model segment its columns back")
	}
	x, y := a.seamModelSpan.from+1, markedRowY(a, chromeLegend, 0)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
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

// Ordinary settled tasks keep the model picker for their next continuation.
func TestASettledTasksModelOpensItsContinuationPicker(t *testing.T) {
	for _, state := range []session.TaskState{
		session.TaskDone, session.TaskFailed, session.TaskUnverified, session.TaskQueued,
	} {
		a, fake := roomModelApp(t, "z-ai/glm-5.2")
		a.width, a.height = 120, 24
		drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Ship the parser fix",
			state, session.TaskNotice{Model: "z-ai/glm-5.2"})})
		rows := strings.Split(plain(frame(a)), "\n")

		if !a.modelSpan.pressable() {
			t.Fatalf("a %s node's model is still a press target: %+v", state, a.modelSpan)
		}
		// The name is still there to be read — this is a door removed, not a fact.
		line := rows[len(rows)-1]
		at := strings.Index(line, "glm-5.2")
		if at < 0 {
			t.Fatalf("a %s node stopped naming its model at all:\n%q", state, line)
		}
		drive(t, a, tea.MouseClickMsg{X: at + 1, Y: a.height - 1, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: at + 1, Y: a.height - 1, Button: tea.MouseLeft})
		if !a.pick.open || a.pick.task != a.room.id {
			t.Fatalf("pressing a %s node's model opened the picker", state)
		}
		if len(fake.retargeted) != 0 {
			t.Fatalf("pressing a %s node's model reached the door: %+v", state, fake.retargeted)
		}
	}
}

// THE SET THAT LIGHTS IS THE SET THE PRESS ACTS ON (hover.go). The model's name
// is pressable at both subjects, so it lights at both — the room's node on the
// status row, the conversation's on the seam above the box — and where it is
// only a fact, it does not.
func TestTheModelSegmentLightsUnderThePointerAtBothItsHomes(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	_ = frame(a)

	a.setHover(a.modelSpan.from+1, a.height-1)
	if !a.hoveringStatusModel() {
		t.Fatal("the running node's model segment does not light under the pointer")
	}
	// One cell to the left of the span is the separator, which is not a control.
	a.setHover(a.modelSpan.from-1, a.height-1)
	if a.hoveringStatusModel() {
		t.Fatal("the model segment lights from outside its own columns")
	}

	// Out in the conversation, the same light on the seam's own columns.
	a.closeRoom()
	_ = frame(a)
	seamRow := markedRowY(a, chromeLegend, 0)
	a.setHover(a.seamModelSpan.from+1, seamRow)
	if !a.hoveringStatusModel() {
		t.Fatal("the conversation's model segment does not light under the pointer")
	}
	a.setHover(a.seamModelSpan.from-1, seamRow)
	if a.hoveringStatusModel() {
		t.Fatal("the seam's model segment lights from outside its own columns")
	}
	a.setHover(a.seamModelSpan.from+1, seamRow)
	// And the hovered row is drawn differently from the resting one, which is what
	// a person actually sees.
	hot := frame(a)
	a.dropHover()
	if cold := frame(a); hot == cold {
		t.Fatal("hovering the model segment changed nothing on the frame")
	}

	// A settled ordinary node lights the same control for its next continuation.
	a2, _ := roomModelApp(t, "z-ai/glm-5.2")
	a2.width, a2.height = 120, 24
	drive(t, a2, streamEventMsg{gen: a2.gen, ev: update(9, "Ship the parser fix",
		session.TaskDone, session.TaskNotice{Model: "z-ai/glm-5.2"})})
	rows := strings.Split(plain(frame(a2)), "\n")
	at := strings.Index(rows[len(rows)-1], "glm-5.2")
	a2.setHover(at+1, a2.height-1)
	if !a2.hoveringStatusModel() {
		t.Fatal("a settled node's continuation picker has no hover")
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

// ── THE CLUSTER IS A ROW, AND A ROW GIVES UP ITS FACTS BEFORE ITS NAME ──────
//
// The two laws below hold each other up, and each one on its own is a bug this
// wave shipped and then fixed.
//
// The FIRST is [rowfit.go]'s: the name is whole until the line cannot hold it.
// It was broken by a fixed cap on the cluster ([roomChipCap], eighteen cells),
// which spent the identity's price at every width — `Ship the parser fix` came
// out `Ship the parser f…` on a row with sixty cells going spare.
//
// The SECOND is what that cap was reaching for and got backwards: a name the row
// genuinely cannot hold must be cut, because the ladder above the cluster
// (render.go's [app.statusLayout]) can only give up SEGMENTS, and a cluster that
// overruns the whole row leaves it nothing to drop but the cluster — at which
// point a hundred-and-twenty-column status line came out as the single word
// `idle`.

// roomStatusLine is the status row at one width, as a reader sees it.
func roomStatusLine(t *testing.T, a *app, width int) string {
	t.Helper()
	a.width = width
	a.touch()
	return statusText(a)
}

// A NAME WITH ROOM TO SPARE IS DRAWN WHOLE, however far past a cap it runs.
func TestTheStatusLinesRoomChipKeepsAWholeNameWhileTheRowHoldsIt(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	// Nineteen cells — four cells past the cap that used to cut it, and nothing
	// like the width of the row it is drawn on.
	if line := roomStatusLine(t, a, 180); !strings.Contains(line, "Ship the parser fix") {
		t.Fatalf("a name the row can afford was cut anyway:\n%q", line)
	}
	if line := roomStatusLine(t, a, 180); !strings.Contains(line, roomModelLead+"glm-5.2") {
		t.Fatalf("the model gave way on a row with cells to spare:\n%q", line)
	}
}

// AND A NAME THE ROW CANNOT HOLD COSTS THE ROW ITS FACTS AND THEN ITS OWN TAIL —
// never the whole line. A hundred and one cells at a hundred and twenty columns
// is the exact frame the `idle` collapse happened at.
func TestALongRoomNameNeverCollapsesTheStatusLine(t *testing.T) {
	long := "Ship the parser fix and the loader flake and the nil-map guard and the key table rewrite as well"
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, long, session.TaskRunning,
		session.TaskNotice{Model: "z-ai/glm-5.2"})})
	a.room.title = long

	for _, width := range []int{160, 120, 100, 80} {
		line := roomStatusLine(t, a, width)
		for _, row := range strings.Split(line, "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("at %d columns the status row is %d wide:\n%q", width, got, row)
			}
		}
		// THE ROW STILL SAYS WHERE YOU ARE. The name is cut, and what is left of
		// it is enough to recognise the work by — never nothing at all, which is
		// what the line collapsed to before the cluster was fitted.
		if !strings.Contains(line, "Ship the") {
			t.Fatalf("at %d columns the status line stopped naming the room:\n%q", width, line)
		}
		if strings.TrimSpace(plain(line)) == "idle" {
			t.Fatalf("at %d columns the whole status line collapsed:\n%q", width, line)
		}
	}
}
