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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// planAgent aliases the shared optional capability so session remains the one
// source of its complete method set.
type planAgent = session.PlanAgent

// planReader is the agent under this surface, when it carries a plan at all.
func (a *app) planReader() (planAgent, bool) {
	agent, ok := a.agent.(planAgent)
	return agent, ok
}

// heldPlanRows is the run's rows as the surface last read them, for the
// conversation in front and no other.
func (a *app) heldPlanRows() ([]session.PlanTaskRow, bool) {
	if _, ok := a.planReader(); !ok || !a.planRowsRead || a.planRowsFront != a.frontGen {
		return nil, false
	}
	return a.planRows, true
}

// refreshPlanRows asks for the run's rows OFF THE LOOP, and decides whether to
// ask from what the loop already holds. THIS RUNS AFTER EVERY MESSAGE AND IT IS
// THE ONLY PLACE THE SIDE LIST'S ROWS ARE READ: the frame, the place's beat and
// the tab strip all draw what is held. Over a connection the read is a call to
// another process, and a call made from a frame holds every key a person presses
// for as long as the link takes to answer.
//
// THREE THINGS MAKE A READ DUE, and they are the three the frame used to read
// on. The conversation in front has never been read. A row of this window's own
// graph moved ([app.railStamp]): a hand-off publishes its row after its store is
// seeded, and a verb on the run's page bumps the stamp when it lands. Or a beat
// has passed while a held row can still move by itself, because a run's workers
// move the store and publish nothing.
//
// A CONVERSATION AT REST READS NOTHING. The beat runs only while a held row is
// queued or running, and the read that finds every row settled is the last.
//
// ONE AT A TIME, AND BESIDE THE LINE. Nobody pressed for this read, so it has no
// place in the order a person's gestures are sent in ([app.besideLine]). The
// stamp is recorded when the read is ASKED: a verb that lands while it is out
// leaves the stamps unequal, and the next message asks once more.
func (a *app) refreshPlanRows() tea.Cmd {
	agent, ok := a.planReader()
	if !ok || a.planRowsReading {
		return nil
	}
	fresh := a.planRowsRead && a.planRowsFront == a.frontGen
	if fresh && a.planRowsStamp == a.railStamp {
		if !planCanMove(a.planRows) || a.now().Sub(a.planRowsAt) < elsewhereEvery {
			return nil
		}
	}
	a.planRowsReading = true
	front, stamp := a.frontGen, a.railStamp
	return a.besideLine(func() func(bool) tea.Cmd {
		rows := agent.PlanTasks()
		return func(here bool) tea.Cmd {
			a.planRowsReading = false
			if !here || front != a.frontGen {
				return nil
			}
			a.planRows, a.planRowsRead, a.planRowsFront = rows, true, front
			a.planRowsStamp, a.planRowsAt = stamp, a.now()
			a.planRowsGen++
			a.touch()
			return nil
		}
	})
}

const runSummaryRefreshEvery = time.Minute

type runSummaryRefreshedMsg struct {
	summary session.RunPlanSummary
	ok      bool
}

