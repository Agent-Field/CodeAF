package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// The task column keeps the existing family tree above its independently
// scrollable facts. Actions reserve their rows first, so neither list can push
// Stop or the model picker below the frame. Short frames keep the established
// compact rail and the header's Stop door instead.
const (
	roomPanelFloor    = 18
	roomDetailsMax    = 8
	roomPanelTree     = 1
	roomPanelDetails  = 2
	roomPanelControls = 3
)

func (a *app) roomPanelShowing(height int) bool {
	return a.roomOrganized() && !a.railFull() && height >= roomPanelFloor
}

// Each section carries its own row ownership through layout, paint and input.
// Even blank padding consumes that section's wheel, rather than scrolling the
// transcript behind it.
func panelOwned(lines []railLine, section int) []railLine {
	for i := range lines {
		lines[i].roomSection = section
	}
	return lines
}

func (a *app) roomPanelView(height int) ([]railLine, int) {
	width := a.railRoom()
	entries := a.railEntries()
	focus := a.railFocusIndex(entries)
	lines := a.railLines(entries, width)
	controls := a.roomControlRows(width)
	foot, marks := a.railFootRows(width, height)
	// Column navigation retains its existing doors and their exact hit targets.
	footer := make([]railLine, len(foot))
	for i, s := range foot {
		footer[i] = railLine{text: s, entry: -1, hint: i == marks.hint,
			more: i == marks.more, keeping: i == marks.keeping}
	}
	// The shared sidebar header owns the hide control in this view too.
	available := height - len(controls) - len(footer)
	detailHeight := min(roomDetailsMax, max(available/3, 3))
	if len(a.roomDetailContent(width)) == 0 {
		detailHeight = 0
	}
	treeHeight := max(available-detailHeight, 2)
	detailHeight = max(available-treeHeight, 0)
	head := a.marginHead(width, len(entries) > 0)
	treeWindow := max(treeHeight-len(head), 1)
	cursor := a.railTop
	if focus >= 0 {
		for i, line := range lines {
			if line.entry == focus && line.head {
				cursor = i
				break
			}
		}
	}
	a.railTop = listTop(cursor, a.railTop, len(lines), treeWindow)
	out := panelOwned(head, roomPanelTree)
	for i := 0; i < treeWindow; i++ {
		line := railLine{entry: -1}
		if at := a.railTop + i; at < len(lines) {
			line = lines[at]
		}
		line.roomSection = roomPanelTree
		out = append(out, line)
	}
	if len(lines) > treeWindow && len(out) > 0 {
		out[0].text = a.pal.dim(fit("tasks · "+itoa(a.railTop+1)+"–"+itoa(min(a.railTop+treeWindow, len(lines)))+" / "+itoa(len(lines)), width))
	}
	out = append(out, a.roomDetailRows(width, detailHeight)...)
	out = append(out, panelOwned(controls, roomPanelControls)...)
	out = append(out, panelOwned(footer, roomPanelControls)...)
	return out[:min(len(out), height)], focus
}

// Context below the tree retains only standing instructions and background jobs.
// The assignment stays with the task transcript; a second narrow prose column
// would repeat that reading while hiding the controls the person came to use.
func (a *app) roomDetailContent(width int) []railLine {
	if len(a.marginStanding()) == 0 && len(a.jobs) == 0 {
		return nil
	}
	return a.marginRows(width, marginStandCost+marginStandMax+marginJobsCost+len(a.jobs)*3+8)
}

