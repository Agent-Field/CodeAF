package tui3

import "github.com/Agent-Field/codeaf/internal/tui2/tokens"

// homeChatState shares the tab's live reading so tasks that outlast an answer
// keep their conversation working. Owned conversations use cached events;
// another window's tasks use Home's latest presence reading.
func (a *app) homeChatState(cell *homeCell) (working, unread bool) {
	if cell == nil || cell.closed {
		return false, false
	}
	key := cell.chatKey
	if key != "" && key == a.frontTabKey() {
		return a.frontSignal() == tabWorking, a.unreadChats[key]
	}
	if held := a.behind[key]; held != nil && held.watch != nil {
		return held.watch.signal() == tabWorking, a.unreadChats[key] || held.watch.landedSince() > 0
	}
	if cell.row != nil {
		_, working = homeMovingAt(homeLine{kind: homeSession, row: cell.row.session})
	}
	return working, a.unreadChats[key]
}

// The first working conversation takes the spinner only if another panel has none.
func (a *app) homeWorkingLine() int {
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && (line.cell.panel == panelRecent || line.cell.panel == panelSessions) {
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
