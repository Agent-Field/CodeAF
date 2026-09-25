package tui3

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ── THE WALL, PAINTED ───────────────────────────────────────────────────────
//
// This is the painting half of the wall (wallcontract.go): pure functions from
// a [wallView] to rows. Nothing here reads a clock, a file or the app. The time
// is v.now, the motion is v.spin, and everything a tile says was put on it by
// the reading half.
//
// The frame reads top to bottom as one hierarchy. A title bar says what this
// is and what the conversations on it are doing; under it the Teams control
// says which set is shown; a rule and a blank row close the head. Then the
// grid, and last the foot, the head mirrored: a blank row, a rule, and the
// toolbar (wallbar.go), whose free middle says what the control under the
// pointer does. The head and the foot end where the grid ends. Three cards can
// float over the grid: the selection tray while conversations are picked, the
// new-team card while one is named, and the Organize card while suggestions
// are up (teamorganize.go); and the help sheet (wallhelp.go) floats over
// everything while it is up.
//
// The words on screen are the person's words: a Conversation is a tile, a
// Team is a named set of them. "wall" and "tab" are this code's names and are
// never drawn.
//
// A TILE READS TITLE, STATE, NOW, HISTORY. The title is the brightest thing
// in it; one dim line under it says what the agent is doing or when it last
// moved; the body under that is the conversation's own rows, faded with age
// by the reading half (wallmini.go), so the newest rows are where the eye
// lands.
//
// A TILE HAS ONE STATE LADDER AND EVERY STEP MAKES ONE CLAIM:
//
//	rest       a dim rounded border and no ground
//	hover      the whole tile on the cursor ground, the border lifted to muted,
//	           and its action row drawn on the bottom border
//	focus      the heavy border; weight, never colour; and the action row
//	selected   the selected ground, a step above hover, and a ☑ at top left
//	needs you  the amber border, and the only amber on the wall; its body
//	           ends in the question and an Answer button
//
// THE ACTIONS ARE WORDS, NOT GLYPHS. A tile's bottom border carries a
// sparkline at rest, and under the pointer or the keyboard's focus it becomes
// a row of labelled buttons, each a verb and its key (wallbar.go's
// [wallTileActs]). The buttons' cells depend on the tile's width alone, so
// nothing moves as the row appears. The ☐ is drawn only in the selection
// mode, on every tile, where the mode itself says what a box is for.

// wallChromeRows is what the frame spends outside the grid. The head is the
// title bar, the Teams row, a rule and a blank row; the foot mirrors it, a
// blank row, a rule and the toolbar, so the grid sits between two matching
// edges and no tile touches a control.
const wallChromeRows = 7

// wallGridTop is the first row a tile is drawn on.
const wallGridTop = 4

// wallFootRows is the foot's share of the chrome: the blank row, the rule and
// the toolbar.
const wallFootRows = 3

// wallGutter is the blank cells between two tiles on one row, wallRowGap the
// blank rows between two rows of tiles, and wallMargin the blank column kept
// either side of the grid so no tile touches the frame's edge.
const (
	wallGutter = 2
	wallRowGap = 1
	wallMargin = 1
)

// A tile's border is padded inside: wallPadX blank cells left and right of
// the body, wallPadY blank rows above and below it.
const (
	wallPadX = 2
	wallPadY = 1
)

// wallInnerW is the width a tile's body is drawn at: the tile less its two
// border cells and the padding either side.
func wallInnerW(tileW int) int { return max(tileW-2-2*wallPadX, 0) }

// wallInnerH is the height inside a tile's padding: the tile less its two
// borders and the padding above and below. Its first row is the tile's state
// line and its second a blank, and the body has the rest.
func wallInnerH(tileH int) int { return max(tileH-2-2*wallPadY, 0) }

// wallMetaRows is what the state line and the blank under it take.
const wallMetaRows = 2

// The tile height is chosen inside these bounds; the width has only a floor.
const (
	wallTileMinW = 44
	// wallTileIdealW is the width the automatic column count aims for.
	wallTileIdealW = 52
	wallTileMinH   = 12
	wallTileMaxH   = 60
)

// wallFreshSettle is how long newly arrived lines stay lifted to ink.
const wallFreshSettle = 600 * time.Millisecond

// wallMadeFor is how long the chip row says a team was just made.
const wallMadeFor = 2 * time.Second

// wallEmptyWord is the whisper an empty wall draws beside its way back.
const wallEmptyWord = "No open conversations"

// wallBox is one border's glyphs.
type wallBox struct{ tl, tr, bl, br, h, v string }

var (
	wallBoxHeavy = wallBox{"┏", "┓", "┗", "┛", "━", "┃"}
	wallBoxLight = wallBox{"╭", tokens.GlyphFrameTopRight, "╰", tokens.GlyphFrameBottomRight, "─", "│"}
	// The ASCII floor keeps focus as weight: `#` and `=` for the focused tile,
	// `+` and `-` for the rest.
	wallBoxHeavyASCII = wallBox{"#", "#", "#", "#", "=", "#"}
	wallBoxLightASCII = wallBox{"+", "+", "+", "+", "-", "|"}
)

