package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	// name on the line is the conversation's again.
	a.closeRoom()
	line = statusText(a)
	if !strings.Contains(line, "deepseek-v4-flash") {
		t.Fatalf("closing the room did not restore the conversation's model:\n%q", line)
	}
	if strings.Contains(line, roomModelLead) || strings.Contains(line, "glm-5.2") {
		t.Fatalf("the closed room's model is still on the line:\n%q", line)
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

// THE DIAL IS THE CONVERSATION'S. The reasoning level is spliced onto the model
// segment by lending a.model its suffixed form (view.go's [app.statusRow]), and
// a task model must never wear it — a knob the person turned for this session,
// printed on a node that was never run with it, is a fact invented on screen.
func TestATaskModelNeverWearsTheConversationsReasoningSuffix(t *testing.T) {
	a, fake := roomModelApp(t, "z-ai/glm-5.2")
	fake.levels = map[string]string{"deepseek/deepseek-v4-flash": "high"}

	line := statusText(a)
	if strings.Contains(line, ":high") {
		t.Fatalf("the task's model is wearing the conversation's reasoning level:\n%q", line)
	}
	// And the level is real: it is on the line the moment the window is the
	// conversation again, which is what makes the absence above a decision.
	a.closeRoom()
	if line := statusText(a); !strings.Contains(line, "deepseek-v4-flash:high") {
		t.Fatalf("the conversation's own level went missing with the room:\n%q", line)
	}
}

// THE SEGMENT IS A FACT AND NOT A DOOR while a room is open: the picker moves
// the CONVERSATION's model, and a name that opened it while naming the TASK's
// would swap the session's engine under a person who pressed the id they were
// reading.
func TestPressingATaskModelDoesNotOpenTheSessionsPicker(t *testing.T) {
	a, _ := roomModelApp(t, "z-ai/glm-5.2")
	a.width, a.height = 120, 24
	a.touch()

	// The frame is what records the columns, so it is drawn before they are read.
	rows := strings.Split(plain(frame(a)), "\n")
	if a.modelSpan.pressable() {
		t.Fatalf("the room's status row recorded a press target for the task's model: %+v", a.modelSpan)
	}
	// Press the cells the task's model is actually drawn on — the name is on
	// screen, and this asserts that pressing it does nothing rather than that the
	// test could not find it.
	line := rows[len(rows)-1]
	at := strings.Index(line, "glm-5.2")
	if at < 0 {
		t.Fatalf("the task's model is not on the status row at all:\n%q", line)
	}
	drive(t, a, tea.MouseClickMsg{X: at + 1, Y: a.height - 1, Button: tea.MouseLeft})
	if a.pick.open {
		t.Fatal("pressing the task's model opened the picker over the conversation's model")
	}

	// AND THE DOOR COMES BACK WITH THE NAME IT BELONGS TO. One esc later the
	// segment is the conversation's model again, and pressing it is the picker.
	a.closeRoom()
	_ = frame(a)
	if !a.modelSpan.pressable() {
		t.Fatal("closing the room did not give the model segment its columns back")
	}
	drive(t, a, tea.MouseClickMsg{X: a.modelSpan.from + 1, Y: a.height - 1, Button: tea.MouseLeft})
	if !a.pick.open {
		t.Fatal("the conversation's model stopped opening the picker after a room closed")
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
