package tui3

// A PROGRAM'S TASK OPENS INSIDE THE CONVERSATION'S TAB, and the tab bar keeps working
// while it is open.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// programTabSaid is a line only the program's page draws: one of its actions in
// [programActions]. Its presence on the frame is the program's page being on
// screen.
const programTabSaid = "wrote your brief down as its spec"

// newProgramTabLab is a window in one conversation, "the run", that handed a
// task to senior-dev: the task is node 7 of its graph and the run's root in
// its store, and the store answers the program's page for it. Home is
// reachable, so the strip carries its Home door.
func newProgramTabLab(t *testing.T, status string) *tabLab {
	t.Helper()
	row := programRow()
	row.ID, row.Status = "7", status
	pages := map[string]session.PlanTaskPage{row.ID: programPage(row, programTurns())}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, pages)
	// The room doors (a lane, a journal, a steer), so an ordinary room and a
	// room reopened on the program's task can both open.
	a.agent = &railPlanCounter{planFake: fake}
	a.resume = func(string) (Agent, error) { return nil, nil }
	a.width, a.height = 160, 40
	state := session.TaskRunning
	if status == "done" {
		state = session.TaskDone
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, row.Title, state, session.TaskNotice{})})
	readPlanRows(t, a)
	lab := &tabLab{a: a}
	// Two frames: the strip settles on the second.
	lab.shoot()
	if shot := lab.shoot(); !shot.drawn || len(shot.selected) != 1 || !shot.home {
		t.Fatalf("the conversation's own frame is not the baseline: drawn=%v selected=%q home=%v\n%s",
			shot.drawn, shot.selected, shot.home, shot.text)
	}
	return lab
}

// Finished program output is opened from history; active rows keep their direct
// sidebar door. Both gestures reach the same canonical stored program page.
func programTabHistoryDoor(t *testing.T, l *tabLab) bool {
	t.Helper()
	if l.a.tasks[7].state != session.TaskDone {
		return false
	}
	l.a.showPage(pageTasks)
	l.a.taskSheet.regroup(l.a)
	l.a.taskSheet.cursor = l.a.tasksSettle(0)
	for i := 0; i < 20; i++ {
		if item, ok := l.a.taskSheetCurrent(); ok && (item.entry.ID == "7" || item.plan != nil && item.plan.ID == "7") {
			drive(t, l.a, runCmd(l.a.taskSheet.enter(l.a))...)
			return true
		}
		l.a.taskSheetMove(1)
	}
	t.Fatal("finished program unavailable in task history")
	return true
}

// programTabDoors is every door onto the program's task a test can drive
// through the loop, each named for what a person does.
var programTabDoors = []struct {
	name string
	open func(t *testing.T, l *tabLab)
}{
	{"a press on active row or task history", func(t *testing.T, l *tabLab) {
		if !programTabHistoryDoor(t, l) {
			clickRail(t, l.a, 0)
		}
	}},
	{"enter on active row or task history", func(t *testing.T, l *tabLab) {
		if programTabHistoryDoor(t, l) {
			return
		}
		l.a.side.open[railDone] = true
		l.a.railWhere = railSpot{id: 7}
		l.a.railHold = true
		drive(t, l.a, tea.KeyPressMsg{Code: tea.KeyEnter})
	}},
	// The card in the transcript, a task link, the task strip, the home panel
	// and the sessions place's own row all come through this one door.
	{"its card (openRoomFor)", func(t *testing.T, l *tabLab) {
		l.a.openRoomFor(7, "rewrite the auth middleware")
		drain(t, l.a, l.a.takeRoomPump())
	}},
	// A switch back to a held conversation, the new-chat page's way back, the →
	// key and the landing's `tell` reopen a room by id.
	{"a room reopened on it (openRoom)", func(t *testing.T, l *tabLab) {
		l.a.openRoom(7, "rewrite the auth middleware")
		drain(t, l.a, l.a.takeRoomPump())
	}},
}

// THE PAGE IS INSIDE THE CONVERSATION'S TAB. Whatever door opened it, the
// frame still draws the conversation's strip, with exactly one tab selected —
// the conversation's — and Home on it; and the program's conversation is what
// the body shows.
func TestAProgramsTaskOpensInsideTheConversationsTab(t *testing.T) {
	for _, status := range []string{"running", "done"} {
		for _, door := range programTabDoors {
			t.Run(status+"/"+door.name, func(t *testing.T) {
				l := newProgramTabLab(t, status)
				door.open(t, l)
				shot := l.shoot()
				if !strings.Contains(shot.text, programTabSaid) {
					t.Fatalf("the door did not put the program's conversation on screen:\n%s", shot.text)
				}
				if !shot.drawn {
					t.Errorf("SYMPTOM: the page took the whole frame and no tab strip is drawn (room=%v)", l.a.roomOpen())
					return
				}
				if len(shot.selected) != 1 {
					t.Errorf("SYMPTOM: %d tabs are drawn selected, %q, want only the conversation's", len(shot.selected), shot.selected)
				}
				if !shot.home {
					t.Errorf("SYMPTOM: Home is not on the strip")
				}
			})
		}
	}
}

// AND THE THREE WAYS OUT ALL LEAVE IT: `esc`, a press on the conversation's
// tab, and a press on Home. After the first two the conversation is on screen
// under its own strip; after the third, Home is — and in no case is the
// program's page still drawn over what the gesture chose.
func TestEveryWayOutOfAProgramsTaskLeavesIt(t *testing.T) {
	for _, status := range []string{"running", "done"} {
		for _, door := range programTabDoors {
			for _, way := range tabWaysOut {
				t.Run(status+"/"+door.name+"/"+way.name, func(t *testing.T) {
					l := newProgramTabLab(t, status)
					door.open(t, l)
					l.shoot()
					way.leave(t, l)
					checkLeft(t, l, way.name, way.home, programTabSaid)
				})
			}
		}
	}
}

// A PROGRAM'S RUN IS NOT A TAB. The strip names conversations; a run that the
// belt switch drives had a tab of its own beside its conversation, and once a
// program's store became readable with the switch off (028247170) every
// senior-dev run grew one too, named after its task — the "new task tab" of
// the report. A program's task opens inside its conversation's tab instead.
func TestAProgramsRunOffersNoTabOfItsOwn(t *testing.T) {
	l := newProgramTabLab(t, "running")
	for _, hit := range l.last {
		if hit.tab.work {
			t.Fatalf("SYMPTOM: the strip offers the program's run a tab of its own, %q", hit.tab.word)
		}
	}
}