// wallSparkCells is the eight-step ramp a sample 0..7 is drawn in.
var (
	wallSparkCells      = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	wallSparkCellsASCII = []string{"_", ".", "-", ":", "=", "+", "*", "#"}
)

// wallGlyphs is the handful of marks the wall draws besides its boxes, in the
// palette's tier.
type wallGlyphs struct {
	sep, tool, cursor, marked, cell, seenCell, gt, more string
}

func wallGlyphsFor(ascii bool) wallGlyphs {
	if ascii {
		return wallGlyphs{sep: "-", tool: ">", cursor: "_", marked: "@", cell: ".", seenCell: "o", gt: ">", more: "~"}
	}
	return wallGlyphs{sep: "·", tool: "▸", cursor: "▌", marked: "☑", cell: tokens.GlyphActionWork, seenCell: "▣", gt: "›", more: "…"}
}

// wallGrid is the grid for n tiles on a frame width by height (the whole frame,
// chrome included): the column count and one tile's size.
//
// The columns come from the width alone, so a tile does not jump column when a
// conversation opens: under 100 cells one column, under 150 two, under 200
// three, else four. cols above zero is a person's override, and either way the
// count falls until a tile is at least [wallTileMinW] wide.
//
// The height is shared out so that as many tile rows as the frame holds at
// [wallTileMinH] or more are drawn, then capped at [wallTileMaxH]; a frame that
// can hold two rows always shows two.
func wallGrid(n, width, height, cols int) (c, tileW, tileH int) {
	room := width - 2*wallMargin
	c = cols
	if c <= 0 {
		c = (room + wallGutter) / (wallTileIdealW + wallGutter)
		if c > 4 {
			c = 4
		}
	}
	// A TILE NEVER GOES UNDER ITS MINIMUM. Past it a tile stops being a
	// conversation you can read and becomes a coloured stripe, so a narrow frame
	// or a person's `+` loses a column instead, and the rows scroll.
	for c > 1 && (room-wallGutter*(c-1))/c < wallTileMinW {
		c--
	}
	if c < 1 {
		c = 1
	}
	tileW = (room - wallGutter*(c-1)) / c
	if tileW < 0 {
		tileW = 0
	}
	gridH := height - wallChromeRows
	if gridH <= 0 {
		return c, tileW, 0
	}
	rows := (n + c - 1) / c
	if rows < 1 {
		rows = 1
	}
	// The rows on screen are as many as fit at the minimum height, never more
	// than there are; they then share the height out so the grid fills the
	// frame. Anything past them scrolls rather than squeezing.
	fit := (gridH + wallRowGap) / (wallTileMinH + wallRowGap)
	if fit < 1 {
		fit = 1
	}
	k := min(rows, fit)
	tileH = (gridH - wallRowGap*(k-1)) / k
	if tileH > wallTileMaxH {
		tileH = wallTileMaxH
	}
	return c, tileW, tileH
}

// wallVisibleRows is how many whole tile rows the grid area holds.
func wallVisibleRows(height, tileH int) int {
	gridH := height - wallChromeRows
	if tileH <= 0 || gridH < tileH {
		return 0
	}
	return (gridH + wallRowGap) / (tileH + wallRowGap)
}