// refreshRunSummary asks for the run's four lines OFF THE LOOP, and decides
// whether to ask from what the loop already holds. THIS RUNS AFTER EVERY
// MESSAGE, so it may not open the store: the run's rows are the ones the task
// sheet already carries ([tasksMine.plan]), and their ids and states are the
// same shape the stored stamp is made of. Nothing moved since the last look
// means no command; something moved means one command, never two at once, and
// never more often than [runSummaryRefreshEvery]. The command does the store
// read and, only when the stored lines are stale, the one model call.
func (a *app) refreshRunSummary() tea.Cmd {
	agent, ok := a.planReader()
	if !ok || a.runSummaryRefreshing {
		return nil
	}
	root, shape := "", ""
	for _, row := range a.taskSheet.mine.plan {
		if row.Parent == "" && root == "" {
			root = row.ID
		}
		shape += row.ID + ":" + row.Status + ";"
	}
	if root == "" || shape == a.runSummaryShape {
		return nil
	}
	now := a.now()
	if !a.runSummaryRefreshedAt.IsZero() && now.Sub(a.runSummaryRefreshedAt) < runSummaryRefreshEvery {
		return nil
	}
	a.runSummaryRefreshing = true
	a.runSummaryRefreshedAt = now
	a.runSummaryShape = shape
	ctx := a.ctx
	// BESIDE THE LINE, NEVER IN IT: nobody pressed for this, and the second call
	// below waits on a model for as long as its budget allows ([app.besideLine]).
	return a.besideLine(func() func(bool) tea.Cmd {
		// NOBODY RECORDS A LOOK AT A RUN YET (the run pane will), so the last
		// look is the zero time and the page's `since` line reads "never".
		stored, stale := agent.PlanRunSummary(root)
		if !stale && strings.TrimSpace(stored.What) != "" {
			return func(bool) tea.Cmd { return func() tea.Msg { return runSummaryRefreshedMsg{summary: stored, ok: true} } }
		}
		summary, kept := agent.RefreshRunSummary(ctx, root, time.Time{})
		return func(bool) tea.Cmd {
			return func() tea.Msg { return runSummaryRefreshedMsg{summary: summary, ok: kept} }
		}
	})
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
func planStateWord(row session.PlanTaskRow) string {
	if row.Stopped {
		return "stopped"
	}
	if row.Hold != "" && (row.Status == "ready" || row.Status == "running") {
		return "queued"
	}
	if row.Interrupted {
		return "interrupted"
	}
	switch strings.TrimSpace(row.Status) {
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
func planStatus(row session.PlanTaskRow) session.TaskStatus {
	if row.Stopped {
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceStopped,
			Word:     planStateWord(row),
		}
	}
	if row.Interrupted {
		// A PART OF A RUN NOTHING WAS DRIVING, set aside when the next request
		// arrived. It reads as the run's own row reads ([session.TaskStatus] of
		// an interrupted row): not in flight, no fault, and no question asked.
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceInterrupted,
			Word:     planStateWord(row),
		}
	}
	store := row.Status
	if row.Hold != "" && (strings.TrimSpace(store) == "ready" || strings.TrimSpace(store) == "running") {
		return session.TaskStatus{
			Tier: session.TaskTierMoving, Presence: session.TaskPresenceQueued,
			Word: planStateWord(row), On: session.TaskWaitMachine, Reason: row.Hold,
		}
	}
	switch strings.TrimSpace(store) {
	case "pending":
		// ADMITTED, NOT STARTED — the queued presence, and the moving tier
		// because nothing waits on the person ([session.TaskStatus] reads the
		// same pair off a queued node).
		return session.TaskStatus{
			Tier:     session.TaskTierMoving,
			Presence: session.TaskPresenceQueued,
			Word:     planStateWord(row),
		}
	case "ready", "claimed", "running":
		return session.TaskStatus{
			Tier:     session.TaskTierMoving,
			Presence: session.TaskPresenceWorking,
			Word:     planStateWord(row),
		}
	case "done":
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceDone,
			Word:     planStateWord(row),
		}
	case "failed", "cancelled":
		return session.TaskStatus{
			Tier:     session.TaskTierOver,
			Presence: session.TaskPresenceIncomplete,
			Word:     planStateWord(row),
		}
	case "paused":
		return session.TaskStatus{
			Tier:      session.TaskTierYourCall,
			Presence:  session.TaskPresenceNeedsLook,
			Word:      planStateWord(row),
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
	status := planStatus(row)
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

// planProgress is the run root progress row shared by every tasks reading. The
// width chooses a vocabulary tier; marks always come through the palette.
func planProgress(row session.PlanTaskRow, width int, pal palette) string {
	if row.Total <= 1 {
		return planStateWord(row)
	}
	if row.Done == row.Total && row.Failed == 0 {
		return "done"
	}
	long := width >= 90
	cells := 0
	switch {
	case width >= 60:
		cells = 10
	case width >= 40:
		cells = 5
	}
	if row.Total <= 10 && cells > row.Total {
		cells = row.Total
	}
	// THE FAILURES STAND AT THE ROW'S END, their share of the cells rounded up so
	// that one failure in a hundred is still one cell, and never at the frontier:
	// laid after the finished work they took the cell where the running mark
	// belongs, and one failure in fourteen tasks straddled two cells.
	failedCells := 0
	if row.Failed > 0 && cells > 0 {
		failedCells = (row.Failed*cells + row.Total - 1) / row.Total
		if failedCells >= cells {
			failedCells = cells - 1
		}
	}
	var dots strings.Builder
	for cell := 0; cell < cells; cell++ {
		lo, hi := cell*row.Total, (cell+1)*row.Total
		doneAt := row.Done * cells
		id := tokens.GEmptyCell
		switch {
		case cell >= cells-failedCells:
			id = tokens.GFailedCell
		case hi <= doneAt:
			id = tokens.GDoneCell
		case lo < doneAt || (row.Running > 0 && lo <= doneAt && hi > doneAt):
			id = tokens.GRunningCell
		}
		dots.WriteString(pal.glyph(id))
	}
	count := itoa(row.Done) + "/" + itoa(row.Total)
	if long {
		count = itoa(row.Done) + " of " + itoa(row.Total)
		// A FAILURE IS SAID IN WORDS AND DRAWN IN ITS CELL, both. The words used
		// to replace the dot row outright, so the one run a person most needs to
		// see at a glance was the one drawn with no picture at all.
		switch {
		case row.Failed > 0:
			count += railSep + itoa(row.Failed) + " failed"
		case row.Running > 0:
			count += railSep + itoa(row.Running) + " running"
		}
	}
	if dots.Len() == 0 {
		return count
	}
	return dots.String() + "  " + count
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
		if dep != nil && strings.TrimSpace(dep.Title) != "" && planStateWord(*dep) != "done" {
			return strings.TrimSpace(dep.Title)
		}
	}
	parent := kin[strings.TrimSpace(row.Parent)]
	if parent != nil && strings.TrimSpace(parent.Title) != "" && planStateWord(*parent) != "done" {
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
// A PLAN-BORN NODE DOES NOT SAY WHICH STORE TASK IT IS, and it does not need
// to: this surface can see this conversation's own node rows, and a plan-born
// node wears the store task's own title — the store is seeded with the node's
// title and every later task is added under it. So the two are matched on the
// title the pair cannot disagree about, restricted to this conversation's rows
// so another chat's work wearing the same words cannot hide a plan row.
//
// A ROW THAT DOES NAME ITS STORE TASK IS NOT MATCHED HERE AT ALL. The run's
// door publishes a row for work the graph holds no node for, and it says which
// task of the store that row is ([session.TaskNotice.PlanTask]) — so those rows
// are taken out by identity before this runs ([planStoreDraws]), and the title
// guess is left to the road that has nothing better.
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

// planStoreDraws is this conversation's own rows with the ones THE STORE IS THE
// AUTHORITY FOR taken out, and it is the first thing the tasks place's reading
// does with them.
//
// A ROW THE RUN'S DOOR PUBLISHED IS NOT A NODE. The door that takes the run
// road never admits a node into the graph — it seeds a plan store, names the
// store's task with the number the person was answered with, and publishes a
// row under that number ([session.Agent.startKnownTaskRun]). So the store and
// that row are one piece of work read from two ends, and unlike the node road
// the surface is TOLD which two ([session.TaskNotice.PlanTask], carried onto
// the row by place_tasks.go).
//
// AND THE HALF THAT IS DRAWN IS THE STORE'S, for two reasons that are the same
// reason. The store's status is what the run actually moves — the published row
// wears the engine's own word for a node nobody is driving, so the pair could
// not even agree on the state — and Enter over a plan row opens the page with
// the worker's trajectory on it ([app.taskSheetPlan]), where Enter over the
// published row opens a room the engine holds no node for and so draws nothing.
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN: the row that opens an
// empty room is not drawn.
//
// A ROW WHOSE TASK THE PLAN READ DOES NOT HOLD STAYS. The read may not have
// landed yet, and the run's store is archived the moment a finished plan is
// replaced ([session.Agent.openBeltRunStore]) — dropping a row on the strength
// of an identity nothing answers for would take the run off the page
// altogether, which is worse than the row it replaces.
func planStoreDraws(rows []tasksMineRow, plan []session.PlanTaskRow) []tasksMineRow {
	if len(rows) == 0 || len(plan) == 0 {
		return rows
	}
	held := make(map[string]bool, len(plan))
	for _, task := range plan {
		if id := strings.TrimSpace(task.ID); id != "" {
			held[id] = true
		}
	}
	kept := make([]tasksMineRow, 0, len(rows))
	for _, row := range rows {
		if held[strings.TrimSpace(row.planTask)] {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// ── THE PAGE ONE PLAN ROW OPENS ─────────────────────────────────────────────

// ── the steering verbs ──────────────────────────────────────────────────────

// The words the plan keys say, each quoted in the manual exactly as it is
// spelled here.
const (
	// taskPlanNoteWord is what the room's box says with nothing typed in it on
	// a run's task: what the words become, which is a note on the task
	// (planroom.go's [app.planRoomSteer]).
	taskPlanNoteWord = "a note for this task"
	// taskPlanPickupWord is WHEN a note is read. A worker is a separate loop, so
	// a note waits in the store until the worker asks for its next step — the
	// manual's own account of a note (worker-harness.md, "Steering a task"),
	// said under the note in the task's room once the store has it
	// (planroom.go's [app.planRoomSteer]).
	taskPlanPickupWord = "the worker reads a note at its next step"
	// taskPlanRefusedWord leads the line a refused action draws in a step's
	// place. It is the permissions page's own word for a call that was refused,
	// taken from that constant so the two places cannot come to disagree.
	taskPlanRefusedWord = permDenyWord
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
func (a *app) taskPlanPaused(rows []session.PlanTaskRow, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return strings.TrimSpace(row.Status) == "paused"
		}
	}
	return false
}

// taskPlanVerb is the one road every plan key takes: resolve the plan door, run
// the store verb the caller names, and put the store's own sentence on the
// pane's one line when it refuses. The store is the authority on its own laws —
// a terminal task cannot be cancelled, a whole run is not held — and its
// sentence is what a person reads back, never a card ([app.pageMsg] is the one
// refusal a place that is not home has to say).
func (a *app) taskPlanVerb(run func(planAgent) error) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		return a.taskPlanVerbFold(run(agent))
	})
}

func (a *app) taskPlanVerbFold(err error) func(bool) tea.Cmd {
	return func(here bool) tea.Cmd {
		if !here {
			return nil
		}
		if err != nil {
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
}

// taskPlanCancel ends a plan task, its descendants and the work hard-depending
// on it, through the store's own cancel ([session.Agent.PlanCancel]). It is the
// cancel a node row already has, reached through the plan verb.
func (a *app) taskPlanCancel(id string) tea.Cmd {
	return a.taskPlanVerb(func(p planAgent) error { return p.PlanCancel(id) })
}

// planOwnTask reports whether a plan row is the run's OWN task: the one row in
// a run's store that hangs under nothing. It is the run as a whole, and the two
// verbs mean something different on it. The store takes a cancel and a hold on
// any part and refuses both on this task for every caller, because no worker
// may end or hold the run it is part of. A person may end it, and that is the
// run's stop, asked for through the card ([app.taskPlanStop]). Nothing holds a
// whole run, so that key is not offered there and is a letter.
func planOwnTask(row session.PlanTaskRow) bool {
	return strings.TrimSpace(row.ID) != "" && strings.TrimSpace(row.Parent) == ""
}

// taskPlanStop is `x` on a plan row in the tasks place. A part is ended by the
// store's own cancel, at once, as it always was. THE RUN'S OWN TASK IS THE
// WHOLE RUN, and ending that is the act the stop card exists to confirm: the
// card is raised, aimed at the run through the plan's own door, and nothing is
// ended by one keystroke.
//
// THE PLACE STEPS ASIDE FOR THE CARD (stop.go's [app.raiseStop] says why): it
// takes the frame whole and the block draws every question above the message
// box, so a card raised over it would be a question nobody could see.
func (a *app) taskPlanStop(row session.PlanTaskRow) tea.Cmd {
	if !planOwnTask(row) {
		return a.taskPlanCancel(row.ID)
	}
	if _, ok := a.planReader(); !ok {
		return nil
	}
	a.closeTaskSheet()
	a.raiseStop(stopTarget{plan: row.ID, noun: stopTaskNoun, detail: stopTaskDetail})
	return nil
}

// taskPlanStopTaken is the card's "stop it" for a run's own task. The store's
// id goes through the plan's door, which ends the run ([session.Agent.PlanCancel]);
// what the run then says about where its work is arrives in the conversation
// from the engine, and a stop that could not be given is said where the person
// now is.
func (a *app) taskPlanStopTaken(id string) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		a.note(stopUnavailableWord)
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		err := agent.PlanCancel(id)
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				a.note(err.Error())
			} else {
				a.railStamp++
			}
			a.touch()
			return nil
		}
	})
}

