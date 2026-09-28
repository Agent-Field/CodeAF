package tui3

// ── A PROGRAM'S TASK READ THROUGH SOMEBODY ELSE'S CONVERSATION ──────────────
//
// A task handed to senior-dev has no worker journal: what the program did is
// its conversation with codeaf, on the task's stored page, and the page a
// person reads is its actions (taskconversation.go). A guest page onto such a
// task used to read the one thing it knew how to read — the owner's journal —
// and drew a header wearing `[senior-dev]` over a body with nothing in it.
//
// These tests hold the page to the program's room this window draws for its
// own run, READ-ONLY: the actions under their steps, `ctrl+y` to the raw calls,
// no stop, no steer, and every read made through the owner's view and never
// through this window's own store, whose task of the same number is different
// work.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// enterAwayPumped presses the away row and settles the attach AND the page's
// first reads, which [enterAway] leaves unrun.
func enterAwayPumped(t *testing.T, a *app) {
	t.Helper()
	awayRowOf(t, a)
	cmd := a.taskSheetEnter()
	if cmd == nil {
		t.Fatal("enter over another window's running work did nothing at all")
	}
	msg, ok := cmd().(taskOwnerMsg)
	if !ok {
		t.Fatalf("enter did not ask the engine for the owner: %T", cmd())
	}
	drain(t, a, a.tookTaskOwner(msg))
}

// guestProgramLab is [guestLab] with the owner's task 7 handed to senior-dev:
// its store answers the program's page for it.
func guestProgramLab(t *testing.T) (*app, *guestDoor) {
	t.Helper()
	a, door := guestLab(t)
	door.pages = map[string]session.PlanTaskPage{"7": programPage(programRow(), programTurns())}
	return a, door
}

