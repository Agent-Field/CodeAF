package tui3

import "github.com/Agent-Field/codeaf/internal/tui2/tokens"

// homeChatState reads only the foreground state and the keeper's cached events.
// A task running in the conversation does not mean its answer is still streaming.
func (a *app) homeChatState(cell *homeCell) (answering, unread bool) {
	if cell == nil || cell.closed || cell.chatKey == "" {
		return false, false
	}
	key := cell.chatKey
	if key == a.frontTabKey() {
		return a.state == stateWorking, a.unreadChats[key]
	}
	if held := a.behind[key]; held != nil && held.watch != nil {
		return held.watch.turning.Load(), a.unreadChats[key] || held.watch.landedSince() > 0
	}
	return false, a.unreadChats[key]
}

// The first answering conversation takes the spinner only if another panel has none.
func (a *app) homeAnsweringLine() int {
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.cell != nil && line.cell.panel == panelRecent {
			if working, _ := a.homeChatState(line.cell); working {
				return i
			}
		}
	}
	return homeNoLine
}

func (a *app) homeConversationBullet(cell *homeCell, pal palette) string {
	working, unread := a.homeChatState(cell)
	switch {
	case working:
		mark := pal.glyph(tokens.GWorking)
		first := a.homeAnsweringLine()
		if !a.linear && a.home.spin < 0 && first >= 0 && a.home.lines[first].cell == cell {
			mark = a.homeSpinGlyph()
		}
		return pal.accent(mark)
	case unread:
		return pal.bold(pal.accent(pal.glyph(tokens.GStepDone)))
	default:
		return pal.dim(pal.glyph(tokens.GProseBullet))
	}
}
