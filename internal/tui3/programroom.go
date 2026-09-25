package tui3

// programroom.go opens a PROGRAM'S TASK the way every other task opens: as a
// room inside the conversation's own tab.
//
// A task handed to a program codeaf carries (senior-dev) has no worker
// transcript. What the program did is its conversation with codeaf, on the
// task's stored page ([session.PlanTaskPage.Program]), and taskconversation.go
// draws it. That page used to be drawn by the tasks place's machinery OVER the
// conversation — a full frame with no tab strip, reached from the side list,
// the card, a task link, the task strip, the home panel and the sessions place
// — and the strip's hit map under it kept answering presses nobody could see;
// the strip also offered the run a tab of its own that drew itself selected
// beside the conversation's, which a press on the conversation's tab never
// left, and Home opened underneath it and vanished from the strip. The owner
// met all of it on the first senior-dev run of 2026-09-24.
//
// SO IT IS A [taskRoom] NOW, the way an adaptive run's graph is (roomorch.go):
// the conversation's own strip stays over it with the conversation's tab the
// one selected tab, the trail and the rail stay beside it, and `esc`, a press
// on the conversation's tab and a press on Home leave it exactly as they leave
// any room. What fills the body is the program's conversation, the facts row
// is the line the stored page pins under its title, and `x` stops the run
// through the store's own door, as the stored page's `x` does.
//
// THE BOX SENDS NOTHING. A program reads no message — nothing a person types
// reaches senior-dev once it is running — so the placeholder says so, and
// enter over a sentence says it again on the page and keeps the sentence in the
// box, where the refusal's door (the conversation) can still take it.

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// programRoom is what a program's room holds instead of a lane: the stored page
// as last read, when it was read and whether a read is out, the room's own fold
// of the brief, the width its body was last laid out at (which `ctrl+o`
// measures the brief against), and the lines the page itself has said.
type programRoom struct {
	page    session.PlanTaskPage
	readAt  time.Time
	reading bool
	// briefFull says the head's dropdown is open: the whole brief the program
	// was handed, drawn between the head's rules ([app.programHeadBriefRows]).
	// It opens shut, and `ctrl+o` or a press on the dropdown turns it.
	briefFull bool
	// briefSpan is where the dropdown was drawn on the title row, for the
	// press that turns it ([app.programBriefPress]).
	briefSpan hudSpan
	// open is the actions whose whole step is shown under their one line, by
	// the moment each was received ([app.toggleProgramAction]).
	open map[int64]bool
	// calls says the room shows the program's raw calls instead of its actions
	// ([programCallsKey]); a room opens on the actions.
	calls bool
	inner int
	said  []string
}

// programRoomRefusal is what a program's room says about its box: the fact, and
// the one place the words can still go ([refusal]'s law). The fact names the
// program when the page knows its name ([app.programRoomRefusal]).
var programRoomRefusal = refusal{
	what:      "this task's program reads no messages",
	shortWhat: "reads no messages",
	door:      refusalMainDoor,
}

// programRoomNoMessages is the fact's tail after the program's own name.
const programRoomNoMessages = " reads no messages"

// programOf is the open room's program, and nil on every other page.
func (a *app) programOf() *programRoom {
	if a.room == nil {
		return nil
	}
	return a.room.program
}

// programTask reports whether the surface holds this conversation's task as a
// program's run: its held row names the program. It reads only what is held,
// never the store, because it is asked on the loop at a key or a click.
func (a *app) programTask(id uint64) bool {
	row, ok := a.heldProgramRow(id)
	return ok && strings.TrimSpace(row.Program) != ""
}

// heldProgramRow is the row the surface holds for one of this conversation's
// tasks, if it holds one.
func (a *app) heldProgramRow(id uint64) (session.PlanTaskRow, bool) {
	rows, ok := a.heldPlanRows()
	if !ok {
		return session.PlanTaskRow{}, false
	}
	want := strconv.FormatUint(id, 10)
	for _, row := range rows {
		if planTaskIDWord(row.ID) == want {
			return row, true
		}
	}
	return session.PlanTaskRow{}, false
}

