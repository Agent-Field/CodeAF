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
	"github.com/charmbracelet/x/ansi"

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
	// THE SIX VERBS are the person's own door onto a run's plan, the hard
	// steering beside the note (plandb_steer.go). Each resolves an id inside THIS
	// conversation's plan and answers the store's own sentence on a refusal — the
	// root is the harness's, a terminal task cannot be cancelled — which is what
	// the pane reads back on its one line.
	PlanNote(id, text string) error
	PlanPause(id string) error
	PlanResume(id string) error
	PlanCancel(id string) error
	PlanAmend(id, text string) error
	PlanPriority(id string, n int) error
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
// surface calls work in flight, so both wear `running`. A `pending` task is
// ADMITTED AND NOT STARTED, which is not running at all: it wears the surface's
// own word for admitted work with only a slot in its way, `queued`
// ([session.TaskQueued], [app.railWaits]) — the one word on the row that was not
// true of the moment while `pending` was folded into `running`. `failed` and
// `cancelled` are the engine's; the person reads `incomplete` for either,
// because nothing was judged and the word must not send them looking for a
// fault. `paused` is a task held at a gate, which is the person's call and
// nothing else's.
func planStateWord(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "queued"
	case "ready", "claimed", "running":
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
	case "pending":
		// ADMITTED, NOT STARTED — the queued presence, and the moving tier
		// because nothing waits on the person ([session.TaskStatus] reads the
		// same pair off a queued node).
		return session.TaskStatus{
			Tier:     session.TaskTierMoving,
			Presence: session.TaskPresenceQueued,
			Word:     planStateWord(store),
		}
	case "ready", "claimed", "running":
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
//
// AND IT IS HANDED THE PAGE THE ROW CAME OFF, because one fact about a row is a
// fact about another: a task the store holds `pending` is held behind named work,
// and the name is the title of the row it hangs under ([planWaits]). Read on its
// own a row could only point at an id, and `waits: t-9c1x2` has told nobody
// anything ([app.taskWaitTitles] states that law for the column's own
// dependencies).
func planItem(row session.PlanTaskRow, chat string, kin planKin) tasksItem {
	status := planStatus(row.Status)
	// A ROW HELD BEHIND NAMED WORK SAYS SO ON THE ROW, and the reason rides the
	// READING rather than being composed at each draw (SURFACE.md §3's second
	// correction). [session.TaskStatus.RowWord] is the one place this surface
	// joins a word and its reason, so the state cell, the line the cursor's row
	// grows and the phone card all read `queued · waits: <the work>` by
	// construction rather than by agreement — which is the property
	// [tasksMiddle] exists to keep.
	if waits := planWaits(&row, kin); waits != "" {
		status.On, status.Reason = session.TaskWaitWork, "waits: "+waits
	}
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
//
// THE WORD IS [session.TaskStatus.RowWord] AND NOT THE BARE WORD, because a row
// held behind named work carries its reason on the reading ([planItem]) and this
// cell is the first place that reads it: `queued · waits: Add rate limiting`.
// What follows is law 2's degradation and never a truncation — twenty cells
// ([tasksStateCells]) hold that sentence only where the work it names is short,
// and where it is not the cell says the word and the reason falls to the line the
// cursor's row grows ([tasksReasonLine]), which has the width of the list. A
// dangling `waits:` with nothing after it is the one shape this cell must not
// draw, and a spelling that does not fit is a spelling that is not drawn.
func planStateField(item tasksItem) rowField {
	status := item.status()
	said := status.RowWord()
	if said == "" {
		said = status.Word
	}
	if steps := planStepWords(item.plan.Steps); steps != "" {
		return rowSay(said+railSep+steps, said, status.Word)
	}
	return rowSay(said, status.Word)
}

// planKin is the page's own answer to which row is which: the store's id onto the
// row it names. It is built once per reading ([planKinOf]) and handed to every
// [planItem] off it, so a row that has to name another does not walk the page
// once per row on it.
type planKin map[string]*session.PlanTaskRow

func planKinOf(rows []session.PlanTaskRow) planKin {
	kin := make(planKin, len(rows))
	for i := range rows {
		if id := strings.TrimSpace(rows[i].ID); id != "" {
			kin[id] = &rows[i]
		}
	}
	return kin
}

// planWaits is the named work a plan row is held behind, or "" for a row nothing
// is holding.
//
// THE STORE'S `pending` IS NOT "WAITING FOR ITS TURN". A task stays `pending`
// until its own hard dependencies and every ancestor's are done — that is
// internal/plandb's `promote`, the one definition of who is ready, and the reason
// `ready` and not `pending` is the store's word for dispatchable. So a pending row
// IS a row held behind named work, and the name this surface can give it is the
// row it hangs under (PlanTaskRow.Parent): the one piece of named work a store row
// carries, and the ancestor whose own dependencies gate this one.
//
// AND IT IS A TITLE OR IT IS NOTHING. A parent this page has never heard of, one
// with no words on it, and one that has already landed are all skipped rather than
// named as an id — the same refusal [app.taskWaitTitles] makes, because a pointer
// a person has to go and follow is not a sentence. What is left is read as the
// rail reads a held row of its own ([app.railWaits] and task.go's
// `waits: <title>`).
func planWaits(row *session.PlanTaskRow, kin planKin) string {
	if row == nil || strings.TrimSpace(row.Status) != "pending" {
		return ""
	}
	parent := kin[strings.TrimSpace(row.Parent)]
	if parent == nil || strings.TrimSpace(parent.Title) == "" {
		return ""
	}
	if planStateWord(parent.Status) == "done" {
		return ""
	}
	return strings.TrimSpace(parent.Title)
}

// planFigures is the telemetry a plan task's under-block carries: the steps its
// worker has taken and what it has cost, joined the way every row on this
// surface joins two facts. Each half is omitted when it is nothing, so a task
// that has run no step and spent nothing draws no line at all — the emptiness
// law, and the reason [planUnderCount] asks before it spends a row.
func planFigures(row *session.PlanTaskRow) string {
	if row == nil {
		return ""
	}
	var segs []string
	if steps := planStepWords(row.Steps); steps != "" {
		segs = append(segs, steps)
	}
	if usd := planSpendWord(row.USD); usd != "" {
		segs = append(segs, usd)
	}
	return strings.Join(segs, railSep)
}

// planUnderCount is how many rows a plan task's under-block spends: none for a row
// with no step in flight, one for the live command alone when the task carries no
// figures, and two when the telemetry stands under it. It is asked at LAYOUT,
// where a row is added per line, and [planUnderRows] draws them; both read the
// same emptiness so the two cannot disagree about how tall the block is.
//
// A HELD ROW SPENDS NOTHING HERE. What it waits on is on the row's own reading
// ([planWaits], drawn by [planStateField] and [tasksReasonLine]), and a block
// that repeated it would be a page saying one fact twice.
func planUnderCount(row *session.PlanTaskRow) int {
	if row == nil || row.Live.Step <= 0 {
		return 0
	}
	if planFigures(row) == "" {
		return 1
	}
	return railUnderRows
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
	a.taskSheet.planNote.reset()
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
	// A half-typed note does not survive the page it was typed on, which is the
	// box's own law everywhere here ([app.placeHomeGesture] resets the box it
	// empties for the same reason).
	a.taskSheet.planNote.reset()
	a.touch()
}

// ── the steering verbs ──────────────────────────────────────────────────────

// The words the plan keys say, each quoted in the manual exactly as it is
// spelled here.
const (
	// taskPlanNoteWord is what the page's composer says with nothing typed in
	// it: the one thing a person can type on a plan task's page, and the reason
	// the box is there at all.
	taskPlanNoteWord = "a note for this task"
	// tasksPlanCancelWord is the cancel key on a plan row and its page, spelled
	// from the roster's own cancel key and verb rather than re-invented here.
	tasksPlanCancelWord = stopRaiseKey + " " + stopActWord
	// tasksPlanPauseWord and tasksPlanResumeWord are the ONE key that holds a
	// plan task and lets it go again, named for the state the row is in. It is
	// `p` because nothing on a node row holds one today, and the pane's key line
	// says so ([app.tasksPlanKeyWords]).
	tasksPlanPauseWord  = "p pause"
	tasksPlanResumeWord = "p resume"
)

// taskPlanPaused reports whether the store holds this task at the pause gate,
// read FRESH rather than out of the place's own snapshot: a `p` pressed twice
// must resume what the first press paused, and the pane's held rows are a
// reading that changes on its own beat ([tasksPlace.regroup]).
func (a *app) taskPlanPaused(agent planAgent, id string) bool {
	for _, row := range agent.PlanTasks() {
		if row.ID == id {
			return strings.TrimSpace(row.Status) == "paused"
		}
	}
	return false
}

// taskPlanVerb is the one road every plan key takes: resolve the plan door, run
// the store verb the caller names, and put the store's own sentence on the
// pane's one line when it refuses. The store is the authority on its own laws —
// the root is the harness's, a terminal task cannot be cancelled — and its
// sentence is what a person reads back, never a card ([app.pageMsg] is the one
// refusal a place that is not home has to say).
func (a *app) taskPlanVerb(run func(planAgent) error) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	if err := run(agent); err != nil {
		a.pageMsg = err.Error()
	} else {
		// A verb that landed clears a refusal a previous one left on the pane's
		// line, which is what keeps the line about the key just pressed.
		a.pageMsg = ""
		// THE STORE MOVED, so the pane takes its plan again on the next frame:
		// the row a person just steered wears the store's new word. The stamp is
		// what [tasksPlace.regroup] hangs a re-read on ([app.railStamp]), and this
		// is the one door that moves it without a node landing.
		a.railStamp++
	}
	// The strip is the node row's own way to end work, and it goes away with the
	// verb it was opened for rather than standing over a row it has acted on.
	a.closeStrip()
	a.touch()
	return nil
}

// taskPlanCancel ends a plan task, its descendants and the work hard-depending
// on it, through the store's own cancel ([session.Agent.PlanCancel]). It is the
// cancel a node row already has, reached through the plan verb.
func (a *app) taskPlanCancel(id string) tea.Cmd {
	return a.taskPlanVerb(func(p planAgent) error { return p.PlanCancel(id) })
}

// taskPlanToggle is `p`: hold the task the store says is running, release the
// one it says is held. The answer is the store's, read at the moment of the key.
func (a *app) taskPlanToggle(id string) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	if a.taskPlanPaused(agent, id) {
		return a.taskPlanVerb(func(p planAgent) error { return p.PlanResume(id) })
	}
	return a.taskPlanVerb(func(p planAgent) error { return p.PlanPause(id) })
}

