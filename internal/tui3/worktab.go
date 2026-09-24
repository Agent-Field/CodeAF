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
	word := strings.TrimSpace(workTabRow(rows).Title)
	if word == "" {
		return chatTab{}, false
	}
	return chatTab{key: a.frontTabKey() + "#work", file: a.file, word: word, full: word, here: a.workTabOn, held: true, work: true}, true
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
	if stable {
		a.workTabOn = false
	}
	return stable
}

// openWorkTab opens the run's tab on the rows the surface holds and asks for
// the run's page OFF THE LOOP. The tab is only offered while rows are held
// ([app.workTab]), so there is nothing to read before it can open; the page
// arrives through the one door every stored page arrives through
// ([app.taskSheetPlanAsk]), and until it does the pane draws the run's own row.
func (a *app) openWorkTab() tea.Cmd {
	rows, ok := a.heldPlanRows()
	if !ok || len(rows) == 0 {
		return nil
	}
	// THE READING IS TAKEN AT THE OPENING, ONCE, the way the tasks place takes
	// it ([app.showTaskPlace]): the disk is walked here and the frames that
	// follow draw what the place holds, refreshed on the paint clock through
	// [tasksPlace.regroup]. A frame that took its own reading would read the
	// disk on every paint (framedisk_law_test.go).
	a.refreshElsewhere()
	a.taskSheet = a.takeTaskReading()
	a.workTabOn, a.taskSheet.planOn, a.taskSheet.detailOn = true, true, true
	row := workTabRow(rows)
	a.taskSheet.plan = session.PlanTaskPage{Row: row}
	a.taskSheet.planNote.reset()
	a.chatTabBar = tabBar{}
	a.touch()
	return a.taskSheetPlanAsk(row.ID, nil, nil, nil)
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

func (a *app) workTabKey(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, taken := a.hopKey(msg); taken {
		return cmd
	}
	if msg.String() == "esc" {
		a.workTabOn = false
		a.closeTaskPlan()
		a.chatTabBar = tabBar{}
		return nil
	}
	return a.taskPlanKey(msg)
}

// workTabFrame reuses the tasks place reading and painter; there is no second
// work-row renderer. THE FRAME NEVER TAKES THE READING: [app.takeTaskReading]
// walks the disk and stamps the look, which is an opening's work, so the frame
// draws the reading the place already holds through [app.tasksFiltered], the
// one door the rail and the tasks place read through (framedisk_law_test.go).
func (a *app) workTabFrame(width, height int) []string {
	a.workTabStable()
	out := a.headRows(width, a.tabsRow(width), a.pal)
	// A PROGRAM'S RUN SHOWS ITS PAGE. The rows below are the whole tasks place
	// — every conversation this machine has held — and for a run the plan
	// switch drives that was the run's own list; a program's run has one row
	// and its page is its conversation with codeaf, so the tab drew a hundred
	// conversations and the run's notes under them and never the program.
	// Its tab draws the page the rail opens, under the tab strip.
	if a.taskSheet.planOn && a.taskPlanIsProgram() {
		page, _, _ := a.taskPlanFrame(width, max(height-len(out), 1))
		return append(out, page...)
	}
	reading := a.tasksFiltered()
	reading.unfolded = true
	rows := reading.rows(width, a.pal)
	room := height - len(out) - 3
	if room < 0 {
		room = 0
	}
	if len(rows) > room {
		rows = rows[:room]
	}
	out = append(out, rows...)
	for len(out) < height-3 {
		out = append(out, "")
	}
	for _, note := range a.taskSheet.plan.Notes {
		who := strings.TrimSpace(note.Author)
		if note.Person {
			who = "you"
		}
		out = append(out, a.pal.dim(who+railSep)+a.pal.ink(note.Body))
	}
	// A PROGRAM'S RUN TAKES NO NOTE, so its tab offers no box: nothing typed
	// there would reach the program, and the keys it would have typed are the
	// page's reading keys and nothing else ([app.taskPlanKey]).
	if a.taskPlanIsProgram() {
		return out
	}
	text := a.taskSheet.planNote.String()
	if strings.TrimSpace(text) == "" {
		text = taskPlanNoteWord
	}
	return append(out, prompt+a.pal.dim(text))
}

// leaveTaskOverlays stands down the two task pages the belt switch still draws
// over the conversation — the work tab, and a run's page opened from the side
// list — and is a no-op when neither is up.
//
// EVERY DOOR OUT OF THE CONVERSATION'S FRAME CALLS IT, because both pages are
// drawn before any place is (view.go's [app.frameBody]): a place opened under
// one of them was a place nobody could see, and Home opened that way dropped
// off the strip the work tab went on drawing. Every place opens through
// [app.standDownRest], and the conversation's own tab calls it on the way back
// ([app.tabGo]).
func (a *app) leaveTaskOverlays() {
	if !a.workTabOn && !a.railTaskPlanOn {
		return
	}
	a.workTabOn, a.railTaskPlanOn = false, false
	a.closeTaskPlan()
	a.chatTabBar = tabBar{}
}
