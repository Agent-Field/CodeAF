package tui3

// A PROGRAM'S TASK OPENS INSIDE THE CONVERSATION'S TAB, and the tab bar keeps
// working while it is open. These tests drive the owner's report of 2026-09-24
// through the surface's own Update loop: open senior-dev's task by every door a
// person has onto it, then leave it by `esc`, by a press on the conversation's
// tab, and by a press on Home, and read what the FRAME shows after each — how
// many tabs are drawn selected, whether Home is on the bar, and whether the
// program's page is still covering whatever the press chose (tabbar_test.go
// holds the readings and the ordinary room they are held against).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// programTabSaid is a line only the program's conversation draws: the model's
// first answer in [programTurns]. Its presence on the frame is the program's
// page being on screen.
const programTabSaid = "I'll read the middleware and the store first."

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

// programTabDoors is every door onto the program's task a test can drive
// through the loop, each named for what a person does.
var programTabDoors = []struct {
	name string
	open func(t *testing.T, l *tabLab)
}{
	{"a press on its rail row", func(t *testing.T, l *tabLab) { clickRail(t, l.a, 0) }},
	{"enter on its rail row", func(t *testing.T, l *tabLab) {
		l.a.railWhere, l.a.railHold = railSpot{id: 7}, true
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
					t.Errorf("SYMPTOM: the page took the whole frame and no tab strip is drawn (railTaskPlanOn=%v workTabOn=%v)",
						l.a.railTaskPlanOn, l.a.workTabOn)
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