// taskPlanToggle is `p`: hold the task the store says is running, release the
// one it says is held. The answer is the store's, read at the moment of the key.
func (a *app) taskPlanToggle(id string) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		paused := a.taskPlanPaused(agent.PlanTasks(), id)
		var err error
		if paused {
			err = agent.PlanResume(id)
		} else {
			err = agent.PlanPause(id)
		}
		return a.taskPlanVerbFold(err)
	})
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
	// AN ENDED ROW TAKES NEITHER KEY, because its foot names neither
	// ([app.tasksPlanKeyWords]): a key the line does not offer is the letter it
	// is, never a verb the store can only refuse.
	if planEnded(*item.plan) {
		return nil, false
	}
	switch key {
	case stopRaiseKey:
		return a.taskPlanStop(*item.plan), true
	case "p":
		if planOwnTask(*item.plan) {
			return nil, false
		}
		return a.taskPlanToggle(item.plan.ID), true
	}
	return nil, false
}

// tasksPlanKeyWords is the pane's key line for a plan row: the cancel and the
// one key that holds the task, named beside the enter clause the foot already
// draws ([tasksPlace.hint] reaches them). A key nobody can find is a key that
// does not exist, so both are said where a person reads what a row can do.
//
// A TASK THAT HAS ENDED IS OFFERED NEITHER. The store refuses to cancel or hold
// work that is done or incomplete, so a foot that named both keys under a
// finished task was two offers that could only be refused, on every finished
// page a person opened.
//
// AND THE RUN'S OWN TASK IS OFFERED ONLY ITS STOP, because nothing holds a whole
// run ([planOwnTask]). THE LAW IS ONE SENTENCE: no verb is named here that the
// store would refuse for this row, and a test presses every word this answers
// against a real store to hold it (stoprun_footlaw_test.go).
func (a *app) tasksPlanKeyWords(row session.PlanTaskRow) []string {
	status := row.Status
	if planEnded(row) {
		return nil
	}
	words := []string{tasksPlanCancelWord}
	if planOwnTask(row) {
		return words
	}
	if strings.TrimSpace(status) == "paused" {
		return append(words, tasksPlanResumeWord)
	}
	return append(words, tasksPlanPauseWord)
}