// planTaskIDWord is a store id in the spelling a node's number has: the store
// answers `7` or `t-7` for the run rooted at task 7.
func planTaskIDWord(id string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "t-"))
}

// programRoomFor is the task a stored page opens a program's room on: the
// page's own number, when the page is a program's. A program's run is always
// its store's root, rooted at the task's own number ([app.programRowNode]).
func (a *app) programRoomFor(page session.PlanTaskPage) (uint64, bool) {
	if pageProgram(page) == "" {
		return 0, false
	}
	id, err := strconv.ParseUint(planTaskIDWord(page.Row.ID), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// heldProgramPage is the page a door can open on before the store answers: the
// row the surface holds for the task, and nothing else yet.
func (a *app) heldProgramPage(id uint64) session.PlanTaskPage {
	row, _ := a.heldProgramRow(id)
	return session.PlanTaskPage{Row: row}
}

// programRowNode is the node of this conversation a program's store row is
// about, or nil for a row that is not a program's or not this conversation's.
// A program's run is always its store's root, and the root is rooted at the
// task's own number (session's startKnownTaskRun), so the id is the node's.
func (a *app) programRowNode(row session.PlanTaskRow) *taskNode {
	if strings.TrimSpace(row.Program) == "" {
		return nil
	}
	n, err := strconv.ParseUint(planTaskIDWord(row.ID), 10, 64)
	if err != nil || n == 0 {
		return nil
	}
	return a.tasks[n]
}

// openProgramRoom opens the program's room on the page the caller holds — the
// store's page when a gesture already read it, the held row alone otherwise —
// and asks the store for the whole page off the loop.
//
// IT NEEDS NO ROOM DOORS. There is no lane to subscribe to and no journal to
// read, so the room opens on a hosted conversation exactly as on a local one:
// the one door it reads through, [session.Agent.PlanTaskPage], is on the wire.
func (a *app) openProgramRoom(id uint64, title string, page session.PlanTaskPage) {
	if a.startingChat() {
		a.parkChatStart()
	}
	room := a.newRoom(id, firstNonEmpty(title, page.Row.Title, taskIDWord(id)))
	room.program = &programRoom{page: page}
	a.room = room
	room.done = a.programRoomDone()
	// AND THE BOX POINTS AT THIS TASK, as every room's does (recipient.go): what
	// was being written for the conversation is stashed under its own reader and
	// comes back with the conversation. Nothing typed here is sent anywhere.
	a.retargetComposer(taskRecipient(id))
	// The rail's clock stops being reported while the person is looking into
	// the work, as it does for every room (task.go's [app.taskNow]).
	a.freezeNode(id)
	a.sel = -1
	a.dropHover()
	a.touch()
	a.roomPump = tea.Batch(a.programRoomRead(), a.wake())
}

// programRoomDone is whether the open program room's work is over: by the
// conversation's own row when it holds one, and by the stored page's state
// only when it does not. A room whose node this window never saw is not taken
// for finished on that absence alone ([roomRowDone] answers true for no node
// at all).
//
// THE ROW OUTRANKS THE STORE WHENEVER THE WINDOW HOLDS ONE. The engine ends
// the store's root at the program's exit and writes the landing — where the
// work went, how to bring it in — only after it, and the row settles last. A
// room that took the store's ending for the end stopped reading in that gap,
// and its landing never reached the page.
func (a *app) programRoomDone() bool {
	p := a.programOf()
	if p == nil {
		return false
	}
	if node := a.roomNode(); node != nil {
		return roomRowDone(node)
	}
	return planEnded(p.page.Row)
}

// programRoomRead re-reads the open program room's page off the loop. The
// answer lands only on the room that asked.
//
// A PAGE READ THROUGH ANOTHER CONVERSATION READS THE OWNER'S STORE, through
// its own view ([app.guestPageRead]). This window's store holds this
// conversation's task of the same number, and reading it here would draw that
// task's actions under the owner's name.
func (a *app) programRoomRead() tea.Cmd {
	if a.roomIsGuest() {
		if a.room.program == nil {
			return nil
		}
		return a.guestPageRead()
	}
	agent, ok := a.planReader()
	room := a.room
	if !ok || room == nil || room.program == nil || room.program.reading || room.id == 0 {
		return nil
	}
	id, gen, p := strconv.FormatUint(room.id, 10), room.gen, room.program
	p.reading = true
	p.readAt = a.now()
	return a.offLoop(func() func(bool) tea.Cmd {
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			p.reading = false
			if !here || !found || a.room != room || room.gen != gen {
				return nil
			}
			p.page = page
			if room.title == "" || room.title == taskIDWord(room.id) {
				room.title = firstNonEmpty(page.Row.Title, room.title)
			}
			// A READ NEVER ENDS A ROOM WHOSE ROW THIS WINDOW HOLDS. The node's
			// landing is what ends it, in [app.programRoomFollow], which reads the
			// page once more from that moment; a read that was already out when
			// the row landed answers with the page from before the landing, and
			// had it ended the room here the last read would never be made. A room
			// with no node has only the store to go by, and this read is it.
			if a.roomNode() == nil {
				room.done = a.programRoomDone()
			}
			room.dirty = true
			a.touch()
			return nil
		}
	})
}

// programRoomFollows reports whether the open program room is on work that can
// still move, which is when the paint clock keeps turning for it: the room's
// age ticks and its page is read on a beat.
func (a *app) programRoomFollows() bool {
	p := a.programOf()
	if p == nil || a.room.done {
		return false
	}
	switch planStateWord(p.page.Row) {
	case "queued", "running":
		return true
	}
	// A page whose read has not come back yet names no state, and a node that is
	// still running is work that can still move.
	node := a.roomNode()
	return node != nil && !roomRowDone(node)
}

// programRoomFollow is the paint clock's read, on the stored page's own beat
// ([app.taskPlanFollow], [elsewhereEvery]).
//
// THE LANDING IS READ ONCE MORE. The conversation's row settles on a notice,
// and the page it closes over is the page as it was a beat ago — without the
// notes the run left or the state its store ended on. So the moment the room
// learns the work is over it reads the page one last time, and after that
// never again.
func (a *app) programRoomFollow() tea.Cmd {
	p := a.programOf()
	if p == nil || p.reading {
		return nil
	}
	if done := a.programRoomDone(); done != a.room.done {
		a.room.done = done
		a.room.dirty = true
		if done {
			return a.programRoomRead()
		}
	}
	if !a.programRoomFollows() || a.now().Sub(p.readAt) < elsewhereEvery {
		return nil
	}
	return a.programRoomRead()
}

// programRoomRows is the room's body: the program's conversation, the notes
// the run left, what the page itself has said, and the foot a landed task's
// room draws — laid out inside the reading gutter the conversation keeps.
func (a *app) programRoomRows(width int) []row {
	p := a.programOf()
	if p == nil {
		return nil
	}
	inner := gutterInner(width)
	p.inner = inner
	pal := a.pal
	var out []row
	// THE BRIEF IS THE HEAD'S (its dropdown), so the actions open the body; and
	// every action with more to show is a press that opens its whole step.
	lines, keys := a.programBodyRows(p.page, inner, p.briefFull, p.calls, !a.programHeadsRoom(), p.open)
	for i, line := range lines {
		r := row{text: line, entry: -1}
		if keys[i] != 0 {
			r.hit, r.turn = hitAction, int(keys[i])
		}
		out = append(out, r)
	}
	if len(p.said) > 0 {
		out = append(out, row{entry: -1})
		for _, said := range p.said {
			for _, line := range railWrap(said, inner) {
				out = append(out, row{text: pal.dim(line), entry: -1})
			}
		}
	}
	// A PAGE READ THROUGH ANOTHER CONVERSATION says what is true of the reading
	// under what it read, exactly as its journal page does (room.go's
	// [app.roomGuestTail]): that the conversation under it was replaced, that
	// it cannot ask the owner what the work is doing now, or that the owner is
	// waiting on somebody.
	var tail []row
	if guest := a.roomGuest(); guest != nil && guest.lost {
		tail = append(tail, row{text: pal.dim(fit(taskGuestGoneWord, inner)), entry: -1})
	}
	if tail = append(tail, a.roomGuestTail(inner)...); len(tail) > 0 {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, tail...)
	}
	if a.room.done && !a.roomLandingAsking() {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, row{text: pal.dim(a.roomDoneRefusal().fit(inner)), entry: -1})
	}
	gutterPass(out, width)
	a.hoverPass(out, width)
	return out
}

