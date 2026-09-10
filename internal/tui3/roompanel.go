package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
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
	a.railCramped = false
	lines := a.railLines(entries, width)
	controls := a.roomControlRows(width)
	foot, marks := a.railFootRows(width, height)
	// Column navigation retains its existing doors and their exact hit targets.
	footer := make([]railLine, len(foot))
	for i, s := range foot {
		footer[i] = railLine{text: s, entry: -1, hint: i == marks.hint, stow: i == marks.door,
			more: i == marks.more, keeping: i == marks.keeping}
	}
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
	if node := a.roomNode(); node != nil && !a.roomIsGuest() {
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
		if _, ok := a.stopDoors(); ok {
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
	if node := a.roomNode(); node != nil {
		f := a.roomFactsOf(node)
		right = rowAll([]rowField{f.state, f.live, f.clock, f.spend})
		state := rowAll([]rowField{f.state})
		painted = a.taskStateInk(node)(state) + a.pal.muted(strings.TrimPrefix(right, state))
	}
	room := max(width-headLabelAt-2-ansi.StringWidth(right)-3, 1)
	left = fit(left, room)
	return strings.Repeat(" ", headLabelAt) + a.pal.bold(a.pal.ink(left)) + strings.Repeat(" ", max(width-headLabelAt-2-ansi.StringWidth(left)-ansi.StringWidth(right), 1)) + painted + "  "
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
	if a.roomIsGuest() {
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
	if node == nil || node.run != "" {
		return false
	}
	if node.state == session.TaskRunning || node.state == session.TaskQueued {
		return !node.stopped
	}
	return taskSetupLater(node) && node.kind != session.TaskKindHarness && node.kind != session.TaskKindSubharness
}