func (a *app) roomDetailRows(width, height int) []railLine {
	if height <= 0 {
		return nil
	}
	content := a.roomDetailContent(width)
	window := max(height-1, 0)
	a.room.detailsTop = min(max(a.room.detailsTop, 0), max(len(content)-window, 0))
	if a.railHold && (a.railWhere.jobs || a.railWhere.job != 0) {
		for i, line := range content {
			if (a.railWhere.jobs && line.jobs) || (a.railWhere.job != 0 && line.job == a.railWhere.job) {
				a.room.detailsTop = listTop(i, a.room.detailsTop, len(content), window)
				break
			}
		}
	}
	title := "Conversation"
	if len(content) > window {
		title += " · " + itoa(a.room.detailsTop+1) + "–" + itoa(min(a.room.detailsTop+window, len(content))) + "/" + itoa(len(content))
	}
	out := []railLine{{text: a.pal.dim(fit(title, width)), entry: -1, roomSection: roomPanelDetails}}
	for i := 0; i < window; i++ {
		line := railLine{entry: -1}
		if at := a.room.detailsTop + i; at < len(content) {
			line = content[at]
		}
		line.roomSection = roomPanelDetails
		out = append(out, line)
	}
	return out
}

func (a *app) roomControlRows(width int) []railLine {
	row := func(word, action string) railLine {
		text := fit(word, width)
		if label, value, ok := strings.Cut(text, " · "); ok {
			text = a.pal.dim(label+" · ") + a.pal.ink(value)
		} else {
			text = a.pal.ink(text)
		}
		return railLine{text: text, entry: -1, roomAction: action}
	}
	heading := "Task setup"
	if taskSetupLater(a.roomNode()) {
		heading = "Next run setup"
	}
	out := []railLine{{entry: -1}, {text: a.pal.dim(fit(heading, width)), entry: -1}}
	if n := a.roomNode(); n != nil && n.model != "" {
		action, word := "", "Model · "+modelBase(firstNonEmpty(n.nextModel, n.model))
		if a.roomModelMovable() {
			action = "model"
			word = fit(word, max(width-2, 0)) + " ▾"
		}
		out = append(out, row(word, action))
	}
	if host, ok := a.agent.(interface{ TaskSetupSupported() bool }); ok && !host.TaskSetupSupported() && !a.roomIsGuest() {
		out = append(out, railLine{text: a.pal.dim(fit("Engine update needed", width)), entry: -1})
	}
	// A PROGRAM'S RUN HAS NO THINKING LEVEL THIS SURFACE CAN MOVE, so its room
	// draws no row for one (programroom.go).
	if node := a.roomNode(); node != nil && !a.roomIsGuest() && a.programOf() == nil {
		movable := a.taskRungMovable(node)
		rung := a.taskRung(node.id).String()
		if movable || rung != "" {
			if rung == "" {
				rung = "auto"
			}
			action := ""
			if movable {
				action = "effort"
				rung += " ↻"
			}
			out = append(out, row("Thinking · "+rung, action))
		}
	}
	if taskSetupLater(a.roomNode()) && a.roomModelMovable() {
		out = append(out, railLine{text: a.pal.dim(fit("Applies when you continue", width)), entry: -1})
	}
	if run := a.orchOf(); run != nil {
		if model := run.plannerWord(); model != "" {
			out = append(out, row(model, ""))
		}
		if fuel := run.fuelWord(); fuel != "" {
			out = append(out, row("Budget · "+fuel, ""))
		}
	}
	if target := a.stopHere(); !target.empty() {
		// A run's own task stops through the store's door and not the stop door
		// ([stopTarget.plan]), and its target is only offered when that door is
		// there (programroom.go's [app.programStopTarget]).
		if _, ok := a.stopDoors(); ok || target.plan != "" {
			out = append(out, railLine{entry: -1}, row("Stop "+target.noun+"…", "stop"))
		}
	}
	if len(out) == 2 {
		return nil
	}
	return out
}

func (a *app) roomPanelActionAt(x, y int) string {
	if !a.roomOpen() || !a.railAt(x, y) || a.railSeamAt(x, y) {
		return ""
	}
	line, ok := a.railLineAt(y)
	if !ok {
		return ""
	}
	return line.roomAction
}

func (a *app) roomPanelTake(action string) {
	switch action {
	case railMainAction:
		a.closeRoom()
		a.railHold = false
	case "model":
		if a.roomModelMovable() {
			a.openTaskPicker(a.room.id)
		}
	case "effort":
		if node := a.roomNode(); node != nil && !a.roomIsGuest() && a.taskRungMovable(node) {
			a.cycleNodeEffort(node)
		}
	case "stop":
		if target := a.stopHere(); !target.empty() {
			a.raiseStop(target)
		}
	}
}

