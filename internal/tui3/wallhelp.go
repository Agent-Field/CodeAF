package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE HELP SHEET: WHAT YOU CAN DO HERE ────────────────────────────────────
//
// `?` on the wall, or its toolbar's Help, puts up one card that lists every
// act the page has, grouped, each with its key:
//
//	╭─ Conversations ──────────────────────────────────────────╮
//	│                                                          │
//	│  Navigate                    Organize                    │
//	│  Move               ←↑↓→     Select                   ␣  │
//	│  First/last          g G     New team                 s  │
//	│  …                                                       │
//	╰──────────────────────────────────────────────────────────╯
//
// A LIST OF KEYS IS ALSO A MENU. Every row is a press and does what its key
// does, through the same [app.wallCommand] the key goes through, with the
// sheet put away first so the act is seen. A row that needs a conversation
// acts on the focused one, as its key does; a row that names a pair of keys
// does the forward one of the two.
//
// It is the painting half's and the wiring's at once, as the popovers are
// (wallbar.go, wallpop.go): the painter is pure, and the wiring below it is
// the only part that touches the app.

// wallHelpRow is one row of the sheet: what it does, its key as drawn, the
// key it presses, and what the toolbar says while the pointer is on it.
type wallHelpRow struct {
	label, key, press, hint string
}

// wallHelpGroup is one titled group of rows.
type wallHelpGroup struct {
	name string
	rows []wallHelpRow
}

// wallHelpList is the sheet's groups, with the keys in the palette's tier.
func wallHelpList(ascii bool) []wallHelpGroup {
	k := wallKeysFor(ascii)
	arrows, span := "←↑↓→", "1–9"
	if ascii {
		arrows, span = "arrows", "1-9"
	}
	row := func(label, key, press, hint string) wallHelpRow {
		return wallHelpRow{label: label, key: key, press: press, hint: hint}
	}
	return []wallHelpGroup{
		{name: "Navigate", rows: []wallHelpRow{
			row("Move", arrows, "right", "Move the focus to the next conversation"),
			row("First/last", "g G", "G", "Go to the last conversation"),
			row("Page", "pgup pgdn", "pgdown", "Go down a screen of rows"),
			row("Next needing you", "n", "n", "Go to the next conversation waiting on you"),
		}},
		{name: "Open", rows: []wallHelpRow{
			row("Open", k.enter, "enter", "Open the focused conversation"),
			row("Answer", k.enter, "enter", "Open the focused conversation to answer it"),
			row("Back", "esc", "esc", "Back one step"),
		}},
		{name: "Organize", rows: []wallHelpRow{
			row("Select", k.pick, "space", "Select the focused conversation"),
			row("New team", "s", "s", "Make a team"),
			row("Organize", "o", "o", "Suggest teams for your conversations"),
			row("Add to teams", "m", "m", "Add the focused conversation to teams"),
			row("Make manager", "m", "m", "In the Teams list: make the focused conversation the shown team's manager"),
			row("Team settings", "e", "e", "Rename, recolour or change the shown team's settings"),
			row("Close team", "D", "D", "Close the shown team; reopen it from Closed on the teams page"),
			row("Switch team", "tab / "+span, "tab", "Show the next team"),
			row("Close view", "x", "x", "Close the focused view; the work keeps running"),
		}},
		{name: "View", rows: []wallHelpRow{
			row("Filter", "/", "/", "Filter conversations by name"),
			row("Fewer/more columns", k.minus+" +", "+", "Show more columns"),
			row("Automatic columns", "0", "0", "Let the width choose the columns"),
		}},
	}
}

// wallHelpRows is every row in the sheet's reading order, which is the order
// a [wallHitHelp]'s arg counts in.
func wallHelpRows(ascii bool) []wallHelpRow {
	var out []wallHelpRow
	for _, g := range wallHelpList(ascii) {
		out = append(out, g.rows...)
	}
	return out
}

// wallHelpColGap is the blank cells between the sheet's two columns.
const wallHelpColGap = 4

// wallHelpCell is one cell of a column: a heading, a blank, or a row.
type wallHelpCell struct {
	s   string
	row int // the row's place in [wallHelpRows], -1 for a heading or a blank
}

// wallHelpColumn lays groups out as one column's cells, colW wide, with a
// blank between two groups. at is the first group's first row's place in
// [wallHelpRows].
func wallHelpColumn(pal palette, v wallView, groups []wallHelpGroup, at, colW int) []wallHelpCell {
	var cells []wallHelpCell
	for gi, g := range groups {
		if gi > 0 {
			cells = append(cells, wallHelpCell{row: -1})
		}
		cells = append(cells, wallHelpCell{s: pal.bold(pal.muted(g.name)), row: -1})
		for _, r := range g.rows {
			gap := max(colW-ansi.StringWidth(r.label)-ansi.StringWidth(r.key), 1)
			s := pal.ink(r.label) + strings.Repeat(" ", gap) + pal.dim(r.key)
			lit := v.hover.kind == wallHitHelp && v.hover.arg == at
			cells = append(cells, wallHelpCell{s: wallPopRowPaint(pal, s, colW, lit), row: at})
			at++
		}
	}
	return cells
}