// taskPlanNoteSend writes what is typed in the page's composer as a person-note
// on the plan task — the store's own note verb, in the person's voice, which the
// worker reads on its next frame ([session.Agent.PlanNote]). IT IS NOT A CHAT
// TURN: the words go to the store and never to the model, so nothing here starts
// one.
func (a *app) taskPlanNoteSend() tea.Cmd {
	text := strings.TrimSpace(a.taskSheet.planNote.String())
	if text == "" {
		return nil
	}
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	id := a.taskSheet.plan.Row.ID
	if err := agent.PlanNote(id, text); err != nil {
		a.pageMsg = err.Error()
		a.touch()
		return nil
	}
	a.taskSheet.planNote.reset()
	a.pageMsg = ""
	a.railStamp++
	// Read the page again so the note a person just left is on the screen, which
	// is the receipt the store cannot draw itself.
	if page, ok := agent.PlanTaskPage(id); ok {
		a.taskSheet.plan = page
	}
	a.touch()
	return nil
}

// taskSheetPlanKey is a plan row's own keys in the LIST, over an empty box the
// way the roster takes its bare letters (stop.go's `x IS TAKEN OVER AN EMPTY
// BOX`): `x` ends the task through the store's cancel — the key that cancels a
// node — and `p` holds it or lets it go again. A letter is a letter the moment
// there is a filter to type, so neither is taken once something is in the box.
func (a *app) taskSheetPlanKey(key string) (tea.Cmd, bool) {
	if a.taskSheetFilter() != "" {
		return nil, false
	}
	item, ok := a.taskSheetCurrent()
	if !ok || item.plan == nil {
		return nil, false
	}
	switch key {
	case stopRaiseKey:
		return a.taskPlanCancel(item.plan.ID), true
	case "p":
		return a.taskPlanToggle(item.plan.ID), true
	}
	return nil, false
}