// wallScrollFor is the first tile row to draw so the focused tile is on
// screen, moving scroll as little as it can: a focus already visible keeps the
// scroll it had.
func wallScrollFor(focus, scroll, n, width, height, cols int) int {
	if n <= 0 {
		return 0
	}
	c, _, tileH := wallGrid(n, width, height, cols)
	vis := wallVisibleRows(height, tileH)
	if vis < 1 {
		vis = 1
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= n {
		focus = n - 1
	}
	rows := (n + c - 1) / c
	row := focus / c
	if row < scroll {
		scroll = row
	}
	if row >= scroll+vis {
		scroll = row - vis + 1
	}
	if top := rows - vis; scroll > top {
		scroll = top
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// wallMarked is how many of the drawn tiles are marked. Any at all is the
// selection mode: every tile shows its box, and a press on a tile toggles it.
func wallMarked(v wallView) int {
	n := 0
	for _, t := range v.tiles {
		if t.marked {
			n++
		}
	}
	return n
}

// renderWall is the whole wall: exactly height rows, none wider than width,
// and where every target landed. The scroll it draws is v.scroll moved just
// enough to keep the focus on screen ([wallScrollFor]), so a stale scroll never
// paints a frame with the focus off it.
func renderWall(pal palette, v wallView, width, height int) ([]string, []wallHit) {
	if height <= 0 {
		return nil, nil
	}
	if width < 0 {
		width = 0
	}
	rows := make([]string, height)
	g := wallGlyphsFor(pal.ascii)
	if len(v.tiles) == 0 && v.filter == "" && v.team == "" {
		// Nothing is open at all: there is nothing to title, narrow or lay out,
		// so the frame is the one sentence and the way back.
		y := (height - 1) / 2
		row, hits := wallEmptyRow(pal, v, width, y)
		rows[y] = row
		return rows, hits
	}

	n := len(v.tiles)
	focus := min(max(v.focus, 0), max(n-1, 0))
	c, tileW, tileH := wallGrid(max(n, 1), width, height, v.cols)
	scroll := wallScrollFor(focus, v.scroll, n, width, height, v.cols)
	vis := wallVisibleRows(height, tileH)
	first, last := scroll*c, min((scroll+vis)*c, n)
	inset := wallInset(width, c, tileW)

	var hits []wallHit
	row, h := wallTitleRow(pal, g, v, width, inset, 0)
	rows[0] = row
	hits = append(hits, h...)
	if height > 1 {
		row, h := wallTeamsRow(pal, g, v, width, height, inset, c, first, last, 1)
		rows[1] = row
		hits = append(hits, h...)
	}
	if height > 2 {
		rows[2] = wallRule(pal, v, width)
	}

	gridEnd := height - wallFootRows // the blank row over the foot's rule
	if n == 0 {
		// A filter or a team with nothing in it keeps the whole frame, so
		// what narrowed it stays on screen beside the way to undo it.
		if gridEnd > wallGridTop {
			y := wallGridTop + (gridEnd-wallGridTop-1)/2
			row, h := wallNoneRow(pal, v, width, y)
			rows[y] = row
			hits = append(hits, h...)
		}
	} else if tileW >= 6 && tileH >= 3 {
		margin := strings.Repeat(" ", wallMargin)
		gap := strings.Repeat(" ", wallGutter)
		top := wallGridTopFor(height, tileH, vis)
		for r := 0; r < vis; r++ {
			y0 := top + r*(tileH+wallRowGap)
			var line []string
			for col := 0; col < c; col++ {
				i := (scroll+r)*c + col
				if i >= n {
					break
				}
				x0 := wallMargin + col*(tileW+wallGutter)
				hits = append(hits, wallTileHits(pal, v, v.tiles[i], i, i == focus, x0, y0, tileW, tileH)...)
				tile := wallPaintTile(pal, g, v, v.tiles[i], i, i == focus, tileW, tileH, y0)
				if line == nil {
					line = tile
					for y := range line {
						line[y] = margin + line[y]
					}
					continue
				}
				for y := range line {
					line[y] += gap + tile[y]
				}
			}
			for y, s := range line {
				if y0+y < gridEnd {
					rows[y0+y] = s
				}
			}
		}
	}
	if height > wallGridTop+wallFootRows-1 {
		rows[height-2] = wallFootRule(pal, width)
	}
	if height > 2 {
		row, h := wallBarIn(pal, v, width, inset, height-1)
		rows[height-1] = row
		hits = append(hits, h...)
	}

	// The cards float over the grid; what they cover stops answering the
	// pointer, so a press lands on the card and never on the tile under it.
	// A popover floats over everything, the tray included.
	switch {
	case v.org.on:
		hits = wallOverlay(rows, hits, wallOrgCard(pal, g, v, width, height), width)
	case v.naming:
		hits = wallOverlay(rows, hits, wallNameCard(pal, g, v, width, height), width)
	case wallMarked(v) > 0:
		hits = wallOverlay(rows, hits, wallTray(pal, g, v, width, height), width)
	}
	if v.pop.kind != wallPopNone && !v.naming && !v.org.on {
		hits = wallOverlay(rows, hits, wallPopCard(pal, g, v, width, height), width)
	}
	if v.help {
		hits = wallOverlay(rows, hits, wallHelpCard(pal, v, width, height), width)
	}

	for i, s := range rows {
		if ansi.StringWidth(s) > width {
			rows[i] = ansi.Truncate(s, width, "")
		}
	}
	return rows, hits
}

// wallGridTopFor is the first row the grid is drawn on. The rows of tiles
// rarely fill the room between the two rules exactly, and what is over is
// shared out above and below them, the odd row going below, so the grid sits
// in the middle rather than leaving a gap only over the toolbar.
func wallGridTopFor(height, tileH, vis int) int {
	if vis < 1 {
		return wallGridTop
	}
	gridH := height - wallChromeRows
	over := gridH - vis*tileH - (vis-1)*wallRowGap
	return wallGridTop + max(over, 0)/2
}

// wallInset is the blank cells kept at the frame's right edge: the margin,
// plus whatever the columns could not share out. The head and the toolbar end
// where the grid ends, so the counts, the minimap and the last button all
// line up with the right edge of the last tile.
func wallInset(width, c, tileW int) int {
	used := wallMargin + c*tileW + max(c-1, 0)*wallGutter
	return max(width-used, wallMargin)
}

// wallTitleRow is the title bar: what this is on the left, and on the right
// what the conversations on it are doing, as quiet counts, each said only when
// it is not zero, three cells apart, ending inset cells from the right edge so
// a count growing a digit moves only what is left of it. The count waiting on
// a person is a button: it goes to the next one, as ? does.
//
//	▦ Conversations · open in this window · in test    2 more in test · Open them   1 open
//
// THE WALL IS WHAT IS OPEN IN THIS WINDOW, narrowed by a team, and the title
// says so in those words. A team's members this window does not have open are
// not tiles; while there are any, one quiet word button says how many and
// resumes them behind ([app.wallResumeAway]), and while there are none it is
// not drawn at all.
func wallTitleRow(pal palette, g wallGlyphs, v wallView, width, inset, y int) (string, []wallHit) {
	mark := "▦"
	run := "⠿"
	if pal.ascii {
		mark, run = "#", "*"
	}
	sub := wallOpenHereWord
	teamName := ""
	if i := v.teamRow(v.team); i >= 0 {
		teamName = v.teams[i].name
		sub += " " + g.sep + " in " + teamName
	}
	left := " " + pal.bold(pal.ink(mark+" Conversations")) + pal.dim(" "+g.sep+" "+sub)
	working, needs := 0, 0
	for _, t := range v.tiles {
		switch {
		case t.signal == tabWorking && t.live:
			working++
		case t.signal == tabNeedsPerson:
			needs++
		}
	}
	// A pill's pad is the cells of ground it wears either side when it is a
	// button; the three cells between two pills count it, so the words are
	// evenly spaced whether or not a pill is pressable.
	type pill struct {
		s    string
		w    int
		pad  int
		act  wallAct
		away bool
	}
	const pillGap = 3
	var pills []pill
	if working > 0 {
		word := run + " " + strconv.Itoa(working) + " running"
		pills = append(pills, pill{s: pal.live(word), w: ansi.StringWidth(word)})
	}
	if needs > 0 {
		word := tokens.GlyphNeedsHuman + " " + strconv.Itoa(needs) + " " + tabNeedsPersonWord
		p := " " + pal.warn(word) + " "
		if v.hover.kind == wallHitAction && v.hover.arg == int(wallActNext) {
			p = pal.cursor(p, 0)
		}
		pills = append(pills, pill{s: p, w: ansi.StringWidth(word) + 2, pad: 1, act: wallActNext})
	}
	awayAt := -1
	awayPill := func(named bool) pill {
		lead := strconv.Itoa(v.away) + " more"
		if named && teamName != "" {
			lead += " in " + teamName
		}
		lead += " " + g.sep + " "
		p := " " + pal.dim(lead) + pal.muted(wallResumeWord) + " "
		if v.hover.kind == wallHitAction && v.hover.arg == int(wallActResume) {
			p = pal.cursor(" "+pal.muted(lead)+pal.ink(wallResumeWord)+" ", 0)
		}
		return pill{s: p, w: ansi.StringWidth(lead+wallResumeWord) + 2, pad: 1, act: wallActResume, away: true}
	}
	if v.away > 0 && v.team != "" {
		awayAt = len(pills)
		pills = append(pills, awayPill(true))
	}
	open := strconv.Itoa(len(v.tiles)) + " open"
	pills = append(pills, pill{s: pal.dim(open), w: len(open)})
	gapBefore := func(i int) int {
		if i == 0 {
			return 0
		}
		return pillGap - pills[i-1].pad - pills[i].pad
	}
	measure := func() int {
		edge := max(inset-pills[len(pills)-1].pad, 0)
		rw := edge
		for i, p := range pills {
			rw += gapBefore(i) + p.w
		}
		return rw
	}

	// The last pill's pad sits in the inset, so its words end where the grid
	// does.
	edge := max(inset-pills[len(pills)-1].pad, 0)
	rw := measure()
	lw := ansi.StringWidth(left)
	if lw+2+rw > width {
		// The subtitle goes before any count does.
		left = " " + pal.bold(pal.ink(mark+" Conversations"))
		lw = ansi.StringWidth(left)
	}
	if lw+2+rw > width && awayAt >= 0 {
		// Then the team's name in the button, which the title already said;
		// then the button, whose key still works.
		pills[awayAt] = awayPill(false)
		if rw = measure(); lw+2+rw > width {
			pills = append(pills[:awayAt], pills[awayAt+1:]...)
			rw = measure()
		}
	}
	if lw+2+rw > width {
		return ansi.Truncate(left, width, ""), nil
	}
	var hits []wallHit
	var b strings.Builder
	b.WriteString(left)
	b.WriteString(strings.Repeat(" ", width-lw-rw))
	x := width - rw
	for i, p := range pills {
		gap := gapBefore(i)
		b.WriteString(strings.Repeat(" ", gap))
		x += gap
		if p.pad > 0 {
			hits = append(hits, wallHit{x0: x, y0: y, x1: x + p.w, y1: y + 1, kind: wallHitAction, arg: int(p.act)})
		}
		b.WriteString(p.s)
		x += p.w
	}
	b.WriteString(strings.Repeat(" ", edge))
	return b.String(), hits
}

// wallOpenHereWord is what the wall is, in the title's words: the
// conversations open in this window, whichever team narrows them.
const wallOpenHereWord = "open in this window"

// wallResumeWord is the title's word button that resumes the shown team's
// members this window does not have open.
const wallResumeWord = "Open them"

// wallRule closes the head: dim, or, while a team is shown, in that team's
// colour, so the whole frame says which set it is showing.
func wallRule(pal palette, v wallView, width int) string {
	if i := v.teamRow(v.team); i >= 0 {
		if ink := pal.teamInk(v.teams[i].hue); ink != nil {
			return ink(wallRuleLine(pal, width))
		}
	}
	return pal.dim(wallRuleLine(pal, width))
}

// wallFootRule opens the foot, the head's rule mirrored. It is always dim:
// the head already says which team is shown, and saying it twice would make
// the team's colour a frame rather than a mark.
func wallFootRule(pal palette, width int) string {
	return pal.dim(wallRuleLine(pal, width))
}

func wallRuleLine(pal palette, width int) string {
	if pal.ascii {
		return strings.Repeat("-", width)
	}
	return strings.Repeat("─", width)
}

// wallMinimap is one cell per tile in strip order, coloured by what the tile
// is doing, with the tiles on screen drawn as the brighter cell. Rows of the
// grid (per tiles each) are parted by a space when there is room for it. It
// also says the column each tile's cell landed on, -1 for one not drawn, so a
// press on a cell can go to its tile.
func wallMinimap(pal palette, g wallGlyphs, v wallView, per, first, last, room int) (string, []int) {
	n := len(v.tiles)
	if room <= 0 || n == 0 {
		return "", nil
	}
	cells := make([]string, n)
	for i, t := range v.tiles {
		cell := g.cell
		shown := i >= first && i < last
		if shown {
			cell = g.seenCell
		}
		switch {
		case t.signal == tabNeedsPerson:
			cell = pal.warn(cell)
		case t.signal == tabWorking && t.live:
			cell = pal.live(cell)
		case shown:
			cell = pal.ink(cell)
		default:
			cell = pal.dim(cell)
		}
		cells[i] = cell
	}
	at := make([]int, n)
	for i := range at {
		at[i] = -1
	}
	var b strings.Builder
	w := 0
	grouped := per > 0 && n+(n-1)/per <= room
	for i, cell := range cells {
		if grouped && i > 0 && i%per == 0 {
			b.WriteString(" ")
			w++
		}
		if w+1 > room {
			break
		}
		b.WriteString(cell)
		at[i] = w
		w++
	}
	return b.String(), at
}

// wallSpread puts left at the start of a width-cell row and right at its end,
// dropping right when the two would touch.
func wallSpread(left, right string, width int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if lw+1+rw > width {
		return ansi.Truncate(left, width, "")
	}
	return left + strings.Repeat(" ", width-lw-rw) + right
}

// wallFit pads or cuts s to exactly w cells.
func wallFit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		s = ansi.Truncate(s, w, "")
		sw = ansi.StringWidth(s)
	}
	return s + strings.Repeat(" ", w-sw)
}

// wallTileLook is one tile's place on the ladder: which box, what ink the
// border takes, and what ground the whole tile sits on. (The field is border
// and not edge: edge is the live-state seam's word, livestate.go.)
type wallTileLook struct {
	box     wallBox
	border  func(string) string
	ground  func(string) string
	hovered bool
	// boxOn says the selection box is drawn on the top border, which is the
	// selection mode; rowOn says the bottom border is the action row.
	boxOn, rowOn bool
}

// wallLookFor is the ladder read for one tile. Each rung claims one thing, and
// the claims are decided in one expression each so no rung can overwrite
// another's by the order of a few assignments.
func wallLookFor(pal palette, v wallView, t wallTile, i int, focused bool) wallTileLook {
	hovered := v.hover.onTile(i)
	box := wallBoxLight
	switch {
	case focused && pal.ascii:
		box = wallBoxHeavyASCII
	case focused:
		box = wallBoxHeavy
	case pal.ascii:
		box = wallBoxLightASCII
	}
	return wallTileLook{
		box:     box,
		border:  wallBorderInk(pal, t, focused, hovered),
		ground:  wallGroundFor(pal, t, hovered),
		hovered: hovered,
		boxOn:   wallMarked(v) > 0,
		rowOn:   hovered || focused,
	}
}

// wallBorderInk is the border's colour: amber for a tile waiting on a person
// (the only amber on the wall), ink for the focus, muted under the pointer,
// and dim at rest. Focus is said twice, by weight and by ink, so it survives a
// palette with no colour.
func wallBorderInk(pal palette, t wallTile, focused, hovered bool) func(string) string {
	switch {
	case t.signal == tabNeedsPerson:
		return pal.warn
	case focused:
		return pal.ink
	case hovered:
		return pal.muted
	}
	return pal.dim
}

// wallGroundFor is what the whole tile sits on: the selected ground, a step
// above hover, for a picked tile; the cursor ground under the pointer; none
// at rest.
func wallGroundFor(pal palette, t wallTile, hovered bool) func(string) string {
	switch {
	case t.marked:
		return func(s string) string { return pal.selected(s, 0) }
	case hovered:
		return func(s string) string { return pal.cursor(s, 0) }
	}
	return func(s string) string { return s }
}

// wallTileRoom is how a tile h rows high spends its inside: the padding above
// and below, whether the state line and its blank are drawn, and the rows left
// for the body. The painter and the pointer both read it, so a target in the
// body is where the body drew it.
func wallTileRoom(h int) (padY int, meta bool, body int) {
	inner := h - 2
	padY = wallPadY
	if inner < 2*wallPadY+wallMetaRows+1 {
		padY = 0
	}
	body = inner - 2*padY
	if body >= wallMetaRows+1 {
		return padY, true, body - wallMetaRows
	}
	return padY, false, max(body, 0)
}

// wallAnswerButton is the button a tile waiting on a person ends in. Pressing
// it is the tile's open, the one door the prototype answers a question
// through, so it carries that key.
func wallAnswerButton(ascii bool) wallButton {
	return wallButton{act: wallActOpen, label: "Answer", key: wallKeysFor(ascii).enter}
}

// wallAnswerAt is where tile t's Answer button sits, in cells from the tile's
// top-left corner, and whether it is drawn at all. It is on the body's last
// row, and its label starts on the text's column: its ground's first cell is
// taken from the padding, as a button's ground is outside its word.
func wallAnswerAt(ascii bool, t wallTile, w, h int) (dx, dy, bw int, ok bool) {
	if t.signal != tabNeedsPerson {
		return 0, 0, 0, false
	}
	padY, _, body := wallTileRoom(h)
	bw = wallButtonW(wallAnswerButton(ascii))
	if body < 1 || bw-1 > wallInnerW(w) {
		return 0, 0, 0, false
	}
	return wallPadX, h - 2 - padY, bw, true
}

// wallRowHot reports whether the pointer is on tile i's target of kind on
// frame row y. A waiting tile's Answer and its row's Answer answer to one
// ref, as a picked tile's ☐ and its row's Select do, and the pointer's row
// says which of the two to light; without it both are lit, which at least
// says they are one door.
func wallRowHot(v wallView, kind wallHitKind, i, y int) bool {
	if v.hover.kind != kind || v.hover.arg != i {
		return false
	}
	return !v.pointerOn || v.pointerY == y
}

// wallPaintTile is one tile, exactly h rows of exactly w cells, its top-left
// corner on frame row y0:
//
//	╭─ ●● the tree walk ───────────────────────────╮
//	│                                              │
//	│  ⠸ running bash · 2m                         │
//	│                                              │
//	│  the body, oldest faded, newest in ink       │
//	│                                              │
//	╰─ ▁▂▃▅▇▅▃ ────────────────────────────────────╯
//
// and under the pointer or the focus its bottom border is the action row:
//
//	╰─ Open ↵ ── Select ␣ ── Teams m ── Close x ──╯
func wallPaintTile(pal palette, g wallGlyphs, v wallView, t wallTile, i int, focused bool, w, h, y0 int) []string {
	look := wallLookFor(pal, v, t, i, focused)
	box, border, ground := look.box, look.border, look.ground
	out := make([]string, 0, h)
	out = append(out, wallTopBorder(pal, v, t, i, look, w, y0))
	padY, meta, room := wallTileRoom(h)
	innerW := wallInnerW(w)
	pad := strings.Repeat(" ", wallPadX)
	row := func(s string) string {
		return ground(border(box.v) + pad + wallFit(s, innerW) + pad + border(box.v))
	}
	blank := row("")
	for y := 0; y < padY; y++ {
		out = append(out, blank)
	}
	if meta {
		out = append(out, row(wallMetaLine(pal, g, v, t, innerW)), blank)
	}
	body := wallTileBody(pal, g, v, t, innerW, room)
	for _, s := range body {
		out = append(out, row(s))
	}
	if dx, dy, _, ok := wallAnswerAt(pal.ascii, t, w, h); ok && dy < len(out) {
		// The button is laid over its row with the tile's ground around it and
		// its own ground under it, as a control in a tile is.
		btn := wallButtonPaint(pal, wallAnswerButton(pal.ascii), wallRowHot(v, wallHitOpen, i, y0+dy))
		bw := wallButtonW(wallAnswerButton(pal.ascii))
		lead := border(box.v) + strings.Repeat(" ", dx-1)
		tail := strings.Repeat(" ", max(w-dx-bw-1, 0)) + border(box.v)
		out[dy] = ground(lead) + btn + ground(tail)
	}
	for y := 0; y < padY; y++ {
		out = append(out, blank)
	}
	if look.rowOn {
		if acts := wallTileActs(pal.ascii, t, w); len(acts) > 0 {
			return append(out, wallActRow(pal, v, i, look, acts, w, y0+h-1))
		}
	}
	out = append(out, ground(wallBottomBorder(pal, t, box, border, w)))
	return out
}

// wallMetaLine is the tile's state in one dim line that never wraps: what a
// working agent is doing and for how long, a question waiting, or when the
// conversation last moved. A conversation this window has not seen move says
// nothing rather than guess.
func wallMetaLine(pal palette, g wallGlyphs, v wallView, t wallTile, w int) string {
	since := ""
	if !t.moved.IsZero() {
		since = wallAge(v.now.Sub(t.moved))
	}
	sep := " " + g.sep + " "
	var s string
	switch {
	case t.signal == tabNeedsPerson:
		s = pal.warn(tokens.GlyphNeedsHuman + " waiting on you")
		if since != "" {
			s += pal.dim(sep + since)
		}
	case t.signal == tabWorking && !t.live:
		if t.age != "" {
			s = pal.dim("seen " + t.age + " ago")
		}
	case t.signal == tabWorking:
		spin := tokens.Spinner(v.spin)
		switch {
		case pal.ascii:
			spin = glyphRunASCII
		case v.reduced:
			spin = tokens.GlyphWorking
		}
		doing := t.doing
		if doing == "" {
			doing = "working"
		}
		s = pal.live(spin + " " + doing)
		if since != "" {
			s += pal.dim(sep + since)
		}
	case !t.live && t.age != "":
		s = pal.dim("seen " + t.age + " ago")
	case since != "":
		s = pal.dim("updated " + since + " ago")
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, g.more)
	}
	return s
}