// programSay puts one line on the program's page, below its conversation. It
// is the room's own note ([app.roomNote]) for a page whose body is not a
// transcript, and like it a line identical to the one before it is not said
// twice.
func (p *programRoom) programSay(text string) {
	if n := len(p.said); n > 0 && p.said[n-1] == text {
		return
	}
	p.said = append(p.said, text)
}

// programRoomRefusal is [programRoomRefusal] with the program named, when the
// page knows what to call it.
func (a *app) programRoomRefusal() refusal {
	out := programRoomRefusal
	if p := a.programOf(); p != nil {
		if name := convProgramName(p.page); name != "" && name != convProgramFallback {
			out.what = name + programRoomNoMessages
		}
	}
	return out
}

// programFactsWord is the room's facts row on a program's page: the line the
// stored page pins under its title — the stage, the spend of the ceiling, the
// calls, the age — with the age read off the node the room stands on, the
// clock the rail and the landed card read. It answers how many of its leading
// cells are the lead word, which the row paints in the node's own ink.
func (a *app) programFactsWord(width int) (string, int) {
	p := a.programOf()
	if p == nil {
		return "", 0
	}
	line := strings.TrimSpace(a.programPinned(p.page, width, a.programRoomClock()))
	lead, _, _ := strings.Cut(line, rowSep)
	if a.roomNode() == nil {
		return line, 0
	}
	return line, ansi.StringWidth(lead)
}

