package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// managerColumnApp is the manager in front on a wide frame, with the column
// remembered open.
func managerColumnApp(t *testing.T) *app {
	t.Helper()
	a, _, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.railAway = false
	a.welcome.open = false
	if a.sideKind() != sideKindManager {
		t.Fatal("the manager is not in front")
	}
	return a
}

// managerTasks gives the manager n running tasks of its own.
func managerTasks(a *app, n int) {
	if a.tasks == nil {
		a.tasks = map[uint64]*taskNode{}
	}
	for i := 1; i <= n; i++ {
		id := uint64(900 + i)
		a.tasks[id] = &taskNode{id: id, title: fmt.Sprintf("Task %d", i), label: fmt.Sprintf("Task %d", i), state: session.TaskRunning}
		a.taskOrder = append(a.taskOrder, id)
	}
	a.touch()
}

// sideWordAt is the screen cell of the header's word that brings view to the
// front.
func sideWordAt(t *testing.T, a *app, view int) (int, int) {
	t.Helper()
	lines, _ := a.railDrawnView(a.viewHeight())
	if len(lines) == 0 || lines[0].side == nil {
		t.Fatal("the column has no header")
	}
	for _, d := range lines[0].side.doors {
		if d.act.kind == sideActView && d.act.view == view {
			return a.railLeft() + len([]rune(railSeam)) + d.span.from, a.topHeight()
		}
	}
	t.Fatalf("the header has no word for view %d", view)
	return 0, 0
}

// WITH THE MANAGER IN FRONT THE ONE COLUMN OPENS ON THE TRAFFIC, and its
// header offers the manager's own tasks as the other word, with its count,
// even at zero. A press on the word lays the tasks in the same column at the
// same width with the header where it was, and a press on the Traffic word
// takes it back. There is no second column and no special case.
func TestManagerColumnOpensOnTheTrafficAndOffersItsTasks(t *testing.T) {
	a := managerColumnApp(t)
	rows := railLines(t, a)
	head := railRowOf(rows, sideTasksWord+" 0"+sideWordSep+sideTrafficWord)
	if head < 0 || a.sideView() != sideTraffic {
		t.Fatalf("the manager's column does not open on the Traffic with both words:\n%s", strings.Join(rows, "\n"))
	}
	managerTasks(a, 2)
	rows = railLines(t, a)
	if railRowOf(rows, sideTasksWord+" 2"+sideWordSep) != head || railRowOf(rows, "Task 1") >= 0 {
		t.Fatalf("the Tasks word does not count the work behind it:\n%s", strings.Join(rows, "\n"))
	}
	body, cols := a.bodyWidth(), a.railWidth()

	x, y := sideWordAt(t, a, sideTasks)
	a.setHover(x, y)
	if a.hot.kind != hoverSide || a.hot.key != sideHeadKey || a.hot.index < 0 {
		t.Fatalf("the Tasks word does not answer the pointer: %+v", a.hot)
	}
	if words := a.dockHoverWords(); !strings.Contains(words, "tasks") || !strings.Contains(words, "click") {
		t.Fatalf("the Tasks word's hint says %q", words)
	}
	a.dropHover()
	sideClick(t, a, x, y)
	if a.sideView() != sideTasks {
		t.Fatal("a press on Tasks did not bring the tasks to the front")
	}
	rows = railLines(t, a)
	if a.bodyWidth() != body || a.railWidth() != cols || railRowOf(rows, sideTasksWord+" 2") != head || railRowOf(rows, "Task 1") <= head {
		t.Fatalf("the tasks are not in the same column under the same header (body %d->%d, cols %d->%d):\n%s", body, a.bodyWidth(), cols, a.railWidth(), strings.Join(rows, "\n"))
	}
	x, y = sideWordAt(t, a, sideTraffic)
	sideClick(t, a, x, y)
	if a.sideView() != sideTraffic || a.bodyWidth() != body {
		t.Fatal("a press on Traffic did not take the column back, or moved the body")
	}
}

// THE WORD IN FRONT IS REMEMBERED PER KIND OF CHAT, for the session: a
// manager opens on the Traffic, a member and a chat in no team on the tasks,
// and a word the person chose in one kind of chat is what that kind opens on
// next, whatever the other kind was left on. A chat in no team has only the
// one word.
func TestTheColumnsWordIsRememberedPerKindOfChat(t *testing.T) {
	a := managerColumnApp(t)
	harbor := a.wall.teams[0].ID
	manager := a.frontTabKey()
	_, priceKey := trafficHandle(t, a, harbor, "openrouter")
	if a.sideView() != sideTraffic {
		t.Fatal("a manager does not open on the Traffic")
	}
	a.sideSetView(sideTasks)

	spend(t, a, a.trafficGo(priceKey))
	if a.sideKind() != sideKindMember || a.sideView() != sideTasks {
		t.Fatalf("a member opens on view %d", a.sideView())
	}
	a.sideSetView(sideTraffic)

	spend(t, a, a.trafficGo(manager))
	if a.sideKind() != sideKindManager || a.sideView() != sideTasks {
		t.Fatalf("the manager forgot its word: kind %d view %d", a.sideKind(), a.sideView())
	}
	spend(t, a, a.trafficGo(priceKey))
	if a.sideView() != sideTraffic {
		t.Fatal("the member forgot its word")
	}

	plainChat, _, _ := tabApp(t)
	plainChat.profileDir = t.TempDir()
	plainChat.width, plainChat.height = 160, 40
	plainChat.welcome.open = false
	managerTasks(plainChat, 1)
	if plainChat.sideKind() != sideKindPlain || plainChat.sideView() != sideTasks {
		t.Fatal("a chat in no team is not on its tasks")
	}
	plainChat.sideSetView(sideTraffic)
	rows := railLines(t, plainChat)
	if plainChat.sideView() != sideTasks || railRowOf(rows, sideTrafficWord) >= 0 || railRowOf(rows, sideTasksWord+" 1") < 0 {
		t.Fatalf("a chat in no team offers a Traffic:\n%s", strings.Join(rows, "\n"))
	}
}

// LEFT AND RIGHT SWITCH THE WORDS WHILE THE COLUMN HOLDS THE KEYBOARD, and
// only then: the keyboard stays with the column, the chat in front stays in
// front, and without the hold the arrows are the draft's.
func TestLeftAndRightSwitchTheColumnsWords(t *testing.T) {
	a := managerColumnApp(t)
	managerTasks(a, 2)
	front := a.frontTabKey()
	drive(t, a, key("right"))
	if a.sideView() != sideTraffic {
		t.Fatal("an arrow with the keyboard in the draft switched the column")
	}
	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("alt+t did not give the column the keyboard")
	}
	drive(t, a, key("right"))
	if a.sideView() != sideTasks || !a.railHold || a.frontTabKey() != front {
		t.Fatalf("right did not switch to the tasks in place: view %d hold %v", a.sideView(), a.railHold)
	}
	drive(t, a, key("left"))
	if a.sideView() != sideTraffic || !a.railHold {
		t.Fatal("left did not switch back to the Traffic")
	}
	drive(t, a, key("esc"))
	if a.railHold {
		t.Fatal("esc did not give the keyboard back")
	}
}