// wallSelW is the cells the selection box takes at a tile's top left in the
// selection mode: ` ☐ ` unmarked, ` ☑ ` marked.
const wallSelW = 3

// wallSelFits reports whether a tile w wide carries the selection box.
func wallSelFits(w int) bool { return w >= 20 }

// wallTitleGap is the least run of border between a title and the corner, so
// a long title reads as cut and never as touching it.
const wallTitleGap = 2

// wallTopBorder is the team's dots and the title, and in the selection mode
// the box before them:
//
//	╭─ ●● the tree walk ───────────────────────────────╮
//	╭─ ☐ ●● the tree walk ─────────────────────────────╮
//
// The title is the brightest thing on the tile and has the whole border to
// itself, less two rule cells before the corner. The box is drawn on every
// tile or on none: it comes with the mode, never with the pointer, so a
// hover changes nothing on this row but the box's own ground.
func wallTopBorder(pal palette, v wallView, t wallTile, i int, look wallTileLook, w, y0 int) string {
	box, border, ground := look.box, look.border, look.ground
	if w < 5 {
		return ground(wallFit(border(box.tl+strings.Repeat(box.h, max(w-2, 0))+box.tr), w))
	}
	var parts []wallPart
	add := func(s string) { parts = append(parts, wallPart{s: s}) }

	add(border(box.tl + box.h))
	used := 2
	if look.boxOn && wallSelFits(w) {
		sel := wallSelGlyph(pal.ascii, t.marked)
		word := " " + sel + " "
		switch {
		case wallRowHot(v, wallHitSelect, i, y0):
			parts = append(parts, wallPart{s: pal.ink(word), hot: true})
		case t.marked:
			add(" " + pal.accent(sel) + " ")
		default:
			add(pal.muted(word))
		}
		used += wallSelW
	} else {
		add(" ")
		used++
	}
	right := 1 + wallTitleGap // the least rule and the corner
	if dots, dw := wallTileDots(pal, v, t); dw > 0 && w-used-right-dw >= 8 {
		add(dots)
		used += dw
	}
	if t.manager {
		mark := teamManagerGlyph
		if pal.ascii {
			mark = teamManagerGlyphASCII
		}
		// `◆ Manager · <title>`, as the strip names the same conversation, and
		// the mark alone where the word would cost the title.
		switch word := teamManagerWord + " · "; {
		case w-used-right-2-len(word) >= 12:
			add(pal.accent(mark) + " " + pal.ink(teamManagerWord) + pal.dim(" · "))
			used += ansi.StringWidth(mark) + 1 + len(word)
		case w-used-right-2 >= 8:
			add(pal.accent(mark) + " ")
			used += ansi.StringWidth(mark) + 1
		}
	}
	nameRoom := w - used - right - 1
	name := t.name
	if nameRoom < 1 {
		name = ""
	} else if ansi.StringWidth(name) > nameRoom {
		name = ansi.Truncate(name, nameRoom, wallGlyphsFor(pal.ascii).more)
	}
	if name != "" {
		add(pal.bold(pal.ink(name)) + " ")
		used += ansi.StringWidth(name) + 1
	}
	add(border(strings.Repeat(box.h, max(w-used-1, 0)) + box.tr))
	return wallFit(wallCompose(pal, parts, ground), w)
}

