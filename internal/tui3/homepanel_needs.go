package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// needsPanel is `needs you`: every conversation and watch stopped on a person,
// the longest wait first (the switcher's own rank, homeattention.go's order).
//
// A ROW CARRIES ITS QUESTION AND ITS ANSWERS. The question is the line under
// the row, and the answers are drawn at its right — `1 yes  2 not now` — so a
// digit pressed anywhere on home answers the top row and the key is never a
// guess (law 7, [app.homeGridAnswer]).
//
// OWED: lane P — a task's `your call` rows and a watch's own NeedsPerson join
// this panel (DESIGN §3 P1); the grid reads whatever this returns.
type needsPanel struct{ homePanelBase }

func (needsPanel) rows(in *homeGridInput) homePanelRows {
	var lines []homeLine
	for _, row := range in.rows {
		if !row.needs {
			continue
		}
		cell := &homeCell{panel: panelNeeds, mark: cellMarkNeeds, title: row.title,
			right: switcherMarginWord(row), sub: strings.TrimPrefix(row.note, switcherAsksWord)}
		if row.kind == switcherConversation {
			cell.subRight = answersWord(row.session.Presence.Question)
		}
		lines = append(lines, switcherRowLine(row, cell))
	}
	return homePanelRows{lines: lines, said: countWord(len(lines))}
}

// answersWord is a question's answers as one clause, each option's key beside
// its own word — the same chips the answer strip draws ([answerChips]).
func answersWord(question session.PresenceQuestion) string {
	var parts []string
	for _, chip := range answerChips(question) {
		parts = append(parts, chip.text)
	}
	return strings.Join(parts, homeCellGap)
}
