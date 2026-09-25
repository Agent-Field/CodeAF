package tui3

// planroom.go is A RUN'S TASK IN THE TASK ROOM.
//
// A task on the run engine is a row of the run's plan store and not a node of
// this window's graph: it has no journal and no live lane, and for a while it
// opened a page of its own that looked like the room and was not one. The owner
// ruled that the two should be the same page, so there is ONE PAGE TYPE and
// this file is its second source. A run's task opens the same [taskRoom], with
// the same head, the same transcript renderer, the same box and the same keys;
// what changes is where the page READS from.
//
//   - The transcript is the worker's trajectory: each step is the conversation's
//     own shell call, with its command and what came back, drawn by the same
//     renderers every call is drawn by ([app.planRoomEntries]).
//   - A person's note on the task is their correction, drawn where a
//     correction is drawn, and what they type into the box becomes one
//     ([app.planRoomSteer], the store's own note verb).
//   - `x` stops it through the plan's door, which for the run's own task is the
//     run's stop ([app.planRoomStopTarget]).
//   - The head reads the store's own figures, and a figure the store has not
//     got is left off rather than drawn as a zero ([app.planRoomNode]).
//
// THE STORE IS READ OFF THE LOOP AND ON A BEAT. A page on work that can still
// move is read again every [elsewhereEvery], the rail's own beat for the same
// store, and a page on work that has ended is read once and never again.

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// planRoom is what a room carries when its task is a row of a run's store.
type planRoom struct {
	// id is the store's own id for the task, the one every plan door takes.
	id string
	// page is the store's last reading of the task, and node the task lent to
	// the room's head as [planRailNode] lends one to the rail.
	page session.PlanTaskPage
	node *taskNode
	// shape is the page's content as it was last drawn, so a read that brings
	// back the same steps, notes and state leaves the page's rows alone.
	shape string
	// reading says a read of the page is out; at is when the page was last read.
	reading bool
	at      time.Time
	// sending is the corrections typed into this page whose note the store has
	// not answered for yet. They are drawn under the page until the answer
	// comes back, so a read that lands meanwhile does not take them away.
	sending []*steerElbow
	// work is the run's working copy as it was last read for the work tab, and
	// workRead says a reading has come back.
	work     session.PlanTaskWork
	workRead bool
	working  bool
}

// roomPlan is the open room's store task, or nil on every other page.
func (a *app) roomPlan() *planRoom {
	if a.room == nil {
		return nil
	}
	return a.room.plan
}

// openPlanRoom opens the task room over one store task's page.
func (a *app) openPlanRoom(page session.PlanTaskPage) tea.Cmd {
	if a.startingChat() {
		a.parkChatStart()
	}
	id := strings.TrimSpace(page.Row.ID)
	title := strings.TrimSpace(page.Row.Title)
	room := a.newRoom(planRailNodeID(id), title)
	room.plan = &planRoom{id: id}
	a.room = room
	a.planRoomTake(page)
	// THE BOX TALKS TO THIS TASK, under its own name, exactly as it does on a
	// node's page ([app.openRoom]): what was being written elsewhere is stashed
	// under its own reader and this page's own unsent line comes back.
	a.retargetComposer(taskRecipient(room.id))
	a.sel = -1
	a.dropHover()
	a.touch()
	if room.done {
		return nil
	}
	return planRoomTick(room.gen)
}

// planRoomTake folds one reading of the page into the open room: the lent
// node the head is drawn from, whether the work is over, and the transcript.
func (a *app) planRoomTake(page session.PlanTaskPage) {
	room := a.room
	if room == nil || room.plan == nil {
		return
	}
	plan := room.plan
	plan.page = page
	plan.node = planRoomNode(page)
	plan.at = a.now()
	if title := strings.TrimSpace(page.Row.Title); title != "" {
		room.title = title
	}
	room.setDone(planEnded(page.Row))
	shape := planRoomShape(page)
	if shape == plan.shape && room.entries != nil {
		a.roomTouched()
		return
	}
	plan.shape = shape
	room.entries, room.turn = a.planRoomEntries(page)
	for _, elbow := range plan.sending {
		room.entries = append(room.entries, entry{kind: entrySteer, turn: room.turn, steer: elbow})
	}
	a.roomTouched()
}