// planEnded reports whether a plan task has ended, read off the ONE word the
// row already draws for its state, so the key line and the page's sentences
// cannot disagree about which tasks can still move.
func planEnded(row session.PlanTaskRow) bool {
	switch planStateWord(row) {
	case "done", "incomplete", "stopped":
		return true
	}
	return false
}

// planDisplayCommand is the one display rule for a task step on the page, rail,
// and tree. The record remains untouched. The session marks each quote-aware
// part that belongs only to the run record or changes into the run copy; this
// surface omits those parts and preserves every other part and separator.
func planDisplayCommand(command string, parts []session.PlanCommandPart) string {
	left := func(part session.PlanCommandPart) bool {
		return part.RecordAddressed || part.RunCopyPrefix || strings.TrimSpace(part.Command) == ""
	}
	cut := false
	for _, part := range parts {
		if left(part) {
			cut = true
			break
		}
	}
	// NOTHING LEFT OUT IS THE LINE AS IT RAN. The parts are only ever a reason
	// to leave something out, never a second spelling of the command.
	if !cut {
		return planFirstLine(strings.TrimSpace(command))
	}
	// WHAT IS KEPT IS CUT FROM THE RECORDED LINE, span by span: each kept part
	// as it was typed, and between two kept parts the boundary that followed
	// the first of them. A part with nothing kept after it brings no boundary,
	// so a line never ends on one.
	var display strings.Builder
	last := -1
	for i, part := range parts {
		if left(part) {
			continue
		}
		// A kept part always has bytes of its own, so an empty or impossible span
		// is a part that was never given one.
		if part.Start < 0 || part.Start >= part.End || part.End > part.SepEnd || part.SepEnd > len(command) {
			return planJoinedParts(parts, left)
		}
		if last >= 0 {
			display.WriteString(command[parts[last].End:parts[last].SepEnd])
		}
		display.WriteString(command[part.Start:part.End])
		last = i
	}
	return planFirstLine(strings.TrimSpace(display.String()))
}

