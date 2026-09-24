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
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	store := row.Status
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

// planRailGap is the least room a plan row keeps between its title and the
// tail at the end of its line, and planRailMinTitle the least the title itself
// keeps once the tail has taken the rest ([planRailRow] says which yields
// first, and why).
const (
	planRailGap      = 2
	planRailMinTitle = 2
	// planRailMinTail is the least a held row's tail is worth drawing: `waits: `
	// and enough of a name to tell one task from another.
	planRailMinTail = 14
	// planRailKeepTitle is the least a title keeps beside a whole tail before
	// the title is laid first instead: enough cells to tell two tasks apart.
	planRailKeepTitle = 10
	// planRailLevels is how deep the rail's tree is drawn before deeper work
	// shares an indent: a task, the task under it, and no further.
	planRailLevels = 2
	// planRailLead is the one cell between the rail's seam and a plan row, the
	// same edge the rail's own rows keep. The page's lead is four cells, a
	// seventh of a rail this narrow.
	planRailLead = " "
)

// planRailRow is one plan task on the rail: the connector, the state mark from
// the vocabulary, the fitted title, and the state's own tail at the end of the
// line. It is the rail's row and not the page's — the page has the width for
// the steps and the money under the title, and the rail, which is read beside
// a conversation somebody is typing into, has one line ([tasksReading.planRows]).
//
// THE TITLE YIELDS BEFORE THE TAIL DOES. The tail is the one fact the row
// exists to carry at its end — what a held row waits on, where a run stands —
// and the narrow rail used to spend the tail's cells on the title first, so a
// row held behind `write the handler` read `queued · w…` and answered nothing.
// So the title is fitted into what is left beside the whole tail, and only
// when even a two-cell title cannot stand beside it does the tail give up its
// own end — never the name of the work it names.
//
// THE TASKS PLACE'S OWN PAGE ROWS ARE NOT THIS ROW. The page keeps its card
// and its figures; this is the projection the rail draws out of the same
// reading, and the two meet only in the layout that owns their tree
// ([tasksReading.lay]).
func planRailRow(line tasksLine, width int, pal palette, now time.Time) string {
	item := line.item
	glyph, ink := tasksGlyph(item, pal)
	lead := planRailLead + pal.dim(line.kin) + ink(glyph) + " "
	room := width - ansi.StringWidth(planRailLead+line.kin) - ansi.StringWidth(glyph) - 1
	if room < 1 {
		room = 1
	}
	tail := planRailTail(item, width, pal, now)
	if tail == "" {
		return lead + placeSubject(fit(planRailLabel(item), room), false, pal)
	}
	label := planRailLabel(item)
	tailWidth := ansi.StringWidth(tail)
	titleRoom := room - tailWidth - planRailGap
	// THE RUN'S ROW KEEPS ITS PROGRESS AND EVERY OTHER ROW KEEPS ITS NAME. The
	// dot row is short and is the one thing the run's row is read for, so its
	// title is fitted beside it. A held row's tail is a sentence (`waits: <the
	// task>`), and on a rail of under thirty cells it took the line and left the
	// title one letter, `w…  waits: write the…`, a row naming neither task. So
	// there the title is laid first, the tail is fitted into what is left, and a
	// remainder too short to name anything ([planRailMinTail]) draws no tail at
	// all: the row's mark already says it is held, and its page says behind what.
	if item.plan == nil || item.plan.Total == 0 {
		// A title that still reads beside the whole tail ([planRailKeepTitle])
		// yields to it, because the name of what a row waits on is worth more
		// than the last word of its own.
		if want := ansi.StringWidth(label); titleRoom < want && titleRoom < planRailKeepTitle {
			left := room - want - planRailGap
			if left < planRailMinTail {
				return lead + placeSubject(fit(label, room), false, pal)
			}
			tail = fit(tail, left)
			tailWidth = ansi.StringWidth(tail)
			titleRoom = room - tailWidth - planRailGap
		}
	}
	if tailWidth < 1 || titleRoom < planRailMinTitle {
		return lead + placeSubject(fit(label, room), false, pal)
	}
	title, titleWidth := fitWidth(label, titleRoom)
	return lead + placeSubject(title, false, pal) +
		strings.Repeat(" ", room-titleWidth-tailWidth) + pal.dim(tail)
}

// planRailDotsUnder is the rail width under which the run's dot row stands on a
// line of its own: the width tier at which [planProgress] stops drawing cells.
const planRailDotsUnder = 40

