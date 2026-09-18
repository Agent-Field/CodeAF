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
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
			// THE PARENT IS WHERE THE ROW IS DRAWN. The store's own parent puts a
			// child under the task that requested it; a row held behind work that
			// is not its parent is drawn under what it waits on ([planAnchor]).
			// The tasks place's existing tree walk ([tasksTreeOf]) nests on this
			// field, so the plan gets the tree the record already draws by
			// answering the one field the walk reads.
			Parent:    planAnchor(&row, kin),
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
	if folded := strings.TrimSpace(item.entry.Activity); folded != "" {
		return rowSay(said+railSep+folded, said)
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
// planAnchor is the row a plan row hangs under: always the parent that requested it.
// Dependencies are named by planWaits but never change the hierarchy.
func planAnchor(row *session.PlanTaskRow, _ planKin) string {
	if row == nil {
		return ""
	}
	return strings.TrimSpace(row.Parent)
}

// planWaits is the title a held plan row names after `queued · waits:`, and ""
// for a row that names none. A row is held behind named work only when the store
// says `pending`; explicit hard dependencies are tried first, with the parent as
// the inherited gate when no explicit dependency is available — the bare word
// `queued`, which is the honest reading of a hold this page cannot name.
func planWaits(row *session.PlanTaskRow, kin planKin) string {
	if row == nil || strings.TrimSpace(row.Status) != "pending" {
		return ""
	}
	for _, id := range row.Waits {
		dep := kin[strings.TrimSpace(id)]
		if dep != nil && strings.TrimSpace(dep.Title) != "" && planStateWord(dep.Status) != "done" {
			return strings.TrimSpace(dep.Title)
		}
	}
	parent := kin[strings.TrimSpace(row.Parent)]
	if parent != nil && strings.TrimSpace(parent.Title) != "" && planStateWord(parent.Status) != "done" {
		return strings.TrimSpace(parent.Title)
	}
	return ""
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
	a.taskSheet.planAt = -1
	a.taskSheet.detailTop = 0
	// A PAGE OPENS AT THE LIVE EDGE. The newest step is the reason the page
	// follows at all, so it opens stuck to the bottom and a scroll is what
	// releases it ([app.taskPlanTopFor], [app.taskPlanScroll]).
	a.taskSheet.planStick = true
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
	a.taskSheet.detailTop, a.taskSheet.planStick = 0, false
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
	// taskPlanPickupWord is the page's one sentence about WHEN a note is read. A
	// worker is a separate loop, so a note waits in the store until the worker
	// asks for its next step — the manual's own account of a note (worker-harness.md,
	// "Steering a task"), said on the page because the page is where the note is
	// typed.
	taskPlanPickupWord = "the worker reads a note at its next step"
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
		if n := len(a.taskSheet.planBack); n > 0 {
			a.taskSheet.plan = a.taskSheet.planBack[n-1]
			a.taskSheet.planBack = a.taskSheet.planBack[:n-1]
			a.taskSheet.planAt = -1
			a.touch()
		} else {
			a.closeTaskPlan()
		}
		return nil
	case taskSheetKey:
		a.closeTaskSheet()
		return nil
	case "up", "ctrl+p":
		if a.taskSheet.planAt >= 0 {
			a.taskSheet.planAt--
		} else {
			a.taskPlanScroll(-1)
		}
		return nil
	case "down", "ctrl+n":
		if a.taskSheet.planAt+1 < len(a.taskSheet.plan.Children) {
			a.taskSheet.planAt++
		} else {
			a.taskPlanScroll(1)
		}
		return nil
	case "pgup":
		a.taskPlanScroll(-taskSheetRows)
		return nil
	case "pgdown":
		a.taskPlanScroll(taskSheetRows)
		return nil
	case "enter":
		if a.taskSheet.planNote.empty() && a.taskSheet.planAt >= 0 && a.taskSheet.planAt < len(a.taskSheet.plan.Children) {
			old := a.taskSheet.plan
			id := old.Children[a.taskSheet.planAt].ID
			if cmd := a.taskSheetPlan(id); a.taskSheet.plan.Row.ID == id {
				a.taskSheet.planBack = append(a.taskSheet.planBack, old)
			} else {
				return cmd
			}
			return nil
		}
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

// taskPlanHead is what the page spends above its body: the task's title, the
// air under it and the rule — three rows, the card's own head.
const taskPlanHead = 3

// taskPlanFoot is what the page spends under its body: the closing rule, the
// note composer, the one sentence saying when a note is read, and the key line,
// in that order — the card's own foot grew three rows for the box the page types
// into and the sentence that says what happens to what is typed in it.
const taskPlanFoot = 4

// taskPlanWindow is the page's body, the rows it is drawn in and the rows its
// foot spends, resolved from the frame once: the draw and the scroll both read
// the bottom off this, so the two cannot disagree about where the bottom is.
func (a *app) taskPlanWindow(width, height int) ([]string, int, int) {
	if height < 1 {
		height = 1
	}
	foot := taskPlanFoot
	if height-taskPlanHead-foot < 1 {
		foot = 0
	}
	room := height - taskPlanHead - foot
	if room < 1 {
		room = 1
	}
	return a.taskPlanBody(width - 2), room, foot
}

// taskPlanTopFor resolves the page's scroll position, sticking to the live edge
// exactly as the room follows its own ([app.roomOffsetFor]) and the conversation
// follows its ([app.offsetFor]): while the page is stuck, or while the offset
// says it is past the bottom, the newest step is what is on screen. It is a
// resolver and not [clampTop] alone because a PINNED page must FOLLOW — a clamp
// holds the number it was given and lets the newest line fall off the bottom.
func (a *app) taskPlanTopFor(count, room int) int {
	bottom := count - room
	if bottom < 0 {
		bottom = 0
	}
	if a.taskSheet.planStick || a.taskSheet.detailTop > bottom {
		return bottom
	}
	return clampTop(a.taskSheet.detailTop, count, room)
}

// taskPlanScroll moves the page's own offset, which is the only thing that
// moves: there is no cursor to walk in a page that is read rather than listed.
//
// IT RE-DECIDES WHETHER THE PAGE IS FOLLOWING, the room's own bargain
// ([app.roomScroll]): a step up off the bottom releases the pin, and a step back
// onto the bottom takes it again, so a person who returns to the live edge
// resumes following without pressing anything.
func (a *app) taskPlanScroll(delta int) {
	width, height := a.size()
	body, room, _ := a.taskPlanWindow(width, height)
	bottom := len(body) - room
	if bottom < 0 {
		bottom = 0
	}
	at := a.taskPlanTopFor(len(body), room) + delta
	switch {
	case at >= bottom:
		a.taskSheet.detailTop, a.taskSheet.planStick = bottom, true
	case at <= 0:
		a.taskSheet.detailTop, a.taskSheet.planStick = 0, false
	default:
		a.taskSheet.detailTop, a.taskSheet.planStick = at, false
	}
	a.touch()
}

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
	// THE FOOT IS THE LAST FOUR ROWS, and a frame too short for the body under
	// it gives the body up rather than the way out — the page's own trim, and the
	// one [app.taskPlanWindow] resolves so the draw and the scroll agree ([app.taskPlanScroll]).
	body, room, foot := a.taskPlanWindow(width, height)
	// THE PAGE RESOLVES ITS OFFSET, it does not hold it: a stuck page reads the
	// bottom where the body now is, so a step appended between frames is on
	// screen at the next draw ([app.taskPlanTopFor]).
	top := a.taskPlanTopFor(len(body), room)
	drawn := room
	if len(body) < drawn {
		drawn = len(body)
	}
	for i := 0; i < drawn; i++ {
		add(" " + fit(body[top+i], width-1))
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
		// AND WHEN THE WORKER READS IT, under the box that writes it: the worker
		// is a separate loop, so a note waits in the store until it asks for its
		// next step — the one thing a person needs to know about the box they are
		// typing into (taskPlanPickupWord).
		add(" " + pal.dim(fit(taskPlanPickupWord, width-1)))
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

	if n := len(a.taskSheet.planBack); n > 0 {
		add(pal.dim("esc/← " + a.taskSheet.planBack[n-1].Row.Title))
	}
	if row := planPageTelemetryLine(page); row != "" {
		add(pal.dim(row))
	}
	if waits := page.WaitRows; len(waits) > 0 {
		section("waits")
		own := map[string]bool{}
		for _, id := range page.Row.Waits {
			own[id] = true
		}
		for _, row := range waits {
			var sentence string
			if own[row.ID] {
				sentence = strings.TrimSpace(page.Row.Title) + railSep + "waits: " + strings.TrimSpace(row.Title)
			} else {
				sentence = strings.TrimSpace(row.Title) + railSep + "waits: " + strings.TrimSpace(page.Row.Title)
			}
			add(pal.ink(padTo(sentence, 51)) + pal.dim(planWaitFigure(pal, row)))
		}
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
	if len(page.Steps) > 0 || !page.Live.Empty() {
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
		// THE LIVE STEP IS DRAWN ONE STEP EARLY: the command whose end line has
		// not reached the trajectory yet, led by the running glyph through
		// [palette.glyph] in place of the number the record will give it, with the
		// call's own clock — the same ten-second clock the rail counts ([taskToolFloor]) —
		// dim under it. It stands below the recorded steps because it is the
		// newest of them; the moment its command ends the store clears the live row
		// and the next re-read draws it as an ordinary step (internal/plandb's
		// live.go states the law, and a live step's zero value draws nothing).
		if live := page.Live; !live.Empty() {
			if command := strings.TrimSpace(live.Command); command != "" {
				add(pal.ink(pal.glyph(tokens.GStepRunning) + "  $ " + command))
			}
			if !live.Since.IsZero() {
				if age := a.now().Sub(live.Since); age >= taskToolFloor {
					add(pal.dim("   running " + countUpWord(age)))
				}
			}
		}
	}
	// A TASK WITH CHILDREN SHOWS THEM UNDER ITS STEPS, the way the rail draws a
	// family: each child on its own line, indented under the parent with the tasks
	// place's own connector ([tasksKin]), and carrying its live step under it when
	// one is in flight. It is the same plan tree the list draws ([planAnchor]), and
	// no new word: a child's line is its state word and its title. The note
	// composer and its receipt below are untouched by the tree.
	if kids := page.Children; len(kids) > 0 {
		section("under it")
		kin := planKinOf(kids)
		reverse := map[string]int{}
		for _, kid := range kids {
			if planRunning(kid.Status) {
				for _, id := range kid.Waits {
					reverse[id]++
				}
			}
		}
		for at, kid := range kids {
			mark := tasksKinCont
			if at == len(kids)-1 || kids[at+1].Depth <= kid.Depth {
				mark = tasksKinLast
			}
			lead := tasksKin(kid.Depth, tasksKinRoom(width), mark)
			word := planChildWordWithKin(kid, kin)
			if n := reverse[kid.ID]; n > 0 {
				word += railSep + itoa(n) + " queued behind it"
			}
			add(pal.ink(lead + word))
			if line := planLiveRow(kid.Live.Command, width-ansi.StringWidth(lead)-2, pal); line != "" {
				add(lead + "  " + line)
			}
		}
	}
	return out
}

// planChildWord is one child's own line on the task's page: its state word and
// its title, joined the way the page's own telemetry line joins two facts. The
// word is the same [planStateWord] every row on this surface wears.
func planChildWord(row session.PlanTaskRow) string {
	word, title := planStateWord(row.Status), strings.TrimSpace(row.Title)
	switch {
	case word != "" && title != "":
		return word + railSep + title
	case word != "":
		return word
	}
	return title
}

// taskPlanFollow re-reads the page while it stands on a task that is still
// running, so the newest step walks in at the live edge as the worker takes it.
//
// IT IS THE PAINT CLOCK'S OWN READ, bounded by two facts: the page must be
// opposite a running task (a settled page is a still page, and the clock that
// carries this stops with it), and the page must be up. It re-reads the whole
// page — the same store read [app.taskSheetPlan] made once on the way in —
// because that is what the room does with its rows on the same clock
// ([app.room.dirty]), and a page that followed only its steps would miss a note
// or a state change that arrived beside them. Whether the newest line is ON
// SCREEN is the resolver's question and not this one's ([app.taskPlanTopFor]):
// a stuck page reads the bottom, a person who scrolled up stays where they
// were.
func (a *app) taskPlanFollow() {
	if !a.taskPlanRunning() {
		return
	}
	agent, ok := a.planReader()
	if !ok {
		return
	}
	page, ok := agent.PlanTaskPage(a.taskSheet.plan.Row.ID)
	if !ok {
		return
	}
	a.taskSheet.plan = page
}

// taskPlanRunning reports whether the page is open on a task that is still
// running — the one condition under which the paint clock has to keep turning
// for the page's own sake, because the page follows a live edge
// ([app.taskPlanFollow]). It reads the row's own state WORD, so a task that has
// ended, or one a person has held, takes the page off the clock: a held task is
// dispatching nothing and a settled one never will again.
func (a *app) taskPlanRunning() bool {
	if !a.taskSheet.detailOn || !a.taskSheet.planOn {
		return false
	}
	return planStateWord(a.taskSheet.plan.Row.Status) == "running"
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

func planChildWordWithKin(row session.PlanTaskRow, kin planKin) string {
	item := planItem(row, "", kin)
	word, title := item.status().RowWord(), strings.TrimSpace(row.Title)
	if word != "" && title != "" {
		return word + railSep + title
	}
	if word != "" {
		return word
	}
	return title
}

// planPageTelemetryLine adds the two subtree figures to the task's own header
// reading. Running and queued are separate facts and each disappears at zero.
// planWaitFigure is the related row's state cell and useful figure on a waits
// sentence. Active work carries its recorded step count; a row without one
// carries its state word, so the relationship never drops the row's state.
func planWaitFigure(pal palette, row session.PlanTaskRow) string {
	figure := planStepWords(row.Steps)
	if figure == "" {
		figure = planStateWord(row.Status)
	}
	return strings.TrimSpace(tierGlyph(pal, planStatus(row.Status)) + " " + figure)
}

func planPageTelemetryLine(page session.PlanTaskPage) string {
	segs := []string{}
	if own := planTelemetryLine(page.Row); own != "" {
		segs = append(segs, own)
	}
	running, queued := 0, 0
	for _, row := range page.Children {
		switch strings.TrimSpace(row.Status) {
		case "ready", "claimed", "running":
			running++
		case "pending":
			queued++
		}
	}
	if running > 0 {
		segs = append(segs, itoa(running)+" running")
	}
	if queued > 0 {
		segs = append(segs, itoa(queued)+" queued")
	}
	return strings.Join(segs, railSep)
}