// planRoomNode lends the page's task a node holding what the store knows about
// it, for the room's head, its state word, its clock and its price.
//
// THE MODEL AND THE TOKENS ARE THE LEDGER'S, and they are empty when it names
// none: a task that has written no spend row has no known model and no known
// count, and the head leaves both clauses off rather than drawing a blank or a
// zero ([session.PlanTaskRow.Model]).
func planRoomNode(page session.PlanTaskPage) *taskNode {
	node := planRailNode(page.Row)
	node.brief = strings.TrimSpace(page.Description)
	node.report = strings.TrimSpace(page.Result)
	node.model = strings.TrimSpace(page.Row.Model)
	node.tokens = page.Row.Tokens
	if !page.Row.Started.IsZero() && !page.Row.Ended.IsZero() && page.Row.Ended.After(page.Row.Started) {
		node.elapsed = page.Row.Ended.Sub(page.Row.Started)
	}
	return node
}

// planRoomShape is what the transcript is drawn from, as one string: a read
// that says the same thing draws nothing again.
func planRoomShape(page session.PlanTaskPage) string {
	var b strings.Builder
	b.WriteString(page.Row.Status)
	b.WriteString("|" + page.Description + "|" + page.Result + "|")
	b.WriteString(itoa(len(page.Steps)) + "|" + itoa(len(page.Notes)) + "|")
	b.WriteString(itoa(page.Live.Step) + ":" + page.Live.Command + "|")
	for _, kid := range page.Children {
		b.WriteString(kid.ID + "=" + kid.Status + ";")
	}
	return b.String()
}

// planRoomEntries is the page's transcript, built as a record and replayed
// through the one door every transcript becomes blocks through
// ([app.roomRecord]), so a step is drawn exactly as the conversation draws a
// shell call and a note as a correction.
//
// THE BRIEF COMES FIRST, the way a node's instruction opens its page. Then the
// steps, in the order the worker took them, then the step it is taking now.
// The notes follow the steps in the order they were left: the store keeps the
// moment of a note and not of a step, so the two cannot be interleaved without
// guessing, and a guess about order is the one thing a transcript may not make.
func (a *app) planRoomEntries(page session.PlanTaskPage) ([]entry, int) {
	var record session.Record
	add := func(e session.DisplayEntry) { record.Entries = append(record.Entries, e) }
	if brief := strings.TrimSpace(page.Description); brief != "" {
		add(session.DisplayEntry{Role: "user", Text: requestDisplayFor(brief)})
	}
	call := func(step int, command, output string, answered bool) {
		args, _ := json.Marshal(map[string]string{"command": command})
		add(session.DisplayEntry{
			Role: "tool", Tool: "bash", CallID: "step-" + itoa(step),
			Args: string(args), Output: output, Answered: answered,
		})
	}
	for _, step := range page.Steps {
		// A CALL THE ENGINE SAYS DID NOT RUN has a row only when it was an action
		// a door refused, and that row says so in the session's own lane rather
		// than as a call: nothing ran ([session.PlanStep.Refused]).
		if step.NotRun {
			if tried := planDisplayCommand(step.Command, step.Parts); step.Refused && tried != "" {
				add(session.DisplayEntry{Role: "aside", Text: taskPlanRefusedWord + railSep + tried})
			}
			continue
		}
		command := planDisplayCommand(step.Command, step.Parts)
		if command == "" {
			continue
		}
		// THE HEAD OF WHAT CAME BACK IS THE ROW'S OWN OR IT IS NOT DRAWN, the law
		// #1420's page kept ([session.PlanStep.ObservationHeadWithheld]).
		output := step.FullOutput
		if output == "" {
			output = step.Observation
		}
		if step.ObservationHeadWithheld {
			output = ""
		}
		call(step.Step, command, output, true)
	}
	// THE LIVE STEP IS DRAWN ONE STEP EARLY, as a call in flight, and only
	// where the store says there is one ([plandb.LiveStep.Empty]).
	if live := page.Live; !live.Empty() && live.Step > 0 {
		if command := planDisplayCommand(live.Command, page.Row.LiveParts); command != "" {
			call(live.Step, command, "", false)
		}
	}
	for _, note := range page.Notes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		if note.Person {
			add(session.DisplayEntry{Role: "user", Text: body, Steer: &session.SteerMark{At: note.At, Consumed: true}})
			continue
		}
		add(session.DisplayEntry{Role: "assistant", Text: body})
	}
	// A TASK THAT HAS LANDED SAYS WHAT IT CAME TO, last, where a node's report is.
	if result := strings.TrimSpace(page.Result); result != "" && planEnded(page.Row) {
		add(session.DisplayEntry{Role: "assistant", Text: result})
	}
	return a.roomRecord(record, roomTail)
}