// planRailDots is the run's dot row on a line of its own, under the run's title,
// on a rail too narrow to carry it at the title's end.
//
// THE PICTURE IS THE POINT OF THE ROW. At the rail's ordinary width the tiers
// leave the run's row a bare `8/14`, which is a figure somebody has to read; the
// cells are the thing seen without reading, so where they cannot share the
// title's line they take the next one, all ten of them, and the title keeps its
// own line whole.
func planRailDots(line tasksLine, width int, pal palette) string {
	plan := line.item.plan
	if plan == nil || plan.Total <= 1 || width >= planRailDotsUnder || planRailFolded(line.item) {
		return ""
	}
	if plan.Done == plan.Total && plan.Failed == 0 {
		return ""
	}
	pad := line.underKin
	if pad == "" {
		pad = strings.Repeat(" ", ansi.StringWidth(line.kin))
	}
	lead := planRailLead + pal.dim(pad) + strings.Repeat(" ", taskSheetPhoneIndent)
	room := width - ansi.StringWidth(planRailLead+pad) - taskSheetPhoneIndent
	// The sixty-column tier is ten cells and `N/M`; the forty-column one is five.
	for _, tier := range []int{60, 40} {
		if dots := planProgress(*plan, tier, pal); ansi.StringWidth(dots) <= room {
			return lead + pal.dim(dots)
		}
	}
	return ""
}

// planRailLive is the one line a plan row with a step in flight spends under
// its own: the running glyph, the shell lead and the command — and nothing
// else. The steps and the money that stand under it on the tasks page
// ([planUnderRows]) are that page's own rows; on the rail they were drawn a
// second time beside the live command, a frame saying one fact twice.
func planRailLive(line tasksLine, width int, pal palette) string {
	if line.item.plan == nil || line.item.plan.Live.Step <= 0 {
		return ""
	}
	// THE UNDER-LINE WEARS THE PAD KIN AND NOT THE CONNECTOR, which is the same
	// choice the page's own under-block made ([tasksReading.lay]): a connector
	// says another row of the tree, and this line belongs to the one above it.
	pad := line.underKin
	if pad == "" {
		pad = strings.Repeat(" ", ansi.StringWidth(line.kin))
	}
	lead := planRailLead + pal.dim(pad) + strings.Repeat(" ", taskSheetPhoneIndent)
	room := width - ansi.StringWidth(planRailLead+line.kin) - taskSheetPhoneIndent
	if room < 1 {
		return ""
	}
	if live := planLiveLine(*line.item.plan, room, pal); live != "" {
		return lead + live
	}
	return ""
}

// planRailLabel is the words a plan row's one line carries. A family's
// finished rows fold to their count — [tasksReading.lay] builds the folded row
// out of them, titled `done` with `N done` as its activity — and the rail draws
// the count AS the line, `✓ 2 done`, rather than a row titled `done` wearing
// its count as a state.
func planRailLabel(item tasksItem) string {
	if planRailFolded(item) {
		return strings.TrimSpace(item.entry.Activity)
	}
	return tasksLabel(item.entry)
}

// planRailFolded reports whether this row is the one line a family's finished
// rows folded to — the row [tasksReading.lay] built out of them, titled `done`
// with their count as its activity.
func planRailFolded(item tasksItem) bool {
	return item.plan != nil &&
		strings.TrimSpace(item.entry.Title) == "done" &&
		strings.TrimSpace(item.entry.Activity) != ""
}