// A PROGRAM'S TASK OPENED FROM ANOTHER WINDOW IS ITS ACTIONS, and the page is
// read-only exactly as every other guest page is.
func TestAGuestPageOntoAProgramsTaskDrawsItsActions(t *testing.T) {
	a, door := guestProgramLab(t)
	enterAwayPumped(t, a)

	if !a.roomIsGuest() {
		t.Fatal("the row opened no reading page")
	}
	if a.programOf() == nil {
		t.Fatalf("a program's task opened from another window is not its actions:\n%s", roomText(a))
	}
	// THE OWNER'S STORE WAS ASKED, BY THE TASK'S OWN NUMBER.
	if len(door.pageAsked) == 0 || door.pageAsked[0] != "7" {
		t.Fatalf("the owner's store was asked for %v, want task 7", door.pageAsked)
	}
	lines := strings.Split(roomText(a), "\n")
	if !underStep(lines, "explore", "read internal/auth/middleware.go") {
		t.Fatalf("the page does not draw the program's reading under EXPLORE:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(roomText(a), programTabSaid) {
		t.Fatalf("the page does not open on the actions:\n%s", roomText(a))
	}
	// AND IT STOPS ASKING FOR A JOURNAL THE PROGRAM NEVER WROTE.
	if a.readRoomRecord() != nil {
		t.Fatal("a program's guest page still reads the owner's journal on its beat")
	}
	if a.room.loading {
		t.Fatal("a program's guest page still says it is loading a conversation")
	}

	// THE PAGE STAYS A READING. No stop, no steer, and the box says what it is.
	if !a.stopHere().empty() {
		t.Fatal("a program's guest page offers a stop, which would end this window's own task 7")
	}
	box, _, _ := a.inputBlock(80)
	if lane := plain(strings.Join(a.roomSteerLaneRows(box, 80), "")); !strings.Contains(lane, roomGuestLane) {
		t.Fatalf("a program's guest page's box offers %q", lane)
	}
	a.input.setText("pause it")
	a.steer()
	if got := a.input.String(); got != "pause it" {
		t.Fatalf("a program's guest page spent the words: %q", got)
	}
	if body := roomText(a); !strings.Contains(body, roomGuestReadingWord) {
		t.Fatalf("enter on a program's guest page does not say it is reading:\n%s", body)
	}
	if trail := a.roomTrail(); !strings.HasPrefix(trail, roomGuestOwnerWord) {
		t.Fatalf("the page's trail hangs off this conversation: %q", trail)
	}

	// `ctrl+y` TURNS IT TO THE RAW CALLS AND BACK, AND THE KEY ROW SAYS SO.
	a.input.setText("")
	if hint := a.roomHint(); hint != programCallsWord {
		t.Fatalf("the key row reads %q, want %q", hint, programCallsWord)
	}
	drive(t, a, key(programCallsKey))
	if text := roomText(a); !strings.Contains(text, "I'll read the middleware and the store first.") {
		t.Fatalf("ctrl+y did not turn the page to the calls:\n%s", text)
	}
	drive(t, a, key(programCallsKey))
	if text := roomText(a); !strings.Contains(text, programTabSaid) {
		t.Fatalf("ctrl+y did not turn the page back to the actions:\n%s", text)
	}

	// THE LOCAL TASK 7 IS NOT A PROGRAM'S AND WAS NOT TOUCHED.
	if node := a.tasks[7]; node == nil || node.state != session.TaskRunning || node.program != "" {
		t.Fatalf("the local task 7 was changed by a page that was never about it: %+v", node)
	}
	a.closeRoom()
	if door.closed != 1 {
		t.Fatalf("closing the page released the view %d times", door.closed)
	}
}

// AN ORDINARY TASK OPENED FROM ANOTHER WINDOW IS THE PAGE IT ALWAYS WAS. The
// owner's store answering a page with no program on it — or a door with no
// store reader at all — leaves the journal reading exactly where it was.
func TestAGuestPageOntoAnOrdinaryTaskIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages map[string]session.PlanTaskPage
	}{
		{"no store reader", nil},
		{"nothing stored", map[string]session.PlanTaskPage{}},
		{"stored without a program", map[string]session.PlanTaskPage{"7": {Row: session.PlanTaskRow{ID: "t-7", Title: "Port the parser", Status: "running"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, door := guestLab(t)
			door.pages = tc.pages
			enterAwayPumped(t, a)
			if !a.roomIsGuest() {
				t.Fatal("the row opened no reading page")
			}
			if a.programOf() != nil {
				t.Fatal("an ordinary task opened from another window was turned into a program's page")
			}
			if door.read == 0 {
				t.Fatal("an ordinary guest page did not read the owner's journal")
			}
			if a.readRoomRecord() == nil {
				t.Fatal("an ordinary guest page stopped reading the owner's journal")
			}
			if body := roomText(a); !strings.Contains(body, roomGuestStaleWord) {
				t.Fatalf("an ordinary guest page with no owner lane lost its caveat:\n%s", body)
			}
		})
	}
}

// A PROGRAM'S GUEST PAGE WHOSE CONVERSATION WAS REPLACED KEEPS WHAT IT READ AND
// STOPS, as every guest page does: the engine's refusal arrives on the store's
// read instead of the journal's, and it is the same final answer.
func TestAProgramsGuestPageWhoseConversationWasReplacedStops(t *testing.T) {
	a, door := guestProgramLab(t)
	door.watching()
	enterAwayPumped(t, a)
	if a.programOf() == nil {
		t.Fatal("no program's guest page to lose")
	}
	door.pageErr = errors.New("engine: that conversation is not open here any more")
	a.programOf().readAt = a.now().Add(-elsewhereEvery)
	drain(t, a, a.programRoomRead())

	if !a.room.guest.lost {
		t.Fatal("the page did not take the engine's final answer")
	}
	if door.left != 1 {
		t.Fatalf("the owner's lane was released %d times at the final answer, want once", door.left)
	}
	body := roomText(a)
	if !strings.Contains(body, taskGuestGoneWord) || !strings.Contains(body, programTabSaid) {
		t.Fatalf("the lost page does not keep what it read and say what happened:\n%s", body)
	}
	asked := len(door.pageAsked)
	if cmd := a.programRoomRead(); cmd != nil {
		drain(t, a, cmd)
	}
	if len(door.pageAsked) != asked {
		t.Fatal("a lost page went on asking the owner's store")
	}
	a.closeRoom()
	if door.left != 1 || door.closed != 1 {
		t.Fatalf("leaving released the lane %d times and the connection %d times, want once each", door.left, door.closed)
	}
}

// THE OWNER'S NOTICE SETTLES A PROGRAM'S GUEST PAGE, and the foot names the
// owner rather than this window's main.
func TestAProgramsGuestPageSettlesOnTheOwnersWord(t *testing.T) {
	a, door := guestProgramLab(t)
	door.watching()
	enterAwayPumped(t, a)
	if a.programOf() == nil {
		t.Fatal("no program's guest page")
	}
	// THE STORE HAS ENDED THE RUN BY THE TIME THE OWNER SAYS SO, and the page
	// reads it once more at that moment: the landing is on that last page.
	landed := programRow()
	landed.Status = "done"
	door.pages["7"] = programPage(landed, programTurns())
	asked := len(door.pageAsked)
	drain(t, a, ownerSays(t, a, session.Event{
		Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: 7, Title: "rewrite the auth middleware", State: session.TaskDone, Program: "senior-dev"},
	}))
	if !a.room.done {
		t.Fatal("the owner said its work landed and the page went on saying it was running")
	}
	if len(door.pageAsked) != asked+1 || a.programOf().page.Row.Status != "done" {
		t.Fatalf("the landing was not read from the owner's store: %d reads after it, page %q",
			len(door.pageAsked)-asked, a.programOf().page.Row.Status)
	}
	body := roomText(a)
	if !strings.Contains(body, refusalOwnerLead+"docs pass") || strings.Contains(body, refusalMainDoor) {
		t.Fatalf("the landed program's guest page does not name the owner's door:\n%s", body)
	}
	a.closeRoom()
}