// ── the beat ───────────────────────────────────────────────────────────────

// planRoomTickMsg is the page's own beat.
type planRoomTickMsg struct{ gen int }

func planRoomTick(gen int) tea.Cmd {
	return surfaceTick(elsewhereEvery, func(time.Time) tea.Msg { return planRoomTickMsg{gen: gen} })
}

// planRoomPoll reads the page again while its work can still move.
func (a *app) planRoomPoll(gen int) tea.Cmd {
	room := a.room
	if room == nil || room.gen != gen || room.plan == nil || room.done {
		return nil
	}
	return tea.Batch(a.planRoomRead(), a.planRoomWorkRead(false), planRoomTick(gen))
}

// planRoomRead asks the store for the page OFF THE LOOP and folds the answer
// into the page that asked, and into no other.
func (a *app) planRoomRead() tea.Cmd {
	agent, ok := a.planReader()
	plan := a.roomPlan()
	if !ok || plan == nil || plan.reading {
		return nil
	}
	id, gen := plan.id, a.room.gen
	plan.reading = true
	return a.besideLine(func() func(bool) tea.Cmd {
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			if a.room == nil || a.room.gen != gen || a.room.plan == nil {
				return nil
			}
			a.room.plan.reading = false
			if here && found {
				a.planRoomTake(page)
			}
			return nil
		}
	})
}

// ── the box ────────────────────────────────────────────────────────────────

// planRoomSteer is enter on a run's task: the words become a note on the task
// in the person's own voice ([session.Agent.PlanNote]), which the task's worker
// is handed at its next step. The line is drawn on the page at once, as the
// correction it is, and the store's own answer settles it.
func (a *app) planRoomSteer(line string) tea.Cmd {
	room := a.room
	agent, ok := a.planReader()
	if room == nil || room.plan == nil || !ok {
		a.roomNote(roomUnavailableRefusal.line())
		return nil
	}
	// A TASK THAT HAS ENDED TAKES NO NOTE, and the room says so in the words its
	// box and its foot already say, with the words left in the box. Sending them
	// would only bring back the store's refusal under a placeholder that named
	// another door: one fact, two sentences.
	if room.done {
		a.roomNote(a.roomDoneRefusal().line())
		return nil
	}
	words := a.pastesUnfolded(line)
	id, gen := room.plan.id, room.gen
	a.pastes = nil
	a.input.reset()
	a.endRecall()
	a.closeLists()
	room.collapseThought()
	elbow := &steerElbow{words: line, at: a.now(), landing: steerSendingWord}
	room.plan.sending = append(room.plan.sending, elbow)
	a.roomSaid(entry{kind: entrySteer, turn: room.turn, steer: elbow})
	return tea.Batch(a.offLoop(func() func(bool) tea.Cmd {
		err := agent.PlanNote(id, words)
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			if a.room == nil || a.room.gen != gen || a.room.plan == nil {
				return nil
			}
			plan := a.room.plan
			for i, held := range plan.sending {
				if held == elbow {
					plan.sending = append(plan.sending[:i], plan.sending[i+1:]...)
					break
				}
			}
			if err != nil {
				// THE STORE'S OWN SENTENCE, on the row that asked: a note the store
				// refused is words nobody will read, and the row must not spin.
				elbow.landing, elbow.stalled = err.Error(), true
				a.roomTouched()
				return nil
			}
			elbow.landing = ""
			a.railStamp++
			if here && found {
				plan.shape = ""
				a.planRoomTake(page)
				// WHEN IT IS READ, said once under the note: the worker is a loop of
				// its own and reads the note at its next step, which is also the
				// read that redraws the room and takes this line away. A task that
				// has ended takes no next step, so it is not said there.
				if !planEnded(page.Row) {
					a.roomNote(taskPlanPickupWord)
				}
			}
			return nil
		}
	}), fadeTicks())
}

// planRoomStopTarget is `x` on a run's task: the plan's own stop, for work
// that has not ended. The run's own task is the whole run, and the plan's door
// stops it as the run ([session.Agent.PlanCancel]).
func (a *app) planRoomStopTarget() stopTarget {
	plan := a.roomPlan()
	if plan == nil || planEnded(plan.page.Row) {
		return stopTarget{}
	}
	if _, ok := a.planReader(); !ok {
		return stopTarget{}
	}
	return stopTarget{plan: plan.id, noun: stopTaskNoun, detail: stopTaskDetail}
}

// ── the parts under it ──────────────────────────────────────────────────────