// wallHelpCard is the sheet, centred over the grid: two columns where the
// frame is wide enough for them and one where it is not. When its lines do
// not fit between the head and the foot it drops its padding first, then
// shows a window of them from v.helpTop, and its bottom border says which way
// the rest lies.
func wallHelpCard(pal palette, v wallView, width, height int) wallCard {
	groups := wallHelpList(pal.ascii)
	colW := 0
	for _, g := range groups {
		for _, r := range g.rows {
			colW = max(colW, ansi.StringWidth(r.label)+2+ansi.StringWidth(r.key))
		}
	}
	colW += 2
	chrome := 2 + 2*wallCardPadX
	two := 2*colW+wallHelpColGap+chrome <= width-2*wallMargin
	var left, right []wallHelpCell
	if two {
		left = wallHelpColumn(pal, v, groups[:2], 0, colW)
		right = wallHelpColumn(pal, v, groups[2:], len(groups[0].rows)+len(groups[1].rows), colW)
	} else {
		left = wallHelpColumn(pal, v, groups, 0, colW)
	}
	inner := colW
	if two {
		inner = 2*colW + wallHelpColGap
	}
	w := inner + chrome
	if w > width-2*wallMargin {
		return wallCard{}
	}
	var lines []wallCardLine
	for r := 0; r < max(len(left), len(right)); r++ {
		var ln wallCardLine
		put := func(cells []wallHelpCell, x int) {
			if r >= len(cells) {
				return
			}
			c := cells[r]
			ln.s += strings.Repeat(" ", max(x-ansi.StringWidth(ln.s), 0)) + wallFit(c.s, colW)
			if c.row >= 0 {
				ln.hits = append(ln.hits, wallHit{x0: x, y0: 0, x1: x + colW, y1: 1, kind: wallHitHelp, arg: c.row})
			}
		}
		put(left, 0)
		put(right, colW+wallHelpColGap)
		lines = append(lines, ln)
	}

	top := wallGridTop
	floor := height - wallFootRows + 1 // the first row a card may not cover
	room := floor - top
	padY := wallCardPadY
	if len(lines)+2+2*padY > room {
		padY = 0
	}
	vis := room - 2 - 2*padY
	if vis < 1 {
		return wallCard{}
	}
	over := max(len(lines)-vis, 0)
	from := min(max(v.helpTop, 0), over)
	shown := lines[from:min(from+vis, len(lines))]
	h := len(shown) + 2 + 2*padY
	x := (width - w) / 2
	y := top + max((room-h)/2, 0)
	card := wallCardBuild(pal, "Conversations", shown, x, y, w, wallCardPadX, padY)
	if over > 0 {
		card.over = over
		card.rows[len(card.rows)-1] = wallHelpFoot(pal, w, from < over)
	}
	return card
}

// wallHelpFoot is a scrolled sheet's bottom border, saying which way the lines
// it is not showing lie.
func wallHelpFoot(pal palette, w int, below bool) string {
	box := wallBoxLight
	word := " ↓ more "
	if !below {
		word = " ↑ more "
	}
	if pal.ascii {
		box = wallBoxLightASCII
		word = strings.NewReplacer("↓", "v", "↑", "^").Replace(word)
	}
	fill := max(w-3-ansi.StringWidth(word), 0)
	return pal.muted(box.bl+box.h) + pal.dim(word) + pal.muted(strings.Repeat(box.h, fill)+box.br)
}

// ── THE SHEET, WIRED ────────────────────────────────────────────────────────

// wallOpenHelp puts the sheet up at its top. It takes the place of a popover
// or the filter's typing, as any card does.
func (a *app) wallOpenHelp() {
	a.wall.help = true
	a.wall.helpTop = 0
	a.wall.pop = wallPop{}
	a.wall.filterOn = false
	a.wall.hover = wallHitRef{}
	a.wall.stirred = true
	a.touch()
}

// wallHelpScroll moves the sheet by lines, inside what the last frame said it
// could scroll.
func (a *app) wallHelpScroll(by int) {
	top := min(max(a.wall.helpTop+by, 0), a.wall.helpMax)
	if top == a.wall.helpTop {
		return
	}
	a.wall.helpTop = top
	a.wall.hover = wallHitRef{}
	a.wall.rehover = true
	a.wall.stirred = true
	a.touch()
}

// wallHelpKey is a key while the sheet is up. esc, q and ? again put it away;
// the arrows and the page keys scroll it. Any other key puts it away and does
// what it does, since a sheet of keys is read to be pressed. It reports
// whether it took the key. The sheet is at most two screens, so a page key
// goes to its top or its end.
func (a *app) wallHelpKey(key string) (tea.Cmd, bool) {
	page := a.wall.helpMax
	switch key {
	case "esc", "q", "?":
		a.wall.help = false
		a.touch()
		return nil, true
	case "up", "k":
		a.wallHelpScroll(-1)
		return nil, true
	case "down", "j":
		a.wallHelpScroll(1)
		return nil, true
	case "pgup", "home":
		a.wallHelpScroll(-page)
		return nil, true
	case "pgdown", "end":
		a.wallHelpScroll(page)
		return nil, true
	}
	a.wall.help = false
	a.touch()
	return nil, false
}

// wallHelpPress is a press on row i of the sheet: the sheet goes, and the
// row's key is pressed.
func (a *app) wallHelpPress(i int, tiles []wallTile) tea.Cmd {
	rows := wallHelpRows(a.pal.ascii)
	if i < 0 || i >= len(rows) {
		return nil
	}
	a.wall.help = false
	a.touch()
	return a.wallCommand(rows[i].press, tiles)
}