func (a *app) roomDetailsScroll(delta int) {
	if !a.roomPanelShowing(a.viewHeight()) {
		return
	}
	a.room.detailsTop = max(a.room.detailsTop+delta, 0)
	a.touch()
}

func (a *app) roomPanelWheel(msg tea.MouseWheelMsg) bool {
	if !a.roomPanelShowing(a.viewHeight()) || !a.railAt(msg.Mouse().X, msg.Mouse().Y) {
		return false
	}
	line, ok := a.railLineAt(msg.Mouse().Y)
	if !ok {
		return false
	}
	delta := 0
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		delta = -3
	case tea.MouseWheelDown:
		delta = 3
	}
	switch line.roomSection {
	case roomPanelTree:
		a.railScroll(delta)
	case roomPanelDetails:
		a.roomDetailsScroll(delta)
	}
	return true
}

// Commands are scoped before interpreting their arguments. A read-only or
// completed task must never fall through and change the conversation instead.
func (a *app) roomModelCommand(rest string) {
	if !a.roomModelMovable() {
		a.roomNote("this task's model cannot be changed here")
		return
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		a.openTaskPicker(a.room.id)
		return
	}
	intent, value := modelArg(rest)
	switch intent {
	case modelQuery:
		a.openTaskPicker(a.room.id)
		a.pick.filter.setText(value)
		a.pick.rank()
	case modelPinLane, modelAutoLane:
		a.roomNote("provider selection is available from the conversation's /model")
	default:
		if warning := a.nonChatWarning(rest); warning != "" {
			a.roomNote(warning)
			return
		}
		a.retargetTask(a.room.id, rest)
	}
	a.touch()
}

// A task title is a terminal heading: one bold row at the existing reading
// gutter, never an image or a second font size.
func (a *app) roomTitleRow(width int) string {
	left := a.roomHereWord()
	right, painted := "", ""
	if node := a.roomNode(); node != nil && a.programOf() == nil {
		f := a.roomFactsOf(node)
		right = rowAll([]rowField{f.state, f.live, f.clock, f.spend, f.model, f.tokens})
		state := rowAll([]rowField{f.state})
		painted = a.taskStateInk(node)(state) + a.pal.muted(strings.TrimPrefix(right, state))
	} else if a.programOf() != nil {
		// A PROGRAM'S ROOM PINS ITS STORED PAGE'S LINE BESIDE THE TITLE: the stage,
		// the spend of the ceiling, the calls and the age (programroom.go). It is
		// given at most half the row, so the title keeps its half.
		line, lead := a.programFactsWord(max((width-headLabelAt-2)/2, 1))
		right = line
		painted = a.pal.muted(line)
		if node != nil && lead > 0 {
			painted = a.taskStateInk(node)(ansi.Cut(line, 0, lead)) + a.pal.muted(ansi.Cut(line, lead, ansi.StringWidth(line)))
		}
	}
	room := max(width-headLabelAt-2-ansi.StringWidth(right)-3, 1)
	// A PROGRAM'S ROOM HANGS THE BRIEF'S DROPDOWN AFTER THE BADGE, paid for
	// before the title is fitted (programroom.go's [app.programHeadBriefRows]).
	chevron := ""
	if p := a.programOf(); p != nil && a.programHeadsRoom() {
		chevron = " " + programBriefChevron(p.briefFull)
		room -= ansi.StringWidth(chevron)
	}
	// A PROGRAM'S TASK WEARS ITS PROGRAM'S BADGE BESIDE ITS TITLE, the one its row
	// wears on the side list (programbadge.go), paid for out of the title's half
	// of the row and never the facts'. An ordinary task spends nothing on it.
	wears := programSpelling(programBadge(a.roomProgram()), left, max(room, 1), railTitleFloor)
	left = fit(left, max(room-programCells(wears), 1))
	used := headLabelAt + ansi.StringWidth(left) + programCells(wears)
	shownChevron := ""
	if chevron != "" {
		p := a.programOf()
		p.briefSpan = hudSpan{from: used + 1, to: used + ansi.StringWidth(chevron)}
		shownChevron = a.pal.dim(chevron)
		used += ansi.StringWidth(chevron)
	}
	return strings.Repeat(" ", headLabelAt) + a.pal.bold(a.pal.ink(left)) + a.pal.programAfter(wears) + shownChevron + strings.Repeat(" ", max(width-used-2-ansi.StringWidth(right), 1)) + painted + "  "
}