// planRailTail is the one fact a plan row's line ends in, and nothing more.
//
// A HELD ROW SAYS WHAT IT WAITS ON — the reason the reading already carries
// ([planItem] parks it there off [planWaits]) — and a running row carries
// nothing, because its mark and the live line under it are the whole of what
// it has to say. The run's row ends in [planProgress] at the rail's own
// width, which is where the dot row's tiers live; a row that has landed ends
// in how long ago it did, which is the last fact anybody watching a rail
// still wants.
func planRailTail(item tasksItem, width int, pal palette, now time.Time) string {
	if item.plan == nil || planRailFolded(item) {
		return ""
	}
	switch strings.TrimSpace(item.plan.Status) {
	case "done", "failed", "cancelled":
		if !item.entry.EndedAt.IsZero() {
			return sinceAt(item.entry.EndedAt, now)
		}
		return ""
	}
	var parts []string
	if item.plan.Total > 0 && width >= planRailDotsUnder {
		if progress := planProgress(*item.plan, width, pal); progress != "" {
			parts = append(parts, progress)
		}
	}
	if reason := item.status().Reason; reason != "" {
		parts = append(parts, reason)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ")
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
func (a *app) taskSheetPlan(id string) tea.Cmd { return a.taskSheetPlanAsk(id, nil, nil, nil) }

// taskSheetPlanFrom is [app.taskSheetPlan] for a step INTO one of a page's
// parts: `from` is the page stepped out of, and it goes on the way back when
// the part's page has opened and not before.
func (a *app) taskSheetPlanFrom(id string, from *session.PlanTaskPage) tea.Cmd {
	return a.taskSheetPlanAsk(id, from, nil, nil)
}

// taskSheetPlanAsk is the ONE door onto a stored page, for every gesture that
// opens one: enter in the list, a step into a part, a press on a rail row. The
// read leaves the loop ([app.offLoop]) and what happens next is decided when it
// comes back: `opened` runs once the page is up, and `missing` is the gesture's
// own answer for a task the store has no page for, so a rail row whose run's
// store is gone still opens what it always opened.
func (a *app) taskSheetPlanAsk(id string, from *session.PlanTaskPage, opened func() tea.Cmd, missing func() tea.Cmd) tea.Cmd {
	agent, ok := a.planReader()
	if !ok {
		if missing != nil {
			return missing()
		}
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			if !here {
				// EVERY ENDING OF THE READ ENDS THE HOLD IT WAS MADE FOR. The keys
				// are held for as long as this read is out and no longer, and the
				// read has its own bound: over the wire a call gives up at its
				// deadline and answers no page (internal/remote's callDeadline),
				// which is the `missing` road below. An answer for a front the
				// person has left opens nothing, and used to leave the hold taking
				// every key until `esc`.
				if opened != nil && a.railPlanPending.id == id {
					a.railPlanPending = railPlanPending{}
				}
				return nil
			}
			if opened != nil && a.railPlanPending.id != id {
				return nil
			}
			if !found {
				if missing != nil {
					return missing()
				}
				return nil
			}
			// THE PAGE STEPPED OUT OF GOES ON THE WAY BACK ONLY WHEN THE NEW ONE
			// OPENED, and only if the person is still on it: a part with no page
			// leaves `esc` exactly one step from the list, as it was.
			if from != nil {
				if !a.taskSheet.planOn || a.taskSheet.plan.Row.ID != from.Row.ID {
					return nil
				}
				a.taskSheet.planBack = append(a.taskSheet.planBack, *from)
			}
			a.taskSheet.plan, a.taskSheet.planOn, a.taskSheet.detailOn = page, true, true
			a.taskSheet.planPageAt = a.now()
			a.taskSheet.planBriefFull = false
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
			var cmd tea.Cmd
			if opened != nil {
				cmd = opened()
			}
			a.touch()
			return cmd
		}
	})
}