// wallSelGlyph is the selection box, open or filled.
func wallSelGlyph(ascii, marked bool) string {
	switch {
	case ascii && marked:
		return "@"
	case ascii:
		return "o"
	case marked:
		return "☑"
	}
	return "☐"
}

// wallBottomBorder carries the sparkline of a live tile that has moved.
func wallBottomBorder(pal palette, t wallTile, box wallBox, border func(string) string, w int) string {
	if w < 5 {
		return wallFit(border(box.bl+strings.Repeat(box.h, max(w-2, 0))+box.br), w)
	}
	s := border(box.bl + box.h)
	used := 2
	cells := wallSparkCells
	if pal.ascii {
		cells = wallSparkCellsASCII
	}
	moved := false
	if t.live {
		for _, x := range t.spark {
			if x > 0 {
				moved = true
				break
			}
		}
	}
	if moved {
		room := w - used - 4 // space, the spark, space, one rule and the corner
		spark := t.spark
		if len(spark) > room {
			spark = spark[len(spark)-max(room, 0):]
		}
		if len(spark) > 0 {
			var b strings.Builder
			for _, x := range spark {
				if x > 7 {
					x = 7
				}
				b.WriteString(cells[x])
			}
			s += " " + pal.muted(b.String()) + " "
			used += 2 + len(spark)
		}
	}
	fill := max(w-used-1, 0)
	s += border(strings.Repeat(box.h, fill)) + border(box.br)
	return wallFit(s, w)
}

