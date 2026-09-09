package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func taskControlApp(t *testing.T) (*app, *stopFake) {
	t.Helper()
	a, f := stopApp(t)
	a.width, a.height = 120, 48
	a.openRoom(7, "Callback handling")
	a.railHold = false
	a.roomNode().model = "z-ai/glm-5.3"
	a.roomNode().where = "/work/sign-in"
	a.roomNode().assignment = "Check redirect destinations and session expiry."
	for i := 0; i < 8; i++ {
		a.jobUpdate(runningJob(i+1, fmt.Sprintf("Background check %d", i+1), "go test ./..."))
	}
	for i := 10; i < 50; i++ {
		a.tasks[uint64(i)] = &taskNode{id: uint64(i), title: fmt.Sprintf("Task %d", i), label: fmt.Sprintf("Task %d", i), state: session.TaskRunning}
		a.taskOrder = append(a.taskOrder, uint64(i))
	}
	a.jobsOpen = true
	return a, f
}

func panelRow(t *testing.T, a *app, action string) int {
	t.Helper()
	lines, _ := a.railView(a.viewHeight())
	for i, l := range lines {
		if l.roomAction == action {
			return a.topHeight() + i
		}
	}
	t.Fatalf("missing action %s", action)
	return -1
}

func TestTaskPanelBoundsScrollAndKeepsActionsReachable(t *testing.T) {
	a, _ := taskControlApp(t)
	for _, size := range [][2]int{{120, 48}, {160, 50}, {100, 40}, {80, 24}, {60, 20}, {40, 12}} {
		a.width, a.height = size[0], size[1]
		frame, _, _ := a.frame()
		rows := strings.Split(plain(frame), "\n")
		if len(rows) > a.height {
			t.Fatalf("height overflow at %v: %d", size, len(rows))
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > a.width {
				t.Fatalf("width overflow at %v: %q", size, r)
			}
		}
		if !a.roomPanelShowing(a.viewHeight()) {
			continue
		}
		before := panelRow(t, a, "stop")
		a.roomDetailsScroll(10000)
		a.railScroll(10000)
		after := panelRow(t, a, "stop")
		if before != after {
			t.Fatal("scrolling moved the Stop target")
		}
		a.roomDetailsScroll(-10000)
		a.railScroll(-10000)
	}
}

func TestTaskPanelWheelDoesNotLeakAcrossSections(t *testing.T) {
	a, _ := taskControlApp(t)
	lines, _ := a.railView(a.viewHeight())
	tree, details := -1, -1
	for i, l := range lines {
		if l.roomSection == roomPanelTree {
			tree = i
		}
		if l.roomSection == roomPanelDetails {
			details = i
		}
	}
	x := a.bodyWidth() + ansi.StringWidth(railSeam) + 3
	wheel := func(y int) { drive(t, a, tea.MouseWheelMsg{X: x, Y: a.topHeight() + y, Button: tea.MouseWheelDown}) }
	wheel(details)
	if a.room.detailsTop == 0 || a.railTop != 0 {
		t.Fatal("details wheel moved wrong section")
	}
	saved := a.room.detailsTop
	wheel(tree)
	if a.railTop == 0 || a.room.detailsTop != saved {
		t.Fatal("tree wheel moved wrong section")
	}
}

