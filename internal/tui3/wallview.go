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
// is and what the conversations on it are doing; under it the Spaces control
// says which set is shown; a rule closes the head. Then the grid, and last the
// toolbar (wallbar.go). Two cards can float over the grid: the selection tray
// while conversations are picked, and the new-space card while one is named.
//
// The words on screen are the person's words: a Conversation is a tile, a
// Space is a named set of them. "wall" and "tab" are this code's names and are
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
//	           and the tile's own controls drawn
//	focus      the heavy border; weight, never colour
//	selected   the selected ground, a step above hover, and a ☑ at top left
//	needs you  the amber border, and the only amber on the wall
//
// Controls a hover reveals are drawn into cells the border kept for them all
// along, so nothing under the pointer ever moves.

// wallChromeRows is what the frame spends outside the grid: the title bar, the
// Spaces row, the rule, a blank row over the grid, and the toolbar.
const wallChromeRows = 5

// wallGridTop is the first row a tile is drawn on.
const wallGridTop = 4

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

// wallMadeFor is how long the chip row says a space was just made.
const wallMadeFor = 2 * time.Second

// wallEmptyWord is the whisper an empty wall draws beside its way back.
const wallEmptyWord = "No open conversations"

// wallBox is one border's glyphs.
type wallBox struct{ tl, tr, bl, br, h, v string }

var (
	wallBoxHeavy = wallBox{"┏", "┓", "┗", "┛", "━", "┃"}
	wallBoxLight = wallBox{"╭", "╮", "╰", "╯", "─", "│"}
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
	return wallGlyphs{sep: "·", tool: "▸", cursor: "▌", marked: "☑", cell: "▪", seenCell: "▣", gt: "›", more: "…"}
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
	if len(v.tiles) == 0 {
		y := (height - 1) / 2
		row, hits := wallEmptyRow(pal, v, width, y)
		rows[y] = row
		return rows, hits
	}

	n := len(v.tiles)
	focus := min(max(v.focus, 0), n-1)
	c, tileW, tileH := wallGrid(n, width, height, v.cols)
	scroll := wallScrollFor(focus, v.scroll, n, width, height, v.cols)
	vis := wallVisibleRows(height, tileH)
	first, last := scroll*c, min((scroll+vis)*c, n)

	var hits []wallHit
	row, h := wallTitleRow(pal, g, v, width, 0)
	rows[0] = row
	hits = append(hits, h...)
	if height > 1 {
		row, h := wallSpacesRow(pal, g, v, width, height, c, first, last, 1)
		rows[1] = row
		hits = append(hits, h...)
	}
	if height > 2 {
		rows[2] = wallRule(pal, v, width)
	}

	if tileW >= 6 && tileH >= 3 {
		margin := strings.Repeat(" ", wallMargin)
		gap := strings.Repeat(" ", wallGutter)
		for r := 0; r < vis; r++ {
			y0 := wallGridTop + r*(tileH+wallRowGap)
			var line []string
			for col := 0; col < c; col++ {
				i := (scroll+r)*c + col
				if i >= n {
					break
				}
				x0 := wallMargin + col*(tileW+wallGutter)
				hits = append(hits, wallTileHits(pal, v, v.tiles[i], i, i == focus, x0, y0, tileW, tileH)...)
				tile := wallPaintTile(pal, g, v, v.tiles[i], i, i == focus, tileW, tileH)
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
				if y0+y < height-1 {
					rows[y0+y] = s
				}
			}
		}
	}
	if height > 2 {
		row, h := wallBar(pal, v, width, height-1)
		rows[height-1] = row
		hits = append(hits, h...)
	}

	// The cards float over the grid; what they cover stops answering the
	// pointer, so a press lands on the card and never on the tile under it.
	// A popover floats over everything, the tray included.
	switch {
	case v.naming:
		hits = wallOverlay(rows, hits, wallNameCard(pal, g, v, width, height), width)
	case wallMarked(v) > 0:
		hits = wallOverlay(rows, hits, wallTray(pal, g, v, width, height), width)
	}
	if v.pop.kind != wallPopNone && !v.naming {
		hits = wallOverlay(rows, hits, wallPopCard(pal, g, v, width, height), width)
	}

	for i, s := range rows {
		if ansi.StringWidth(s) > width {
			rows[i] = ansi.Truncate(s, width, "")
		}
	}
	return rows, hits
}

