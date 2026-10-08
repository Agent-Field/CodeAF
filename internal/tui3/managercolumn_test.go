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
	a, _, _, _ := teamChatApp(t)
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

// A TEAM OVERLAY OPENS ON TASKS, with Traffic still selectable. Switching
// either word leaves the column, header and conversation in place.
func TestManagerColumnOpensOnTasksAndOffersTraffic(t *testing.T) {
	a := managerColumnApp(t)
	if _, ok := a.teamActive(); !ok || a.teamViews.id == "" {
		t.Fatal("the fixture has no active team overlay")
	}
	rows := railLines(t, a)
	head := railRowOf(rows, sideTasksWord+" 0"+sideWordSep+sideTrafficWord)
	if head < 0 || a.sideView() != sideTasks {
		t.Fatalf("the team overlay does not open on Tasks with both words:\n%s", strings.Join(rows, "\n"))
	}
	managerTasks(a, 2)
	rows = railLines(t, a)
	if railRowOf(rows, sideTasksWord+" 2"+sideWordSep) != head || railRowOf(rows, "Task 1") <= head {
		t.Fatalf("the default Tasks view does not show the manager's work:\n%s", strings.Join(rows, "\n"))
	}
	body, cols := a.bodyWidth(), a.railWidth()
	for _, view := range []int{sideTraffic, sideTasks} {
		x, y := sideWordAt(t, a, view)
		a.setHover(x, y)
		if a.hot.kind != hoverSide || a.hot.key != sideHeadKey || a.hot.index < 0 {
			t.Fatalf("view %d does not answer the pointer: %+v", view, a.hot)
		}
		if words := a.dockHoverWords(); !strings.Contains(words, "click") {
			t.Fatalf("view %d has no click hint: %q", view, words)
		}
		a.dropHover()
		sideClick(t, a, x, y)
		rows = railLines(t, a)
		if a.sideView() != view || a.bodyWidth() != body || a.railWidth() != cols || railRowOf(rows, sideTasksWord+" 2") != head {
			t.Fatalf("view %d did not switch in the same column:\n%s", view, strings.Join(rows, "\n"))
		}
		if shown := railRowOf(rows, "Task 1") > head; shown != (view == sideTasks) {
			t.Fatalf("view %d shows tasks=%v:\n%s", view, shown, strings.Join(rows, "\n"))
		}
	}
}

// THE WORD IN FRONT IS REMEMBERED PER KIND OF CHAT, for the session: a
// manager, a member and a chat in no team open on the tasks,
// and a word the person chose in one kind of chat is what that kind opens on
// next, whatever the other kind was left on. A chat in no team has only the
// one word.
func TestTheColumnsWordIsRememberedPerKindOfChat(t *testing.T) {
	a := managerColumnApp(t)
	harbor := a.wall.teams[0].ID
	manager := a.frontTabKey()
	_, priceKey := trafficHandle(t, a, harbor, "openrouter")
	if a.sideView() != sideTasks {
		t.Fatal("a manager does not open on Tasks")
	}
	a.sideSetView(sideTraffic)

	spend(t, a, a.trafficGo(priceKey))
	if a.sideKind() != sideKindMember || a.sideView() != sideTasks {
		t.Fatalf("a member opens on view %d", a.sideView())
	}
	a.sideSetView(sideTraffic)
	a.sideSetView(sideTasks)

	spend(t, a, a.trafficGo(manager))
	if a.sideKind() != sideKindManager || a.sideView() != sideTraffic {
		t.Fatalf("the manager forgot its word: kind %d view %d", a.sideKind(), a.sideView())
	}
	spend(t, a, a.trafficGo(priceKey))
	if a.sideView() != sideTasks {
		t.Fatal("the member forgot its word")
	}

	spend(t, a, a.trafficGo(manager))
	a.teamViewSet("")
	if a.sideKind() != sideKindPlain || a.sideView() != sideTasks {
		t.Fatal("clearing the overlay did not restore ordinary Tasks")
	}
	spend(t, a, a.teamActivate(harbor))
	if a.sideView() != sideTraffic {
		t.Fatal("reactivating the overlay forgot the chosen Traffic view")
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
	if a.sideView() != sideTasks {
		t.Fatal("an arrow with the keyboard in the draft switched the column")
	}
	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("alt+t did not give the column the keyboard")
	}
	drive(t, a, key("right"))
	if a.sideView() != sideTraffic || !a.railHold || a.frontTabKey() != front {
		t.Fatalf("right did not switch to Traffic in place: view %d hold %v", a.sideView(), a.railHold)
	}
	drive(t, a, key("left"))
	if a.sideView() != sideTasks || !a.railHold {
		t.Fatal("left did not switch back to Tasks")
	}
	drive(t, a, key("esc"))
	if a.railHold {
		t.Fatal("esc did not give the keyboard back")
	}
}