// roomProgram is the program the open room's task was handed to: the name its
// stored page gives it on a program's room, and the node's own otherwise — ""
// for every ordinary task.
func (a *app) roomProgram() string {
	if p := a.programOf(); p != nil {
		if name := pageProgram(p.page); name != "" {
			return name
		}
	}
	return a.nodeProgram(a.roomNode())
}

// The expanded layout is a height decision independent of the body's measured
// height, because the input and header participate in measuring that body.
func (a *app) roomOrganized() bool {
	_, height := a.size()
	return a.room != nil && a.railShowing() && height >= roomPanelFloor+roomHeadRowCount+13
}

func (a *app) roomRecipientWord() string {
	if a.pick.open {
		if a.pick.task != 0 {
			if taskSetupLater(a.roomNode()) {
				return "Next model for: " + a.roomHereWord()
			}
			return "Model for: " + a.roomHereWord()
		}
		return "Conversation model"
	}
	// A PROGRAM'S ROOM IS READ AND NEVER WRITTEN TO (programroom.go), so the box's
	// label says what a borrowed page's does rather than naming a recipient.
	if a.roomIsGuest() || a.programOf() != nil {
		return "Reading: " + a.roomHereWord()
	}
	return "To: " + a.roomHereWord()
}

// The heading already names the current task. The navigation row keeps its
// ancestors and their original identities, including folded ancestor targets.
func (a *app) roomAncestorParts(width int) (string, []crumbHit) {
	crumbs := a.roomCrumbs()
	if len(crumbs) > 1 {
		crumbs = crumbs[:len(crumbs)-1]
	}
	room := max(width-headLabelAt-len(" "+roomBackWord+" ")-3, 1)
	word, hits, _ := fitCrumbChain(crumbs, room)
	return word, crumbsAt(hits, headLabelAt)
}

// Rendering and height accounting share the recipient row's one predicate.
func (a *app) roomRecipientHeight() int {
	// A PROGRAM'S ROOM NAMES ITS TASK ONCE, on its title row: the box's
	// `Reading: <title>` label was the third spelling of it on one screen, and
	// its placeholder already says the program reads no messages.
	if a.programOf() != nil {
		return 0
	}
	if a.roomOrganized() && a.breathingRows() > 0 && !a.welcomeHolds() {
		return 1
	}
	return 0
}

// A settled ordinary task can save settings without reopening its last attempt.
func taskSetupLater(node *taskNode) bool {
	return node != nil && (node.state == session.TaskDone || node.state == session.TaskFailed || node.state == session.TaskUnverified)
}

func taskSetupAvailable(node *taskNode) bool {
	// A ROW LENT TO A RUN'S TASK HAS NO NODE TO SET UP: its worker is the run's,
	// and no door here reaches it (planroom.go).
	if node == nil || node.run != "" || node.planRow != nil {
		return false
	}
	if node.state == session.TaskRunning || node.state == session.TaskQueued {
		return !node.stopped
	}
	// AND ONLY OVER WORK THERE IS SOMETHING TO SET UP FOR. A saved shape being
	// made, a saved shape being run and a quick node are three kinds with no
	// second attempt behind them: a quick node ran where the person works and
	// its last message was the whole of it (session's TaskKindQuick), so a
	// picker offering to point it at another model would be offering to redo
	// work that has no shape left to redo.
	return taskSetupLater(node) && node.kind != session.TaskKindHarness &&
		node.kind != session.TaskKindSubharness && node.kind != session.TaskKindQuick
}