// tasksPlanKeyWords is the pane's key line for a plan row: the cancel and the
// one key that holds the task, named beside the enter clause the foot already
// draws ([tasksPlace.hint] reaches them). A key nobody can find is a key that
// does not exist, so both are said where a person reads what a row can do.
func (a *app) tasksPlanKeyWords(status string) []string {
	words := []string{tasksPlanCancelWord}
	if strings.TrimSpace(status) == "paused" {
		return append(words, tasksPlanResumeWord)
	}
	return append(words, tasksPlanPauseWord)
}

// taskPlanKey is the page's keyboard: `esc` and the chord out, the four reading
// keys the card also spends (the page is read down, so the wheel and the arrows
// move an offset rather than a cursor), the two verbs a plan row has — `x` and
// `p`, taken over an EMPTY composer — and the note itself, where every printable
// key goes into the box and `enter` sends it ([app.taskPlanNoteSend]) rather
// than a chat turn.
func (a *app) taskPlanKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	// The caret's own chords first, the route every box on this surface takes
	// (place_tasks.go's filter, the conversation's composer).
	if editorMotion(&a.taskSheet.planNote, key) ||
		editorUndo(&a.taskSheet.planNote, key) ||
		editorWordKill(&a.taskSheet.planNote, key) {
		a.touch()
		return nil
	}
	// A letter is a letter the moment there is a note to type, so the row's own
	// keys are read over an empty box and never over a sentence (the list's own
	// law, [app.taskSheetPlanKey]).
	if a.taskSheet.planNote.empty() {
		switch key {
		case stopRaiseKey:
			return a.taskPlanCancel(a.taskSheet.plan.Row.ID)
		case "p":
			return a.taskPlanToggle(a.taskSheet.plan.Row.ID)
		}
	}
	switch key {
	case "esc", "left":
		a.closeTaskPlan()
		return nil
	case taskSheetKey:
		a.closeTaskSheet()
		return nil
	case "up", "ctrl+p":
		a.taskPlanScroll(-1)
		return nil
	case "down", "ctrl+n":
		a.taskPlanScroll(1)
		return nil
	case "pgup":
		a.taskPlanScroll(-taskSheetRows)
		return nil
	case "pgdown":
		a.taskPlanScroll(taskSheetRows)
		return nil
	case "enter":
		return a.taskPlanNoteSend()
	case "backspace":
		a.taskSheet.planNote.deleteBackward()
	case "ctrl+u":
		a.taskSheet.planNote.killToStart()
	case "ctrl+k":
		a.taskSheet.planNote.killToEnd()
	case "ctrl+w":
		a.taskSheet.planNote.deleteWord()
	default:
		// EVERY OTHER PRINTABLE KEY IS THE NOTE. A space types a space here — the
		// card pages with it, but a page with a box types spaces.
		if text := msg.Key().Text; text != "" {
			a.taskSheet.planNote.insert(text)
		}
	}
	a.touch()
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