// A WINDOW ON THE ENGINE ROAD DRAWS THE OTHER CONVERSATIONS' WORK AND OPENS IT.
// Bare `codeaf` holds a connection to its engine, and a connection has no
// reading of the disk ([elsewhereAgent] is the agent's own), so the rows of work
// another conversation was running were never drawn there — and the door behind
// them, [app.openOwnerRoom], could not be reached from any real window. The
// launch now hands the surface the reading ([Options.Elsewhere]), asked with the
// transcript this window is drawing, and the row opens its reading page.
func TestAnEngineWindowReadsTheOtherConversationsOffTheDiskAndOpensThem(t *testing.T) {
	a, door := guestLab(t)
	door.pages = map[string]session.PlanTaskPage{"7": programPage(programRow(), programTurns())}
	// NOTHING READ YET, and the agent under this window answers no reading.
	a.away = elsewhereCache{}
	if _, answers := a.agent.(elsewhereAgent); answers {
		t.Fatal("the fixture's agent reads the disk itself, so this test would prove nothing")
	}
	var asked []string
	a.elsewhereOf = func(file string, now time.Time) session.Elsewhere {
		asked = append(asked, file)
		return session.NewElsewhere(now, map[string]string{"the-other-window": "docs pass"},
			window("the-other-window", session.PresenceTask{
				ID: "7", Title: "Port the parser", State: string(session.TaskRunning)}))
	}
	enterAwayPumped(t, a)
	if len(asked) == 0 || asked[0] != a.file {
		t.Fatalf("the reading was asked for %q, want this window's own transcript %q", asked, a.file)
	}
	if !a.roomIsGuest() || a.programOf() == nil {
		t.Fatalf("the other conversation's program task did not open its reading page:\n%s", roomText(a))
	}
	a.closeRoom()
}