// programRoomClock is the age the program room's facts row draws: the span
// the program ran once its process has ended, the node's clock when this
// conversation holds one for it, and the stored page's own stamps otherwise.
func (a *app) programRoomClock() string {
	p := a.programOf()
	if p != nil {
		if word, ok := programExitClock(p.page.Row, a.roomNode()); ok {
			return word
		}
	}
	if word, ok := a.nodeClock(a.roomNode()); ok {
		return word
	}
	if p != nil {
		return a.taskPlanAge(p.page.Row)
	}
	return ""
}

// programExitClock is the span a program's run ran for when its process has
// ended and the conversation's row has not yet settled: the page's own pair,
// which session puts on the hand-off and the program's recorded exit, rounded
// as every finished span is ([taskNode.ranFor]).
//
// THE CLOCK STOPS AT THE PROGRAM'S EXIT, NOT AT THE LANDING. After the process
// ends the engine waits for the receipts of calls it still owes — up to
// seventy seconds on a cut call — and lands the work, and only then settles
// the row; a room and a page that went on reading the row's running clock
// counted through all of that and jumped back when it settled. A row that has
// settled has its own span, and a run still working has no exit to stop at.
func programExitClock(row session.PlanTaskRow, node *taskNode) (string, bool) {
	if node == nil || node.state != session.TaskRunning || strings.TrimSpace(row.Program) == "" {
		return "", false
	}
	if row.Started.IsZero() || row.Ended.IsZero() || row.Ended.Before(row.Started) {
		return "", false
	}
	return countUpWord(row.Ended.Sub(row.Started).Round(time.Second)), true
}

// programStopTarget is what `x`, `/stop` and the room's Stop end on a
// program's room: the run's own task, through the store's own door
// ([session.Agent.PlanCancel]) — the target the stored page's `x` has always
// raised ([app.taskPlanStop]), which ends a live run and a run whose process
// is already gone alike. Only work that can still stop is offered.
func (a *app) programStopTarget() stopTarget {
	p := a.programOf()
	if p == nil || a.room.done {
		return stopTarget{}
	}
	if _, ok := a.planReader(); !ok {
		return stopTarget{}
	}
	row := p.page.Row
	if strings.TrimSpace(row.ID) == "" {
		// The page has not been read and the surface holds no row for it: the
		// store's id is the task's own number, as it is for every program's run.
		row.ID = strconv.FormatUint(a.room.id, 10)
	}
	if planEnded(row) {
		return stopTarget{}
	}
	return stopTarget{plan: row.ID, noun: stopTaskNoun, detail: stopTaskDetail}
}