// taskPlanFoot is what the page spends under its body: the closing rule, the
// note composer, and the key line, in that order — the card's own foot grew one
// row for the box the page types into.
const taskPlanFoot = 3

// taskPlanFrame is the whole screen while the page is up: a head, the body, and
// the foot. It is drawn in the card's slot and in the card's own shape — one
// frame, one rule, one foot — so the two pages of this place read as one. The
// one thing the card has not got and this page has is the note composer: the box
// a person types into, in the foot, under the rule.
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
	// THE FOOT IS THE LAST THREE ROWS, and a frame too short for the body under
	// it gives the body up rather than the way out (the card's own trim).
	foot := taskPlanFoot
	if height-head-foot < 1 {
		foot = 0
	}
	room := height - head - foot
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
	for len(lines) < height-foot {
		add("")
	}
	caretX, caretY := 0, 0
	if foot > 0 {
		// A refusal the store answered rides on the closing rule, which is the
		// pane's one line for a place that is not home ([app.pageMsg]); this page
		// draws its own frame and so draws it here.
		legend := []string{}
		if a.pageMsg != "" {
			legend = append(legend, " "+pal.dim(a.pageMsg))
		}
		add(placeNoteRule(legend, width, pal))
		if a.taskSheet.planNote.empty() {
			add(" " + pal.dim(fit(prompt+taskPlanNoteWord, width-1)))
		} else {
			add(" " + fit(prompt+a.taskSheet.planNote.String(), width-1))
		}
		caretX, caretY = ansi.StringWidth(prompt)+1, len(lines)-1
		add(" " + paintHint(hintFit(a.taskPlanKeys(), width-2), pal, pal.dim))
	}
	if len(lines) > height {
		lines = lines[:height]
		if caretY >= height {
			caretX, caretY = 0, 0
		}
	}
	return lines, caretX, caretY
}

// taskPlanKeys is the page's key line: the reading keys the card also spends,
// the send, and the two verbs a plan task has, over the way back. It is fitted
// by [hintFit], so `esc back` is kept last and the clause a narrow frame drops
// first is the scroll.
func (a *app) taskPlanKeys() string {
	parts := []string{"↑↓ scroll", "enter send", tasksPlanCancelWord}
	if strings.TrimSpace(a.taskSheet.plan.Row.Status) == "paused" {
		parts = append(parts, tasksPlanResumeWord)
	} else {
		parts = append(parts, tasksPlanPauseWord)
	}
	parts = append(parts, taskCardBackWord)
	return strings.Join(parts, railSep)
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
