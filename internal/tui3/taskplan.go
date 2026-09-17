package tui3

// taskplan.go is the run's PLAN as this place draws it: the rows of the store a
// conversation seeded, and the page one of those rows opens.
//
// THE PLAN IS A SECOND READING OF THE SAME WORK, and the two are not copies.
// The record beside a conversation is what sessions WROTE DOWN — a node lands
// and a row survives it. The plan is what a run CARRIES while it is still
// turning: the root task, the children a worker added or split, and the notes
// and steps each of them left, live, in a store the worker's own CLI writes.
// A node the plan dispatches is born FROM a store task, so the two describe one
// piece of work from two ends — and this file is what keeps the place from
// drawing it twice.
//
// IT IS AN OPTIONAL SEAM, like the other-window reading next door: a surface
// driven by a scripted agent has no plan store, and the honest answer for one is
// no rows rather than a door every test has to implement. [planAgent] is the
// slice of [session.Agent] this file needs.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// planAgent is the slice of [session.Agent] the tasks place reads a run's plan
// through. It is asserted rather than added to [Agent] for the reason every
// optional seam here is: a scripted agent offers a tasker and has never heard of
// a plan store, and a surface driven by one must stay representable.
type planAgent interface {
	// PlanTasks is the plan this conversation seeded, as rows ready to draw.
	// Nil is the honest answer for a conversation with no plan.
	PlanTasks() []session.PlanTaskRow
	// PlanTaskPage is one task's description, notes and trajectory, for the id a
	// row carries. False is the answer for a task this chat did not spawn.
	PlanTaskPage(id string) (session.PlanTaskPage, bool)
}

// planReader is the agent under this surface, when it carries a plan at all.
func (a *app) planReader() (planAgent, bool) {
	agent, ok := a.agent.(planAgent)
	return agent, ok
}

// planStateWord maps one store status onto the ONE state word a row wears
// (docs/design/task-states/DESIGN.md: a row says what a person does next, never
// a machinery word).
//
// THE STORE'S VOCABULARY IS NOT THE SURFACE'S. `ready` and `claimed` are the
// store saying a task is deliverable and a worker has it — the same fact this
// surface calls work in flight, so both wear `running`. `failed` and `cancelled`
// are the engine's; the person reads `incomplete` for either, because nothing
// was judged and the word must not send them looking for a fault. `paused` is a
// task held at a gate, which is the person's call and nothing else's.
func planStateWord(status string) string {
	switch strings.TrimSpace(status) {
	case "pending", "ready", "claimed", "running":
		return "running"
	case "done":
		return "done"
	case "failed", "cancelled":
		return "incomplete"
	case "paused":
		return "your call"
	}
	// A status this build has never heard of draws NOTHING rather than a word it
	// invents — the emptiness law, applied to a vocabulary that may grow.
	return ""
}

// planStatus is that word as the reading every row is drawn from: the tier the
// glyph comes off (tasktier.go's [tierSlot]), the presence, and the word. It is
// a [session.TaskStatus] so the place's own row machinery — the glyph, the state
// cell, the phone card — draws a plan row the one way it draws every other.
func planStatus(store string) session.TaskStatus {
	switch strings.TrimSpace(store) {
	case "pending", "ready", "claimed", "running":
		return session.TaskStatus{
			Tier:     session.TaskTierMoving,
			Presence: session.TaskPresenceWorking,
			Word:     planStateWord(store),
		}
	case "done":
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceDone,
			Word:     planStateWord(store),
		}
	case "failed", "cancelled":
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceIncomplete,
			Word:     planStateWord(store),
		}
	case "paused":
		return session.TaskStatus{
			Tier:      session.TaskTierYourCall,
			Presence:  session.TaskPresenceNeedsLook,
			Word:      planStateWord(store),
			Attention: true,
		}
	}
	return session.TaskStatus{}
}