// closeTaskPlan backs out one layer to the list, which is the card's own `esc`.
func (a *app) closeTaskPlan() {
	a.taskSheet.plan, a.taskSheet.planOn, a.taskSheet.detailOn = session.PlanTaskPage{}, false, false
	a.taskSheet.planBriefFull = false
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

// taskPlanStop is `x` on a plan row or its page. A part is ended by the store's
// own cancel, at once, as it always was. THE RUN'S OWN TASK IS THE WHOLE RUN,
// and ending that is the act the stop card exists to confirm: the card is
// raised, aimed at the run through the plan's own door, and nothing is ended by
// one keystroke.
//
// THE PAGE STEPS ASIDE FOR THE CARD, the way a background job's page does
// (stop.go's [app.raiseStop] says why): it takes the frame whole and the block
// draws every question above the message box, so a card raised over it would be
// a question nobody could see, answered by the next key they pressed. The run's
// row is still on the side list and opens the page again.
func (a *app) taskPlanStop(row session.PlanTaskRow) tea.Cmd {
	if !planOwnTask(row) {
		return a.taskPlanCancel(row.ID)
	}
	if _, ok := a.planReader(); !ok {
		return nil
	}
	a.closeTaskPlan()
	a.railTaskPlanOn = false
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
				// A stop that could not be given is said where the person is: on
				// the program's room when that is what they stopped it from.
				if a.programOf() != nil {
					a.roomNote(err.Error())
				} else {
					a.note(err.Error())
				}
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
	return a.offLoop(func() func(bool) tea.Cmd {
		err := agent.PlanNote(id, text)
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			if err != nil {
				a.pageMsg = err.Error()
				a.touch()
				return nil
			}
			a.taskSheet.planNote.reset()
			a.pageMsg = ""
			a.railStamp++
			// Read the page again so the note a person just left is on the screen, which
			// is the receipt the store cannot draw itself.
			if found {
				a.taskSheet.plan = page
				a.taskSheet.planPageAt = a.now()
			}
			a.touch()
			return nil
		}
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

// taskPlanKey is the page's keyboard: `esc` and the chord out, the four reading
// keys the card also spends (the page is read down, so the wheel and the arrows
// move an offset rather than a cursor), the two verbs a plan row has — `x` and
// `p`, taken over an EMPTY composer — and the note itself, where every printable
// key goes into the box and `enter` sends it ([app.taskPlanNoteSend]) rather
// than a chat turn.
func (a *app) taskPlanKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	// A PROGRAM'S PAGE IS READ, NEVER TYPED INTO. It has no box, so it takes the
	// reading keys and the way back below exactly as every page takes them, `x`
	// for the stop its run's own task has, and `enter` only into a part it
	// lists; every other key is nothing, rather than a note no program reads or
	// a letter aimed at a box that is not there.
	if a.taskPlanIsProgram() {
		switch key {
		case stopRaiseKey:
			return a.taskPlanStop(a.taskSheet.plan.Row)
		case "enter":
			if a.taskSheet.planAt >= 0 && a.taskSheet.planAt < len(a.taskSheet.plan.Children) {
				old := a.taskSheet.plan
				return a.taskSheetPlanFrom(old.Children[a.taskSheet.planAt].ID, &old)
			}
			return nil
		case "esc", "left", taskSheetKey, "up", "ctrl+p", "down", "ctrl+n", "pgup", "pgdown", "ctrl+o":
		default:
			return nil
		}
	}
	// The caret's own chords first, the route every box on this surface takes
	// (place_tasks.go's filter, the conversation's composer).
	if !a.taskPlanIsProgram() && (editorMotion(&a.taskSheet.planNote, key) ||
		editorUndo(&a.taskSheet.planNote, key) ||
		editorWordKill(&a.taskSheet.planNote, key)) {
		a.touch()
		return nil
	}
	// A letter is a letter the moment there is a note to type, so the row's own
	// keys are read over an empty box and never over a sentence (the list's own
	// law, [app.taskSheetPlanKey]).
	if a.taskSheet.planNote.empty() {
		switch key {
		case stopRaiseKey:
			return a.taskPlanStop(a.taskSheet.plan.Row)
		case "p":
			// NOTHING HOLDS A WHOLE RUN, so on the run's own page this key is the
			// letter it is and starts a note ([planOwnTask]).
			if !planOwnTask(a.taskSheet.plan.Row) {
				return a.taskPlanToggle(a.taskSheet.plan.Row.ID)
			}
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
	case "ctrl+o":
		if a.taskPlanBriefFolds() {
			a.taskSheet.planBriefFull = !a.taskSheet.planBriefFull
			a.taskSheet.detailTop = 0
			a.taskSheet.planStick = false
			a.touch()
		}
		return nil
	case "enter":
		if a.taskSheet.planNote.empty() && a.taskSheet.planAt >= 0 && a.taskSheet.planAt < len(a.taskSheet.plan.Children) {
			old := a.taskSheet.plan
			id := old.Children[a.taskSheet.planAt].ID
			return a.taskSheetPlanFrom(id, &old)
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

// taskPlanBriefFolds reports whether the open page's brief is long enough for
// `ctrl+o` to fold: measured at the width a program's conversation draws it at
// on a program's page ([app.taskConversationFolds]), and at the body's width on
// every other.
func (a *app) taskPlanBriefFolds() bool {
	if a.taskPlanIsProgram() {
		return a.taskConversationFolds()
	}
	return len(planBriefRows(a.taskSheet.plan.Description, a.bodyWidth())) > briefFoldLines
}

// taskPlanHeadRows is what the page spends above its body: the task's title,
// the line under it, and the rule — the card's own head. It is drawn and
// counted by this one function, so the frame, the window and the scroll cannot
// disagree about where the body starts.
//
// ON A PROGRAM'S PAGE THE LINE UNDER THE TITLE IS PINNED: where the program is,
// what it has spent, how many calls it has made and how long it has been going
// ([app.taskPlanPinned]). The page opens stuck to its bottom edge and follows
// the conversation down, so a figure drawn as the body's first line — where
// every other page draws its telemetry — is a figure that scrolls away the
// moment there is more than a screen of it. On every other page, and on a
// program's page with nothing yet to say, that line is the air it always was.
func (a *app) taskPlanHeadRows(width int) []string {
	pal := a.pal
	under := ""
	if pinned := a.taskPlanPinned(a.taskSheet.plan, width); pinned != "" {
		under = pal.dim(pinned)
	}
	return []string{fit(pal.bold(pal.ink(a.taskSheet.plan.Row.Title)), width), under, pal.dim(rule(width))}
}

// taskPlanFoot is what the page spends under its body: the closing rule, the
// note composer, the one sentence saying when a note is read, and the key line,
// in that order — the card's own foot grew three rows for the box the page types
// into and the sentence that says what happens to what is typed in it.
const taskPlanFoot = 4

// taskPlanProgramFoot is a program's page's foot: the closing rule and the key
// line, and no box. A program reads no note — nothing a person types on its
// page would ever reach it — so the box, and the sentence promising that a
// worker reads a note at its next step, are absent there rather than false.
const taskPlanProgramFoot = 2

// taskPlanFootRows is how many rows the open page spends under its body.
func (a *app) taskPlanFootRows() int {
	if a.taskPlanIsProgram() {
		return taskPlanProgramFoot
	}
	return taskPlanFoot
}

// taskPlanWindow is the page's head, its body, the rows the body is drawn in and
// the rows its foot spends, resolved from the frame once: the draw and the scroll
// both read the bottom off this, so the two cannot disagree about where the
// bottom is — and the head is counted here, never assumed, so a pinned line is
// a row the body gives up rather than a row drawn over it.
func (a *app) taskPlanWindow(width, height int) ([]string, []string, int, int) {
	if height < 1 {
		height = 1
	}
	head := a.taskPlanHeadRows(width)
	foot := a.taskPlanFootRows()
	if height-len(head)-foot < 1 {
		foot = 0
	}
	room := height - len(head) - foot
	if room < 1 {
		room = 1
	}
	return head, a.taskPlanBody(width - 2), room, foot
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
	_, body, room, _ := a.taskPlanWindow(width, height)
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
//
// A PROGRAM'S PAGE HAS NO BOX, so it has no caret either: its foot is the rule
// and the keys ([taskPlanProgramFoot]), and a blinking bar over nothing a person
// can type into is a cursor pointing at a key that does not exist — the card's
// own law (place_sessions.go's ownFrame hides it for the same reason).
func (a *app) taskPlanFrame(width, height int) ([]string, int, int) {
	pal := a.pal
	if height < 1 {
		height = 1
	}
	program := a.taskPlanIsProgram()
	if program {
		a.caret = false
	}
	lines := make([]string, 0, height)
	add := func(text string) { lines = append(lines, text) }

	// THE HEAD AND THE FOOT ARE THE PAGE'S OWN ROWS, and a frame too short for
	// the body between them gives the body up rather than the way out — the
	// page's own trim, and the one [app.taskPlanWindow] resolves so the draw and
	// the scroll agree ([app.taskPlanScroll]).
	head, body, room, foot := a.taskPlanWindow(width, height)
	for _, row := range head {
		add(row)
	}
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
		if !program {
			if a.taskSheet.planNote.empty() {
				add(" " + pal.dim(fit(prompt+taskPlanNoteWord, width-1)))
			} else {
				add(" " + fit(prompt+a.taskSheet.planNote.String(), width-1))
			}
			caretX, caretY = ansi.StringWidth(prompt)+1, len(lines)-1
			// AND WHEN THE WORKER READS IT, under the box that writes it: the
			// worker is a separate loop, so a note waits in the store until it
			// asks for its next step — the one thing a person needs to know about
			// the box they are typing into (taskPlanPickupWord).
			//
			// A TASK THAT HAS ENDED TAKES NO NEXT STEP, so the sentence is absent
			// there rather than false. Its row stays, empty, because the foot's
			// height is fixed and the caret is placed against it.
			if planEnded(a.taskSheet.plan.Row) {
				add("")
			} else {
				add(" " + pal.dim(fit(taskPlanPickupWord, width-1)))
			}
		}
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
	parts := []string{"↑↓ scroll"}
	// A PROGRAM'S PAGE SENDS NOTHING, so its key line offers no send.
	if !a.taskPlanIsProgram() {
		parts = append(parts, "enter send")
	}
	parts = append(parts, a.tasksPlanKeyWords(a.taskSheet.plan.Row)...)
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
	// A PROGRAM'S PAGE IS ITS CONVERSATION, drawn where every other page draws
	// its steps (taskconversation.go).
	if a.taskPlanIsProgram() {
		return a.taskProgramBody(width)
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
		section("brief")
		// THE BRIEF IS DRAWN THROUGH THE READER THE TRANSCRIPT ALREADY USES
		// ([requestDisplayFor]). A plan task's description can be the generated
		// work order a run hands its workers, and a page that drew it raw opened
		// on the machinery addressed to the model — the shouted scaffold heading,
		// the rule under it, and only then the person's ask. The reader reshapes
		// that document into plain headings with this task's own work first, the
		// same service the conversation's own transcript gives the same text;
		// a brief that is not the generated document passes through unchanged, so
		// a person's own typed brief draws exactly as it always did. THE STORED
		// TEXT IS NEVER TOUCHED: only what is drawn changes.
		lines := planBriefRows(desc, width)
		if !a.taskSheet.planBriefFull && len(lines) > briefFoldLines {
			for _, line := range lines[:briefFoldLines] {
				add(pal.ink(line))
			}
			add(pal.dim(bandFoldWord(len(lines)-briefFoldLines, briefFoldWhat, true) + railSep + briefFoldKey))
		} else {
			for _, line := range lines {
				add(pal.ink(line))
			}
		}
	}
	if len(page.Checks) > 0 {
		section("checks")
		for _, check := range page.Checks {
			if check = strings.TrimSpace(check); check != "" {
				addWrapped(check, pal.ink)
			}
		}
	}
	if len(page.Notes) > 0 {
		section("notes")
		out = append(out, a.taskPlanNoteRows(page.Notes, width)...)
	}
	if len(page.Steps) > 0 || !page.Live.Empty() {
		section("steps")
		for _, step := range page.Steps {
			// A CALL THE ENGINE SAYS DID NOT RUN IS ONE OF TWO THINGS, and the
			// engine says which. A correction about the FORM of the worker's reply
			// was addressed to the worker and nothing was attempted: it stays in
			// the record and in the head's count, and has no row. AN ACTION THE
			// WORKER ATTEMPTED AND A DOOR REFUSED is something a person steering
			// the run wants to see, so it draws as one dim line in the step's
			// place: the word the permissions page already uses for a refused call
			// and what was tried. It carries NO NUMBER, because a number on this
			// page is a step that ran, and the rows around it keep the numbers the
			// record gave them. Both facts are fields set where the event is known;
			// this surface never reads the answer's sentence, which was written for
			// the worker, and a record without the fields draws as before.
			if step.NotRun {
				if tried := planDisplayCommand(step.Command, step.Parts); step.Refused && tried != "" {
					add(pal.dim("   " + taskPlanRefusedWord + railSep + tried))
				}
				continue
			}
			// A STEP WITH NOTHING OF THE WORK IN IT HAS NO ROW, AND EVERY OTHER ROW
			// KEEPS THE NUMBER THE RECORD GAVE IT. The head counts the steps that
			// ran, the live step is called by its number elsewhere, and a row
			// renumbered to close the gap would make both of them wrong about it.
			command := planDisplayCommand(step.Command, step.Parts)
			if command == "" {
				continue
			}
			add(pal.ink(itoa(step.Step) + "  " + command))
			// THE HEAD IS THE ROW'S OWN OR IT IS NOT DRAWN. The engine says when
			// the row left out a part that could have written it
			// ([session.PlanStep.ObservationHeadWithheld]); this surface reads
			// that fact and never the words that came back.
			if !step.ObservationHeadWithheld {
				if head := planObservationHead(step.Observation); head != "" {
					add(pal.dim("   " + head))
				}
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
			if command := planDisplayCommand(live.Command, page.Row.LiveParts); command != "" {
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
			if line := planLiveRow(kid.Live.Command, kid.LiveParts, width-ansi.StringWidth(lead)-2, pal); line != "" {
				add(lead + "  " + line)
			}
		}
	}
	return out
}

// taskPlanNoteRows is every note on a task as the page draws them under its
// `notes` heading: each one's author and moment on a dim line, then its words.
func (a *app) taskPlanNoteRows(notes []session.PlanTaskNote, width int) []string {
	pal := a.pal
	var out []string
	for _, note := range notes {
		// AN AUTHOR IS DRAWN ONLY AS A WORD A PERSON WOULD RECOGNISE. `you` is
		// one. Every other author the store holds is an id of its own, the run's
		// number or a worker's handle, and this page has no word for the kind of
		// task that left the note; a page headed `1 · now` or `2ytmh2 · now` names
		// nobody. The moment is kept and the id is never drawn, which is the
		// owner's ruling on this surface: no internal name on a person's screen.
		who := ""
		if note.Person {
			who = "you"
		}
		when := sinceAt(note.At, a.now())
		switch {
		case who != "" && when != "":
			out = append(out, pal.dim(who+railSep+when))
		case who != "":
			out = append(out, pal.dim(who))
		case when != "":
			out = append(out, pal.dim(when))
		}
		for _, para := range strings.Split(note.Body, "\n") {
			if strings.TrimSpace(para) == "" {
				continue
			}
			for _, line := range wrap(strings.TrimSpace(para), width) {
				out = append(out, pal.ink(line))
			}
		}
	}
	return out
}

func planBriefLines(text string, width int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if para = strings.TrimSpace(para); para != "" {
			lines = append(lines, wrap(para, width)...)
		}
	}
	return lines
}

// planBriefRows is the brief section's own read of a stored description: the
// work order drawn through the reader the conversation's transcript already
// uses ([requestDisplayFor]) before the page wraps it. Every surface that
// holds this text gives a person the same reading of it — plain headings, the
// task's own work first, no machinery addressed to a worker — and the door
// counts the lines this function draws, so the fold and the count it names can
// never disagree. A description that is not the generated document is wrapped
// as it was always wrapped.
func planBriefRows(desc string, width int) []string {
	return planBriefLines(requestDisplayFor(strings.TrimSpace(desc)), width)
}

// planChildWord is one child's own line on the task's page: its state word and
// its title, joined the way the page's own telemetry line joins two facts. The
// word is the same [planStateWord] every row on this surface wears.
func planChildWord(row session.PlanTaskRow) string {
	word, title := planStateWord(row), strings.TrimSpace(row.Title)
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
//
// AND IT IS TAKEN ON A BEAT, NOT ON EVERY TICK. The paint clock offers this read
// many times a second, and holding back only while one was out meant a fast
// engine was asked again the moment it answered: 509 reads over the wire in
// ninety seconds on a real screen, for one open page. A worker lands a step
// every few seconds at best, so the page learns of it the way the rail learns
// of the run, once every [elsewhereEvery] and only while the task can still
// move ([app.taskPlanFollows], place_tasks.go's [tasksPlace.planDue]). The beat
// is counted from the last time the page was read for any reason, so a page
// just opened, or just re-read for a note, is not read again at once.
//
// IT STAYS IN THE ORDERED LINE although nobody pressed for it ([app.besideLine]
// says who may leave). Its FOLD replaces the page, and a note a person sends
// re-reads the page too: outside the line, a follow asked before the note and
// answered after it would put back a page without the note on it.
func (a *app) taskPlanFollow() tea.Cmd {
	if !a.taskPlanFollows() || a.taskSheet.planFollowing {
		return nil
	}
	if a.now().Sub(a.taskSheet.planPageAt) < elsewhereEvery {
		return nil
	}
	agent, ok := a.planReader()
	if !ok {
		return nil
	}
	id := a.taskSheet.plan.Row.ID
	a.taskSheet.planFollowing = true
	a.taskSheet.planPageAt = a.now()
	return a.offLoop(func() func(bool) tea.Cmd {
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			a.taskSheet.planFollowing = false
			// THE ANSWER IS FOR THE PAGE THAT ASKED. A person who opened another
			// task, or closed the page, while this read was out is not handed the
			// page they left.
			if here && found && a.taskSheet.planOn && a.taskSheet.plan.Row.ID == id {
				a.taskSheet.plan = page
			}
			return nil
		}
	})
}

// taskPlanRunning reports whether the page is open on a task that is still
// running — the one condition under which the paint clock has to keep turning
// for the page's own sake, because the page follows a live edge
// ([app.taskPlanFollow]). It reads the row's own state WORD, so a task that has
// ended, or one a person has held, takes the page off the clock: a held task is
// dispatching nothing and a settled one never will again.
// taskPlanFollows reports whether the open page is on a task that can still
// move: queued or running, by the ONE state word its row already wears. A page
// on a task that has ended, or one a person is holding, is a still page and is
// never read again; the read that opened it was the last.
func (a *app) taskPlanFollows() bool {
	if !a.taskSheet.detailOn || !a.taskSheet.planOn {
		return false
	}
	switch planStateWord(a.taskSheet.plan.Row) {
	case "queued", "running":
		return true
	}
	return false
}

func (a *app) taskPlanRunning() bool {
	if !a.taskSheet.detailOn || !a.taskSheet.planOn {
		return false
	}
	return planStateWord(a.taskSheet.plan.Row) == "running"
}

// planTelemetryLine is a plan task's own figures as one dim line: whether the
// work is running, how many steps its worker has taken, and what it has cost —
// each clause omitted when it has nothing behind it.
func planTelemetryLine(row session.PlanTaskRow) string {
	var segs []string
	if word := planStateWord(row); word != "" {
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
		figure = planStateWord(row)
	}
	return strings.TrimSpace(tierGlyph(pal, planStatus(row)) + " " + figure)
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

// planRailNow draws the stored now sentence beneath the root's dot row. It is
// pure frame work: wrapping plain data already carried by the reading.
func planRailNow(line tasksLine, width int, pal palette, sentence string) []string {
	sentence = strings.TrimSpace(sentence)
	if sentence == "" || width >= planRailDotsUnder {
		return nil
	}
	pad := line.underKin
	if pad == "" {
		pad = strings.Repeat(" ", ansi.StringWidth(line.kin))
	}
	lead := planRailLead + pal.dim(pad) + strings.Repeat(" ", taskSheetPhoneIndent)
	room := width - ansi.StringWidth(planRailLead+pad) - taskSheetPhoneIndent
	if room < 4 {
		return nil
	}
	lines := wrap(sentence, room)
	if len(lines) > 2 {
		lines[1] = fit(strings.Join(lines[1:], " "), room)
		lines = lines[:2]
	}
	out := make([]string, 0, len(lines))
	for _, text := range lines {
		out = append(out, lead+pal.dim(text))
	}
	return out
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

// planWithoutOwnFolder drops a leading change into the task's own folder, which
// the page's head names once, and leaves every other directory change as typed.
func planWithoutOwnFolder(command, folder string) string {
	if command == "" || folder == "" {
		return command
	}
	quotedSingle := "'" + strings.ReplaceAll(folder, "'", "'\\''") + "'"
	quotedDouble := `"` + strings.ReplaceAll(strings.ReplaceAll(folder, `\`, `\\`), `"`, `\"`) + `"`
	for _, path := range []string{folder, quotedSingle, quotedDouble} {
		prefix := "cd " + path + " && "
		if strings.HasPrefix(command, prefix) {
			if rest := strings.TrimSpace(strings.TrimPrefix(command, prefix)); rest != "" {
				return rest
			}
		}
	}
	return command
}

// railPlanPending is the gap between a press on a run's row in the side list
// and that task's page being drawn.
//
// THE KEYS TYPED IN THE GAP ARE THE PAGE'S. The page is read off the update
// loop ([app.taskSheetPlanAsk]), and on a hosted conversation the answer took
// 2.4 seconds on a real screen. Until it folds back the conversation is still
// what is drawn, and its box used to take whatever was typed: a note meant for
// a task was sent to the model as a message. A person types at what they
// pressed, so from the press on, every key is held here, in order, and handed
// to the page's own keyboard the moment the page is up ([app.finishRailPlan]).
//
// FOUR WAYS OUT, and none of them reaches the conversation: the answer opens
// the page and replays the keys; the answer says there is no page, the row's
// room opens as it always did and the keys are dropped, because a room's box
// is a different receiver again; `esc` withdraws the press; and so does going
// to a place, whose answer then opens nothing ([app.railPlanFront]). A second
// press replaces the first and starts with no keys.
type railPlanPending struct {
	id   string
	keys []tea.KeyPressMsg
}

func (a *app) beginRailPlan(id string) { a.railPlanPending = railPlanPending{id: id} }

// railPlanFront is whether the answer to a row's press may still open
// anything: only while the conversation the row was pressed in is what is in
// front. A place opened since is a way out of the press ([app.leaveTaskOverlays]
// withdraws it), and this is the same rule read where the answer lands, so no
// door that forgot to withdraw it can open a page over a place or a room under
// one.
func (a *app) railPlanFront() bool { return a.showing() == nil }

func (a *app) finishRailPlan(id string) tea.Cmd {
	if a.railPlanPending.id != id {
		return nil
	}
	keys := a.railPlanPending.keys
	a.railPlanPending = railPlanPending{}
	if !a.railPlanFront() {
		return nil
	}
	// A PROGRAM'S PAGE IS A ROOM IN THE CONVERSATION'S TAB (programroom.go), opened
	// on the page this read just brought back — which is how a row the surface
	// did not yet hold as a program's, and a run's own line under its row, reach
	// it. The keys held for a page are dropped, as they are when the answer is a
	// room: a room's box is a different receiver.
	if a.taskPlanIsProgram() {
		if n, err := strconv.ParseUint(planTaskIDWord(id), 10, 64); err == nil && n != 0 {
			page := a.taskSheet.plan
			a.closeTaskPlan()
			a.openProgramRoom(n, page.Row.Title, page)
			return a.takeRoomPump()
		}
	}
	a.railTaskPlanOn = true
	// THE SIDE LIST GIVES THE KEYBOARD BACK, because the page covers it: a list
	// holding keys nobody can see would spend the page's first `esc` on itself.
	a.railHold = false
	var cmds []tea.Cmd
	for _, key := range keys {
		// A KEY THAT LEFT THE PAGE ENDS THE REPLAY. The keys were kept for the
		// page, and one of them can close it (`esc`, or the stop on the run's own
		// page, which steps aside for its card): what was typed after it was typed
		// blind, and is dropped rather than aimed at whatever is up now.
		if !a.taskSheet.planOn {
			break
		}
		if cmd := a.taskPlanKey(key); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if !a.taskSheet.planOn {
		a.railTaskPlanOn = false
	}
	return tea.Batch(cmds...)
}
