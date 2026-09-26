package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

func (a *app) workTab() (chatTab, bool) {
	// THE TAB STRIP IS FRAME CODE, AND A FRAME NEVER OPENS THE STORE. The run's
	// rows are the ones the task sheet already carries ([tasksMine.plan], read
	// off the loop); asking the agent here opened the plan store twice on
	// every frame of a conversation with a run in it.
	//
	// A PROGRAM'S RUN HAS NO TAB. Its task opens inside this conversation's own
	// tab, as a room, from its row, its card and every other door
	// (programroom.go); a tab of its own drew itself selected beside the
	// conversation's, and a press on the conversation's tab never left it. Only
	// the rows of a run the belt switch drives make the tab.
	rows := beltRows(a.taskSheet.mine.plan)
	if len(rows) == 0 {
		return chatTab{}, false
	}
	live := false
	for _, row := range rows {
		if planRunning(row.Status) {
			live = true
			break
		}
	}
	if !live && a.workTabStable() {
		return chatTab{}, false
	}
	word := strings.TrimSpace(workTabRow(rows).Title)
	if word == "" {
		return chatTab{}, false
	}
	return chatTab{key: a.frontTabKey() + "#work", file: a.file, word: word, full: word, here: a.workTabHere(workTabRow(rows).ID), held: true, work: true}, true
}

// beltRows is the rows of runs the belt switch drives, which are the only runs
// with a tab of their own: every row that names no program.
func beltRows(rows []session.PlanTaskRow) []session.PlanTaskRow {
	var out []session.PlanTaskRow
	for _, row := range rows {
		if strings.TrimSpace(row.Program) == "" {
			out = append(out, row)
		}
	}
	return out
}

func (a *app) workTabStable() bool {
	var sig strings.Builder
	for _, row := range beltRows(a.taskSheet.mine.plan) {
		if planRunning(row.Status) {
			a.workTabSettled = ""
			return false
		}
		sig.WriteString(row.ID + "=" + row.Status + ";")
	}
	now := sig.String()
	stable := now != "" && now == a.workTabSettled
	a.workTabSettled = now
	return stable
}

// openWorkTab opens the run's own task in the task room, the one page every
// task has (planroom.go). The tab is only offered while rows are held
// ([app.workTab]), so there is nothing to read before it can open; the page
// arrives through the one door every stored page arrives through
// ([app.openRailPlan]).
func (a *app) openWorkTab() tea.Cmd {
	rows, ok := a.heldPlanRows()
	rows = beltRows(rows)
	if !ok || len(rows) == 0 {
		return nil
	}
	a.chatTabBar = tabBar{}
	a.touch()
	return a.openRailPlan(workTabRow(rows).ID, nil)
}

// workTabRow is the row the run's tab is about: the first one still working,
// and the first row when none is. A conversation that handed senior-dev two
// tasks holds two runs' rows, and a tab named after the one that had already
// landed opened on it while the other was the work in front of the person.
func workTabRow(rows []session.PlanTaskRow) session.PlanTaskRow {
	for _, row := range rows {
		if planRunning(row.Status) {
			return row
		}
	}
	return rows[0]
}

// workTabHere reports whether the run's tab is the page on screen: the room is
// open on the run's own task.
func (a *app) workTabHere(root string) bool {
	plan := a.roomPlan()
	return plan != nil && plan.id == strings.TrimSpace(root)
}

// withdrawRailPlan withdraws a row's page still on its way ([railPlanPending]).
//
// EVERY DOOR OUT OF THE CONVERSATION'S FRAME CALLS IT: every place opens
// through [app.standDownRest]. A person who pressed a run's row and then went
// Home has left the press behind, and its answer — milliseconds later here,
// seconds over a connection — opened the run's room under Home, with the box
// pointed at the run while Home's box was the one on screen.
func (a *app) withdrawRailPlan() {
	a.railPlanPending = railPlanPending{}
}