// planEntryStatus is the lifecycle state a plan row's synthetic index entry
// carries, which is what files it under a section and decides whether it is
// live. `running` is the live state; the two endings are the ones
// [tasksLandedToday] dates a row from.
func planEntryStatus(store string) string {
	switch strings.TrimSpace(store) {
	case "pending", "ready", "claimed", "running", "paused":
		return string(session.TaskRunning)
	case "done":
		return string(session.TaskDone)
	case "failed", "cancelled":
		return string(session.TaskFailed)
	}
	return ""
}

// planRunning is whether the plan says a worker or a gate holds this task, which
// is what a row's `runs` answer means everywhere else on this page.
func planRunning(store string) bool {
	switch strings.TrimSpace(store) {
	case "pending", "ready", "claimed", "running", "paused":
		return true
	}
	return false
}

// planItem is one row of the store as a row of this place: the work, its state
// word, and the two figures the row shows — the steps its worker recorded and
// the dollars its spend rows carry.
//
// IT IS KEYED UNDER THE CHAT AND ITS STORE ID, which is the pair that identifies
// a plan row: the store's ids are unique machine-wide but a row of this place is
// still labelled with the conversation that seeded the plan ([tasksKey]).
func planItem(row session.PlanTaskRow, chat string) tasksItem {
	status := planStatus(row.Status)
	return tasksItem{
		entry: session.TaskIndexEntry{
			ID:        row.ID,
			Title:     row.Title,
			Label:     row.Title,
			Status:    planEntryStatus(row.Status),
			SessionID: chat,
			Cost:      row.USD,
			StartedAt: row.Started,
			EndedAt:   row.Ended,
		},
		runs: planRunning(row.Status),
		live: &status,
		plan: &row,
	}
}

// planStepsField is the trailing telemetry a plan row earns: how many steps its
// worker took, and what the task has cost.
//
// EACH FACT IS ITS OWN AND EACH IS OMITTED WHEN IT IS NOTHING. A task that has
// run no steps says nothing about steps and one that has spent nothing says
// nothing about money — the emptiness law, which on a row is the difference
// between a figure a person can act on and a `0 steps` that is noise.
func planStepWords(steps int) string {
	if steps <= 0 {
		return ""
	}
	return itoa(steps) + " " + plural("step", steps)
}

func planSpendWord(usd float64) string {
	if usd <= 0 {
		return ""
	}
	return dollars(usd)
}

// planStateField is the state cell of a plan row: the word every row wears, with
// the step count beside it — the one figure on a running task that changes while
// somebody watches it.
func planStateField(item tasksItem) rowField {
	word := item.status().Word
	if steps := planStepWords(item.plan.Steps); steps != "" {
		return rowSay(word+railSep+steps, word)
	}
	return rowSay(word)
}

// planSpendField is the second column of a plan row: what the task has cost.
// Nothing is drawn where nothing was spent, which is the same law as above.
func planSpendField(item tasksItem) rowField {
	if usd := planSpendWord(item.plan.USD); usd != "" {
		return rowSay(usd)
	}
	return rowSay()
}

// planTitleFor returns the title the store and a plan-born node share.
func planTitleFor(title string) string { return strings.ToLower(strings.TrimSpace(title)) }

// planRowShown reports whether a plan row is ALREADY drawn as one of this
// session's own node rows, which is the whole of the dedupe.
//
// A PLAN-BORN NODE IS A NODE WHOSE GRAPH KNOWS ITS STORE ID (session's
// taskSpec.planID, the link [planReviseThrough] revises through): the run
// dispatches that node from the store task, and landing writes the node's ending
// back over it. So the store row and the node row are one piece of work read
// from two ends, and the place draws it ONCE.
//
// THIS SURFACE CANNOT SEE THE STORE ID, and it does not need to: it can see
// this conversation's own node rows, and a plan-born node wears the store task's
// own title — the store is seeded with the node's title and every later task is
// added under it. So the two are matched on the title the pair cannot disagree
// about, restricted to this conversation's rows so another chat's work wearing
// the same words cannot hide a plan row.
func planRowShown(names map[string]bool, title string) bool {
	if len(names) == 0 {
		return false
	}
	return names[planTitleFor(title)]
}