// wallTileBody is the tail fit to the tile, h rows of w cells.
//
// THE BODY HANGS FROM THE TOP. Under the state line and its one blank row the
// body starts at once, in every tile, so the eye finds the same rhythm down
// the whole grid; a short tail leaves its room empty at the bottom, and a long
// one shows its newest rows. Blank rows at either end of what is shown are
// dropped, so a turn break never doubles the blank under the state line.
//
// A tile waiting on a person keeps its last rows for the ask, pinned to the
// bottom where a dialog keeps its buttons: a blank row, the question in ink, a
// blank row, and the row the Answer button is laid on (wallPaintTile).
// A working tile's last row ends in the cursor.
func wallTileBody(pal palette, g wallGlyphs, v wallView, t wallTile, w, h int) []string {
	body := make([]string, h)
	if w <= 0 || h <= 0 {
		return body
	}
	var ask []string
	if t.signal == tabNeedsPerson {
		ask = wallAskRows(pal, g, t.question, w, h)
	}

	lift := !v.reduced && t.fresh > 0 && !t.freshAt.IsZero() && v.now.Sub(t.freshAt) < wallFreshSettle
	freshFrom := len(t.lines) - t.fresh
	var tail []string
	lines := t.lines
	if t.rows != nil {
		// The chat's own rows, drawn once per reading (wallmini.go).
		tail = append(tail, t.rows...)
		lines = nil
	}
	for i, ln := range lines {
		text := ln.text
		if ln.kind == wallTool {
			text = strings.TrimPrefix(strings.TrimPrefix(text, "▸"), ">")
			text = g.tool + " " + strings.TrimLeft(text, " ")
		}
		paint := wallLineInk(pal, ln.kind)
		if lift && i >= freshFrom {
			paint = pal.ink
		}
		for _, r := range wallWrap(text, w) {
			tail = append(tail, paint(r))
		}
	}
	tail = wallTrimBlank(tail)
	if room := h - len(ask); len(tail) > room {
		tail = wallTrimBlank(tail[len(tail)-room:])
	}
	// The chat's own rows are already faded by age (wallmini.go) and say
	// nothing more; the cursor is for the flattened lines alone.
	if t.rows == nil && t.signal == tabWorking && t.live && len(tail) > 0 && len(ask) == 0 {
		lastRow := tail[len(tail)-1]
		if ansi.StringWidth(lastRow) >= w {
			lastRow = ansi.Truncate(lastRow, w-1, "")
		}
		tail[len(tail)-1] = lastRow + pal.ink(g.cursor)
	}
	copy(body, tail)
	copy(body[h-len(ask):], ask)
	return body
}

