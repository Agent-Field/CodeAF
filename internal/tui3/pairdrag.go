package tui3

// ── THE SWEEP, IN THE PAIRING PANEL ─────────────────────────────────────────
//
// The pairing panel is an overlay that used to answer every press and act on
// none of them (pair.go states the law: a press must never answer whether a
// device may have your chats), so the code it showed sat outside the one
// gesture every terminal user owns — drag over text, it is copied. This file
// keeps the law and adds the gesture: a press still answers nothing, but a
// press on the panel can PARK, a sweep over the panel's rows wears the
// selection, and a release puts their text on the clipboard over the same
// OSC 52 road with the same word on the status line the transcript's sweep
// writes (dragselect.go).
//
// The rows swept are the overlay's own SCREEN lines, marked chromeOverlay with
// their index (view.go's addOverlay), so both of the panel's faces are covered
// by the same arithmetic: a code's lines and a card's rows (approve.go) are
// drawn through [pairPanel.draw] either way.

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// pairDrag is a sweep over the pairing panel's rows.
type pairDrag struct {
	on    bool
	swept bool
	// px, py is where the button went down, so a press that has not moved past
	// the slop is still a click and not a one-character selection.
	px, py int
	// anchorRow/anchorCol is the overlay line the press landed on, and
	// row/col the one under the pointer now; the stream between them is the
	// selection, exactly as the transcript's is (dragselect.go).
	anchorRow, row int
	anchorCol, col int
	// unit is what the gesture took, for the status line's word.
	unit dragUnit
}

// pairPressed arms a sweep over the panel, and answers a multi-click on the
// spot. It answers NOTHING else — the panel's own law (pair.go) is kept: a
// press never answers whether a device may have your chats.
func (a *app) pairPressed(x, y int) tea.Cmd {
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else in the panel's modal world answers nothing.
		return nil
	}
	// A new press retires the lit remnant of the last copy: one selection on
	// screen at a time (boxselect.go's rule, kept here).
	a.dragCopied, a.dragInBox = 0, false
	a.pairSel = pairDrag{on: true, px: x, py: y,
		anchorRow: mark.index, anchorCol: x, row: mark.index, col: x}
	switch a.countClick(x, y) {
	case 2:
		a.pairSel.swept, a.pairSel.unit = true, wordUnit
	case 3:
		a.pairSel.swept, a.pairSel.unit = true, rowUnit
	default:
		a.pairSel.swept = false
	}
	a.touch()
	return nil
}

// pairMotion folds one moved-with-the-button-down event into a panel sweep and
// reports whether it was taken.
func (a *app) pairMotion(x, y int) bool {
	if !a.pairSel.on {
		return false
	}
	if !a.pairSel.swept {
		if abs(y-a.pairSel.py) <= dragSlopRows && abs(x-a.pairSel.px) < dragSlop {
			return true
		}
		a.pairSel.swept, a.pairSel.unit = true, cellsUnit
	}
	at, col := a.pairSel.row, max(x, 0)
	if mark, ok := a.chromeAt(y); ok && mark.kind == chromeOverlay {
		at = mark.index
	}
	if a.pairSel.row != at || a.pairSel.col != col {
		a.pairSel.row, a.pairSel.col = at, col
		if a.pairSel.unit != cellsUnit {
			a.pairSel.unit = cellsUnit
		}
		a.touch()
	}
	return true
}

// pairRelease ends a panel sweep: a selection is copied, and a press that never
// moved is nothing at all — the panel answered no press to spend.
func (a *app) pairRelease() (tea.Cmd, bool) {
	drag := a.pairSel
	if !drag.on {
		return nil, false
	}
	a.pairSel = pairDrag{}
	if !drag.swept {
		return nil, true
	}
	return a.pairYank(drag), true
}

