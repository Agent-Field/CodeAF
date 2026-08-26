package tui3

// A CLICK ON THE BOX PUTS THE CARET UNDER THE POINTER.
//
// Every text field a person has ever used answers a click this way, and this
// one did not: a press on the draft fell through to the body's drag parking and
// moved nothing, so the one place on the surface where somebody is actually
// writing was the one place their pointer was dead. The input's rows now carry
// their own chrome mark (view.go's chromeDraft), and this file is the other
// half — turning the marked row and the pointer's column back into a rune
// offset through the SAME layout arithmetic the frame drew the block with
// (input.go's draftWindow), so the caret lands on the letter the person aimed
// at and not on a neighbour computed by a second copy of the wrap.

import (
	"github.com/charmbracelet/x/ansi"
)

// draftPress answers a click that landed on the input block, and reports
// whether it took it. Only the MAIN draft answers: while an overlay's filter or
// edit box is standing in the input's position (the picker, memory, resume, the
// subharness card, the files shelf, the connections panel, the rewind bar), the
// keyboard is pointed somewhere else and a caret moved under it would be a
// caret in a box the person is not in.
//
// THE THINKING LADDER IS IN THAT LIST FOR THE SAME REASON READ ONE STEP EARLIER
// (effortchip.go). Its rows hang ABOVE the box rather than in it, so the draft
// is still on the frame — but every key belongs to the ladder while it is up, so
// a caret placed under the pointer would be a caret nothing can move.
func (a *app) draftPress(x, y int) bool {
	if a.rew.on || a.pick.open || a.at(pageMemory) || a.roster.open ||
		a.subPage.open || a.shelf.open || a.connPanel.open || a.effPick.open {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeDraft {
		return false
	}
	width, height := a.size()
	boxWidth := width - len(inputPad)
	// The tray is the block's first row when there is one (inputBlock). A click
	// on it is the chips' own business and [app.chipPress] has already had its
	// chance earlier in the dispatch — the row is consumed here so a miss on a
	// chip does not park a drag on the tray.
	at := mark.index
	if a.chipStrip(boxWidth) != "" {
		if at == 0 {
			return true
		}
		at--
	}
	// An empty draft has one row and one place the caret can be.
	if len(a.input.value) == 0 {
		return true
	}
	// The same numbers inputBlock laid the block out with, derived the same way.
	rows := min(draftRows, height-2)
	if rows < 1 {
		rows = 1
	}
	head := ansi.StringWidth(a.roomLead(boxWidth)) + ansi.StringWidth(prompt)
	room := boxWidth - head
	if room < 4 {
		room = 4
	}
	col := x - len(inputPad) - head
	a.input.cursor = draftClickIndex(a.input.value, a.input.cursor, at, col, room, rows)
	a.touch()
	return true
}

// draftClickIndex is the pure inverse of the draft's layout: which rune offset
// a press on display row `row` (within the block's window) and text column
// `col` names. It rebuilds the same window [draftBlockWithTags] drew — same
// wrap, same top — so the answer and the drawing cannot disagree.
//
// A row past the window's end is the end of the draft, a column past a row's
// end is that row's end, and a column left of the text is its start: every miss
// lands on the nearest place a caret can actually be, which is what every text
// field does with an inexact finger.
func draftClickIndex(value []rune, cursor, row, col, room, maxRows int) int {
	segments, _, top, _ := draftWindow(value, cursor, room, maxRows)
	at := top + row
	if at < 0 {
		at = 0
	}
	if at >= len(segments) {
		return len(value)
	}
	seg := segments[at]
	if col <= 0 {
		return seg.from
	}
	// Walk the row's runes accumulating the same display width the caret's own
	// column is measured in (caretColumnIn), so double-width cells count double
	// here exactly as they do there.
	index, walked := seg.from, 0
	for index < seg.to {
		w := cells(value[index])
		if walked+w > col {
			break
		}
		walked += w
		index++
	}
	return index
}
