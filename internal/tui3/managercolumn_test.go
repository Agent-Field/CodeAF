package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// managerColumnApp is the manager in front on a wide frame, with the task
// column remembered open.
func managerColumnApp(t *testing.T) *app {
	t.Helper()
	a, _, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.railAway = false
	if !a.trafficOn() {
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

// WITH THE MANAGER IN FRONT THE RIGHT COLUMN IS THE TRAFFIC. A task column
// remembered open is not drawn and not reserved, no folded edge of either
// stands beside the other, and with no tasks there is no Tasks word.
func TestManagerColumnIsTrafficWhateverTheTaskColumnSaid(t *testing.T) {
	a := managerColumnApp(t)
	rows := railLines(t, a)
	joined := strings.Join(rows, "\n")
	if a.railWidth() != 0 || a.railShowing() || a.railStowed() {
		t.Fatalf("the task column is on the frame beside the manager: width %d showing %v stowed %v", a.railWidth(), a.railShowing(), a.railStowed())
	}
	if a.trafficWidth() <= trafficGripCols || railRowOf(rows, trafficWord) < 0 {
		t.Fatalf("the traffic is not the right column (%d):\n%s", a.trafficWidth(), joined)
	}
	for _, never := range []string{railStowHint, "/task", trafficTasksWord} {
		if strings.Contains(joined, never) {
			t.Fatalf("%q is in the right column:\n%s", never, joined)
		}
	}
	// ctrl+g has nothing to swap to, and the task column does not come back.
	drive(t, a, key(railStowKey))
	if a.railShowing() || a.trafficWidth() <= trafficGripCols {
		t.Fatal("ctrl+g brought an empty task column back over the traffic")
	}
	// AND ITS OWN HIDE folds it to the edge, still with no task column beside it.
	drive(t, a, key(trafficKey))
	if a.trafficWidth() != trafficGripCols || a.railWidth() != 0 {
		t.Fatalf("alt+l left traffic %d and tasks %d", a.trafficWidth(), a.railWidth())
	}
	drive(t, a, key(trafficKey))
	if a.trafficWidth() <= trafficGripCols {
		t.Fatal("alt+l did not bring the traffic back")
	}
}

// WITH LIVE TASKS OF ITS OWN the header offers them, `Traffic · Tasks 2`; a
// press on the word lays them in the same column at the same width, and the
// header line takes it back to the traffic.
func TestManagerColumnOffersItsTasks(t *testing.T) {
	a := managerColumnApp(t)
	managerTasks(a, 2)
	rows := railLines(t, a)
	head := railRowOf(rows, trafficWord+trafficTabSep+trafficTasksWord+" 2")
	if head < 0 {
		t.Fatalf("the header does not offer the tasks:\n%s", strings.Join(rows, "\n"))
	}
	body := a.bodyWidth()
	cols := a.trafficWidth()
	tab := a.traffic.drawn.tab
	at, ok := a.trafficHoverAt(tab.from, head)
	if !ok || at.kind != hoverTrafficTab {
		t.Fatalf("the Tasks word does not answer the pointer: %+v", at)
	}
	a.hot = at
	if words := a.dockHoverWords(); !strings.Contains(words, railStowKey) {
		t.Fatalf("the Tasks word's hint says %q", words)
	}
	a.hot = hoverAt{}
	if _, took := a.trafficPress(tab.from, head); !took || !a.trafficTasksShowing() {
		t.Fatal("a press on Tasks did not lay the tasks in the column")
	}
	frame, _, _ := a.frame()
	var right []string
	for _, r := range strings.Split(plain(frame), "\n") {
		right = append(right, plainCells(r, a.width-a.railWidth(), a.width))
	}
	joined := strings.Join(right, "\n")
	if a.bodyWidth() != body || a.railWidth() != cols || !strings.Contains(joined, "Task 1") || !strings.Contains(joined, trafficTasksWord+" 2") {
		t.Fatalf("the tasks are not in the traffic's column (body %d→%d, cols %d→%d):\n%s", body, a.bodyWidth(), cols, a.railWidth(), joined)
	}
	drive(t, a, key(railStowKey))
	if a.trafficTasksShowing() || a.trafficWidth() != cols {
		t.Fatal("ctrl+g did not take the column back to the traffic")
	}
	// AND A PRESS ON THE TASKS' HEADER LINE TAKES IT BACK TOO.
	a.trafficTasksShow(true)
	a.frame()
	drive(t, a, clickAt(a.bodyWidth()+3, a.bodyTop()))
	if a.trafficTasksShowing() {
		t.Fatal("a press on the header line left the tasks in the column")
	}
}