// pairYank copies the selected runs of the panel's drawn rows: the same fields,
// the same OSC 52 road and the same status word as every copy on this surface.
// The rows are re-read at release from what the panel paints now, and stripped
// of paint exactly as a transcript copy is.
func (a *app) pairYank(drag pairDrag) tea.Cmd {
	width, _ := a.size()
	rows := a.pair.draw(width, a.overlayHeight(), a.now(), a.pal)
	sr, _, er, _ := drag.ordered()
	sr, er = max(sr, 0), min(er, len(rows)-1)
	if sr > er || len(rows) == 0 {
		return nil
	}
	pieces := make([]string, 0, er-sr+1)
	chars := 0
	for at := sr; at <= er; at++ {
		plain := strings.TrimRight(ansi.Strip(rows[at]), " ")
		from, to, ok := drag.cellsOn(at, width)
		if !ok {
			continue
		}
		var piece string
		if from == 0 && to >= width {
			piece = plain
		} else if from, to, ok = snapCells(plain, from, to); ok {
			piece = strings.TrimRight(ansi.Cut(plain, from, to), " ")
		}
		chars += utf8.RuneCountInString(piece)
		pieces = append(pieces, piece)
	}
	a.dragCopied, a.dragUntil = len(pieces), time.Now().Add(dragFlashFor)
	a.dragLit = dragSelect{unit: drag.unit}
	a.dragChars = chars
	a.dragInBox = true
	a.touch()
	return tea.Batch(
		tea.Raw(osc52(strings.Join(pieces, "\n"), a.tmux)),
		surfaceTick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}

// ordered is the selection's two ends in reading order: start first.
func (d pairDrag) ordered() (sr, sc, er, ec int) {
	if d.anchorRow < d.row || (d.anchorRow == d.row && d.anchorCol <= d.col) {
		return d.anchorRow, d.anchorCol, d.row, d.col
	}
	return d.row, d.col, d.anchorRow, d.anchorCol
}

// cellsOn is the selection's cell range [from, to) on one overlay line, before
// snapping, and whether the line is in the selection at all. width is the
// frame's, and a line that is not the start or the end is selected whole.
func (d pairDrag) cellsOn(at, width int) (int, int, bool) {
	sr, sc, er, ec := d.ordered()
	if at < sr || at > er {
		return 0, 0, false
	}
	if d.unit == rowUnit {
		return 0, width, true
	}
	from, to := 0, width
	if at == sr {
		from = sc
	}
	if at == er {
		to = ec + 1
	}
	if to > width {
		to = width
	}
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// pairPaintSelection lights the live sweep over the panel's drawn rows with the
// one selection paint this surface owns (dragselect.go's markCells). A copied
// panel selection lights nothing afterwards — the panel has no cells of its own
// to keep lit, the way a box's copy leaves no mark on the transcript.
func (a *app) pairPaintSelection(rows []string) []string {
	sel := a.pairSel
	if !sel.on || !sel.swept {
		return rows
	}
	width, _ := a.size()
	sr, _, er, _ := sel.ordered()
	for at := range rows {
		if at < sr || at > er {
			continue
		}
		if from, to, ok := sel.cellsOn(at, width); ok {
			rows[at] = markCells(a.pal, rows[at], from, to)
		}
	}
	return rows
}

// pairCopyCode puts the code as it is shown — digits with their dashes, the
// whole of what the other computer types — on the clipboard, and says so the
// way every copy on this surface says it.
func (a *app) pairCopyCode() tea.Cmd {
	if a.pair.code == nil {
		return nil
	}
	text := a.pair.code.Shown()
	a.dragCopied, a.dragChars = 1, utf8.RuneCountInString(text)
	a.dragUntil = time.Now().Add(dragFlashFor)
	a.dragLit = dragSelect{unit: cellsUnit}
	a.dragInBox = true
	a.touch()
	return tea.Batch(
		tea.Raw(osc52(text, a.tmux)),
		surfaceTick(dragFlashFor, func(time.Time) tea.Msg { return dragFlashMsg{} }),
	)
}