// wallAskRows is the foot of a tile waiting on a person, at most h rows: a
// blank row parting it from the tail, the question in ink, a blank row, and a
// blank last row the Answer button is laid on. Short of room the blanks go
// first, then the question's last rows, the cut one ending in an ellipsis;
// the button's row is the last thing to go.
func wallAskRows(pal palette, g wallGlyphs, question string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	var q []string
	if question != "" {
		q = wallWrap(question, w)
	}
	lead, mid := len(q) > 0, len(q) > 0
	for 1+len(q)+b2i(lead)+b2i(mid) > h {
		switch {
		case lead:
			lead = false
		case mid:
			mid = false
		default:
			q = q[:len(q)-1]
			if len(q) > 0 {
				last := q[len(q)-1]
				q[len(q)-1] = ansi.Truncate(last, max(w-1, 0), "") + g.more
			}
		}
	}
	var rows []string
	if lead {
		rows = append(rows, "")
	}
	for _, r := range q {
		rows = append(rows, pal.ink(r))
	}
	if mid {
		rows = append(rows, "")
	}
	return append(rows, "")
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// wallTrimBlank drops blank rows from both ends of rows.
func wallTrimBlank(rows []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(rows) > 0 && blank(rows[0]) {
		rows = rows[1:]
	}
	for len(rows) > 0 && blank(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// wallLineInk is the hue one kind of line is drawn in.
func wallLineInk(pal palette, k wallLineKind) func(string) string {
	switch k {
	case wallTool, wallUser:
		return pal.dim
	case wallNote:
		return func(s string) string { return pal.italic(pal.dim(s)) }
	}
	return pal.muted
}

// wallWrap cleans one logical line and wraps it to w cells. A tail line is
// plain text by contract; anything that looks like an escape is dropped so a
// transcript cannot paint over the tile.
func wallWrap(s string, w int) []string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r", "")
	var out []string
	for _, r := range strings.Split(ansi.Wrap(s, w, ""), "\n") {
		out = append(out, ansi.Truncate(strings.TrimRight(r, " "), w, ""))
	}
	return out
}