// planNamesOf is the set of titles THIS conversation's own node rows wear, which
// is what [planRowShown] matches a plan row against.
func planNamesOf(rows []tasksMineRow, chat string) map[string]bool {
	if len(rows) == 0 {
		return nil
	}
	chat = strings.TrimSpace(chat)
	out := map[string]bool{}
	for _, row := range rows {
		if strings.TrimSpace(row.entry.SessionID) != chat {
			continue
		}
		if name := planTitleFor(row.entry.Title); name != "" {
			out[name] = true
		}
	}
	return out
}

// ── THE PAGE ONE PLAN ROW OPENS ─────────────────────────────────────────────

// taskSheetPlan opens the page over one plan row: the description the worker was
// given, every note left on the task with its author and moment, and the
// trajectory its worker recorded.
//
// IT IS THE SHEET'S OWN MACHINERY AND NOT A FOURTH SURFACE. Enter over a record
// row opens the card through [app.taskSheetInside]; this is the same latch,
// [tasksPlace.detailOn], the same full frame and the same `esc` that backs out
// one layer to the list — so a person who has learned the card has learned this
// page, and the foot of either names the same way out. The one thing that is not
// reused is the CONTENT, because a plan task has no record row to read: the page
// is built from the store's own read ([session.Agent.PlanTaskPage]).
//
// A PAGE THE ENGINE WILL NOT ANSWER FOR IS NOT OPENED. A task this chat did not
// spawn, or one whose store has gone, leaves the list where it was rather than
// raising a page of blanks.
func (a *app) taskSheetPlan(id string) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	page, ok := agent.PlanTaskPage(id)
	if !ok {
		return nil
	}
	a.taskSheet.plan, a.taskSheet.planOn, a.taskSheet.detailOn = page, true, true
	a.taskSheet.detailTop = 0
	// The card's recovery band belongs to the row the CARD was opened from, and
	// this page is not that row ([app.taskSheetInside] clears it at the one other
	// door for the same reason).
	a.taskSheet.awayOwner = tasksAwayOwner{}
	a.touch()
	return nil
}

// closeTaskPlan backs out one layer to the list, which is the card's own `esc`.
func (a *app) closeTaskPlan() {
	a.taskSheet.plan, a.taskSheet.planOn, a.taskSheet.detailOn = session.PlanTaskPage{}, false, false
	a.taskSheet.detailTop = 0
	a.touch()
}

// taskPlanKey is the page's keyboard: `esc` and the chord out, and the four
// reading keys the card also spends — the page is read down, so the wheel and
// the arrows move an offset rather than a cursor ([app.taskCardScroll]).
func (a *app) taskPlanKey(key string) tea.Cmd {
	switch key {
	case "esc", "left":
		a.closeTaskPlan()
	case taskSheetKey:
		a.closeTaskSheet()
	case "up", "ctrl+p":
		a.taskPlanScroll(-1)
	case "down", "ctrl+n":
		a.taskPlanScroll(1)
	case "pgup":
		a.taskPlanScroll(-taskSheetRows)
	case "pgdown", " ":
		a.taskPlanScroll(taskSheetRows)
	}
	return nil
}

// taskPlanScroll moves the page's own offset, which is the only thing that
// moves: there is no cursor to walk in a page that is read rather than listed.
func (a *app) taskPlanScroll(delta int) {
	a.taskSheet.detailTop += delta
	if a.taskSheet.detailTop < 0 {
		a.taskSheet.detailTop = 0
	}
	a.touch()
}