// planJoinedParts is the kept parts joined by their own boundaries, for parts
// that carry no spans into the line they came from.
func planJoinedParts(parts []session.PlanCommandPart, left func(session.PlanCommandPart) bool) string {
	var display strings.Builder
	wrote := false
	boundary := ""
	for _, part := range parts {
		if left(part) {
			continue
		}
		if wrote {
			display.WriteString(boundary)
		}
		display.WriteString(part.Command)
		boundary, wrote = part.Separator, true
	}
	return planFirstLine(strings.TrimSpace(display.String()))
}

// planFirstLine is a command as ONE ROW. A command that writes a document is
// many lines long, and a row that carried them all pushed the rest of the page
// off the screen: the page's foot and the box a note is typed in were drawn
// below the last row the terminal has. The first line says what the command is,
// and the mark says there was more. What ran is untouched; this is what is drawn.
func planFirstLine(command string) string {
	first, _, more := strings.Cut(command, "\n")
	first = strings.TrimRight(first, " \t\r")
	if more {
		return first + " …"
	}
	return first
}

// railPlanPending is the gap between a press on a run's task and that task's
// room being drawn.
//
// THE KEYS TYPED IN THE GAP ARE THE ROOM'S. The page is read off the update
// loop ([app.openRailPlan]), and on a hosted conversation the answer took 2.4
// seconds on a real screen. Until it folds back the conversation is still what
// is drawn, and its box used to take whatever was typed: a note meant for a
// task was sent to the model as a message. A person types at what they
// pressed, so from the press on, every key is held here, in order, and typed
// into the room's box the moment the room is up — into the box and nowhere
// else ([app.railPlanReplay]).
//
// THREE WAYS OUT, and none of them reaches the conversation: the answer opens
// the room and types the keys into its box; the answer says there is no page,
// the row's room opens as it always did and the keys are dropped; `esc`
// withdraws the press. A second press replaces the first and starts with no
// keys.
type railPlanPending struct {
	id   string
	keys []tea.KeyPressMsg
}

func (a *app) beginRailPlan(id string) { a.railPlanPending = railPlanPending{id: id} }

// railPlanReplay puts one key typed in the gap into the room's BOX, and does
// nothing else with it.
//
// THE GAP'S KEYS ARE THE NOTE AND NEVER THE PAGE'S VERBS. They were replayed
// through the page's whole keyboard, so a sentence that began with the stop key
// (`x-axis labels are wrong`) cancelled a running part with nothing asked, and
// the `enter` after it sent the rest to the store, all before the page had been
// drawn (#1244). A person typing at a page they cannot see yet is writing to its
// box, so the text keys and the box's own editing keys are replayed and every
// other key is dropped, `enter` included: a note typed blind waits in the box,
// unsent, until the person has read the page it is about to go to.
func (a *app) railPlanReplay(key tea.KeyPressMsg) {
	note := &a.input
	switch key.String() {
	case "backspace":
		note.deleteBackward()
		return
	case "ctrl+w":
		note.deleteWord()
		return
	case "ctrl+u":
		note.killToStart()
		return
	case "ctrl+k":
		note.killToEnd()
		return
	}
	if text := key.Key().Text; text != "" {
		note.insert(text)
	}
}