// wallTitleRow is the title bar: what this is on the left, and on the right
// what the conversations on it are doing, as quiet counts, each said only when
// it is not zero. The count waiting on a person is a button: it goes to the
// next one, as ? does.
//
//	▦ Conversations · every open conversation, live      ⠿ 3 running  ? 1 needs you  4 open
func wallTitleRow(pal palette, g wallGlyphs, v wallView, width, y int) (string, []wallHit) {
	mark := "▦"
	run := "⠿"
	if pal.ascii {
		mark, run = "#", "*"
	}
	sub := "every open conversation, live"
	if v.space != "" {
		sub = "the conversations in " + v.space
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
	type pill struct {
		s    string
		w    int
		next bool
	}
	var pills []pill
	if working > 0 {
		word := run + " " + strconv.Itoa(working) + " running"
		pills = append(pills, pill{s: pal.live(word), w: ansi.StringWidth(word)})
	}
	if needs > 0 {
		word := tokens.GlyphNeedsHuman + " " + strconv.Itoa(needs) + " " + tabNeedsPersonWord
		p := pal.warn(word)
		if v.hover.kind == wallHitAction && v.hover.arg == int(wallActNext) {
			p = pal.cursor(" "+p+" ", 0)
		} else {
			p = " " + p + " "
		}
		pills = append(pills, pill{s: p, w: ansi.StringWidth(word) + 2, next: true})
	}
	open := strconv.Itoa(len(v.tiles)) + " open"
	pills = append(pills, pill{s: pal.dim(open), w: len(open)})

	rw := 1 // the cell kept at the right edge
	for i, p := range pills {
		if i > 0 {
			rw += 2
		}
		rw += p.w
	}
	lw := ansi.StringWidth(left)
	if lw+2+rw > width {
		// The subtitle goes before any count does.
		left = " " + pal.bold(pal.ink(mark+" Conversations"))
		lw = ansi.StringWidth(left)
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
		if i > 0 {
			b.WriteString("  ")
			x += 2
		}
		if p.next {
			hits = append(hits, wallHit{x0: x, y0: y, x1: x + p.w, y1: y + 1, kind: wallHitAction, arg: int(wallActNext)})
		}
		b.WriteString(p.s)
		x += p.w
	}
	b.WriteString(" ")
	return b.String(), hits
}

// wallRule closes the head: dim, or, while a space is shown, in that space's
// colour, so the whole frame says which set it is showing.
func wallRule(pal palette, v wallView, width int) string {
	rule := "─"
	if pal.ascii {
		rule = "-"
	}
	line := strings.Repeat(rule, width)
	for i, name := range v.spaces {
		if name == v.space && v.space != "" && i < len(v.hues) {
			if ink := pal.spaceInk(v.hues[i]); ink != nil {
				return ink(line)
			}
		}
	}
	return pal.dim(line)
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
// border takes, and what ground the whole tile sits on.
type wallTileLook struct {
	box     wallBox
	edge    func(string) string
	ground  func(string) string
	hovered bool
	// boxOn says the selection box is drawn, and ctlOn the top-right controls.
	boxOn, ctlOn bool
}

func wallLookFor(pal palette, v wallView, t wallTile, i int, focused bool) wallTileLook {
	look := wallTileLook{box: wallBoxLight, edge: pal.dim, ground: func(s string) string { return s }}
	switch {
	case focused && pal.ascii:
		look.box = wallBoxHeavyASCII
	case focused:
		look.box = wallBoxHeavy
	case pal.ascii:
		look.box = wallBoxLightASCII
	}
	look.hovered = v.hover.onTile(i)
	switch {
	case t.signal == tabNeedsPerson:
		look.edge = pal.warn
	case focused:
		look.edge = pal.ink
	case look.hovered:
		look.edge = pal.muted
	}
	switch {
	case t.marked:
		look.ground = func(s string) string { return pal.selected(s, 0) }
	case look.hovered:
		look.ground = func(s string) string { return pal.cursor(s, 0) }
	}
	look.boxOn = look.hovered || t.marked || wallMarked(v) > 0
	look.ctlOn = look.hovered || focused
	return look
}

// wallPaintTile is one tile, exactly h rows of exactly w cells:
//
//	╭─ ☐ the tree walk ─────────────── open ↗  × ─╮
//	│                                              │
//	│  ⠸ running bash · 2m                         │
//	│                                              │
//	│  the body, oldest faded, newest in ink       │
//	│                                              │
//	╰─ ▁▂▃▅▇▅▃ ────────────────────────────────────╯
func wallPaintTile(pal palette, g wallGlyphs, v wallView, t wallTile, i int, focused bool, w, h int) []string {
	look := wallLookFor(pal, v, t, i, focused)
	box, edge, ground := look.box, look.edge, look.ground
	out := make([]string, 0, h)
	out = append(out, wallTopBorder(pal, v, t, i, look, w))
	inner := h - 2
	padY := wallPadY
	if inner < 2*wallPadY+wallMetaRows+1 {
		padY = 0
	}
	innerW := wallInnerW(w)
	pad := strings.Repeat(" ", wallPadX)
	row := func(s string) string {
		return ground(edge(box.v) + pad + wallFit(s, innerW) + pad + edge(box.v))
	}
	blank := row("")
	for y := 0; y < padY; y++ {
		out = append(out, blank)
	}
	room := inner - 2*padY
	if room >= wallMetaRows+1 {
		out = append(out, row(wallMetaLine(pal, g, v, t, innerW)), blank)
		room -= wallMetaRows
	}
	for _, body := range wallTileBody(pal, g, v, t, innerW, room) {
		out = append(out, row(body))
	}
	for y := 0; y < padY; y++ {
		out = append(out, blank)
	}
	out = append(out, ground(wallBottomBorder(pal, t, box, edge, w)))
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

// wallSelW is the cells the selection box takes at a tile's top left, drawn
// or kept: ` ☐ ` unmarked, ` ☑ ` marked.
const wallSelW = 3

// wallSelFits reports whether a tile w wide carries the selection box.
func wallSelFits(w int) bool { return w >= 20 }

// wallTopBorder is the selection box and the title on the left, and the
// tile's own controls on the right:
//
//	╭─ ☐ the tree walk ────────────────────────── open ↗  × ─╮
//
// The title is the brightest thing on the tile. Every piece a hover reveals
// has its cells whether or not it is drawn.
func wallTopBorder(pal palette, v wallView, t wallTile, i int, look wallTileLook, w int) string {
	box, edge, ground := look.box, look.edge, look.ground
	if w < 5 {
		return ground(wallFit(edge(box.tl+strings.Repeat(box.h, max(w-2, 0))+box.tr), w))
	}
	ctls, ctlW := wallTileCtls(pal.ascii, w)
	selW := 0
	if wallSelFits(w) {
		selW = wallSelW
	}

	// The parts, left to right. A part marked hot is a control under the
	// pointer, which wears its own ground over the tile's.
	type part struct {
		s   string
		hot bool
	}
	var parts []part
	add := func(s string) { parts = append(parts, part{s: s}) }

	add(edge(box.tl + box.h))
	used := 2
	if selW > 0 {
		switch {
		case look.boxOn:
			sel := wallSelGlyph(pal.ascii, t.marked)
			word := " " + sel + " "
			switch {
			case v.hover.kind == wallHitSelect && v.hover.arg == i:
				parts = append(parts, part{s: pal.ink(word), hot: true})
			case t.marked:
				add(" " + pal.accent(sel) + " ")
			default:
				add(pal.muted(word))
			}
		default:
			add(edge(box.h+box.h) + " ")
		}
		used += selW
	} else {
		add(" ")
		used++
	}
	if dots, dw := wallTileDots(pal, v, t); dw > 0 && w-used-ctlW-2-dw >= 8 {
		add(dots)
		used += dw
	}
	right := ctlW + 2 // the controls, one rule and the corner
	nameRoom := w - used - right - 2
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
	add(edge(strings.Repeat(box.h, max(w-used-right, 0))))
	if ctlW > 0 {
		if look.ctlOn {
			for _, c := range ctls {
				if v.hover.kind == c.kind && v.hover.arg == i {
					parts = append(parts, part{s: pal.ink(c.word), hot: true})
				} else {
					add(pal.muted(c.word))
				}
			}
		} else {
			add(edge(strings.Repeat(box.h, ctlW)))
		}
	}
	add(edge(box.h + box.tr))

	var b strings.Builder
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(ground(run.String()))
			run.Reset()
		}
	}
	for _, p := range parts {
		if p.hot {
			flush()
			b.WriteString(pal.background(p.s, 0, pal.ramp.mark))
			continue
		}
		run.WriteString(p.s)
	}
	flush()
	return wallFit(b.String(), w)
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
func wallBottomBorder(pal palette, t wallTile, box wallBox, edge func(string) string, w int) string {
	if w < 5 {
		return wallFit(edge(box.bl+strings.Repeat(box.h, max(w-2, 0))+box.br), w)
	}
	s := edge(box.bl + box.h)
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
	s += edge(strings.Repeat(box.h, fill)) + edge(box.br)
	return wallFit(s, w)
}

// wallTileBody is the tail fit to the tile: every logical line wrapped to the
// inner width, and the last rows that fit. A tile waiting on a person ends with
// its question and the verbs that answer it. A working tile's last row ends in
// the cursor.
func wallTileBody(pal palette, g wallGlyphs, v wallView, t wallTile, w, h int) []string {
	body := make([]string, h)
	if w <= 0 || h <= 0 {
		return body
	}
	var ask []string
	if t.signal == tabNeedsPerson && t.question != "" {
		for _, r := range wallWrap(t.question, w) {
			ask = append(ask, pal.ink(r))
		}
		verbs := wallVerbs(pal, w)
		if verbs != "" && len(ask)+1 <= h-1 {
			ask = append(ask, verbs)
		}
		if len(ask) > h {
			ask = ask[len(ask)-h:]
		}
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
	room := h - len(ask)
	if len(tail) > room {
		tail = tail[len(tail)-room:]
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
	rows := append(tail, ask...)
	// The body sits at the bottom of the tile, like a terminal's tail.
	copy(body[h-len(rows):], rows)
	return body
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

// wallVerbs is the answer row of a tile waiting on a person, shortened to fit.
func wallVerbs(pal palette, w int) string {
	for _, words := range [][][2]string{
		// The prototype answers a question inside its conversation only, so the
		// tile offers the door and never a digit it would not act on.
		{{"enter", "open to answer"}},
		{{"enter", "answer"}},
	} {
		var parts []string
		plain := 0
		for _, kv := range words {
			parts = append(parts, pal.ink(kv[0])+" "+pal.dim(kv[1]))
			plain += len(kv[0]) + 1 + len(kv[1])
		}
		plain += 3 * (len(words) - 1)
		if plain <= w {
			return strings.Join(parts, "   ")
		}
	}
	return ""
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