// planRoomPartsWord heads the section a task's parts are drawn in.
const planRoomPartsWord = "under it"

// planPageKinWidth is the most a task's room spends on one row of its parts. A
// part's row is the rail's row, whose handle stands at the row's far end; on a
// page the width of the terminal that handle would sit a screen away from the
// title it belongs to, so the rows are drawn at a width a column could have.
const planPageKinWidth = 64

// planRoomRunning reports whether the room on screen is a run's task whose
// work can still move, which is what keeps the paint clock turning for it.
func (a *app) planRoomRunning() bool {
	plan := a.roomPlan()
	return plan != nil && !a.room.done
}

// planRoomWaitsWord heads the section a task's waits are drawn in.
const planRoomWaitsWord = "waits"

// planRoomPartRows is what the task hangs on and what hangs under it, under the
// transcript: the tasks it waits on and the tasks waiting on it, its own first,
// then its parts, each drawn as the rail draws a task ([app.planRailLines], the
// row shape #1420 settled on). A section with nothing in it is absent. Every
// row is a door: a press opens that task's room ([app.press]).
func (a *app) planRoomPartRows(width int) []row {
	plan := a.roomPlan()
	if plan == nil {
		return nil
	}
	var out []row
	if waits := plan.page.WaitRows; len(waits) > 0 {
		out = append(out, row{entry: -1}, row{text: a.pal.dim(fit(planRoomWaitsWord, width)), entry: -1})
		own := map[string]bool{}
		for _, id := range plan.page.Row.Waits {
			own[id] = true
		}
		title := strings.TrimSpace(plan.page.Row.Title)
		for _, wait := range waits {
			sentence := strings.TrimSpace(wait.Title) + railSep + "waits: " + title
			if own[wait.ID] {
				sentence = title + railSep + "waits: " + strings.TrimSpace(wait.Title)
			}
			line := a.pal.ink(fit(sentence, max(width-18, 8))) + "  " + a.pal.dim(planWaitFigure(a.pal, wait))
			out = append(out, row{text: line, entry: -1, plan: wait.ID})
		}
	}
	if len(plan.page.Children) > 0 {
		out = append(out, row{entry: -1}, row{text: a.pal.dim(fit(planRoomPartsWord, width)), entry: -1})
		for _, line := range a.planRailLines(planTwigsOf(plan.page.Children), nil, false, min(width, planPageKinWidth)) {
			out = append(out, row{text: line.text, entry: -1, plan: line.plan})
		}
	}
	return out
}

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