// taskPlanFrame is the whole screen while the page is up: a head, the body, and
// the way back. It is drawn in the card's slot and in the card's own shape — one
// frame, one rule, one foot — so the two pages of this place read as one.
func (a *app) taskPlanFrame(width, height int) ([]string, int, int) {
	pal := a.pal
	if height < 1 {
		height = 1
	}
	lines := make([]string, 0, height)
	add := func(text string) { lines = append(lines, text) }

	add(fit(pal.bold(pal.ink(a.taskSheet.plan.Row.Title)), width))
	add("")
	add(pal.dim(rule(width)))
	head := len(lines)
	room := height - head - 1
	if room < 1 {
		room = 1
	}
	body := a.taskPlanBody(width - 2)
	a.taskSheet.detailTop = clampTop(a.taskSheet.detailTop, len(body), room)
	drawn := room
	if len(body) < drawn {
		drawn = len(body)
	}
	for i := 0; i < drawn; i++ {
		add(" " + fit(body[a.taskSheet.detailTop+i], width-1))
	}
	for len(lines) < height-1 {
		add("")
	}
	add(pal.dim(rule(width)))
	return lines, 0, 0
}

// taskPlanBody is what a person reads: the work order, the notes, and the steps.
//
// EVERY SECTION WITH NOTHING BEHIND IT IS ABSENT, along with the air that would
// have separated it — the emptiness law, applied to a page. A task that has left
// no note and run no step draws its description and stops.
func (a *app) taskPlanBody(width int) []string {
	if width < 1 {
		width = 1
	}
	page, pal := a.taskSheet.plan, a.pal
	var out []string
	add := func(text string) { out = append(out, text) }
	addWrapped := func(text string, ink func(string) string) {
		for _, para := range strings.Split(text, "\n") {
			if strings.TrimSpace(para) == "" {
				continue
			}
			for _, line := range wrap(strings.TrimSpace(para), width) {
				add(ink(line))
			}
		}
	}
	section := func(word string) {
		if len(out) > 0 {
			add("")
		}
		add(pal.dim(word))
	}

	if row := planTelemetryLine(page.Row); row != "" {
		add(pal.dim(row))
	}
	if desc := strings.TrimSpace(page.Description); desc != "" {
		section("description")
		addWrapped(desc, pal.ink)
	}
	if len(page.Notes) > 0 {
		section("notes")
		for _, note := range page.Notes {
			who := strings.TrimSpace(note.Author)
			if note.Person {
				who = "you"
			}
			when := sinceAt(note.At, a.now())
			switch {
			case who != "" && when != "":
				add(pal.dim(who + railSep + when))
			case who != "":
				add(pal.dim(who))
			case when != "":
				add(pal.dim(when))
			}
			addWrapped(note.Body, pal.ink)
		}
	}
	if len(page.Steps) > 0 {
		section("steps")
		for _, step := range page.Steps {
			command := strings.TrimSpace(step.Command)
			if command == "" {
				continue
			}
			add(pal.ink(itoa(step.Step) + "  " + command))
			if head := planObservationHead(step.Observation); head != "" {
				add(pal.dim("   " + head))
			}
		}
	}
	return out
}

// planTelemetryLine is a plan task's own figures as one dim line: whether the
// work is running, how many steps its worker has taken, and what it has cost —
// each clause omitted when it has nothing behind it.
func planTelemetryLine(row session.PlanTaskRow) string {
	var segs []string
	if word := planStateWord(row.Status); word != "" {
		segs = append(segs, word)
	}
	if steps := planStepWords(row.Steps); steps != "" {
		segs = append(segs, steps)
	}
	if usd := planSpendWord(row.USD); usd != "" {
		segs = append(segs, usd)
	}
	return strings.Join(segs, railSep)
}

// planObservationHead is the head of one step's observation: the first line that
// says anything, which is as much of what came back as a step line can carry.
// The whole of it is on disk behind the row's trajectory ([PlanTaskRow.TrajectoryPath]).
func planObservationHead(observation string) string {
	for _, line := range strings.Split(observation, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