func TestTaskPanelHoverAndClickShareScopeAndPadding(t *testing.T) {
	a, f := taskControlApp(t)
	y := panelRow(t, a, "stop")
	x := a.bodyWidth() + ansi.StringWidth(railSeam) + 2
	if got := a.hoverTarget(x, y); got.kind != hoverRoomControl || got.key != "stop" {
		t.Fatalf("missing hover: %+v", got)
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !a.stopping() || len(f.asked) != 0 {
		t.Fatal("Stop must open confirmation without stopping")
	}
	if a.stop.target.id != session.CancelTask+":7" {
		t.Fatalf("wrong scope: %s", a.stop.target.id)
	}
}

func TestTaskModelCommandCannotChangeConversationFromTask(t *testing.T) {
	a, f := roomModelApp(t, "z-ai/glm-5.2")
	original := a.model
	typeLine(t, a, "/model")
	if a.pick.task != a.room.id {
		t.Fatal("slash model opened conversation picker")
	}
	drive(t, a, key("esc"))
	typeLine(t, a, "/model other/model")
	if a.model != original || len(f.retargeted) != 1 {
		t.Fatalf("slash model changed the wrong scope: model=%s retarget=%+v input=%s picker=%v", a.model, f.retargeted, a.input.String(), a.pick.open)
	}
	a.roomNode().state = session.TaskDone
	typeLine(t, a, "/model another/model")
	if a.model != original || len(f.retargeted) != 1 {
		t.Fatal("finished task fell through to conversation")
	}
}

func TestTaskStopCommandAndRecipientStayOnTask(t *testing.T) {
	a, f := taskControlApp(t)
	frame, _, _ := a.frame()
	if !strings.Contains(plain(frame), "To: Callback handling") {
		t.Fatal("input lost recipient")
	}
	typeLine(t, a, "/stop")
	if !a.stopping() || len(f.asked) != 0 {
		t.Fatal("slash stop did not ask first")
	}
}

func TestTaskRecipientChargesItsOwnRow(t *testing.T) {
	a, _ := taskControlApp(t)
	for _, size := range [][2]int{{120, 48}, {100, 40}, {80, 24}, {40, 12}} {
		a.width, a.height = size[0], size[1]
		rows, _, _, _ := a.chrome(a.width)
		if len(rows) != a.chromeHeight() {
			t.Fatalf("drawn input rows %d differ from budget %d at %v", len(rows), a.chromeHeight(), size)
		}
	}
}

func TestTaskPanelLeavesNoEmptyDetailsAndKeepsTheHeadingHierarchy(t *testing.T) {
	a, _ := taskControlApp(t)
	a.jobs = nil
	a.width, a.height = 120, 38
	rows := a.roomHeadRows(a.width)
	if len(a.roomKinRows(a.width)) != 0 {
		t.Fatal("expanded header repeats the family tree")
	}
	if strings.Contains(plain(rows[0]), a.roomHereWord()) {
		t.Fatal("breadcrumb repeats the task heading")
	}
	if plain(rows[1]) != strings.Repeat(" ", headLabelAt)+a.roomHereWord() {
		t.Fatal("task lost its own heading")
	}
	if strings.Contains(plain(rows[2]), "─") {
		t.Fatal("divider runs through the state")
	}
	if strings.Trim(plain(rows[3]), "─") != "" {
		t.Fatal("divider is not below the complete header")
	}
	lines, _ := a.railView(a.viewHeight())
	for _, line := range lines {
		if line.roomSection == roomPanelDetails {
			t.Fatal("empty context took rows from the tree")
		}
	}
	frame, _, _ := a.frame()
	if strings.Count(plain(frame), "To: Callback handling") != 1 || !strings.Contains(plain(frame), "Conversation totals") {
		t.Fatal("input and footer lost their scopes")
	}
}

func TestTaskSetupThinkingClickIgnoresAnotherTreeSelection(t *testing.T) {
	a, f := taskControlApp(t)
	e := &effortFake{roomFake: f.roomFake, rungs: map[uint64]string{7: "low", 10: "high"}}
	a.agent = e
	a.railHold = true
	a.railWhere = railSpot{id: 10}
	y := panelRow(t, a, "effort")
	drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam) + 2, Y: y, Button: tea.MouseLeft})
	if len(e.asked) != 1 || e.asked[0] != "task 7:medium" || e.rungs[10] != "high" {
		t.Fatalf("thinking targeted the tree cursor: %+v", e.asked)
	}
}

func TestTaskPanelHoverUsesTheCurrentThemeAndOnlyActionRows(t *testing.T) {
	for _, theme := range []theme{themeDark, themeLight} {
		a, _ := taskControlApp(t)
		a.pal = newThemedPalette(a.pal.profile, false, theme, nil)
		y := panelRow(t, a, "model")
		x := a.bodyWidth() + ansi.StringWidth(railSeam) + 2
		before := strings.Join(a.railRows(a.viewHeight()), "\n")
		drive(t, a, motionTo(x, y))
		after := strings.Join(a.railRows(a.viewHeight()), "\n")
		if a.hot.kind != hoverRoomControl || a.hot.key != "model" || before == after {
			t.Fatal("model action has no themed hover")
		}
		if a.roomPanelActionAt(a.bodyWidth()-1, y) != "" {
			t.Fatal("hover target crosses into transcript")
		}
	}
}
