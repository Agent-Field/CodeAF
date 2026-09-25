package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (a *app) workTab() (chatTab, bool) {
	// THE TAB STRIP IS FRAME CODE, AND A FRAME NEVER OPENS THE STORE. The run's
	// rows are the ones the task sheet already carries ([tasksMine.plan], read
	// off the loop); asking the agent here opened the plan store twice on
	// every frame of a conversation with a run in it.
	rows := a.taskSheet.mine.plan
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
	word := strings.TrimSpace(rows[0].Title)
	if word == "" {
		return chatTab{}, false
	}
	return chatTab{key: a.frontTabKey() + "#work", file: a.file, word: word, full: word, here: a.workTabHere(rows[0].ID), held: true, work: true}, true
}

func (a *app) workTabStable() bool {
	var sig strings.Builder
	for _, row := range a.taskSheet.mine.plan {
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
	if !ok || len(rows) == 0 {
		return nil
	}
	a.chatTabBar = tabBar{}
	a.touch()
	return a.openRailPlan(rows[0].ID, nil)
}

// workTabHere reports whether the run's tab is the page on screen: the room is
// open on the run's own task.
func (a *app) workTabHere(root string) bool {
	plan := a.roomPlan()
	return plan != nil && plan.id == strings.TrimSpace(root)
}