// planRoomAncestors is the run's tasks this one hangs under, the run's own
// task first, each lent a node so its crumb is a door onto its own room. They
// are read off the rows the surface already holds ([app.heldPlanRows]): a
// frame never opens the store.
func (a *app) planRoomAncestors() []*taskNode {
	plan := a.roomPlan()
	if plan == nil {
		return nil
	}
	rows, _ := a.heldPlanRows()
	byID := make(map[string]session.PlanTaskRow, len(rows))
	for _, row := range rows {
		byID[strings.TrimSpace(row.ID)] = row
	}
	seen := map[string]bool{plan.id: true}
	var up []*taskNode
	for parent := strings.TrimSpace(plan.page.Row.Parent); parent != "" && !seen[parent]; {
		row, ok := byID[parent]
		if !ok {
			break
		}
		seen[parent] = true
		up = append(up, planRailNode(row))
		parent = strings.TrimSpace(row.Parent)
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up
}

// ── the work tab ────────────────────────────────────────────────────────────

// planRoomWorkRead reads the run's working copy for the work tab, off the loop,
// while the tab is the one on screen. `now` asks for it even when a read has
// already come back, which is what switching to the tab does.
func (a *app) planRoomWorkRead(now bool) tea.Cmd {
	plan := a.roomPlan()
	if plan == nil || a.room.tab != roomTabWork || plan.working {
		return nil
	}
	if plan.workRead && !now && a.room.done {
		return nil
	}
	door, ok := a.agent.(session.PlanWorkAgent)
	if !ok {
		return nil
	}
	id, gen := plan.id, a.room.gen
	plan.working = true
	return a.besideLine(func() func(bool) tea.Cmd {
		work, found := door.PlanTaskWork(id)
		return func(bool) tea.Cmd {
			if a.room == nil || a.room.gen != gen || a.room.plan == nil {
				return nil
			}
			plan := a.room.plan
			plan.working = false
			if found || work.NoDoor {
				plan.work, plan.workRead = work, true
			} else {
				plan.work, plan.workRead = session.PlanTaskWork{}, true
			}
			a.roomTouched()
			return nil
		}
	})
}

// The work tab's own sentences, each the whole of what the tab can truthfully
// say when there is no difference to draw.
const (
	planWorkNoDoorWord  = "this engine does not read the run's working copy"
	planWorkReadingWord = "reading the run's working copy…"
	planWorkGoneWord    = "the run's working copy is not here any more"
	planWorkNoneWord    = "nothing in the run's working copy has changed yet"
	planWorkCutWord     = "the rest of the difference is in the working copy"
	planWorkNewWord     = "new "
	planWorkHeadWord    = "the run's working copy · every task of this run works in it"
	nodeWorkHeadWord    = "files this task changed"
	nodeWorkYetWord     = "the files this task changes are listed here when it lands"
	nodeWorkNoneWord    = "this task changed no files"
)

// roomWorkRows is the work tab's body: what the task has changed.
//
// ON A RUN'S TASK it is the difference in the run's working copy, which every
// part of the run shares. ON A NODE it is the files the node's record says it
// wrote, which is what the engine keeps about a node's work.
func (a *app) roomWorkRows(width int) []row {
	dim := func(text string) row { return row{text: a.pal.dim(fit(text, width)), entry: -1} }
	if plan := a.roomPlan(); plan != nil {
		if _, ok := a.agent.(session.PlanWorkAgent); !ok {
			return []row{dim(planWorkNoDoorWord)}
		}
		if !plan.workRead {
			return []row{dim(planWorkReadingWord)}
		}
		if plan.work.NoDoor {
			return []row{dim(planWorkNoDoorWord)}
		}
		work := plan.work
		if !work.Read {
			return []row{dim(planWorkGoneWord)}
		}
		if strings.TrimSpace(work.Patch) == "" && len(work.Added) == 0 {
			return []row{dim(planWorkNoneWord)}
		}
		out := []row{dim(planWorkHeadWord), {entry: -1}}
		for _, path := range work.Added {
			out = append(out, row{text: a.pal.add(fit(a.icon(tokens.GDiffAdd)+planWorkNewWord+path, width)), entry: -1})
		}
		if len(work.Added) > 0 && work.Patch != "" {
			out = append(out, row{entry: -1})
		}
		for _, line := range a.planWorkLines(work.Patch, width) {
			out = append(out, row{text: line, entry: -1})
		}
		if work.Cut {
			out = append(out, row{entry: -1}, dim(planWorkCutWord))
		}
		return out
	}
	node := a.roomNode()
	if node == nil || len(node.changed) == 0 {
		if node != nil && roomRowDone(node) {
			return []row{dim(nodeWorkNoneWord)}
		}
		return []row{dim(nodeWorkYetWord)}
	}
	out := []row{dim(nodeWorkHeadWord), {entry: -1}}
	for _, path := range node.changed {
		out = append(out, row{text: a.pal.ink(fit(path, width)), entry: -1})
	}
	return out
}

// planWorkLines paints a unified patch the way a diff in a question is painted
// ([app.questionDiffLines]): the mark from the vocabulary and the hue from the
// ramp, with each file's header in ink so the eye finds where one file ends.
func (a *app) planWorkLines(patch string, width int) []string {
	lines := strings.Split(strings.TrimRight(patch, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = expandTabs(line)
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, a.pal.ink(fit(planPatchHeadPath(line), width)))
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "index "), strings.HasPrefix(line, "new file"),
			strings.HasPrefix(line, "deleted file"), strings.HasPrefix(line, "similarity"),
			strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "old mode"),
			strings.HasPrefix(line, "new mode"):
			continue
		case strings.HasPrefix(line, "@@"):
			out = append(out, a.pal.dim(fit(line, width)))
		case strings.HasPrefix(line, "+"):
			out = append(out, a.pal.add(fit(a.icon(tokens.GDiffAdd)+strings.TrimPrefix(line, "+"), width)))
		case strings.HasPrefix(line, "-"):
			out = append(out, a.pal.del(fit(a.icon(tokens.GDiffDel)+strings.TrimPrefix(line, "-"), width)))
		default:
			out = append(out, a.pal.dim(fit(line, width)))
		}
	}
	return out
}

// planPatchHeadPath is the file a patch section is about, off its header.
func planPatchHeadPath(line string) string {
	head := strings.TrimPrefix(line, "diff --git ")
	if at := strings.LastIndex(head, " b/"); at >= 0 {
		return head[at+3:]
	}
	return head
}
