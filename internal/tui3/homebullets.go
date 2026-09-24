package tui3

import "github.com/Agent-Field/codeaf/internal/tui2/tokens"

// homeChatState shares the tab's live reading of work so tasks that outlast an
// answer keep their conversation working. Owned conversations use cached events;
// another window's tasks use Home's latest presence reading.
//
// IT READS THE WORK, NOT THE TAB'S RANKED SIGNAL. On a tab a question outranks
// work; on Home the question has its own mark, [cellMarkNeeds], which already
// outranks working in [conversationBullet], and a landed task's question sits on
// the task's row. A conversation waiting on one task's decision while another
// runs is still working.
//
// THE CONVERSATION IN FRONT IS KNOWN BY ITS ROW AS WELL AS BY ITS KEY. One that
// has never been named has no tab ([app.tabList]), so Home draws it from the
// saved rows, which carry `here` and no key — and a `/task` can be the first
// thing it does.
func (a *app) homeChatState(cell *homeCell) (working, unread bool) {
	if cell == nil || cell.closed {
		return false, false
	}
	key := cell.chatKey
	if (cell.row != nil && cell.row.here) || (key != "" && key == a.frontTabKey()) {
		front := a.frontTabKey()
		return a.frontWorking(), a.unreadChats[front]
	}
	if key == "" {
		// Another window's conversation, or one this window holds without a name:
		// the saved count is the only reading there is, and nothing here is unread.
		return cell.row != nil && cell.row.session.Tasks.Running > 0, false
	}
	if held := a.behind[key]; held != nil && held.watch != nil {
		return held.watch.working(), a.unreadChats[key] || held.watch.landedSince() > 0
	}
	if cell.row != nil {
		working = cell.row.session.Tasks.Running > 0
	}
	return working, a.unreadChats[key]
}

// The first working conversation takes the spinner only if another panel has none.
//
// A ROW WEARING A QUESTION IS PASSED OVER, because its `?` outranks the working
// mark and would hide the spinner: Home's frame clock would then run for a
// frame that never changes.
func (a *app) homeWorkingLine() int {
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.mark != cellMarkNeeds &&
			(line.cell.panel == panelRecent || line.cell.panel == panelSessions) {
			if working, _ := a.homeChatState(line.cell); working {
				return i
			}
		}
	}
	return homeNoLine
}

func (a *app) homeConversationBullet(cell *homeCell, pal palette) string {
	working, unread := a.homeChatState(cell)
	mark := pal.glyph(tokens.GWorking)
	first := a.homeWorkingLine()
	if working && !a.linear && ((a.home.spin >= 0 && a.home.lines[a.home.spin].cell == cell) ||
		(a.home.spin < 0 && first >= 0 && a.home.lines[first].cell == cell)) {
		mark = a.homeSpinGlyph()
	}
	return conversationBullet(pal, working, unread, cell != nil && cell.mark == cellMarkNeeds, mark)
}

// conversationBullet keeps conversation state marks identical across Home and Tasks.
func conversationBullet(pal palette, working, unread, question bool, workingMark string) string {
	switch {
	case question:
		return pal.warn(pal.glyph(tokens.GNeedsHuman))
	case working:
		return pal.accent(workingMark)
	case unread:
		return pal.bold(pal.accent(pal.glyph(tokens.GStepDone)))
	default:
		return pal.dim(pal.glyph(tokens.GProseBullet))
	}
}