// programRoomKey is what a program's room takes before the room's own keys:
// `ctrl+o` folds and unfolds the brief when it is long enough to fold,
// [programCallsKey] turns the page between the program's actions and its raw
// calls, and the thinking chord is taken and does nothing, because a program's
// run has no thinking level this surface can move. Everything else is the
// room's.
func (a *app) programRoomKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := a.programOf()
	if p == nil {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+o":
		// THE KEY TURNS THE HEAD'S DROPDOWN, whatever the brief's length: the
		// brief is drawn whole up there or not at all ([app.programHeadBriefRows]).
		// On the raw calls it still unfolds the brief those draw in their body.
		p.briefFull = !p.briefFull
		a.room.dirty = true
		a.touch()
		return nil, true
	case programCallsKey:
		p.calls = !p.calls
		a.room.dirty = true
		a.touch()
		return nil, true
	case effortKey:
		return nil, true
	}
	return nil, false
}

// ── THE HEAD: ONE TITLE, AND THE BRIEF BEHIND A DROPDOWN ─────────────────────
//
// A program's room used to open under two titles: the trail's crumb — the
// conversation's name, which a conversation named after its work spells the
// same as the task — and the task's own bold title under it, and a third in the
// box's `Reading:` label. The owner asked on 2026-09-25 for one: the head is
// the title row alone (the task's name, its badge, the dropdown, the pinned
// facts), and the brief the program was handed is behind the dropdown, drawn
// whole in the dim ink between the head's rules. The way back is `esc`, named
// on the key line, and the side list's `‹ Back to main`.

// programBriefChevron is the dropdown on the title row: shut, or open.
func programBriefChevron(open bool) string {
	if open {
		return glyphOpen + " brief"
	}
	return glyphShut + " brief"
}

// programHeadsRoom says the open room is a program's in the frame that draws
// the head as its own rows ([app.roomOrganized]): the one layout the dropdown
// lives in. A frame too short for it keeps the compact trail, which names the
// task already.
func (a *app) programHeadsRoom() bool {
	return a.programOf() != nil && a.roomOrganized()
}

// programHeadBriefRows is the brief, whole, between the head's rules while the
// dropdown is open, and nothing while it is shut. It is pinned with the head,
// so a brief longer than half the frame gives up its tail to a count rather
// than the body its rows.
func (a *app) programHeadBriefRows(width int) []string {
	p := a.programOf()
	if p == nil || !p.briefFull || !a.roomOrganized() {
		return nil
	}
	text := max(width-headLabelAt-2, 1)
	lines := planBriefRows(p.page.Description, text)
	if len(lines) == 0 {
		return nil
	}
	_, height := a.size()
	if most := max(height/2, 3); len(lines) > most {
		cut := len(lines) - (most - 1)
		lines = append(append([]string(nil), lines[:most-1]...), bandFoldWord(cut, briefFoldWhat, true))
	}
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = strings.Repeat(" ", headLabelAt) + a.pal.dim(fit(line, text))
	}
	return rows
}

// programBriefPress turns the dropdown when the press landed on it.
func (a *app) programBriefPress(x, y int) bool {
	p := a.programOf()
	if p == nil || !a.programHeadsRoom() || a.headHeight() == 0 || y != a.roomHeadRow() || !p.briefSpan.holds(x) {
		return false
	}
	p.briefFull = !p.briefFull
	a.room.dirty = true
	a.touch()
	return true
}

// toggleProgramAction opens or shuts one action's whole step under its line.
func (a *app) toggleProgramAction(key int64) {
	p := a.programOf()
	if p == nil {
		return
	}
	if p.open == nil {
		p.open = map[int64]bool{}
	}
	p.open[key] = !p.open[key]
	a.room.dirty = true
	a.touch()
}
