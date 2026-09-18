package tui3

import (
	tea "charm.land/bubbletea/v2"
	"strings"
)

func (a *app) workTab() (chatTab, bool) {
	p, ok := a.planReader()
	if !ok {
		return chatTab{}, false
	}
	rows := p.PlanTasks()
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
	return chatTab{key: a.frontTabKey() + "#work", file: a.file, word: word, full: word, here: a.workTabOn, held: true, work: true}, true
}

func (a *app) workTabStable() bool {
	p, ok := a.planReader()
	if !ok {
		return true
	}
	var sig strings.Builder
	for _, row := range p.PlanTasks() {
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

func (a *app) openWorkTab() {
	p, ok := a.planReader()
	if !ok {
		return
	}
	rows := p.PlanTasks()
	if len(rows) == 0 {
		return
	}
	a.workTabOn, a.taskSheet.planOn, a.taskSheet.detailOn = true, true, true
	if page, found := p.PlanTaskPage(rows[0].ID); found {
		a.taskSheet.plan = page
	} else {
		a.taskSheet.plan.Row = rows[0]
	}
	a.taskSheet.planNote.reset()
	a.chatTabBar = tabBar{}
	a.touch()
}

func (a *app) workTabKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "esc" {
		a.workTabOn = false
		a.closeTaskPlan()
		a.chatTabBar = tabBar{}
		return nil
	}
	return a.taskPlanKey(msg)
}

// workTabFrame reuses the tasks place reading and painter; there is no second work-row renderer.
func (a *app) workTabFrame(width, height int) []string {
	a.workTabStable()
	out := a.headRows(width, a.tabsRow(width), a.pal)
	reading := a.takeTaskReading().reading
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
	text := a.taskSheet.planNote.String()
	if strings.TrimSpace(text) == "" {
		text = taskPlanNoteWord
	}
	return append(out, prompt+a.pal.dim(text))
}
